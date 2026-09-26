package core

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Provenance identifies the source of a fact, separate from heuristic ranking or AI prose.
type Provenance struct {
	Provider string `json:"provider"`
	Path     string `json:"path"`
	Detail   string `json:"detail"`
	Commit   string `json:"commit,omitempty"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}

// Node is a normalized source location. UnitID addresses the parser's unit within its file.
type Node struct {
	UnitID     string     `json:"unit_id,omitempty"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	ID         string     `json:"id"`
	Path       string     `json:"path"`
	Language   string     `json:"language"`
	SourceHash string     `json:"source_hash"`
	Roles      []string   `json:"roles"`
	Reasons    []string   `json:"reasons"`
	Provenance Provenance `json:"provenance"`
	Start      int        `json:"start"`
	End        int        `json:"end"`
	Score      int        `json:"score"`
}

// Edge is a deterministic relationship; ambiguous/unresolved targets are omitted.
type Edge struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	Kind       string     `json:"kind"`
	Provenance Provenance `json:"provenance"`
}

// Graph is built once per repository snapshot and queried without rescanning or model calls.
type Graph struct {
	byID        map[string]int
	outgoing    map[string][]int
	incoming    map[string][]int
	edgeSet     map[string]bool
	Nodes       []Node       `json:"nodes"`
	Edges       []Edge       `json:"edges"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// BuildGraph normalizes current parser facts and local relationships.
func BuildGraph(idx *Index) *Graph {
	g := &Graph{Nodes: []Node{}, Edges: []Edge{}, Diagnostics: []Diagnostic{}, byID: map[string]int{}, outgoing: map[string][]int{}, incoming: map[string][]int{}, edgeSet: map[string]bool{}}
	for _, f := range idx.Files {
		p := Provenance{Provider: f.Kind + " parser", Path: f.Path, Start: 1, End: countLines([]byte(idx.sources[f.Path])), Detail: "Indexed source file"}
		g.addNode(Node{ID: "file:" + f.Path, Kind: "file", Name: path.Base(f.Path), Path: f.Path, Language: f.Kind, Start: 1, End: p.End, Provenance: p, SourceHash: Hash(idx.sources[f.Path])})
		for _, u := range f.Units {
			p.Start = u.Start
			p.End = u.End
			p.Detail = "Source unit identified by parser"
			roles := []string{}
			if strings.HasSuffix(f.Path, "_test.go") {
				roles = append(roles, "test support")
			}
			if f.Kind == "terraform" {
				roles = append(roles, u.Kind)
			}
			g.addNode(Node{ID: "unit:" + u.ID, UnitID: u.ID, Kind: u.Kind, Name: u.Name, Path: f.Path, Language: f.Kind, Start: u.Start, End: u.End, Roles: roles, Provenance: p, SourceHash: Hash(idx.sources[f.Path])})
			g.addEdge(Edge{From: "file:" + f.Path, To: "unit:" + u.ID, Kind: "contains", Provenance: p})
		}
	}
	for _, f := range idx.Files {
		for _, u := range f.Units {
			for _, link := range u.Links {
				for _, target := range idx.Files {
					if target.Kind != "terraform" || path.Dir(target.Path) != link {
						continue
					}
					for _, dest := range target.Units {
						g.addEdge(Edge{From: "unit:" + u.ID, To: "unit:" + dest.ID, Kind: "local-module-source", Provenance: Provenance{Provider: "HCL AST", Path: f.Path, Start: u.Start, End: u.End, Detail: "Literal local module source " + link}})
					}
				}
			}
		}
	}
	addGoRelationships(g, idx)
	g.finalize()
	return g
}

func (g *Graph) addNode(n Node) {
	if _, ok := g.byID[n.ID]; ok {
		return
	}
	if n.Roles == nil {
		n.Roles = []string{}
	}
	if n.Reasons == nil {
		n.Reasons = []string{}
	}
	g.byID[n.ID] = len(g.Nodes)
	g.Nodes = append(g.Nodes, n)
}

func (g *Graph) addEdge(e Edge) {
	if _, ok := g.byID[e.From]; !ok {
		return
	}
	if _, ok := g.byID[e.To]; !ok {
		return
	}
	key := e.From + "\x00" + e.To + "\x00" + e.Kind
	if g.edgeSet[key] {
		return
	}
	g.edgeSet[key] = true
	g.Edges = append(g.Edges, e)
}

func (g *Graph) role(id, role string) {
	if i, ok := g.byID[id]; ok {
		for _, r := range g.Nodes[i].Roles {
			if r == role {
				return
			}
		}
		g.Nodes[i].Roles = append(g.Nodes[i].Roles, role)
	}
}

func (g *Graph) finalize() {
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		return a.From+"\x00"+a.Kind+"\x00"+a.To < b.From+"\x00"+b.Kind+"\x00"+b.To
	})
	for i, e := range g.Edges {
		g.outgoing[e.From] = append(g.outgoing[e.From], i)
		g.incoming[e.To] = append(g.incoming[e.To], i)
	}
	for i := range g.Nodes {
		g.scoreNode(&g.Nodes[i])
	}
}

func (g *Graph) scoreNode(n *Node) {
	if n.UnitID == "" {
		return
	}
	sort.Strings(n.Roles)
	for _, r := range n.Roles {
		switch r {
		case "entry point":
			n.Score += 100
			n.Reasons = append(n.Reasons, "Entry point declared in source")
		case "network boundary", "storage boundary", "configuration":
			n.Score += 25
			n.Reasons = append(n.Reasons, r)
		case "test":
			n.Score += 5
			n.Reasons = append(n.Reasons, "Test declaration")
		case "resource", "module":
			n.Score += 20
			n.Reasons = append(n.Reasons, "Terraform "+r)
		}
	}
	callers, dependencies := g.connectionCounts(n.ID)
	if callers > 0 {
		n.Score += min(callers, 10) * 4
		n.Reasons = append(n.Reasons, fmt.Sprintf("%d production callers", callers))
	}
	if dependencies > 0 {
		n.Score += min(dependencies, 10) * 2
		n.Reasons = append(n.Reasons, fmt.Sprintf("%d local dependencies", dependencies))
	}
	if len(n.Reasons) == 0 {
		n.Reasons = append(n.Reasons, "Structural unit; no stronger role evidence available")
	}
}

func (g *Graph) connectionCounts(id string) (int, int) {
	callers, dependencies := 0, 0
	for _, e := range g.Incoming(id) {
		caller, _ := g.Node(e.From)
		if e.Kind == "calls" && !hasRole(caller, "test support") {
			callers++
		}
	}
	for _, e := range g.Outgoing(id) {
		if e.Kind == "calls" || e.Kind == "local-module-source" {
			dependencies++
		}
	}
	return callers, dependencies
}

// Node returns a copy of a known location.
func (g *Graph) Node(id string) (Node, bool) {
	i, ok := g.byID[id]
	if !ok {
		return Node{}, false
	}
	return g.Nodes[i], true
}

// Outgoing returns local facts originating at a node.
func (g *Graph) Outgoing(id string) []Edge {
	out := make([]Edge, 0, len(g.outgoing[id]))
	for _, i := range g.outgoing[id] {
		out = append(out, g.Edges[i])
	}
	return out
}

// Incoming returns local facts pointing to a node.
func (g *Graph) Incoming(id string) []Edge {
	out := make([]Edge, 0, len(g.incoming[id]))
	for _, i := range g.incoming[id] {
		out = append(out, g.Edges[i])
	}
	return out
}

// Ranked lists source units by an explicit deterministic heuristic, not inferred correctness.
func (g *Graph) Ranked() []Node {
	out := []Node{}
	for _, n := range g.Nodes {
		if n.UnitID != "" && n.Language != "document" {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Recommendation provides a destination with its actual relationship and provenance.
type Recommendation struct {
	Reason       string     `json:"reason"`
	Relationship string     `json:"relationship"`
	Provenance   Provenance `json:"provenance"`
	Node         Node       `json:"node"`
}

// ReadNext offers bounded, reproducible alternatives instead of guessing what a user should ask.
func (g *Graph) ReadNext(id string) []Recommendation {
	out := []Recommendation{}
	seen := map[string]bool{}
	add := func(target, kind, reason string, p Provenance) {
		n, ok := g.Node(target)
		if !ok || target == id || seen[target] {
			return
		}
		seen[target] = true
		out = append(out, Recommendation{Node: n, Reason: reason, Relationship: kind, Provenance: p})
	}
	for _, e := range g.Outgoing(id) {
		reason := map[string]string{"calls": "Called by this unit", "tested-by": "Test directly calls this unit", "local-module-source": "Implementation of this local module", "imports": "Imported local package", "contains": "Contained source", "route-to-handler": "Handler registered here"}[e.Kind]
		if reason != "" {
			add(e.To, e.Kind, reason, e.Provenance)
		}
	}
	for _, e := range g.Incoming(id) {
		if e.Kind == "calls" {
			add(e.From, "called-by", "Calls this unit", e.Provenance)
		}
		if e.Kind == "tested-by" {
			add(e.From, "tests", "Production unit exercised by this test", e.Provenance)
		}
	}
	priority := map[string]int{"route-to-handler": 0, "calls": 1, "local-module-source": 2, "tested-by": 3, "called-by": 4, "tests": 5, "imports": 6, "contains": 7}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if priority[a.Relationship] != priority[b.Relationship] {
			return priority[a.Relationship] < priority[b.Relationship]
		}
		if a.Node.Score != b.Node.Score {
			return a.Node.Score > b.Node.Score
		}
		return strings.Compare(a.Node.ID, b.Node.ID) < 0
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
