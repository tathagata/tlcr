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
	Hunks                []Hunk           `json:"hunks"`
	Node                 Node             `json:"node"`
	Added                int              `json:"added"`
	Removed              int              `json:"removed"`
	// WhitespaceOnly marks a unit whose two sides differ only in spacing.
	WhitespaceOnly bool `json:"whitespace_only"`
}

// setDiff attaches line hunks numbered from each side's position in its file.
func (c *UnitChange) setDiff() {
	beforeStart := 1
	if c.Before != nil {
		beforeStart = c.Before.Start
	}
	c.apply(diffSources(c.BeforeSource, c.AfterSource, beforeStart, c.Node.Start))
}

func (c *UnitChange) apply(d unitDiff) {
	c.Hunks, c.Added, c.Removed = d.hunks, d.added, d.removed
	c.WhitespaceOnly = whitespaceOnly(c.BeforeSource, c.AfterSource)
}

// Review sides that are not a commit. A colon cannot appear in a ref name, so
// these never collide with a revision.
const (
	SideWorktree = ":worktree"
	SideIndex    = ":index"
	SideEmpty    = ":empty"
)

// ChangeSelection names the two sides of a review. Each is a local commit-ish
// or one of the Side constants; Commit instead selects one commit against its
// first parent.
type ChangeSelection struct {
	Base   string `json:"base,omitempty"`
	Head   string `json:"head,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// ChangeReview is a local comparison of two snapshots: commits, the staged
// index, or the indexed working tree (which includes untracked source).
type ChangeReview struct {
	Base           string       `json:"base"`
	Head           string       `json:"head"`
	BaseLabel      string       `json:"base_label"`
	HeadLabel      string       `json:"head_label"`
	Revision       string       `json:"revision"`
	Tour           Tour         `json:"tour"`
	Changes        []UnitChange `json:"changes"`
	Limitations    []string     `json:"limitations"`
	Files          int          `json:"files"`
	GeneratedFiles int          `json:"generated_files"`
	Added          int          `json:"added"`
	Removed        int          `json:"removed"`
	// Live reports that the head is the indexed working tree, so every
	// surviving stop can be opened as current source.
	Live bool `json:"live"`
}

type treeBlob struct{ path, id string }

// reviewSide is a resolved side: id is an object ID for a commit and the
// Side constant otherwise.
type reviewSide struct{ kind, id, label string }

const sideCommit = "commit"

// Review compares an explicit local commit (HEAD by default) with the indexed working tree.
func (r *Repository) Review(ctx context.Context, base string) (ChangeReview, error) {
	return r.ReviewChange(ctx, ChangeSelection{Base: base})
}

// ReviewChange compares the selected sides. It never fetches, checks out,
// executes repository code, or modifies the Git index.
func (r *Repository) ReviewChange(ctx context.Context, selection ChangeSelection) (ChangeReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	idx, graph := r.View()
	base, head, err := resolveSelection(ctx, idx.Root, selection)
	if err != nil {
		return ChangeReview{}, err
	}
	live := head.kind == SideWorktree
	check := idx.CheckPolicy
	if live {
		check = func() error { return validateSnapshot(idx) }
	}
	if err := check(); err != nil {
		return ChangeReview{}, err
	}
	loader := &sideLoader{idx: idx, graph: graph, permitted: map[string]bool{}, hashes: map[string]string{}}
	if err := loader.list(ctx, base, head); err != nil {
		return ChangeReview{}, err
	}
	before, oldGraph, err := loader.snapshot(base)
	if err != nil {
		return ChangeReview{}, err
	}
	after, newGraph, err := loader.snapshot(head)
	if err != nil {
		return ChangeReview{}, err
	}
	review := compareSnapshots(before, after, oldGraph, newGraph)
	review.Base, review.BaseLabel, review.Head, review.HeadLabel = base.id, base.label, head.id, head.label
	review.Revision, review.Tour.Revision, review.Live = idx.Revision, idx.Revision, live
	size := 0
	for _, change := range review.Changes {
		size += len(change.BeforeSource) + len(change.AfterSource)
	}
	if size > 4*1024*1024 || len(review.Changes) > 2000 {
		return ChangeReview{}, failure(TooLarge, errors.New("change review exceeds 4 MiB or 2000 changes; choose a smaller root or closer base"))
	}
	review.Limitations = reviewLimitations(live)
	review.Tour.Limitations = append(review.Tour.Limitations, review.Limitations...)
	if err := check(); err != nil {
		return ChangeReview{}, err
	}
	return review, nil
}

func reviewLimitations(live bool) []string {
	scope := "Both sides are read from local Git objects. Files the current working tree excludes or ignores are omitted on both sides, and current source may differ from the reviewed head."
	if live {
		scope = "Comparison is against the indexed working-tree snapshot (refresh to include newly created files), including staged and untracked indexed source. Excluded files are omitted on both sides."
	}
	return []string{scope, "Exact parser unit identity and source are compared; renamed symbols are shown as removed/added, and semantic intent is not inferred.", "Changes outside structural units are shown as file-level changes. Generated files are collapsed only when a standard Go generated-code marker is present on both sides."}
}

func resolveSelection(ctx context.Context, root string, selection ChangeSelection) (reviewSide, reviewSide, error) {
	none := reviewSide{}
	if selection.Commit != "" {
		if selection.Base != "" || selection.Head != "" {
			return none, none, failure(Invalid, errors.New("a single commit cannot be combined with a base or head"))
		}
		return resolveCommitPair(ctx, root, selection.Commit)
	}
	base, err := resolveSide(ctx, root, "base", selection.Base, "HEAD")
	if err != nil {
		return none, none, err
	}
	head, err := resolveSide(ctx, root, "head", selection.Head, SideWorktree)
	if err != nil {
		return none, none, err
	}
	if base.kind == SideWorktree || head.kind == SideEmpty || base.id == head.id {
		return none, none, failure(Invalid, errors.New("base and head must differ, and the working tree can only be the head"))
	}
	return base, head, nil
}

// resolveCommitPair selects a commit against its first parent, or against the
// empty tree when it has none.
func resolveCommitPair(ctx context.Context, root, revision string) (reviewSide, reviewSide, error) {
	none := reviewSide{}
	head, err := resolveSide(ctx, root, "commit", revision, "")
	if err != nil {
		return none, none, err
	}
	if head.kind != sideCommit {
		return none, none, failure(Invalid, errors.New("commit must resolve to an available local commit"))
	}
	data, err := gitRead(ctx, root, nil, "rev-list", "--parents", "-n", "1", head.id)
	if err != nil {
		return none, none, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return reviewSide{kind: SideEmpty, id: SideEmpty, label: "empty tree (first commit)"}, head, nil
	}
	if !objectID.MatchString(fields[1]) {
		return none, none, failure(Invalid, errors.New("invalid parent object"))
	}
	return commitSide(fields[1]), head, nil
}

func commitSide(oid string) reviewSide {
	return reviewSide{kind: sideCommit, id: oid, label: "commit " + oid[:12]}
}

func resolveSide(ctx context.Context, root, role, name, fallback string) (reviewSide, error) {
	if name == "" {
		name = fallback
	}
	switch name {
	case SideWorktree:
		return reviewSide{kind: SideWorktree, id: SideWorktree, label: "indexed working tree"}, nil
	case SideIndex:
		return reviewSide{kind: SideIndex, id: SideIndex, label: "staged index"}, nil
	case SideEmpty:
		return reviewSide{kind: SideEmpty, id: SideEmpty, label: "empty tree"}, nil
	}
	if name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n") {
		return reviewSide{}, failure(Invalid, errors.New("invalid "+role+" revision"))
	}
	resolved, err := gitRead(ctx, root, nil, "rev-parse", "--verify", "--end-of-options", name+"^{commit}")
	if err != nil {
		return reviewSide{}, failure(Invalid, errors.New(role+" must resolve to an available local commit"))
	}
	oid := strings.TrimSpace(string(resolved))
	if !objectID.MatchString(oid) {
		return reviewSide{}, failure(Invalid, errors.New("invalid "+role+" object"))
	}
	return commitSide(oid), nil
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

// sideLoader reads both sides in one pass: an object shared by the two
// sides, or identical to indexed working-tree source, is read at most once.
type sideLoader struct {
	idx       *Index
	graph     *Graph
	permitted map[string]bool
	hashes    map[string]string
	contents  map[string]string
	blobs     map[string][]treeBlob
}

func (l *sideLoader) list(ctx context.Context, sides ...reviewSide) error {
	l.blobs = map[string][]treeBlob{}
	need := map[string]bool{}
	for _, side := range sides {
		blobs, err := sideBlobs(ctx, l.idx.Root, side)
		if err != nil {
			return err
		}
		kept := make([]treeBlob, 0, len(blobs))
		for _, blob := range blobs {
			if !l.allowed(blob.path) {
				continue
			}
			kept = append(kept, blob)
			if _, ok := l.current(blob); !ok {
				need[blob.id] = true
			}
		}
		l.blobs[side.id] = kept
	}
	ids := make([]string, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	contents, err := readBlobs(ctx, l.idx.Root, ids)
	l.contents = contents
	return err
}

func (l *sideLoader) allowed(path string) bool {
	allowed, seen := l.permitted[path]
	if !seen {
		allowed = permitted(l.idx.Root, path)
		l.permitted[path] = allowed
	}
	return allowed
}

// current returns indexed working-tree source when it is byte-identical to the blob.
func (l *sideLoader) current(blob treeBlob) (string, bool) {
	source, ok := l.idx.sources[blob.path]
	if !ok {
		return "", false
	}
	key := blob.path + "\x00" + strconv.Itoa(len(blob.id))
	hash, seen := l.hashes[key]
	if !seen {
		hash = gitBlobHash(source, len(blob.id))
		l.hashes[key] = hash
	}
	return source, hash == blob.id
}

func (l *sideLoader) snapshot(side reviewSide) (*Index, *Graph, error) {
	if side.kind == SideWorktree {
		return l.idx, l.graph, nil
	}
	idx := l.idx
	snapshot := &Index{Root: idx.Root, registry: idx.registry, Files: []FileEntry{}, sources: map[string]string{}, policy: idx.policy, Revision: side.id}
	module := ""
	snapshot.modulePath = &module
	total := 0
	for _, blob := range l.blobs[side.id] {
		source, ok := l.current(blob)
		if !ok {
			source, ok = l.contents[blob.id]
		}
		if !ok {
			continue
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
		if total > maxRepositoryBytes || len(snapshot.Files) >= maxRepositoryFiles {
			return nil, nil, failure(TooLarge, errors.New(side.label+" exceeds analysis limits"))
		}
		snapshot.sources[blob.path] = source
		snapshot.Files = append(snapshot.Files, entry)
	}
	return snapshot, BuildGraph(snapshot), nil
}

// sideBlobs lists the regular files of a commit or of the staged index,
// relative to the analysis root.
func sideBlobs(ctx context.Context, root string, side reviewSide) ([]treeBlob, error) {
	var args []string
	switch side.kind {
	case sideCommit:
		args = []string{"ls-tree", "-r", "-z", side.id, "--", "."}
	case SideIndex:
		args = []string{"ls-files", "--stage", "-z"}
	default:
		return nil, nil
	}
	data, err := gitRun(ctx, root, nil, 8*1024*1024, gitListTimeout, args...)
	if err != nil {
		return nil, err
	}
	blobs := []treeBlob{}
	for _, record := range strings.Split(string(data), "\x00") {
		if blob, ok := parseTreeBlob(record); ok {
			blobs = append(blobs, blob)
		}
	}
	return blobs, nil
}

// parseTreeBlob accepts an `ls-tree` record (mode, type, object) or an
// `ls-files --stage` record (mode, object, stage); unmerged stages, symbolic
// links and submodules are skipped.
func parseTreeBlob(record string) (treeBlob, bool) {
	meta, name, ok := strings.Cut(record, "\t")
	fields := strings.Fields(meta)
	if !ok || len(fields) != 3 || !fs.ValidPath(name) {
		return treeBlob{}, false
	}
	id := fields[2]
	if fields[1] != "blob" {
		if fields[2] != "0" {
			return treeBlob{}, false
		}
		id = fields[1]
	}
	if !objectID.MatchString(id) || (fields[0] != "100644" && fields[0] != "100755") {
		return treeBlob{}, false
	}
	return treeBlob{path: name, id: id}, true
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
		review.Added, review.Removed = review.Added+change.Added, review.Removed+change.Removed
	}
	review.Tour.Stops, review.Tour.Limitations = changeStops(review.Changes, before.Revision)
	return review
}

// changeStops turns the ordered changes into a bounded tour.
func changeStops(changes []UnitChange, base string) ([]TourStop, []string) {
	stops, limitations := []TourStop{}, []string{}
	for _, change := range changes {
		if len(stops) >= 32 {
			limitations = append(limitations, "Tour limited to 32 stops; all detected changes remain in the changes list.")
			break
		}
		p := change.Node.Provenance
		p.Provider = "Local Git comparison"
		if objectID.MatchString(base) {
			p.Commit = base
		}
		p.Detail = change.Status + " source unit against local base"
		stops = append(stops, TourStop{Node: change.Node, Reason: change.Status + " · " + change.Cohort, Provenance: p})
	}
	return stops, limitations
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
		change := UnitChange{Node: node, Status: "file context changed", Cohort: changeCohort(node), BeforeSource: before.sources[path], AfterSource: after.sources[path], RelationshipsAdded: []Edge{}, RelationshipsRemoved: []Edge{}, Related: []Recommendation{}}
		// Unit bodies have their own stops: diff only the text outside them.
		oldLines, oldNumbers := uncoveredLines(before.sources[path], oldNodes)
		newLines, newNumbers := uncoveredLines(after.sources[path], newNodes)
		change.apply(diffNumbered(oldLines, newLines, func(i int) int { return oldNumbers[i] }, func(j int) int { return newNumbers[j] }))
		out = append(out, change)
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
	change.setDiff()
	if currentOK {
		change.Related = newGraph.ReadNext(id)
	} else {
		change.Related = oldGraph.ReadNext(id)
	}
	return &change
}

// uncoveredLines returns the lines no unit covers, with their file line numbers.
func uncoveredLines(source string, nodes map[string]Node) ([]string, []int) {
	covered := map[int]bool{}
	for _, n := range nodes {
		for line := n.Start; line <= n.End; line++ {
			covered[line] = true
		}
	}
	lines, numbers := []string{}, []int{}
	for i, line := range sourceLines(source) {
		if !covered[i+1] {
			lines, numbers = append(lines, line), append(numbers, i+1)
		}
	}
	return lines, numbers
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
