package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"
)

type goGroup struct {
	pkg      *types.Package
	key      string
	dir      string
	name     string
	files    []*ast.File
	paths    []string
	checking bool
}
type localTypes struct {
	idx     *Index
	g       *Graph
	set     *token.FileSet
	info    *types.Info
	groups  map[string]*goGroup
	imports map[string]*goGroup
	objects map[types.Object]string
}

// Import resolves only already-indexed local packages. External packages are opaque stubs;
// no build command, compiler subprocess, module download or registry lookup occurs.
func (l *localTypes) Import(importPath string) (*types.Package, error) {
	if group := l.imports[importPath]; group != nil {
		return l.check(group), nil
	}
	pkg := types.NewPackage(importPath, path.Base(importPath))
	pkg.MarkComplete()
	return pkg, nil
}

func (l *localTypes) check(group *goGroup) *types.Package {
	if group.pkg != nil {
		return group.pkg
	}
	if group.checking {
		p := types.NewPackage(group.key, group.name)
		p.MarkComplete()
		return p
	}
	group.checking = true
	cfg := types.Config{Importer: l, Error: func(error) {}}
	pkg, _ := cfg.Check(group.key, l.set, group.files, l.info)
	if pkg == nil {
		pkg = types.NewPackage(group.key, group.name)
		pkg.MarkComplete()
	}
	group.pkg = pkg
	group.checking = false
	return pkg
}

func addGoRelationships(g *Graph, idx *Index) {
	l := &localTypes{idx: idx, g: g, set: token.NewFileSet(), info: &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}, groups: map[string]*goGroup{}, imports: map[string]*goGroup{}, objects: map[types.Object]string{}}
	module := localModule(idx)
	ordered := l.collectFiles(module)
	for _, group := range ordered {
		l.check(group)
	}
	for ident, obj := range l.info.Defs {
		if obj == nil {
			continue
		}
		switch obj.(type) {
		case *types.Func, *types.TypeName:
		default:
			continue
		}
		pos := l.set.Position(ident.Pos())
		if id := l.owner(pos.Filename, pos.Line); id != "" {
			l.objects[obj] = id
		}
	}
	for _, group := range ordered {
		for i, file := range group.files {
			l.analyzeFile(group, group.paths[i], file)
		}
	}
}

func localModule(idx *Index) string {
	if idx.modulePath != nil {
		return *idx.modulePath
	}
	module := ""
	if permitted(idx.Root, "go.mod") {
		if data, err := readWithin(idx.Root, "go.mod", 64*1024); err == nil {
			module = moduleDeclaration(string(data))
		}
	}

	return module
}

func moduleDeclaration(source string) string {
	for _, line := range strings.Split(source, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

func (l *localTypes) collectFiles(module string) []*goGroup {
	ordered := []*goGroup{}
	for _, entry := range l.idx.Files {
		if entry.Kind != "go" {
			continue
		}
		file, err := parser.ParseFile(l.set, entry.Path, l.idx.sources[entry.Path], 0)
		if err != nil {
			l.g.Diagnostics = append(l.g.Diagnostics, Diagnostic{Path: entry.Path, Message: "Go syntax incomplete; relationships omitted"})
			continue
		}
		dir := path.Dir(entry.Path)
		key := dir + "/" + file.Name.Name
		group := l.groups[key]
		if group == nil {
			group = &goGroup{key: key, dir: dir, name: file.Name.Name}
			l.groups[key] = group
			ordered = append(ordered, group)
		}
		group.files = append(group.files, file)
		group.paths = append(group.paths, entry.Path)
		if module != "" && !strings.HasSuffix(file.Name.Name, "_test") {
			importPath := module
			if dir != "." {
				importPath += "/" + dir
			}
			l.imports[importPath] = group
		}
		l.g.addNode(Node{ID: "package:" + key, Kind: "package", Name: file.Name.Name, Path: dir, Language: "go", Provenance: Provenance{Provider: "Go AST", Path: entry.Path, Start: 1, End: 1, Detail: "Package declaration", Commit: ""}})
		l.g.addEdge(Edge{From: "package:" + key, To: "file:" + entry.Path, Kind: "contains", Provenance: Provenance{Provider: "Go AST", Path: entry.Path, Start: 1, End: 1, Detail: "Package membership", Commit: ""}})
	}
	return ordered
}

func (l *localTypes) owner(file string, line int) string {
	entry, err := l.idx.Entry(file)
	if err != nil {
		return ""
	}
	for _, u := range entry.Units {
		if line >= u.Start && line <= u.End {
			return "unit:" + u.ID
		}
	}
	return "file:" + file
}

func (l *localTypes) callee(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		if _, ok := l.info.Uses[x].(*types.Func); ok {
			return l.objects[l.info.Uses[x]]
		}
		return ""
	case *ast.SelectorExpr:
		if selection := l.info.Selections[x]; selection != nil {
			// Dynamic interface dispatch is not a proven concrete call target.
			if _, ok := selection.Recv().Underlying().(*types.Interface); ok {
				return ""
			}
			return l.objects[selection.Obj()]
		}
		if _, ok := l.info.Uses[x.Sel].(*types.Func); ok {
			return l.objects[l.info.Uses[x.Sel]]
		}
		return ""
	case *ast.IndexExpr:
		return l.callee(x.X)
	case *ast.IndexListExpr:
		return l.callee(x.X)
	}
	return ""
}

func (l *localTypes) analyzeFile(group *goGroup, filePath string, file *ast.File) {
	l.analyzeImports(filePath, file)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		l.analyzeFunction(group, filePath, fn)
	}
}

