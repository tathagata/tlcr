package core

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const briefWork = `package service

import "os"

// Work sums the configured values.
func Work() int {
	helper()
	_ = os.Getenv("MODE")
	return 1
}

func helper() {}
`

func briefFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := t.TempDir()
	testGit(t, root, "init")
	put(t, root, "go.mod", "module example.test/demo\n\ngo 1.24\n")
	put(t, root, "service/work.go", briefWork)
	put(t, root, "service/work_test.go", "package service\nimport \"testing\"\nfunc TestWorkReturnsHTTPTotal(t *testing.T){\n\tt.Run(\"empty config\", func(t *testing.T){ Work() })\n}\n")
	commitAt(t, root, "2026-01-01T00:00:00Z", "Add Work")
	// The body changes later and the comment does not.
	put(t, root, "service/work.go", strings.Replace(briefWork, "return 1", "return 2", 1))
	commitAt(t, root, "2026-02-01T00:00:00Z", "Return the real total")
	return root
}

func commitAt(t *testing.T, root, date, message string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_DATE", date)
	t.Setenv("GIT_COMMITTER_DATE", date)
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", message)
}

func briefFor(t *testing.T, root, id string) Brief {
	t.Helper()
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	brief, err := NewRepository(idx).Brief(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return brief
}

func factsOf(b Brief, kind string) []BriefFact {
	out := []BriefFact{}
	for _, f := range b.Facts {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestBriefRatesSourcesByReliability(t *testing.T) {
	root := briefFixture(t)
	brief := briefFor(t, root, "unit:service/work.go:func Work")
	if calls := factsOf(brief, "calls"); len(calls) != 1 || calls[0].Text != "func helper" || calls[0].Reliability != ReliabilityCurrent {
		t.Fatalf("calls: %+v", calls)
	}
	// The test calls Work, and is reported as a test rather than also as a caller.
	if callers := factsOf(brief, "called-by"); len(callers) != 0 {
		t.Fatalf("test reported as caller: %+v", callers)
	}
	if tests := factsOf(brief, "test-states"); len(tests) != 1 || tests[0].Text != "work returns http total; subtests: empty config" || tests[0].Reliability != ReliabilityTest {
		t.Fatalf("tests: %+v", tests)
	}
	assertBriefDated(t, brief)
}

// assertBriefDated checks line history and the comment rated against it.
func assertBriefDated(t *testing.T, brief Brief) {
	t.Helper()
	commits := factsOf(brief, "commit")
	if len(commits) != 2 || commits[0].Text != "Return the real total" || commits[0].Date != "2026-02-01" || commits[1].Text != "Add Work" || commits[0].Provenance.Commit == "" {
		t.Fatalf("commits: %+v", commits)
	}
	comment := factsOf(brief, "comment")
	if len(comment) != 1 || comment[0].Text != "Work sums the configured values." || comment[0].Reliability != ReliabilityCommentStale || comment[0].Date != "2026-01-01" || !strings.Contains(comment[0].Provenance.Detail, "2026-02-01") {
		t.Fatalf("comment: %+v", comment)
	}
	for i := 1; i < len(brief.Facts); i++ {
		if reliabilityOrder[brief.Facts[i-1].Reliability] > reliabilityOrder[brief.Facts[i].Reliability] {
			t.Fatalf("facts not ordered by reliability: %+v", brief.Facts)
		}
	}
	if len(factsOf(brief, "uncommitted")) != 0 || len(brief.Diagnostics) != 0 {
		t.Fatalf("clean unit: %+v", brief)
	}
}

func TestBriefFollowsTheUnitThroughUncommittedEdits(t *testing.T) {
	root := briefFixture(t)
	// Lines added above the unit move it; history still finds it by its first line.
	put(t, root, "service/work.go", strings.Replace(briefWork, "import \"os\"\n", "import \"os\"\n\nvar extra = 1\nvar more = 2\n", 1))
	moved := briefFor(t, root, "unit:service/work.go:func Work")
	if len(factsOf(moved, "commit")) != 2 || len(factsOf(moved, "uncommitted")) != 1 {
		t.Fatalf("moved unit: %+v", moved)
	}
	// A comment rewritten now is newer than the committed code under it.
	put(t, root, "service/work.go", strings.Replace(briefWork, "// Work sums the configured values.", "// Work returns the real total.", 1))
	fresh := factsOf(briefFor(t, root, "unit:service/work.go:func Work"), "comment")
	if len(fresh) != 1 || fresh[0].Reliability != ReliabilityCommentFresh {
		t.Fatalf("fresh comment: %+v", fresh)
	}
	// A unit that is not in HEAD has no history, and says so.
	put(t, root, "service/new.go", "package service\n\n// Fresh is new.\nfunc Fresh() {}\n")
	added := briefFor(t, root, "unit:service/new.go:func Fresh")
	if len(factsOf(added, "commit")) != 0 || len(added.Diagnostics) != 1 || factsOf(added, "comment")[0].Reliability != ReliabilityCommentAge {
		t.Fatalf("new unit: %+v", added)
	}
}

func TestBriefTextDropsLeastReliableFactsFirst(t *testing.T) {
	brief := Brief{Node: Node{Name: "func Work", Path: "work.go"}, Facts: []BriefFact{
		{Kind: "calls", Text: "func helper", Reliability: ReliabilityCurrent, Provenance: Provenance{Path: "work.go", Start: 3}},
		{Kind: "commit", Text: "Return the real total", Reliability: ReliabilityDated, Date: "2026-02-01", Provenance: Provenance{Path: "work.go", Commit: strings.Repeat("a", 40)}},
		{Kind: "comment", Text: strings.Repeat("old words ", 30), Reliability: ReliabilityCommentStale, Provenance: Provenance{Path: "work.go", Start: 1}},
	}}
	full := BriefText(brief, 4000)
	var decoded struct {
		Unit  string
		Facts []struct{ Kind, Reliability, Source string }
	}
	if err := json.Unmarshal([]byte(full), &decoded); err != nil || decoded.Unit != "func Work" || len(decoded.Facts) != 3 || decoded.Facts[1].Source != "aaaaaaaaaaaa" || decoded.Facts[0].Source != "work.go:3" {
		t.Fatalf("brief text: %v %s", err, full)
	}
	short := BriefText(brief, 300)
	if len(short) > 300 || strings.Contains(short, "comment") || !strings.Contains(short, "Return the real total") {
		t.Fatalf("bounded brief text: %s", short)
	}
}

func TestSplitWords(t *testing.T) {
	for in, want := range map[string]string{"RepositoryPortIsStable": "repository port is stable", "ParseHTTPHeader": "parse http header", "snake_case_name": "snake case name", "": ""} {
		if got := splitWords(in); got != want {
			t.Errorf("splitWords(%q) = %q, want %q", in, got, want)
		}
	}
}
