package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/tathagata/coderead/internal/core"
)

func TestReviewCLIProducesLocalChangeTour(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := t.TempDir()
	put(t, root, "main.go", "package main\nfunc main(){}\n")
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Base"}} {
		command := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s %v", output, err)
		}
	}
	put(t, root, "main.go", "package main\nfunc main(){println(1)}\n")
	var output bytes.Buffer
	if err := runReview(context.Background(), []string{"--json", root}, &output); err != nil {
		t.Fatal(err)
	}
	var review core.ChangeReview
	if err := json.Unmarshal(output.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Files != 1 || len(review.Tour.Stops) != 1 || review.Changes[0].Status != "modified" {
		t.Fatalf("unexpected review: %#v", review)
	}
}

func TestReviewCLISelectionFlags(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := t.TempDir()
	put(t, root, "main.go", "package main\nfunc main(){}\n")
	git := func(args ...string) {
		t.Helper()
		command := exec.CommandContext(context.Background(), "git", append([]string{"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s %v", output, err)
		}
	}
	git("init")
	git("add", ".")
	git("commit", "-m", "Base")
	put(t, root, "main.go", "package main\nfunc main(){}\nfunc Staged(){}\n")
	git("add", ".")
	put(t, root, "main.go", "package main\nfunc main(){}\nfunc Staged(){}\nfunc Unstaged(){}\n")
	names := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := runReview(context.Background(), append(args, "--json", root), &output); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var review core.ChangeReview
		if err := json.Unmarshal(output.Bytes(), &review); err != nil {
			t.Fatal(err)
		}
		found := ""
		for _, change := range review.Changes {
			found += change.Node.Name + ";"
		}
		return found
	}
	for args, want := range map[string]string{"--staged": "func Staged;", "--unstaged": "func Unstaged;", "--commit=HEAD": "main.go;func main;", "--base=HEAD": "func Staged;func Unstaged;"} {
		if got := names(args); got != want {
			t.Errorf("%s: %q", args, got)
		}
	}
	for _, args := range [][]string{{"--staged", "--unstaged"}, {"--staged", "--base", "HEAD"}, {"--commit", "HEAD", "--head", "HEAD"}} {
		if err := runReview(context.Background(), append(args, root), &bytes.Buffer{}); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
