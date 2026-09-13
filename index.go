package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Unit struct {
	ID    string   `json:"id"`
	Kind  string   `json:"kind"`
	Name  string   `json:"name"`
	Links []string `json:"links,omitempty"`
	Start int      `json:"start"`
	End   int      `json:"end"`
}

type FileEntry struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Units []Unit `json:"units"`
}

type Index struct {
	Root  string      `json:"-"`
	Files []FileEntry `json:"files"`
}

const maxFileBytes = 256 * 1024

func excluded(path string, isDir bool) bool {
	name := filepath.Base(path)
	if isDir {
		switch name {
		case ".git", ".terraform", "node_modules", "vendor", ".venv", "dist", "build", ".coderead":
			return true
		}
		return false
	}
	if strings.HasSuffix(name, ".tfvars") || strings.HasSuffix(name, ".tfvars.json") || strings.HasSuffix(name, ".tfstate") || strings.Contains(name, ".tfstate.") || strings.HasSuffix(name, ".tfplan") || strings.HasSuffix(name, ".lock.hcl") || strings.HasSuffix(name, ".min.js") {
		return true
	}
	if name == "config.json" || name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	return false
}

func fileKind(path string) string {
	switch filepath.Ext(path) {
	case ".tf":
		return "terraform"
	case ".go":
		return "go"
	case ".js", ".ts", ".html", ".css":
		return "frontend"
	default:
		return ""
	}
}

//nolint:gocyclo // walks and classifies files in one pass; splitting up hurts readability more than it helps
func Scan(root string) (*Index, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{Root: root, Files: []FileEntry{}}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if excluded(path, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
			return nil
		}
		kind := fileKind(path)
		if kind == "" {
			return nil
		}
		dInfo, err := d.Info()
		if err != nil {
			return err
		}
		if dInfo.Size() > maxFileBytes {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304 G122 -- path comes from filepath.WalkDir over the scanned root, not external input; a symlink swap TOCTOU here only affects what this local reader shows the user their own filesystem
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		entry := FileEntry{Path: rel, Kind: kind, Units: []Unit{}}
		switch kind {
		case "terraform":
			entry.Units = terraformUnits(rel, data)
		case "go":
			entry.Units = goUnits(rel, data)
		}
		if len(entry.Units) == 0 {
			entry.Units = []Unit{{ID: rel + ":file", Kind: "file", Name: filepath.Base(rel), Start: 1, End: countLines(data)}}
		}
		idx.Files = append(idx.Files, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(idx.Files, func(i, j int) bool { return idx.Files[i].Path < idx.Files[j].Path })
	return idx, nil
}

func countLines(data []byte) int { return strings.Count(string(data), "\n") + 1 }

func terraformUnits(path string, data []byte) []Unit {
	p := hclparse.NewParser()
	f, _ := p.ParseHCL(data, path)
	if f == nil {
		return nil
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil
	}
	units := []Unit{}
	for _, b := range body.Blocks {
		name := b.Type
		if len(b.Labels) > 0 {
			name += " " + strings.Join(b.Labels, ".")
		}
		u := Unit{ID: path + ":" + name, Kind: b.Type, Name: name, Start: b.Range().Start.Line, End: b.Range().End.Line}
		if b.Type == "module" {
			if attr := b.Body.Attributes["source"]; attr != nil {
				if value, diags := attr.Expr.Value(nil); !diags.HasErrors() && value.IsKnown() && !value.IsNull() && value.Type().FriendlyName() == "string" {
					source := value.AsString()
					if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
						u.Links = []string{filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(path), source)))}
					}
				}
			}
		}
		units = append(units, u)
	}
	return units
}

func goUnits(path string, data []byte) []Unit {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, 0)
	if err != nil {
		return nil
	}
	units := []Unit{}
	for _, d := range f.Decls {
		switch node := d.(type) {
		case *ast.FuncDecl:
			name := node.Name.Name
			if node.Recv != nil {
				name = "method " + name
			} else {
				name = "func " + name
			}
			units = append(units, Unit{ID: path + ":" + name, Kind: "function", Name: name, Start: fset.Position(node.Pos()).Line, End: fset.Position(node.End()).Line})
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				if t, ok := spec.(*ast.TypeSpec); ok {
					units = append(units, Unit{ID: path + ":type " + t.Name.Name, Kind: "type", Name: "type " + t.Name.Name, Start: fset.Position(t.Pos()).Line, End: fset.Position(t.End()).Line})
				}
			}
		}
	}
	return units
}

func (idx *Index) Entry(path string) (*FileEntry, error) {
	for i := range idx.Files {
		if idx.Files[i].Path == path {
			return &idx.Files[i], nil
		}
	}
	return nil, errors.New("file not indexed")
}

func (idx *Index) Read(path string) (string, *FileEntry, error) {
	entry, err := idx.Entry(path)
	if err != nil {
		return "", nil, err
	}
	full := filepath.Join(idx.Root, filepath.FromSlash(entry.Path))
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(idx.Root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, errors.New("file escapes repository")
	}
	info, err := os.Lstat(full)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return "", nil, errors.New("file changed or unsupported; rescan")
	}
	data, err := os.ReadFile(full) // #nosec G304 -- full is resolved and checked above to stay inside idx.Root
	if err != nil {
		return "", nil, err
	}
	return string(data), entry, nil
}

func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
