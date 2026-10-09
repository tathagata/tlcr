package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// Unit identifies one structural source range.
type Unit struct {
	ID    string   `json:"id"`
	Kind  string   `json:"kind"`
	Name  string   `json:"name"`
	Links []string `json:"links,omitempty"`
	Start int      `json:"start"`
	End   int      `json:"end"`
}

// FileEntry is a recognized file and its parser-derived units.
type FileEntry struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Units []Unit `json:"units"`
}

// Index is an immutable, bounded snapshot of permitted repository source.
type Index struct {
	modulePath  *string
	policy      map[string]string
	Revision    string `json:"revision"`
	registry    *Registry
	Root        string      `json:"-"`
	Files       []FileEntry `json:"files"`
	sources     map[string]string
	Diagnostics []Diagnostic `json:"diagnostics"`
}

const maxFileBytes = 256 * 1024
const maxRepositoryBytes = 32 * 1024 * 1024
const maxRepositoryFiles = 4096

// Diagnostic reports incomplete analysis without guessing.
type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func excluded(path string, isDir bool) bool {
	name := filepath.Base(path)
	if isDir {
		switch name {
		case ".git", ".terraform", "node_modules", "vendor", ".venv", "dist", "build", ".tlcr", ".coderead", ".dockerbuild", ".ssh", ".aws":
			return true
		}
		return false
	}
	for _, suffix := range []string{".tfvars", ".tfvars.json", ".tfstate", ".tfplan", ".lock.hcl", ".min.js", ".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	if strings.Contains(name, ".tfstate.") {
		return true
	}

	if name == "config.json" || name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	return false
}

// Scan analyzes a repository using the default local parser registry.
func Scan(root string) (*Index, error) { return ScanWithRegistry(root, DefaultRegistry()) }

// ScanWithRegistry collects a bounded local snapshot through the supplied parsers.
func ScanWithRegistry(root string, registry *Registry) (*Index, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{Root: root, Files: []FileEntry{}, sources: map[string]string{}, Diagnostics: []Diagnostic{}}
	idx.registry = registry
	state := &scanState{idx: idx, ignoreCache: map[string]*ignoreLayer{}}
	err = filepath.WalkDir(root, state.walk)

	if err != nil {
		return nil, err
	}
	sort.Slice(idx.Files, func(i, j int) bool { return idx.Files[i].Path < idx.Files[j].Path })
	idx.policy = map[string]string{}
	for dir := range state.ignoreCache {
		p := filepath.ToSlash(filepath.Join(dir, ".gitignore"))
		idx.policy[p] = policyDigest(root, p)
	}
	idx.policy[".gitignore"] = policyDigest(root, ".gitignore")
	idx.policy["go.mod"] = policyDigest(root, "go.mod")
	parts := []string{}
	for _, file := range idx.Files {
		parts = append(parts, file.Path, idx.sources[file.Path])
	}
	module := localModule(idx)
	idx.modulePath = &module
	parts = append(parts, module)
	idx.Revision = Hash(parts...)

	return idx, nil
}

type scanState struct {
	idx         *Index
	ignoreCache map[string]*ignoreLayer
	totalBytes  int
	visited     int
}

func (s *scanState) walk(path string, d fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("cannot traverse repository: %w", walkErr)
	}
	if path == s.idx.Root {
		return nil
	}
	s.visited++
	if s.visited > 50000 {
		return errors.New("repository exceeds traversal limit (50000 entries); choose a smaller root")
	}
	rel, err := filepath.Rel(s.idx.Root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if excluded(path, d.IsDir()) || gitignored(s.idx.Root, rel, d.IsDir(), s.ignoreCache) {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if strings.Count(rel, "/") > 64 {
		return errors.New("repository exceeds directory depth limit")
	}
	if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil
	}
	return s.readFile(rel)
}

func (s *scanState) readFile(rel string) error {
	data, err := readWithin(s.idx.Root, rel, maxFileBytes)
	if err != nil {
		return err
	}
	if vaultEncrypted(rel, string(data)) {
		return nil
	}
	entry, ok := s.idx.registry.Parse(rel, data)
	if !ok {
		return nil
	}
	if len(s.idx.Files) >= maxRepositoryFiles || s.totalBytes+len(data) > maxRepositoryBytes {
		return errors.New("repository exceeds analysis limits (4096 files / 32 MiB); choose a smaller root")
	}
	s.totalBytes += len(data)
	s.idx.sources[rel] = string(data)
	s.idx.Files = append(s.idx.Files, entry)
	return nil
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
	return uniqueUnits(units)
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
				name = "method " + receiverName(node.Recv.List[0].Type) + "." + name
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
	return uniqueUnits(units)
}

// Entry looks up an indexed file without reading arbitrary paths.
func (idx *Index) Entry(path string) (*FileEntry, error) {
	for i := range idx.Files {
		if idx.Files[i].Path == path {
			return &idx.Files[i], nil
		}
	}
	return nil, errors.New("file not indexed")
}

// Read reads only current, permitted, confined source within an indexed file.
func (idx *Index) Read(path string) (string, *FileEntry, error) {
	if err := idx.CheckPolicy(); err != nil {
		return "", nil, err
	}
	entry, err := idx.Entry(path)
	if err != nil {
		return "", nil, err
	}
	if !permitted(idx.Root, path) {
		return "", nil, errors.New("file excluded; refresh the repository")
	}
	data, err := readWithin(idx.Root, path, maxFileBytes)
	if err != nil {
		return "", nil, err
	}
	if vaultEncrypted(path, string(data)) {
		return "", nil, errors.New("encrypted source excluded")
	}

	return string(data), entry, nil
}

// Hash includes boundaries between parts to prevent ambiguous concatenations.
func Hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	default:
		return "?"
	}
}

func uniqueUnits(units []Unit) []Unit {
	counts := map[string]int{}
	for i := range units {
		id := units[i].ID
		counts[id]++
		if counts[id] > 1 {
			units[i].ID = fmt.Sprintf("%s#%d", id, counts[id])
		}
	}
	return units
}

// readWithin confines every open to the repository, rejects symlinks/special files and caps reads.
func readWithin(root, path string, limit int64) ([]byte, error) {
	if !fs.ValidPath(path) || path == "." {
		return nil, errors.New("invalid repository path")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	parts := strings.Split(path, "/")
	for i := range parts {
		info, statErr := dir.Lstat(strings.Join(parts[:i+1], "/"))
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symbolic links are not indexed")
		}
		if i == len(parts)-1 && (!info.Mode().IsRegular() || info.Size() > limit) {
			return nil, errors.New("file is not regular or exceeds limit")
		}
	}
	file, err := dir.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("unsupported file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file exceeds limit")
	}
	return data, nil
}

func permitted(root, path string) bool {
	if !fs.ValidPath(path) || path == "." {
		return false
	}
	cache := map[string]*ignoreLayer{}
	parts := strings.Split(path, "/")
	for i := range parts {
		candidate := strings.Join(parts[:i+1], "/")
		isDir := i < len(parts)-1
		if excluded(candidate, isDir) || gitignored(root, candidate, isDir, cache) {
			return false
		}
	}
	return true
}

func policyDigest(root, path string) string {
	data, err := readWithin(root, path, 64*1024)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		return "unreadable"
	}
	return Hash(string(data))
}

// CheckPolicy prevents a stale snapshot from exposing newly ignored paths.
func (idx *Index) CheckPolicy() error {
	for path, digest := range idx.policy {
		if policyDigest(idx.Root, path) != digest {
			return failure(Stale, errors.New("repository exclusions changed; refresh before continuing"))
		}
	}
	return nil
}
