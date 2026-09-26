package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return string(out)
}
func TestGitWorkingTreeIdentityAndEvidence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := t.TempDir()
	testGit(t, root, "init")
	put(t, root, "main.go", "package main\nfunc main(){}\n")
	testGit(t, root, "add", "main.go")
	testGit(t, root, "commit", "-m", "Initial fixture")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	source, _, err := idx.Read("main.go")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := BlobIdentity(context.Background(), idx, "main.go", source)
	if !ok {
		t.Fatal("tracked blob identity unavailable")
	}
	b, ok := BlobIdentity(context.Background(), idx, "main.go", source+"// uncommitted\n")
	if !ok || a == b {
		t.Fatal("dirty source failed to invalidate identity")
	}
	testGit(t, root, "commit", "--allow-empty", "-m", "Unrelated commit")
	again, _ := BlobIdentity(context.Background(), idx, "main.go", source)
	if a != again {
		t.Fatal("unchanged source identity changed with HEAD")
	}
	assertGitEvidenceAndReadOnly(t, idx, source)
}

func assertGitEvidenceAndReadOnly(t *testing.T, idx *Index, source string) {
	t.Helper()
	root := idx.Root
	g := BuildGraph(idx)
	result, err := (GitEvidence{}).Evidence(context.Background(), EvidenceQuery{Index: idx, Graph: g, Node: named(g, "func main")})
	if err != nil || len(result) < 2 {
		t.Fatalf("history: %#v %v", result, err)
	}
	for _, e := range result {
		if e.Kind == "commit" && (!objectID.MatchString(e.Provenance.Commit) || !strings.Contains(e.Detail, "Initial fixture")) {
			t.Fatal("history provenance incorrect")
		}
	}
	recent, err := RecentFiles(context.Background(), idx)
	if err != nil || len(recent) != 1 || recent[0] != "main.go" {
		t.Fatalf("recent: %v %v", recent, err)
	}
	before := testGit(t, root, "status", "--porcelain=v1")
	_, _ = BlobIdentity(context.Background(), idx, "main.go", source)
	_, _ = (GitEvidence{}).Evidence(context.Background(), EvidenceQuery{Index: idx, Graph: g, Node: named(g, "func main")})
	if after := testGit(t, root, "status", "--porcelain=v1"); before != after {
		t.Fatal("evidence modified repository")
	}
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "wrong-repository"))
	if _, ok := BlobIdentity(context.Background(), idx, "main.go", source); !ok {
		t.Fatal("ambient Git environment overrode repository boundary")
	}
}
func TestGitAbsentAndUntrackedFallback(t *testing.T) {
	idx, _, _ := explainFixture(t)
	if _, ok := BlobIdentity(context.Background(), idx, "main.go", "source"); ok {
		t.Fatal("non-Git identity accepted")
	}
	t.Setenv("PATH", t.TempDir())
	if _, ok := BlobIdentity(context.Background(), idx, "main.go", "source"); ok {
		t.Fatal("missing Git did not fall back")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gitRead(ctx, idx.Root, nil, "status"); err == nil {
		t.Fatal("cancelled operation succeeded")
	}
}
