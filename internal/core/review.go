package core

import (
	"context"
	"crypto/sha1" // #nosec G505 -- Git object identity, not a cryptographic trust decision.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// UnitChange compares exact parser ranges, with old source retained for removed units.
type UnitChange struct {
	Before               *Node            `json:"before,omitempty"`
	Status               string           `json:"status"`
	Cohort               string           `json:"cohort"`
	BeforeSource         string           `json:"before_source"`
	AfterSource          string           `json:"after_source"`
	RelationshipsAdded   []Edge           `json:"relationships_added"`
	RelationshipsRemoved []Edge           `json:"relationships_removed"`
	Related              []Recommendation `json:"related"`
	Node                 Node             `json:"node"`
}

// ChangeReview is a local base-to-working-tree comparison, including staged and untracked source.
type ChangeReview struct {
	Base           string       `json:"base"`
	Revision       string       `json:"revision"`
	Tour           Tour         `json:"tour"`
	Changes        []UnitChange `json:"changes"`
	Limitations    []string     `json:"limitations"`
	Files          int          `json:"files"`
	GeneratedFiles int          `json:"generated_files"`
}

type treeBlob struct{ path, id string }

var errHistoricalFileTooLarge = errors.New("historical file exceeds analysis limit")

// Review compares an explicit local commit (HEAD by default) with the indexed working tree.
// It never fetches, checks out, executes repository code, or modifies the Git index.
func (r *Repository) Review(ctx context.Context, base string) (ChangeReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	idx, graph := r.View()
	if err := validateSnapshot(idx); err != nil {
		return ChangeReview{}, err
	}
	if base == "" {
		base = "HEAD"
	}
	if len(base) > 256 || strings.ContainsAny(base, "\x00\r\n") {
		return ChangeReview{}, failure(Invalid, errors.New("invalid base revision"))
	}
	resolved, err := gitRead(ctx, idx.Root, nil, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return ChangeReview{}, failure(Invalid, errors.New("base must resolve to an available local commit"))
	}
	oid := strings.TrimSpace(string(resolved))
	if !objectID.MatchString(oid) {
		return ChangeReview{}, failure(Invalid, errors.New("invalid base object"))
	}
	before, err := baseSnapshot(ctx, idx, oid)
	if err != nil {
		return ChangeReview{}, err
	}
	review := compareSnapshots(before, idx, BuildGraph(before), graph)
	review.Base = oid
	size := 0
	for _, change := range review.Changes {
		size += len(change.BeforeSource) + len(change.AfterSource)
	}
	if size > 4*1024*1024 || len(review.Changes) > 2000 {
		return ChangeReview{}, failure(TooLarge, errors.New("change review exceeds 4 MiB or 2000 changes; choose a smaller root or closer base"))
	}
	review.Limitations = []string{"Comparison is against the indexed working-tree snapshot (refresh to include newly created files), including staged and untracked indexed source. Excluded files are omitted on both sides.", "Exact parser unit identity and source are compared; renamed symbols are shown as removed/added, and semantic intent is not inferred.", "Changes outside structural units are shown as file-level changes. Generated files are collapsed only when a standard Go generated-code marker is present on both sides."}
	review.Tour.Limitations = append(review.Tour.Limitations, review.Limitations...)
	if err := validateSnapshot(idx); err != nil {
		return ChangeReview{}, err
	}
	return review, nil
}

func validateSnapshot(idx *Index) error {
	if err := idx.CheckPolicy(); err != nil {
		return err
	}
	for _, file := range idx.Files {
		source, _, err := idx.Read(file.Path)
		if err != nil {
			return failure(Stale, err)
		}
		if source != idx.sources[file.Path] {
			return failure(Stale, errors.New("source changed; refresh before comparing changes"))
		}
	}
	return nil
}

func baseSnapshot(ctx context.Context, idx *Index, oid string) (*Index, error) {
	data, err := gitRead(ctx, idx.Root, nil, "ls-tree", "-r", "-z", oid, "--", ".")
	if err != nil {
		return nil, err
	}
	base := &Index{Root: idx.Root, registry: idx.registry, Files: []FileEntry{}, sources: map[string]string{}, policy: idx.policy, Revision: oid}
	module := ""
	base.modulePath = &module
	total, reads := 0, 0
	for _, record := range strings.Split(string(data), "\x00") {
		blob, ok := parseTreeBlob(record)
		if !ok || !permitted(idx.Root, blob.path) {
			continue
		}
		source, err := readBaseBlob(ctx, idx, blob, &reads)
		if errors.Is(err, errHistoricalFileTooLarge) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if blob.path == "go.mod" {
			module = moduleDeclaration(source)
			continue
		}
		if vaultEncrypted(blob.path, source) {
			continue
		}
		entry, ok := idx.registry.Parse(blob.path, []byte(source))
		if !ok {
			continue
		}
		total += len(source)
		if total > maxRepositoryBytes || len(base.Files) >= maxRepositoryFiles {
			return nil, failure(TooLarge, errors.New("base exceeds analysis limits"))
		}
		base.sources[blob.path] = source
		base.Files = append(base.Files, entry)
	}
	return base, nil
}

func parseTreeBlob(record string) (treeBlob, bool) {
	meta, name, ok := strings.Cut(record, "\t")
	fields := strings.Fields(meta)
	if !ok || len(fields) != 3 || fields[1] != "blob" || !objectID.MatchString(fields[2]) || !fs.ValidPath(name) {
		return treeBlob{}, false
	}
	if fields[0] != "100644" && fields[0] != "100755" {
		return treeBlob{}, false
	}
	return treeBlob{path: name, id: fields[2]}, true
}

func readBaseBlob(ctx context.Context, idx *Index, blob treeBlob, reads *int) (string, error) {
	if current, ok := idx.sources[blob.path]; ok && gitBlobHash(current, len(blob.id)) == blob.id {
		return current, nil
	}
	*reads++
	if *reads > 128 {
		return "", failure(TooLarge, errors.New("base needs more than 128 source reads; choose a closer base or smaller root"))
	}
	size, err := gitRead(ctx, idx.Root, nil, "cat-file", "-s", blob.id)
	if err != nil {
		return "", err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(size)))
	if err != nil || count > maxFileBytes || count < 0 {
		return "", errHistoricalFileTooLarge
	}
	source, err := gitRead(ctx, idx.Root, nil, "cat-file", "blob", blob.id)
	if err != nil {
		return "", err
	}
	return string(source), nil
}

