package core

import "testing"

func TestPythonTopLevelUnitsAndDecorators(t *testing.T) {
	root := t.TempDir()
	put(t, root, "app.py", `import os

@app.route("/")
@login_required
def index():
    """Docstring with an unindented example:

x = 1
    """
    return "hi"


class Handler:
    def method(self):
        pass


x = 1
`)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := idx.Entry("app.py")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Units) != 2 {
		t.Fatalf("expected 2 top-level units (func index, class Handler), got %#v", entry.Units)
	}
	fn := entry.Units[0]
	if fn.Name != "func index" || fn.Start != 3 || fn.End != 12 {
		t.Errorf("decorated function range wrong: %#v", fn)
	}
	cls := entry.Units[1]
	if cls.Name != "class Handler" {
		t.Errorf("expected class Handler, got %#v", cls)
	}
}

func TestPythonNoTopLevelDefsFallsBackToWholeFile(t *testing.T) {
	root := t.TempDir()
	put(t, root, "script.py", "import sys\nprint(sys.argv)\n")
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := idx.Entry("script.py")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Units) != 1 || entry.Units[0].Kind != "file" {
		t.Fatalf("expected whole-file fallback, got %#v", entry.Units)
	}
}

func TestPythonTabIndentation(t *testing.T) {
	root := t.TempDir()
	put(t, root, "tabbed.py", "def a():\n\tif True:\n\t\treturn 1\n\ndef b():\n\treturn 2\n")
	units := pythonUnits("tabbed.py", []byte("def a():\n\tif True:\n\t\treturn 1\n\ndef b():\n\treturn 2\n"))
	if len(units) != 2 {
		t.Fatalf("expected 2 functions with tab indentation, got %#v", units)
	}
	if units[0].Name != "func a" || units[0].Start != 1 || units[0].End != 4 {
		t.Errorf("func a range wrong: %#v", units[0])
	}
	if units[1].Name != "func b" || units[1].Start != 5 {
		t.Errorf("func b range wrong: %#v", units[1])
	}
}
