package core

import "testing"

func TestBashFunctionSyntaxesAndNestedBraces(t *testing.T) {
	src := `#!/bin/bash
set -euo pipefail

deploy() {
  if [ "$1" = "prod" ]; then
    for f in "${files[@]}"; do
      echo "shipping ${f}"
    done
  fi
  echo "done # not a comment brace }"
}

function rollback {
  echo "rolling back"
}

function cleanup() {
  echo "cleanup"
}
`
	units := bashUnits("deploy.sh", []byte(src))
	if len(units) != 3 {
		t.Fatalf("expected 3 functions, got %#v", units)
	}
	names := map[string]Unit{}
	for _, u := range units {
		names[u.Name] = u
	}
	deploy, ok := names["function deploy"]
	if !ok {
		t.Fatalf("missing function deploy: %#v", units)
	}
	if deploy.Start != 4 || deploy.End != 11 {
		t.Errorf("deploy range wrong (nested if/for/quoted-brace-comment should not close it early): %#v", deploy)
	}
	if _, ok := names["function rollback"]; !ok {
		t.Errorf("missing 'function name {' syntax: %#v", units)
	}
	if _, ok := names["function cleanup"]; !ok {
		t.Errorf("missing 'function name() {' syntax: %#v", units)
	}
}

func TestBashNoFunctionsFallsBackToWholeFile(t *testing.T) {
	root := t.TempDir()
	put(t, root, "flat.sh", "#!/bin/bash\necho hello\nls -la\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := idx.Entry("flat.sh")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Units) != 1 || entry.Units[0].Kind != "file" {
		t.Fatalf("expected whole-file fallback, got %#v", entry.Units)
	}
}

func TestBashExtensionlessShebangDetection(t *testing.T) {
	root := t.TempDir()
	put(t, root, "bin/deploy", "#!/usr/bin/env bash\n\ngreet() {\n  echo hi\n}\n")
	put(t, root, "bin/fishy", "#!/usr/bin/env fish\necho hi\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := idx.Entry("bin/deploy")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != "shell" || len(entry.Units) != 1 || entry.Units[0].Name != "function greet" {
		t.Fatalf("expected extension-less bash script to be detected via shebang: %#v", entry)
	}
	if _, err := idx.Entry("bin/fishy"); err == nil {
		t.Fatal("a non-bash/sh shebang (fish) must not be classified as shell")
	}
}

func TestBashQuotedBraceDoesNotCloseFunctionEarly(t *testing.T) {
	units := bashUnits("q.sh", []byte("f() {\n  echo \"unmatched { in a string\"\n  echo 'also }'\n}\n"))
	if len(units) != 1 || units[0].End != 4 {
		t.Fatalf("brace inside quotes should not affect depth tracking: %#v", units)
	}
}
