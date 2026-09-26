package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// Orientation is a deterministic view of a source unit, usable by every surface.
type Orientation struct {
	Revision string           `json:"revision"`
	Incoming []Edge           `json:"incoming"`
	Outgoing []Edge           `json:"outgoing"`
	Next     []Recommendation `json:"next"`
	Node     Node             `json:"node"`
}

// Overview answers where to begin without a model or account.
type Overview struct {
	Name          string       `json:"name"`
	Revision      string       `json:"revision"`
	EntryPoints   []Node       `json:"entry_points"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	Files         int          `json:"files"`
	Units         int          `json:"units"`
	Relationships int          `json:"relationships"`
}

// Overview returns a bounded set of ranked starting locations and analysis limitations.
func (r *Repository) Overview() (Overview, error) {
	idx, g := r.View()
	if err := idx.CheckPolicy(); err != nil {
		return Overview{}, err
	}
	ranked := g.Ranked()
	count := len(ranked)
	production := []Node{}
	for _, n := range ranked {
		if !hasRole(n, "test support") {
			production = append(production, n)
		}
	}
	ranked = production
	if len(ranked) > 8 {
		ranked = ranked[:8]
	}
	diagnostics := append([]Diagnostic{}, idx.Diagnostics...)
	diagnostics = append(diagnostics, g.Diagnostics...)
	return Overview{Name: filepath.Base(idx.Root), Files: len(idx.Files), Units: count, Relationships: len(g.Edges), EntryPoints: ranked, Diagnostics: diagnostics, Revision: idx.Revision}, nil
}

// Orient validates the selected source against the snapshot before returning relationships.
func (r *Repository) Orient(id string) (Orientation, error) {
	idx, g := r.View()
	node, err := checkedNode(idx, g, id)
	if err != nil {
		return Orientation{}, err
	}
	return Orientation{Node: node, Incoming: g.Incoming(id), Outgoing: g.Outgoing(id), Next: g.ReadNext(id), Revision: idx.Revision}, nil
}

func checkedNode(idx *Index, g *Graph, id string) (Node, error) {
	if err := idx.CheckPolicy(); err != nil {
		return Node{}, err
	}
	node, ok := g.Node(id)
	if !ok {
		return Node{}, failure(NotFound, errors.New("source location not indexed"))
	}
	if node.Kind == "package" {
		return node, nil
	}
	source, _, err := idx.Read(node.Path)
	if err != nil {
		return Node{}, failure(NotFound, err)
	}
	if Hash(source) != node.SourceHash {
		return Node{}, failure(Stale, errors.New("source changed; refresh the repository to rebuild relationships"))
	}
	return node, nil
}

// Evidence returns local observations independently of optional model configuration.
func (r *Repository) Evidence(ctx context.Context, id string) (EvidenceResult, error) {
	idx, g := r.View()
	node, err := checkedNode(idx, g, id)
	if err != nil {
		return EvidenceResult{}, err
	}
	return CollectEvidence(ctx, EvidenceQuery{Index: idx, Graph: g, Node: node}, DefaultEvidenceProviders()...), nil
}

// TourStop explains the source evidence behind one step in a reading path.
type TourStop struct {
	Reason     string     `json:"reason"`
	Provenance Provenance `json:"provenance"`
	Node       Node       `json:"node"`
}

// Tour is an ordered, reproducible path through known graph facts.
type Tour struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Revision    string     `json:"revision"`
	Stops       []TourStop `json:"stops"`
	Limitations []string   `json:"limitations"`
}

// Tour generates a bounded reading path. Weak evidence produces fewer stops, never invented links.
func (r *Repository) Tour(ctx context.Context, kind string) (Tour, error) {
	idx, g := r.View()
	if err := idx.CheckPolicy(); err != nil {
		return Tour{}, err
	}
	titles := map[string]string{"architecture": "Architecture Tour", "execution": "Execution Flow", "state": "Data & State", "testing": "Testing Strategy", "recent": "Recent Changes"}
	title, ok := titles[kind]
	if !ok {
		return Tour{}, failure(Invalid, errors.New("unknown tour"))
	}
	tour := Tour{ID: kind, Title: title, Revision: idx.Revision, Stops: []TourStop{}, Limitations: []string{"Ranking is a deterministic heuristic. Unresolved and external calls are omitted."}}
	builder := tourBuilder{tour: &tour, graph: g, ranked: g.Ranked(), seen: map[string]bool{}}
	switch kind {
	case "architecture", "execution":
		builder.architecture(kind)
	case "state":
		builder.state()
	case "testing":
		builder.testing()
	case "recent":
		builder.recent(ctx, idx)
	}

	if len(tour.Stops) == 0 {
		tour.Limitations = append(tour.Limitations, "Insufficient local evidence for this tour.")
	}
	return tour, nil
}

type tourBuilder struct {
	tour   *Tour
	graph  *Graph
	seen   map[string]bool
	ranked []Node
}

func (b *tourBuilder) add(n Node, reason string, p Provenance) {
	if len(b.tour.Stops) >= 16 || b.seen[n.ID] || n.UnitID == "" || n.Language == "document" {
		return
	}
	b.seen[n.ID] = true
	b.tour.Stops = append(b.tour.Stops, TourStop{Node: n, Reason: reason, Provenance: p})
}

func (b *tourBuilder) architecture(kind string) {
	starts := []Node{}
	for _, n := range b.ranked {
		if hasRole(n, "entry point") {
			starts = append(starts, n)
		}
	}
	if len(starts) == 0 && kind == "architecture" && len(b.ranked) > 0 {
		starts = append(starts, b.ranked[0])
		b.tour.Limitations = append(b.tour.Limitations, "No explicit entry point found; starting from highest-ranked structural unit.")
	}
	queue := make([]string, 0, len(starts))
	for _, n := range starts {
		b.add(n, strings.Join(n.Reasons, " · "), n.Provenance)
		queue = append(queue, n.ID)
	}
	b.follow(queue)
	if kind == "architecture" {
		for _, n := range b.ranked {
			if hasRole(n, "test support") {
				continue
			}
			b.add(n, "Additional structural context · "+strings.Join(n.Reasons, " · "), n.Provenance)
		}
	}
}

func (b *tourBuilder) state() {
	for _, n := range b.ranked {
		if hasRole(n, "storage boundary") || hasRole(n, "configuration") || hasRole(n, "resource") || hasRole(n, "variable") || hasRole(n, "output") {
			b.add(n, strings.Join(n.Reasons, " · "), n.Provenance)
		}
	}
}

func (b *tourBuilder) testing() {
	for _, n := range b.ranked {
		for _, e := range b.graph.Outgoing(n.ID) {
			if e.Kind != "tested-by" {
				continue
			}
			test, _ := b.graph.Node(e.To)
			b.add(n, "Production unit directly called by a test", e.Provenance)
			b.add(test, "Test directly calls "+n.Name, e.Provenance)
		}
	}
	for _, n := range b.ranked {
		if hasRole(n, "test") {
			b.add(n, "Test declaration; inspect assertions for intended behavior", n.Provenance)
		}
	}
}

func (b *tourBuilder) recent(ctx context.Context, idx *Index) {
	files, err := RecentFiles(ctx, idx)
	if err != nil {
		b.tour.Limitations = append(b.tour.Limitations, err.Error())
		return
	}
	for _, file := range files {
		for _, n := range b.ranked {
			if n.Path == file {
				b.add(n, "Unit in a recently changed file (file-level history)", Provenance{Provider: "Git log", Path: file, Start: n.Start, End: n.End, Detail: "File appeared in the latest 30 local commits", Commit: ""})
				break
			}
		}
	}
}

func (b *tourBuilder) follow(queue []string) {
	visited := map[string]bool{}
	for len(queue) > 0 && len(b.tour.Stops) < 16 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		for _, next := range b.graph.ReadNext(id) {
			if next.Relationship != "calls" && next.Relationship != "local-module-source" && next.Relationship != "route-to-handler" {
				continue
			}
			parent, _ := b.graph.Node(id)
			reason := map[string]string{"calls": "Called by ", "local-module-source": "Local implementation referenced by ", "route-to-handler": "Handler registered by "}[next.Relationship] + parent.Name
			b.add(next.Node, reason, next.Provenance)
			queue = append(queue, next.Node.ID)
		}
	}
}

func hasRole(n Node, role string) bool {
	for _, r := range n.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Command describes navigation semantics shared by adapters; key bindings belong to a surface.
type Command struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Commands lists the supported comprehension actions independently of browser keys.
func Commands() []Command {
	return []Command{{"next", "Next tour stop"}, {"previous", "Previous tour stop"}, {"back", "Reading history back"}, {"forward", "Reading history forward"}, {"search", "Find file or symbol"}, {"tour", "Choose tour"}, {"read-next", "Read Next"}, {"tests", "Related tests"}, {"callers", "Callers"}, {"dependencies", "Dependencies"}, {"evidence", "Show evidence"}, {"explain", "Preview AI enrichment"}, {"help", "Keyboard help"}, {"escape", "Close dialog"}}
}
