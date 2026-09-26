package core

import "testing"

func TestGitignoreBasics(t *testing.T) {
	root := t.TempDir()
	// "target" (not "build"/"dist"/"vendor") deliberately avoids
	// index.go's own pre-existing hardcoded directory exclusions, so this
	// isolates gitignore-driven behavior specifically.
	put(t, root, ".gitignore", "*.log\n/target\nartifacts/\n")
	put(t, root, "app.go", "package main\n")
	put(t, root, "app.log", "noise")
	put(t, root, "target/output.go", "package main\n")
	put(t, root, "nested/target/output.go", "package main\n")
	put(t, root, "artifacts/app.go", "package main\n")
	put(t, root, "notartifacts.go", "package main\n")

	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range idx.Files {
		got[f.Path] = true
	}
	if got["app.log"] {
		t.Error("*.log should be ignored")
	}
	if got["target/output.go"] {
		t.Error("/target should be ignored (anchored to root)")
	}
	if !got["nested/target/output.go"] {
		t.Error("nested/target should NOT be ignored (/target is anchored to root only)")
	}
	if got["artifacts/app.go"] {
		t.Error("artifacts/ should be ignored")
	}
	if !got["app.go"] || !got["notartifacts.go"] {
		t.Error("unrelated files should still be indexed")
	}
}

func TestGitignoreNegationAndNesting(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "vendor/\n")
	put(t, root, "vendor/pkg/lib.go", "package pkg\n")
	put(t, root, "vendor/keep/.gitignore", "!*.go\n")
	put(t, root, "vendor/keep/lib.go", "package keep\n")

	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range idx.Files {
		got[f.Path] = true
	}
	if got["vendor/pkg/lib.go"] {
		t.Error("vendor/ should be ignored")
	}
	// vendor/ is a directory pattern, so WalkDir will SkipDir on "vendor"
	// entirely and never see vendor/keep's negation — this matches git's
	// own documented limitation: you cannot re-include a file inside an
	// excluded directory. Assert that documented behavior explicitly.
	if got["vendor/keep/lib.go"] {
		t.Error("negation inside an already-excluded directory cannot re-include it (matches git's own limitation)")
	}
}

func TestGitignoreNegationWithoutDirExclusion(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "*.go\n!keep.go\n")
	put(t, root, "a.go", "package main\n")
	put(t, root, "keep.go", "package main\n")

	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range idx.Files {
		got[f.Path] = true
	}
	if got["a.go"] {
		t.Error("a.go should be ignored by *.go")
	}
	if !got["keep.go"] {
		t.Error("keep.go should be re-included by !keep.go (last match wins)")
	}
}

func TestGitignoreDirOnlyDoesNotMatchFiles(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "build/\n")
	cache := map[string]*ignoreLayer{}
	if !gitignored(root, "build", true, cache) {
		t.Error("a directory named build should be ignored by build/")
	}
	if gitignored(root, "build", false, cache) {
		t.Error("a *file* named build should NOT be ignored by the dir-only pattern build/")
	}
}

func TestGitignoreDoubleStarMiddle(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "a/**/target.go\n")
	put(t, root, "a/target.go", "package a\n")
	put(t, root, "a/x/target.go", "package a\n")
	put(t, root, "a/x/y/target.go", "package a\n")
	put(t, root, "a/xtarget.go", "package a\n")

	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range idx.Files {
		got[f.Path] = true
	}
	for _, p := range []string{"a/target.go", "a/x/target.go", "a/x/y/target.go"} {
		if got[p] {
			t.Errorf("%s should be ignored by a/**/target.go", p)
		}
	}
	if !got["a/xtarget.go"] {
		t.Error("a/xtarget.go should NOT be ignored (no path-segment boundary before 'target.go')")
	}
}
