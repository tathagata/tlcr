package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func graphFixture(t *testing.T) (*Index, *Graph) {
	t.Helper()
	root := t.TempDir()
	put(t, root, "go.mod", "module example.test/demo\n\ngo 1.24\n")
	put(t, root, "main.go", `package main
import "example.test/demo/service"
func main(){service.Work()}
`)
	put(t, root, "service/work.go", `package service
import "os"
func Work(){helper(); _=os.Getenv("MODE")}
func helper(){}
type First struct{}
func (First) Read(){helper()}
type Second struct{}
func (*Second) Read(){}
`)
	put(t, root, "service/work_test.go", `package service
import "testing"
func TestWork(t *testing.T){Work()}
`)
	put(t, root, "docs/adr/001.md", "# Runtime choice\n`service/work.go` owns this behavior.\n")
	put(t, root, "ignored/secret.go", "package secret\nfunc Hidden(){}\n")
	put(t, root, ".gitignore", "ignored/\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	return idx, BuildGraph(idx)
}

func named(g *Graph, name string) Node {
	for _, n := range g.Nodes {
		if n.Name == name && n.UnitID != "" {
			return n
		}
	}
	return Node{}
}
func edgeExists(g *Graph, from, to, kind string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestGraphLocalFactsAndProvenance(t *testing.T) {
	_, g := graphFixture(t)
	main, work, helper, test := named(g, "func main"), named(g, "func Work"), named(g, "func helper"), named(g, "func TestWork")
	for _, edge := range [][3]string{{main.ID, work.ID, "calls"}, {work.ID, helper.ID, "calls"}, {work.ID, test.ID, "tested-by"}} {
		if !edgeExists(g, edge[0], edge[1], edge[2]) {
			t.Errorf("missing edge %v", edge)
		}
	}
	if !hasRole(main, "entry point") || !hasRole(work, "configuration") {
		t.Fatalf("roles not grounded: %v / %v", main.Roles, work.Roles)
	}
	if named(g, "method First.Read").ID == named(g, "method Second.Read").ID {
		t.Fatal("receiver identity collision")
	}
	for _, e := range g.Edges {
		if e.Provenance.Path == "" || e.Provenance.Start < 1 {
			t.Fatalf("edge lacks source provenance: %#v", e)
		}
	}
	for _, n := range g.Nodes {
		if strings.HasPrefix(n.Path, "ignored/") {
			t.Fatal("excluded source in graph")
		}
	}
	if len(g.ReadNext(work.ID)) < 2 {
		t.Fatal("read next lacks callee and test")
	}
}

func TestGraphDoesNotGuessShadowedOrInterfaceCalls(t *testing.T) {
	root := t.TempDir()
	put(t, root, "app.go", `package app
func Target(){}
func Caller(){Target:=func(){}; Target()}
type I interface{Run()}
type Real struct{}
func(Real) Run(){}
func Dynamic(i I){i.Run()}
`)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	g := BuildGraph(idx)
	if edgeExists(g, named(g, "func Caller").ID, named(g, "func Target").ID, "calls") {
		t.Fatal("shadowed identifier resolved to wrong function")
	}
	if edgeExists(g, named(g, "func Dynamic").ID, named(g, "method Real.Run").ID, "calls") {
		t.Fatal("dynamic interface dispatch invented a concrete target")
	}
}

func TestGraphAndToursStableWithoutToolsOrModel(t *testing.T) {
	idx, g := graphFixture(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	a, _ := json.Marshal(g)
	b, _ := json.Marshal(BuildGraph(idx))
	if !reflect.DeepEqual(a, b) {
		t.Fatal("graph serialization is nondeterministic")
	}
	repo := NewRepository(idx)
	first, err := repo.Tour(context.Background(), "architecture")
	if err != nil || len(first.Stops) < 3 {
		t.Fatalf("tour: %#v %v", first, err)
	}
	second, _ := repo.Tour(context.Background(), "architecture")
	if !reflect.DeepEqual(first, second) {
		t.Fatal("unstable tour ordering")
	}
	for _, stop := range first.Stops {
		if stop.Node.Start < 1 || stop.Reason == "" || stop.Provenance.Path == "" {
			t.Fatal("tour stop lacks explanation/provenance")
		}
	}
	recent, err := repo.Tour(context.Background(), "recent")
	if err != nil || len(recent.Stops) != 0 || len(recent.Limitations) < 2 {
		t.Fatal("missing Git must degrade explicitly")
	}
}

func TestEvidenceDocumentsAndProviderFailure(t *testing.T) {
	idx, g := graphFixture(t)
	q := EvidenceQuery{Index: idx, Graph: g, Node: named(g, "func Work")}
	result := CollectEvidence(context.Background(), q, StructureEvidence{}, DocumentEvidence{}, brokenEvidence{})
	kinds := map[string]bool{}
	for _, e := range result.Items {
		kinds[e.Kind] = true
	}
	if !kinds["tested-by"] || !kinds["document-reference"] || len(result.Diagnostics) != 1 {
		t.Fatalf("evidence: %#v", result)
	}
	text := EvidenceText(result.Items, 4096)
	if !strings.Contains(text, "service/work.go") || len(EvidenceText(result.Items, 30)) > 30 {
		t.Fatal("evidence serialization did not preserve provenance/bound")
	}
}

type brokenEvidence struct{}

func (brokenEvidence) Name() string { return "Broken provider" }
func (brokenEvidence) Evidence(context.Context, EvidenceQuery) ([]Evidence, error) {
	return nil, errors.New("unavailable")
}

func TestSnapshotRejectsChangedSourceAndNewIgnore(t *testing.T) {
	idx, g := graphFixture(t)
	repo := NewRepository(idx)
	work := named(g, "func Work")
	put(t, idx.Root, "service/work.go", "package service\nfunc Different(){}\n")
	if _, err := repo.Orient(work.ID); err == nil {
		t.Fatal("stale source silently received old graph facts")
	}
	put(t, idx.Root, ".gitignore", "ignored/\nservice/\n")
	if _, err := repo.Overview(); err == nil {
		t.Fatal("new ignore did not invalidate snapshot")
	}
	if _, _, err := idx.Read("service/work.go"); err == nil {
		t.Fatal("newly ignored source readable")
	}
	if err := repo.Refresh(); err != nil {
		t.Fatal(err)
	}
	_, fresh := repo.View()
	for _, n := range fresh.Nodes {
		if strings.HasPrefix(n.Path, "service/") {
			t.Fatal("ignored node survived refresh")
		}
	}
}

func TestConfinedReadsAndMalformedIgnore(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	put(t, root, "app.go", "package app\n")
	put(t, outside, "secret.go", "package secret\n")
	put(t, root, ".gitignore", "[]\n[z-a]\n[!]\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "app.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "app.go")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, err := idx.Read("app.go"); err == nil {
		t.Fatal("symlink swap escaped read boundary")
	}
	for _, path := range []string{"../secret.go", "/etc/passwd", "a/../../secret.go"} {
		if _, err := readWithin(root, path, maxFileBytes); err == nil {
			t.Fatal("unsafe path read")
		}
	}
}

func TestWholeFileFallbackAndDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	put(t, root, "app.go", "package app\nvar Thing=1\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	source, entry, err := idx.Read("app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entry.Units, idx.CurrentUnits("app.go", entry, source)) {
		t.Fatal("scan/read fallback differ")
	}
	units := goUnits("init.go", []byte("package app\nfunc init(){}\nfunc init(){}\n"))
	if len(units) != 2 || units[0].ID == units[1].ID {
		t.Fatal("duplicate init IDs")
	}
}

type matchParser struct{ kind string }

func (p matchParser) Kind() string { return p.kind }
func (p matchParser) Match(_ string, peek []byte) bool {
	return len(peek) <= 4096 && strings.HasPrefix(string(peek), "match")
}
func (matchParser) Units(string, []byte) []Unit { return nil }
func TestParserPrecedenceAndBoundedPeek(t *testing.T) {
	reg, err := NewRegistry(ParserRegistration{Parser: matchParser{kind: "low"}, Priority: 1}, ParserRegistration{Parser: matchParser{kind: "high"}, Priority: 10})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Parse("no-extension", []byte("match"+strings.Repeat("x", 5000)))
	if !ok || entry.Kind != "high" || len(entry.Units) != 1 {
		t.Fatal("content matching/precedence/fallback failed")
	}
	if _, err := NewRegistry(ParserRegistration{Parser: matchParser{kind: "a"}, Priority: 1}, ParserRegistration{Parser: matchParser{kind: "b"}, Priority: 1}); err == nil {
		t.Fatal("ambiguous parser priority accepted")
	}
}

func TestCurrentUnitsUsesRegisteredParser(t *testing.T) {
	root := t.TempDir()
	put(t, root, "custom.odd", "match custom contents")
	registry, err := NewRegistry(ParserRegistration{Parser: matchParser{kind: "custom"}, Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ScanWithRegistry(root, registry)
	if err != nil {
		t.Fatal(err)
	}
	source, entry, err := idx.Read("custom.odd")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entry.Units, idx.CurrentUnits("custom.odd", entry, source)) {
		t.Fatal("fresh units ignored custom parser")
	}
}
