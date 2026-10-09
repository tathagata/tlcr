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

func TestBashHeredocBodyDoesNotAffectBraceDepth(t *testing.T) {
	src := `usage() {
  cat <<-EOF
	Usage: tool {start|stop
	fake() {
	EOF
  cat <<'JSON'
{ "open": [
JSON
  echo $((1 << 4)) <<< "}"
}

after() {
  echo after
}
`
	units := bashUnits("h.sh", []byte(src))
	if len(units) != 2 {
		t.Fatalf("expected usage and after only, got %#v", units)
	}
	if units[0].Name != "function usage" || units[0].Start != 1 || units[0].End != 10 {
		t.Errorf("heredoc braces should not move the function end: %#v", units[0])
	}
	if units[1].Name != "function after" || units[1].Start != 12 || units[1].End != 14 {
		t.Errorf("function after a heredoc has the wrong range: %#v", units[1])
	}
}

func TestBashOneLineFunctionsAndParameterExpansion(t *testing.T) {
	src := `die() { echo "$*" >&2; exit 1; }
function ns::log-info { echo "info"; }
count() {
  local n=${#items[@]} rest=${1#--}
  echo "$n" # trailing } comment
}
not_a_function {
  echo no
}
`
	units := bashUnits("o.sh", []byte(src))
	want := []Unit{
		{Name: "function die", Start: 1, End: 1},
		{Name: "function ns::log-info", Start: 2, End: 2},
		{Name: "function count", Start: 3, End: 6},
	}
	if len(units) != len(want) {
		t.Fatalf("expected %d functions, got %#v", len(want), units)
	}
	for i, w := range want {
		if units[i].Name != w.Name || units[i].Start != w.Start || units[i].End != w.End {
			t.Errorf("got %#v, want %#v", units[i], w)
		}
	}
}