func gitBlobHash(source string, width int) string {
	body := []byte(fmt.Sprintf("blob %d\x00%s", len(source), source))
	if width == 64 {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	sum := sha1.Sum(body) // #nosec G401 -- compatibility with Git SHA-1 object IDs; not used for authentication.
	return hex.EncodeToString(sum[:])
}

func compareSnapshots(before, after *Index, oldGraph, newGraph *Graph) ChangeReview {
	review := ChangeReview{Revision: after.Revision, Changes: []UnitChange{}, Tour: Tour{ID: "changes", Title: "Change Tour", Revision: after.Revision, Stops: []TourStop{}, Limitations: []string{}}}
	paths := map[string]bool{}
	for name := range before.sources {
		paths[name] = true
	}
	for name := range after.sources {
		paths[name] = true
	}
	ordered := make([]string, 0, len(paths))
	for name := range paths {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		oldSource, oldOK := before.sources[name]
		newSource, newOK := after.sources[name]
		if oldOK == newOK && oldSource == newSource {
			continue
		}
		review.Files++
		if generatedGo(name, oldSource) && generatedGo(name, newSource) {
			review.GeneratedFiles++
			continue
		}
		review.Changes = append(review.Changes, changedFileUnits(before, after, oldGraph, newGraph, name)...)
	}
	sort.SliceStable(review.Changes, func(i, j int) bool {
		a, b := review.Changes[i], review.Changes[j]
		if a.Cohort != b.Cohort {
			return a.Cohort < b.Cohort
		}
		if a.Node.Path != b.Node.Path {
			return a.Node.Path < b.Node.Path
		}
		if a.Node.Start != b.Node.Start {
			return a.Node.Start < b.Node.Start
		}
		return a.Node.ID < b.Node.ID
	})
	for _, change := range review.Changes {
		if len(review.Tour.Stops) >= 32 {
			break
		}
		p := change.Node.Provenance
		p.Provider = "Local Git comparison"
		p.Commit = before.Revision
		p.Detail = change.Status + " source unit against local base"
		review.Tour.Stops = append(review.Tour.Stops, TourStop{Node: change.Node, Reason: change.Status + " · " + change.Cohort, Provenance: p})
	}
	if len(review.Changes) > 32 {
		review.Tour.Limitations = append(review.Tour.Limitations, "Tour limited to 32 stops; all detected changes remain in the changes list.")
	}
	return review
}

func generatedGo(path, source string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(line, "package ") {
			break
		}
		if strings.HasPrefix(line, "// Code generated ") && strings.HasSuffix(line, " DO NOT EDIT.") {
			return true
		}
	}
	return false
}