func (l *localTypes) analyzeFunction(group *goGroup, filePath string, fn *ast.FuncDecl) {
	owner := l.owner(filePath, l.set.Position(fn.Pos()).Line)
	isTest := strings.HasSuffix(filePath, "_test.go") && (strings.HasPrefix(fn.Name.Name, "Test") || strings.HasPrefix(fn.Name.Name, "Fuzz") || strings.HasPrefix(fn.Name.Name, "Benchmark") || strings.HasPrefix(fn.Name.Name, "Example"))
	if isTest {
		l.g.role(owner, "test")
	}
	if group.name == "main" && fn.Name.Name == "main" && fn.Recv == nil {
		l.g.role(owner, "entry point")
	}
	if fn.Body == nil {
		return
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		p := l.provenance(filePath, call.Pos(), call.End(), "Resolved local Go reference")
		target := l.callee(call.Fun)
		if target != "" {
			l.g.addEdge(Edge{From: owner, To: target, Kind: "calls", Provenance: p})
			if isTest && owner != target {
				l.g.addEdge(Edge{From: target, To: owner, Kind: "tested-by", Provenance: p})
			}
		}
		l.boundary(owner, filePath, call)
		return true
	})
}

func (l *localTypes) analyzeImports(filePath string, file *ast.File) {
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		target := l.imports[importPath]
		if target == nil {
			continue
		}
		p := l.provenance(filePath, spec.Pos(), spec.End(), "Local import "+importPath)
		for _, targetFile := range target.paths {
			if strings.HasSuffix(targetFile, "_test.go") {
				continue
			}
			l.g.addEdge(Edge{From: "file:" + filePath, To: "file:" + targetFile, Kind: "imports", Provenance: p})
		}
	}
}

func (l *localTypes) provenance(file string, start, end token.Pos, detail string) Provenance {
	return Provenance{Provider: "Go AST/types", Path: file, Start: l.set.Position(start).Line, End: l.set.Position(end).Line, Detail: detail, Commit: ""}
}

func (l *localTypes) boundary(owner, file string, call *ast.CallExpr) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	base, ok := selector.X.(*ast.Ident)
	if !ok {
		return
	}
	imported, ok := l.info.Uses[base].(*types.PkgName)
	if !ok {
		return
	}
	pkg := imported.Imported().Path()
	name := selector.Sel.Name
	roles := map[string]string{"net/http": "network boundary", "net": "network boundary", "net/rpc": "network boundary", "database/sql": "storage boundary"}
	role := roles[pkg]
	if pkg == "os" {
		role = map[string]string{"Getenv": "configuration", "LookupEnv": "configuration", "Open": "storage boundary", "OpenFile": "storage boundary", "ReadFile": "storage boundary", "WriteFile": "storage boundary", "Create": "storage boundary"}[name]
	}
	if role != "" {
		l.g.role(owner, role)
	}

	if pkg == "net/http" && (name == "HandleFunc" || name == "Handle") && len(call.Args) == 2 {
		if dest := l.callee(call.Args[1]); dest != "" {
			l.g.addEdge(Edge{From: owner, To: dest, Kind: "route-to-handler", Provenance: l.provenance(file, call.Pos(), call.End(), "HTTP handler registration")})
		}
	}
}
