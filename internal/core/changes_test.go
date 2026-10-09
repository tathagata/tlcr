package core

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func listChanges(t *testing.T, root string) (*Repository, ChangeList) {
	t.Helper()
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(idx)
	list, err := repo.Changes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return repo, list
}

func TestChangesListsWorkCommitsAndBranches(t *testing.T) {
	root := reviewFixture(t)
	trunk := strings.TrimSpace(testGit(t, root, "rev-parse", "--abbrev-ref", "HEAD"))
	if trunk != "main" {
		testGit(t, root, "branch", "-m", trunk, "main")
	}
	testGit(t, root, "checkout", "-b", "feature")
	put(t, root, "feature.go", "package main\nfunc Feature(){}\n")
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "Add feature\x07 with a bell", "-m", "Body is not listed")
	testGit(t, root, "branch", "merged", "main")
	put(t, root, "staged.go", "package main\nfunc Staged(){}\n")
	testGit(t, root, "add", "staged.go")
	put(t, root, "main.go", "package main\nfunc main(){ Before() }\nfunc Before(){ println(1) }\n")
	put(t, root, "untracked.go", "package main\nfunc Untracked(){}\n")
	put(t, root, "notes.bin", "not indexed\n")
	put(t, root, ".gitignore", "ignored.go\n")
	put(t, root, "ignored.go", "package main\n")
	status := testGit(t, root, "status", "--porcelain=v1")
	repo, list := listChanges(t, root)
	if list.Branch != "feature" || list.DefaultBranch != "main" || len(list.Notes) != 0 {
		t.Fatalf("branches: %#v", list)
	}
	assertListedChanges(t, repo, list)
	_, again := listChanges(t, root)
	a, _ := json.Marshal(list)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("nondeterministic listing")
	}
	if after := testGit(t, root, "status", "--porcelain=v1"); status != after {
		t.Fatal("listing modified Git state")
	}
}

func assertListedChanges(t *testing.T, repo *Repository, list ChangeList) {
	t.Helper()
	files := map[string]int{}
	for _, change := range list.Working {
		files[change.Kind] = change.Files
	}
	// .gitignore is itself untracked but is not indexed source.
	if files["staged"] != 1 || files["unstaged"] != 2 || files["uncommitted"] != 3 {
		t.Fatalf("working: %v %#v", files, list.Working)
	}
	if len(list.Commits) != 2 || list.Commits[0].Title != "Add feature with a bell" || list.Commits[0].Files != 1 || strings.Contains(list.Commits[0].Detail, "@") {
		t.Fatalf("commits: %#v", list.Commits)
	}
	if len(list.Branches) != 1 || list.Branches[0].Title != "feature" || !strings.HasPrefix(list.Branches[0].Detail, "Current branch · 1 commit ahead of main") {
		t.Fatalf("branches: %#v", list.Branches)
	}
	assertListedSelectionsReview(t, repo, list)
}

func assertListedSelectionsReview(t *testing.T, repo *Repository, list ChangeList) {
	t.Helper()
	for _, change := range append(append(append([]ReviewableChange{}, list.Working...), list.Commits...), list.Branches...) {
		review, err := repo.ReviewChange(context.Background(), change.Selection)
		if err != nil {
			t.Fatalf("%s is not reviewable: %v", change.Title, err)
		}
		if change.Kind == "branch" && (len(review.Changes) != 2 || review.Changes[0].Node.Path != "feature.go") {
			t.Fatalf("branch review: %#v", review.Changes)
		}
	}
}

func TestChangesWithoutCommitsDetachedOrGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	plain := t.TempDir()
	put(t, plain, "main.go", "package main\n")
	if _, list := listChanges(t, plain); len(list.Notes) != 1 || len(list.Working)+len(list.Commits)+len(list.Branches) != 0 {
		t.Fatalf("no repository: %#v", list)
	}
	unborn := t.TempDir()
	testGit(t, unborn, "init")
	put(t, unborn, "main.go", "package main\nfunc main(){}\n")
	testGit(t, unborn, "add", ".")
	repo, list := listChanges(t, unborn)
	if len(list.Notes) != 1 || len(list.Commits) != 0 || len(list.Working) != 3 || list.Working[1].Files != 1 {
		t.Fatalf("unborn: %#v", list)
	}
	review, err := repo.ReviewChange(context.Background(), list.Working[1].Selection)
	if err != nil || len(review.Changes) != 2 {
		t.Fatalf("staged review before the first commit: %#v %v", review.Changes, err)
	}
	detached := reviewFixture(t)
	testGit(t, detached, "branch", "-m", "trunk")
	testGit(t, detached, "checkout", "--detach")
	if _, list := listChanges(t, detached); list.Branch != "" || len(list.Commits) != 1 || len(list.Notes) != 2 || list.DefaultBranch != "" {
		t.Fatalf("detached without a default branch: %#v", list)
	}
}