func fileNodes(g *Graph, path string) map[string]Node {
	nodes := map[string]Node{}
	for _, n := range g.Nodes {
		if n.Path == path && n.UnitID != "" {
			nodes[n.ID] = n
		}
	}
	return nodes
}
func nodeSource(idx *Index, n Node) string {
	lines := strings.Split(idx.sources[n.Path], "\n")
	if n.Start < 1 || n.End > len(lines) || n.Start > n.End {
		return ""
	}
	return strings.Join(lines[n.Start-1:n.End], "\n")
}
func changedFileUnits(before, after *Index, oldGraph, newGraph *Graph, path string) []UnitChange {
	oldNodes, newNodes := fileNodes(oldGraph, path), fileNodes(newGraph, path)
	ids := map[string]bool{}
	for id := range oldNodes {
		ids[id] = true
	}
	for id := range newNodes {
		ids[id] = true
	}
	out := []UnitChange{}
	for id := range ids {
		if change := compareUnit(before, after, oldGraph, newGraph, oldNodes, newNodes, id); change != nil {
			out = append(out, *change)
		}
	}

	// File-level evidence captures imports, globals, comments, and other text outside units.
	if outsideUnits(before, oldNodes, path) != outsideUnits(after, newNodes, path) {
		node, ok := newGraph.Node("file:" + path)
		if !ok {
			node, _ = oldGraph.Node("file:" + path)
		}
		out = append(out, UnitChange{Node: node, Status: "file context changed", Cohort: changeCohort(node), BeforeSource: before.sources[path], AfterSource: after.sources[path], RelationshipsAdded: []Edge{}, RelationshipsRemoved: []Edge{}, Related: []Recommendation{}})
	}
	return out
}
func compareUnit(before, after *Index, oldGraph, newGraph *Graph, oldNodes, newNodes map[string]Node, id string) *UnitChange {
	old, oldOK := oldNodes[id]
	current, currentOK := newNodes[id]
	if !currentOK {
		current = old
	}
	previous, next := nodeSource(before, old), nodeSource(after, current)
	if !oldOK {
		previous = ""
	}
	if !currentOK {
		next = ""
	}
	added, removed := relationshipDelta(oldGraph, newGraph, id)
	if oldOK && currentOK && previous == next && len(added) == 0 && len(removed) == 0 {
		return nil
	}
	change := UnitChange{Node: current, Status: "modified", Cohort: changeCohort(current), BeforeSource: previous, AfterSource: next, Related: []Recommendation{}, RelationshipsAdded: []Edge{}, RelationshipsRemoved: []Edge{}}
	if oldOK {
		copyNode := old
		change.Before = &copyNode
	}
	if !oldOK {
		change.Status = "added"
	}
	if !currentOK {
		change.Status = "removed"
	}
	change.RelationshipsAdded, change.RelationshipsRemoved = added, removed
	if currentOK {
		change.Related = newGraph.ReadNext(id)
	} else {
		change.Related = oldGraph.ReadNext(id)
	}
	return &change
}

func outsideUnits(idx *Index, nodes map[string]Node, path string) string {
	source, ok := idx.sources[path]
	if !ok {
		return ""
	}
	lines := strings.Split(source, "\n")
	covered := make([]bool, len(lines))
	for _, n := range nodes {
		for i := n.Start - 1; i < n.End && i < len(lines); i++ {
			if i >= 0 {
				covered[i] = true
			}
		}
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if !covered[i] {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func changeCohort(n Node) string {
	if strings.HasSuffix(n.Path, "_test.go") {
		return "3 · tests"
	}
	if n.Language == "document" {
		return "4 · documentation"
	}
	if n.Kind == "type" {
		return "1 · types and contracts"
	}
	return "2 · implementation and file context"
}
func relationshipDelta(before, after *Graph, id string) ([]Edge, []Edge) {
	old, newEdges := map[string]Edge{}, map[string]Edge{}
	for _, e := range before.Outgoing(id) {
		old[e.Kind+"\x00"+e.To] = e
	}
	for _, e := range after.Outgoing(id) {
		newEdges[e.Kind+"\x00"+e.To] = e
	}
	added, removed := []Edge{}, []Edge{}
	for key, e := range newEdges {
		if _, ok := old[key]; !ok {
			added = append(added, e)
		}
	}
	for key, e := range old {
		if _, ok := newEdges[key]; !ok {
			removed = append(removed, e)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Kind+added[i].To < added[j].Kind+added[j].To })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Kind+removed[i].To < removed[j].Kind+removed[j].To })
	return added, removed
}
