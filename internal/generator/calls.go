package generator

// calls.go — page calls: the Go types a route declares for its calls and live
// values, read from the route package's source, checked for E20 and mapped to
// TypeScript as encoding/json writes them.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	routepkg "github.com/sergei-svistunov/go-ssr/internal/generator/route"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

const (
	muxPath      = "github.com/sergei-svistunov/go-ssr/pkg/mux"
	renderPath   = "github.com/sergei-svistunov/go-ssr/pkg/render"
	reactivePath = "github.com/sergei-svistunov/go-ssr/pkg/reactive"

	fixE20 = "use a struct with json tags, or struct{}"

	// callNotFound is the message of the 404 a route's Call answers for a name
	// its template does not declare.
	callNotFound = "This call is not on this page."
)

// generatedImports are the packages a route's generated file may import, by the
// name it uses. A type written in a template resolves against them first.
var generatedImports = map[string]string{
	"context": "context", "json": "encoding/json", "io": "io", "http": "net/http", "sync": "sync",
	"strings": "strings", "strconv": "strconv", "math": "math",
	"form": "github.com/sergei-svistunov/go-ssr/pkg/form", "mux": muxPath, "reactive": reactivePath, "render": renderPath,
}

func routeCalls(r *routepkg.Route) []*node.SsrCall {
	if r.Template() == nil {
		return nil
	}
	return r.Template().GetCalls()
}

// hasLiveConnection reports whether a page that renders r opens a live
// connection for it: r has live values or calls.
func hasLiveConnection(r *routepkg.Route) bool {
	return isReactiveRoute(r) || len(routeCalls(r)) > 0
}

func callMethod(name string) string { return "Call" + getExportedName(name) }

// goPackage is what the generator reads of a route package's source: its type
// declarations, the methods that make a type marshal itself, and its imports.
type goPackage struct {
	fset    *token.FileSet
	types   map[string]*goTypeDecl
	methods map[string]map[string]recvKind
	// imports maps each name a file of the package imports a package as to its
	// path.
	imports map[string]string
}

type recvKind uint8

const (
	valueRecv recvKind = 1 << iota
	pointerRecv
)

type goTypeDecl struct {
	spec  *ast.TypeSpec
	scope *typeScope
}

// typeScope is where a type expression is written: a Go file, or a template
// attribute on line.
type typeScope struct {
	fset *token.FileSet
	file string
	line int
	// imports maps the names a Go file imports packages as to their paths.
	imports map[string]string
	// resolve, set for a template, returns the path of a qualifier.
	resolve func(name string) string
}

// path returns the import path of the package that the qualifier name stands
// for, or "".
func (s *typeScope) path(name string) string {
	if s.resolve != nil {
		return s.resolve(name)
	}
	return s.imports[name]
}

// at returns the file and line of n.
func (s *typeScope) at(n ast.Node) (string, int) {
	if s.line > 0 || n == nil {
		return s.file, s.line
	}
	p := s.fset.Position(n.Pos())
	return p.Filename, p.Line
}

var reMajorVersion = regexp.MustCompile(`^v[0-9]+$`)

// loadGoPackage reads the Go files of the route package in dir, leaving out
// tests and generated files. A file that does not parse is left out too; the
// compiler reports it.
func loadGoPackage(dir string) *goPackage {
	pkg := &goPackage{
		fset:    token.NewFileSet(),
		types:   map[string]*goTypeDecl{},
		methods: map[string]map[string]recvKind{},
		imports: map[string]string{},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return pkg
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			continue
		}
		file := filepath.Join(dir, name)
		f, err := parser.ParseFile(pkg.fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		sc := &typeScope{fset: pkg.fset, file: file, imports: map[string]string{}}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			local := path.Base(p)
			if reMajorVersion.MatchString(local) && path.Dir(p) != "." {
				local = path.Base(path.Dir(p))
			}
			if imp.Name != nil {
				local = imp.Name.Name
			}
			if local == "_" || local == "." {
				continue
			}
			sc.imports[local] = p
			pkg.imports[local] = p
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, s := range d.Specs {
					ts := s.(*ast.TypeSpec)
					pkg.types[ts.Name.Name] = &goTypeDecl{spec: ts, scope: sc}
				}
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) != 1 {
					continue
				}
				recv, kind := d.Recv.List[0].Type, valueRecv
				if star, ok := recv.(*ast.StarExpr); ok {
					recv, kind = star.X, pointerRecv
				}
				switch g := recv.(type) {
				case *ast.IndexExpr:
					recv = g.X
				case *ast.IndexListExpr:
					recv = g.X
				}
				if id, ok := recv.(*ast.Ident); ok {
					if pkg.methods[id.Name] == nil {
						pkg.methods[id.Name] = map[string]recvKind{}
					}
					pkg.methods[id.Name][d.Name.Name] |= kind
				}
			}
		}
	}
	return pkg
}

// templateScope is the scope of a type written in a template on line, whose
// qualifiers importPath resolves.
func (p *goPackage) templateScope(file string, line int) *typeScope {
	return &typeScope{fset: p.fset, file: file, line: line, resolve: p.importPath}
}

// callImports returns the imports, by name, that the in and out types of calls
// need in a generated file that imports the packages named in have.
func (p *goPackage) callImports(calls []*node.SsrCall, have map[string]bool) map[string]string {
	extra := map[string]string{}
	for _, c := range calls {
		for _, t := range []string{c.In, c.Out} {
			e, err := parser.ParseExpr(t)
			if err != nil {
				continue
			}
			ast.Inspect(e, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && !have[x.Name] {
					if path := p.importPath(x.Name); path != "" {
						extra[x.Name] = path
					}
				}
				return false
			})
		}
	}
	return extra
}

// importPath returns the path of the package a type written in a template
// names as name: the one a generated file imports as name, else the one a file
// of the route package imports as name, else the standard library package of
// that path, or "". The generated route file and data provider stub import it
// so. Analyze refuses a call type with a qualifier it cannot resolve.
func (p *goPackage) importPath(name string) string {
	path, _ := p.resolveImport(name)
	return path
}

// resolveImport is importPath, with the error that kept it from looking in
// the standard library.
func (p *goPackage) resolveImport(name string) (string, error) {
	if path := generatedImports[name]; path != "" {
		return path, nil
	}
	if path := p.imports[name]; path != "" {
		return path, nil
	}
	ctxt := build.Default
	if ctxt.GOROOT == "" {
		root, err := goEnvGOROOT()
		if err != nil {
			return "", err
		}
		ctxt.GOROOT = root
	}
	if pkg, err := ctxt.Import(name, "", build.FindOnly); err == nil && pkg.Goroot {
		return name, nil
	}
	return "", nil
}

// goEnvCommand is the command that prints the go command's GOROOT.
var goEnvCommand = func(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "go", "env", "GOROOT")
}

// goroot caches the result of goEnvCommand.
var goroot struct {
	once sync.Once
	dir  string
	err  error
}

// goEnvGOROOT returns the GOROOT of the go command, for a generator built
// without one of its own, as with -trimpath.
func goEnvGOROOT() (string, error) {
	goroot.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := goEnvCommand(ctx).Output()
		goroot.dir = strings.TrimSpace(string(out))
		if err == nil && goroot.dir == "" {
			err = errors.New("it printed nothing")
		}
		if err != nil {
			goroot.dir, goroot.err = "", fmt.Errorf("GOROOT could not be found: go env GOROOT failed: %v; set the GOROOT environment variable to the Go installation, or put the go command on PATH", err)
		}
	})
	return goroot.dir, goroot.err
}

// checkCalls returns the E20 diagnostic of the first call of r whose in or out
// type cannot travel as JSON.
func (g *Generator) checkCalls(rPath string, r *routepkg.Route) *Diagnostic {
	calls := routeCalls(r)
	if len(calls) == 0 {
		return nil
	}
	dir := filepath.Dir(g.routeIndexPath(rPath))
	pkg := loadGoPackage(dir)
	indexPath := g.routeIndexPath(rPath)
	for _, c := range calls {
		for _, t := range []struct{ attr, typ string }{{"in", c.In}, {"out", c.Out}} {
			e, err := parser.ParseExpr(t.typ)
			if err != nil {
				continue
			}
			if msg := unresolvedQualifier(pkg, e); msg != "" {
				return &Diagnostic{
					Route:   rPath,
					File:    g.relPath(indexPath),
					Line:    c.Line,
					Message: fmt.Sprintf("<ssr:call name=%q>: the %s type %q %s", c.Name, t.attr, t.typ, msg),
				}
			}
			ck := &jsonChecker{g: g, pkg: pkg, seen: map[string]bool{}}
			if why := ck.check(e, pkg.templateScope(indexPath, c.Line), ""); why != "" {
				return &Diagnostic{
					Route:   rPath,
					File:    g.relPath(indexPath),
					Line:    c.Line,
					Code:    "E20",
					Message: fmt.Sprintf("<ssr:call name=%q>: the %s type %q cannot travel as JSON: %s", c.Name, t.attr, t.typ, why),
					Fix:     fixE20,
				}
			}
		}
	}
	return nil
}

// unresolvedQualifier returns why a package qualifier in the type e, written in
// a template, names no package the generated files can import, or "".
func unresolvedQualifier(pkg *goPackage, e ast.Expr) string {
	msg := ""
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || msg != "" {
			return msg == ""
		}
		if x, ok := sel.X.(*ast.Ident); ok {
			switch path, err := pkg.resolveImport(x.Name); {
			case err != nil:
				msg = fmt.Sprintf("names the package %s, which the generator cannot look up: %v", x.Name, err)
			case path == "":
				msg = fmt.Sprintf("names the package %s, which no Go file of the route imports and the standard library does not have; fix the name, or import the package in a Go file next to the template", x.Name)
			}
		}
		return false
	})
	return msg
}

// checkWritableVars returns the diagnostic of the first client-writable
// variable of r whose type holds a render type: a page must never write
// markup.
func (g *Generator) checkWritableVars(rPath string, r *routepkg.Route) *Diagnostic {
	if r.Template() == nil {
		return nil
	}
	indexPath := g.routeIndexPath(rPath)
	var pkg *goPackage
	for _, v := range r.Template().GetVariables() {
		if !v.ClientWritable {
			continue
		}
		e, err := parser.ParseExpr(v.Type)
		if err != nil {
			continue
		}
		if pkg == nil {
			pkg = loadGoPackage(filepath.Dir(indexPath))
		}
		ck := &jsonChecker{g: g, pkg: pkg, seen: map[string]bool{}, markup: true}
		why := ck.check(e, pkg.templateScope(indexPath, v.Line), "")
		if why == "" {
			continue
		}
		msg := fmt.Sprintf("<ssr:var name=%q> is client-writable, but its type %s holds %s, which is markup the page would write; declare a string and write it with {{ }}", v.Name, v.Type, why)
		if ck.generic {
			msg = fmt.Sprintf("<ssr:var name=%q> is client-writable, but its type %s uses %s; client-writable variables cannot use generic types, so declare a concrete struct instead", v.Name, v.Type, why)
		}
		return &Diagnostic{Route: rPath, File: g.relPath(indexPath), Line: v.Line, Message: msg}
	}
	return nil
}

// marshals reports whether the package's type name has a MarshalJSON or a
// MarshalText method.
func (p *goPackage) marshals(name string) bool {
	m := p.methods[name]
	return m["MarshalJSON"] != 0 || m["MarshalText"] != 0
}

// underlyingStruct returns the struct type that the package's type name
// declares, following the names it is declared as, or nil.
func (p *goPackage) underlyingStruct(name string) (*ast.StructType, *typeScope) {
	seen := map[string]bool{}
	for d := p.types[name]; d != nil && d.spec.TypeParams == nil && !seen[name]; d = p.types[name] {
		seen[name] = true
		switch t := ast.Unparen(d.spec.Type).(type) {
		case *ast.StructType:
			return t, d.scope
		case *ast.Ident:
			name = t.Name
		default:
			return nil, nil
		}
	}
	return nil, nil
}

// underlying returns the type that the package's type name has underneath,
// following the package's type names, and its scope; nil for a generic one.
func (p *goPackage) underlying(name string) (ast.Expr, *typeScope) {
	seen := map[string]bool{}
	for {
		d := p.types[name]
		if seen[name] || d.spec.TypeParams != nil {
			return nil, nil
		}
		seen[name] = true
		u := ast.Unparen(d.spec.Type)
		id, ok := u.(*ast.Ident)
		if !ok || p.types[id.Name] == nil {
			return u, d.scope
		}
		name = id.Name
	}
}

// basicKind returns the predeclared type that e has underneath, following the
// package's type names, or "" when it is none.
func (p *goPackage) basicKind(e ast.Expr) string {
	seen := map[string]bool{}
	for {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return ""
		}
		d := p.types[id.Name]
		if d == nil {
			if _, ok := basicTS[id.Name]; ok {
				return id.Name
			}
			return ""
		}
		if seen[id.Name] || d.spec.TypeParams != nil {
			return ""
		}
		seen[id.Name] = true
		e = d.spec.Type
	}
}

// basicTS maps the predeclared types that encoding/json writes to their
// TypeScript types.
var basicTS = map[string]string{
	"string": "string", "bool": "boolean",
	"int": "number", "int8": "number", "int16": "number", "int32": "number", "int64": "number",
	"uint": "number", "uint8": "number", "uint16": "number", "uint32": "number", "uint64": "number",
	"uintptr": "number", "byte": "number", "rune": "number", "float32": "number", "float64": "number",
}

// mapKey reports whether encoding/json takes e, written in sc, as a map key,
// and whether the generator could tell.
func (p *goPackage) mapKey(e ast.Expr, sc *typeScope) (ok, known bool) {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return p.mapKey(t.X, sc)
	case *ast.SelectorExpr:
		if x, _ := t.X.(*ast.Ident); x != nil && sc.path(x.Name) == "time" && t.Sel.Name == "Time" {
			return true, true
		}
		return false, false
	case *ast.StarExpr:
		if id, isIdent := t.X.(*ast.Ident); isIdent && p.types[id.Name] != nil {
			return p.methods[id.Name]["MarshalText"] != 0, true
		}
		if _, isSel := t.X.(*ast.SelectorExpr); isSel {
			return false, false
		}
		return false, true
	case *ast.Ident:
		if d := p.types[t.Name]; d != nil {
			if p.methods[t.Name]["MarshalText"]&valueRecv != 0 {
				return true, true
			}
			if d.spec.Assign != 0 {
				return p.mapKey(d.spec.Type, d.scope)
			}
			// A defined type has none of the methods of the type it is defined from.
			switch u, usc := p.underlying(t.Name); u := u.(type) {
			case nil, *ast.SelectorExpr:
				return false, false
			case *ast.Ident:
				return p.mapKey(u, usc)
			}
			return false, true
		}
		switch k := basicTS[t.Name]; {
		case k == "string":
			return true, true
		case k == "number" && !strings.HasPrefix(t.Name, "float"):
			return true, true
		case k != "" || t.Name == "any" || t.Name == "error" || strings.HasPrefix(t.Name, "complex"):
			return false, true
		}
		return false, false
	}
	return false, true
}

// jsonChecker finds what in a call type cannot travel as JSON (E20).
//
// With markup set it finds instead a render type or a generic type, whose
// type arguments it does not follow, in what a page writes to a
// client-writable variable, looking through types that marshal themselves;
// generic then reports which of the two it found.
type jsonChecker struct {
	g       *Generator
	pkg     *goPackage
	seen    map[string]bool
	markup  bool
	generic bool
}

// check returns what in e, written in sc, cannot travel as JSON, or "". owner
// names the type whose struct e is.
func (c *jsonChecker) check(e ast.Expr, sc *typeScope, owner string) string {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return c.check(t.X, sc, owner)
	case *ast.StarExpr:
		return c.check(t.X, sc, "")
	case *ast.ArrayType:
		return c.check(t.Elt, sc, "")
	case *ast.FuncType:
		if !c.markup {
			return "a func"
		}
	case *ast.ChanType:
		if !c.markup {
			return "a chan"
		}
	case *ast.InterfaceType:
		if !c.markup && len(t.Methods.List) > 0 {
			return "an interface with methods"
		}
	case *ast.MapType:
		if c.markup {
			if why := c.check(t.Key, sc, ""); why != "" {
				return why
			}
		} else if ok, known := c.pkg.mapKey(t.Key, sc); !ok && known {
			return "a map with " + exprString(t.Key) + " keys"
		}
		return c.check(t.Value, sc, "")
	case *ast.StructType:
		for _, f := range t.Fields.List {
			if jsonTag(f) == "-" {
				continue
			}
			name := embeddedName(f)
			if len(f.Names) > 0 {
				if !slices.ContainsFunc(f.Names, func(n *ast.Ident) bool { return n.IsExported() }) {
					continue
				}
				name = f.Names[0].Name
			} else if !ast.IsExported(name) && (!c.markup || !c.pkg.genericBeneath(f.Type)) {
				if s, _ := c.pkg.underlyingStruct(name); s == nil {
					continue
				}
			}
			if why := c.check(f.Type, sc, ""); why != "" {
				where := "field " + name
				if owner != "" {
					where = "field " + owner + "." + name
				}
				if sc.line == 0 {
					file, line := sc.at(f)
					where += fmt.Sprintf(" (%s:%d)", c.g.relPath(file), line)
				}
				return why + " in " + where
			}
		}
	case *ast.Ident:
		if strings.HasPrefix(t.Name, "complex") && c.pkg.types[t.Name] == nil && !c.markup {
			return "a " + t.Name
		}
		if t.Name == "error" && c.pkg.types[t.Name] == nil && !c.markup {
			return "an error"
		}
		d := c.pkg.types[t.Name]
		if d == nil || c.seen[t.Name] || c.pkg.marshals(t.Name) && !c.markup {
			return ""
		}
		c.seen[t.Name] = true
		return c.check(d.spec.Type, d.scope, t.Name)
	case *ast.IndexExpr, *ast.IndexListExpr:
		if c.markup {
			c.generic = true
			return "the generic type " + exprString(t)
		}
	case *ast.SelectorExpr:
		x, _ := t.X.(*ast.Ident)
		if x == nil {
			return ""
		}
		switch p := sc.path(x.Name); {
		case c.markup:
			if p == renderPath {
				return "a render." + t.Sel.Name
			}
		case p == "unsafe":
			if t.Sel.Name == "Pointer" {
				return "an unsafe.Pointer"
			}
		case p == muxPath || p == renderPath || p == reactivePath:
			return "a " + path.Base(p) + "." + t.Sel.Name
		}
	}
	return ""
}

// genericBeneath reports whether e, or the type e points to, is an
// instantiation of a generic type, or a type name of the package declared,
// through its type names, as one or with type parameters.
func (p *goPackage) genericBeneath(e ast.Expr) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	seen := map[string]bool{}
	for {
		switch t := ast.Unparen(e).(type) {
		case *ast.IndexExpr, *ast.IndexListExpr:
			return true
		case *ast.Ident:
			d := p.types[t.Name]
			if d == nil || seen[t.Name] {
				return false
			}
			if d.spec.TypeParams != nil {
				return true
			}
			seen[t.Name] = true
			e = d.spec.Type
		default:
			return false
		}
	}
}

// embeddedName returns the type name of the embedded field f.
func embeddedName(f *ast.Field) string {
	e := f.Type
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	switch t := e.(type) {
	case *ast.IndexExpr:
		e = t.X
	case *ast.IndexListExpr:
		e = t.X
	}
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// jsonTag returns the json key of f's tag.
func jsonTag(f *ast.Field) string {
	if f.Tag == nil {
		return ""
	}
	v, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(v).Get("json")
}

// validJSONName reports whether encoding/json takes s, the name in a json tag.
func validJSONName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c) && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

func exprString(e ast.Expr) string {
	var b strings.Builder
	writeExpr(&b, e)
	return b.String()
}

func writeExpr(b *strings.Builder, e ast.Expr) {
	switch t := e.(type) {
	case *ast.Ident:
		b.WriteString(t.Name)
	case *ast.SelectorExpr:
		writeExpr(b, t.X)
		b.WriteString(".")
		b.WriteString(t.Sel.Name)
	case *ast.StarExpr:
		b.WriteString("*")
		writeExpr(b, t.X)
	case *ast.ParenExpr:
		b.WriteString("(")
		writeExpr(b, t.X)
		b.WriteString(")")
	case *ast.ArrayType:
		b.WriteString("[")
		if t.Len != nil {
			writeExpr(b, t.Len)
		}
		b.WriteString("]")
		writeExpr(b, t.Elt)
	case *ast.MapType:
		b.WriteString("map[")
		writeExpr(b, t.Key)
		b.WriteString("]")
		writeExpr(b, t.Value)
	case *ast.BasicLit:
		b.WriteString(t.Value)
	case *ast.IndexExpr:
		writeExpr(b, t.X)
		b.WriteString("[")
		writeExpr(b, t.Index)
		b.WriteString("]")
	case *ast.IndexListExpr:
		writeExpr(b, t.X)
		b.WriteString("[")
		for i, ix := range t.Indices {
			if i > 0 {
				b.WriteString(", ")
			}
			writeExpr(b, ix)
		}
		b.WriteString("]")
	case *ast.StructType:
		b.WriteString("struct{…}")
	case *ast.InterfaceType:
		b.WriteString("interface{…}")
	case *ast.FuncType:
		b.WriteString("func(…)")
	case *ast.ChanType:
		b.WriteString("chan ")
		writeExpr(b, t.Value)
	default:
		b.WriteString("…")
	}
}

// tsMapper maps Go types to the TypeScript types of the values encoding/json
// writes for them. A named type of the route package becomes a TypeScript
// declaration of the same name. A type it cannot map exactly becomes unknown,
// with a warning at the place it is written.
type tsMapper struct {
	g       *Generator
	pkg     *goPackage
	decls   map[string]string
	started map[string]bool
	warned  map[string]bool
}

func newTSMapper(g *Generator, pkg *goPackage) *tsMapper {
	return &tsMapper{g: g, pkg: pkg, decls: map[string]string{}, started: map[string]bool{}, warned: map[string]bool{}}
}

func (m *tsMapper) warn(file string, line int, format string, args ...any) {
	w := fmt.Sprintf("%s:%d: warning: %s\n", m.g.relPath(file), line, fmt.Sprintf(format, args...))
	if !m.warned[w] {
		m.warned[w] = true
		fmt.Fprint(m.g.errOut, w)
	}
}

// typeOf maps the type written as src in a template on line.
func (m *tsMapper) typeOf(src, file string, line int) string {
	e, err := parser.ParseExpr(src)
	if err != nil {
		m.warn(file, line, "%q is not a Go type; its TypeScript type is unknown", src)
		return "unknown"
	}
	return m.ts(e, m.pkg.templateScope(file, line))
}

// declarations returns the TypeScript declarations of the named types mapped
// so far, sorted by name.
func (m *tsMapper) declarations() []string {
	names := make([]string, 0, len(m.decls))
	for n := range m.decls {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = m.decls[n]
	}
	return out
}

func (m *tsMapper) ts(e ast.Expr, sc *typeScope) string {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return m.ts(t.X, sc)
	case *ast.Ident:
		return m.ident(t, sc)
	case *ast.SelectorExpr:
		if x, _ := t.X.(*ast.Ident); x != nil && sc.path(x.Name) == "time" && t.Sel.Name == "Time" {
			return "string"
		}
		file, line := sc.at(t)
		m.warn(file, line, "%s is from another package; its TypeScript type is unknown", exprString(t))
		return "unknown"
	case *ast.StarExpr:
		inner := m.ts(t.X, sc)
		if inner == "unknown" || strings.HasSuffix(inner, " | null") {
			return inner
		}
		return inner + " | null"
	case *ast.ArrayType:
		if t.Len == nil && m.isByte(t.Elt) {
			return "string"
		}
		elem := m.ts(t.Elt, sc)
		if strings.Contains(elem, " | ") {
			elem = "(" + elem + ")"
		}
		return elem + "[]"
	case *ast.MapType:
		if ok, known := m.pkg.mapKey(t.Key, sc); !ok || !known {
			file, line := sc.at(t)
			m.warn(file, line, "%s has keys encoding/json may not write; its TypeScript type is unknown", exprString(t))
			return "unknown"
		}
		return "Record<string, " + m.ts(t.Value, sc) + ">"
	case *ast.StructType:
		fields, why := m.fields(t, sc)
		if why != "" {
			file, line := sc.at(t)
			m.warn(file, line, "the struct %s; its TypeScript type is unknown", why)
			return "unknown"
		}
		return objectType(fields)
	case *ast.InterfaceType:
		return "unknown"
	}
	file, line := sc.at(e)
	m.warn(file, line, "%s has no JSON form the generator can name; its TypeScript type is unknown", exprString(e))
	return "unknown"
}

// isByte reports whether e is a byte type, which makes its slices base64 strings.
func (m *tsMapper) isByte(e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok && m.pkg.marshals(id.Name) {
		return false
	}
	k := m.pkg.basicKind(e)
	return k == "byte" || k == "uint8"
}

func (m *tsMapper) ident(t *ast.Ident, sc *typeScope) string {
	d := m.pkg.types[t.Name]
	if d == nil {
		if ts, ok := basicTS[t.Name]; ok {
			return ts
		}
		switch t.Name {
		case "any", "error":
			return "unknown"
		case "complex64", "complex128":
			file, line := sc.at(t)
			m.warn(file, line, "%s has no JSON form; its TypeScript type is unknown", t.Name)
			return "unknown"
		}
		if !m.started[t.Name] {
			m.started[t.Name] = true
			file, line := sc.at(t)
			m.warn(file, line, "%s is not declared in the route package; its TypeScript type is unknown", t.Name)
			m.decls[t.Name] = "export type " + t.Name + " = unknown;"
		}
		return t.Name
	}
	m.named(t.Name, d)
	return t.Name
}

// named declares the package's type name in TypeScript.
func (m *tsMapper) named(name string, d *goTypeDecl) {
	if m.started[name] {
		return
	}
	m.started[name] = true
	sc := d.scope
	file, line := sc.at(d.spec)
	unknown := func(format string, args ...any) {
		m.warn(file, line, format, args...)
		m.decls[name] = "export type " + name + " = unknown;"
	}
	methods := m.pkg.methods[name]
	switch {
	case d.spec.TypeParams != nil:
		unknown("%s is generic; its TypeScript type is unknown", name)
		return
	case methods["MarshalJSON"] != 0:
		unknown("%s has a MarshalJSON method; its TypeScript type is unknown", name)
		return
	case methods["MarshalText"] == valueRecv:
		m.decls[name] = "export type " + name + " = string;"
		return
	case methods["MarshalText"] != 0:
		unknown("%s has a MarshalText method on its pointer type; its TypeScript type is unknown", name)
		return
	}
	expr := d.spec.Type
	if id, ok := expr.(*ast.Ident); ok && d.spec.Assign == 0 && m.pkg.types[id.Name] != nil {
		// A defined type has none of the methods of the type it is defined from,
		// so encoding/json writes it by its underlying type.
		u, usc := m.pkg.underlying(id.Name)
		switch u.(type) {
		case nil:
			unknown("%s is defined from a generic type; its TypeScript type is unknown", name)
			return
		case *ast.SelectorExpr:
			unknown("%s is defined from %s of another package; its TypeScript type is unknown", name, exprString(u))
			return
		}
		expr, sc = u, usc
	}
	st, isStruct := expr.(*ast.StructType)
	if !isStruct || d.spec.Assign != 0 {
		m.decls[name] = "export type " + name + " = " + m.ts(expr, sc) + ";"
		return
	}
	fields, why := m.fields(st, sc)
	switch {
	case why != "":
		unknown("%s %s; its TypeScript type is unknown", name, why)
	case len(fields) == 0:
		m.decls[name] = "export type " + name + " = Record<string, never>;"
	default:
		var b strings.Builder
		b.WriteString("export interface " + name + " {\n")
		for _, f := range fields {
			b.WriteString("  " + f.String() + ";\n")
		}
		b.WriteString("}")
		m.decls[name] = b.String()
	}
}

// tsField is a property of the JSON object encoding/json writes for a struct.
type tsField struct {
	name     string
	tagged   bool
	index    []int
	ts       string
	optional bool
}

var reTSIdent = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func (f tsField) String() string {
	name := f.name
	if !reTSIdent.MatchString(name) {
		name = strconv.Quote(name)
	}
	if f.optional {
		name += "?"
	}
	return name + ": " + f.ts
}

func objectType(fields []tsField) string {
	if len(fields) == 0 {
		return "Record<string, never>"
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f.String()
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

// fields returns the properties encoding/json writes for st, written in sc, in
// its order, or why it cannot tell them.
func (m *tsMapper) fields(st *ast.StructType, sc *typeScope) ([]tsField, string) {
	var all []tsField
	if why := m.collect(st, sc, nil, false, map[string]bool{}, &all); why != "" {
		return nil, why
	}
	byName := map[string][]tsField{}
	for _, f := range all {
		byName[f.name] = append(byName[f.name], f)
	}
	var out []tsField
	for _, group := range byName {
		depth := len(group[0].index)
		for _, f := range group {
			depth = min(depth, len(f.index))
		}
		var top, tagged []tsField
		for _, f := range group {
			if len(f.index) == depth {
				top = append(top, f)
				if f.tagged {
					tagged = append(tagged, f)
				}
			}
		}
		switch {
		case len(top) == 1:
			out = append(out, top[0])
		case len(tagged) == 1:
			out = append(out, tagged[0])
		}
	}
	sort.Slice(out, func(i, j int) bool { return slices.Compare(out[i].index, out[j].index) < 0 })
	return out, ""
}

// collect appends the fields of st, embedded structs' promoted fields
// included, to out. optional marks fields reached through an embedded pointer.
func (m *tsMapper) collect(st *ast.StructType, sc *typeScope, index []int, optional bool, visiting map[string]bool, out *[]tsField) string {
	i := -1
	for _, f := range st.Fields.List {
		n := max(len(f.Names), 1)
		for k := range n {
			i++
			tag := jsonTag(f)
			if tag == "-" {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if !validJSONName(name) {
				name = ""
			}
			idx := append(slices.Clone(index), i)
			fieldName := embeddedName(f)
			if len(f.Names) > 0 {
				fieldName = f.Names[k].Name
				if !ast.IsExported(fieldName) {
					continue
				}
			} else {
				base, ptr := f.Type, false
				if s, ok := base.(*ast.StarExpr); ok {
					base, ptr = s.X, true
				}
				id, isIdent := base.(*ast.Ident)
				switch {
				case !isIdent:
					return "embeds " + exprString(base) + ", whose JSON form the generator cannot see"
				case m.pkg.types[id.Name] == nil:
					if _, basic := basicTS[id.Name]; basic || id.Name == "any" || id.Name == "error" {
						continue
					}
					return "embeds " + id.Name + ", which is not declared in the route package"
				case m.pkg.marshals(id.Name):
					return "embeds " + id.Name + ", which marshals itself"
				}
				embedded, esc := m.pkg.underlyingStruct(id.Name)
				if !id.IsExported() && embedded == nil {
					continue
				}
				if name == "" && embedded != nil {
					if visiting[id.Name] {
						continue
					}
					visiting[id.Name] = true
					why := m.collect(embedded, esc, idx, optional || ptr, visiting, out)
					delete(visiting, id.Name)
					if why != "" {
						return why
					}
					continue
				}
			}
			ts := m.ts(f.Type, sc)
			if hasOpt(opts, "string") {
				ft := f.Type
				_, isPtr := ft.(*ast.StarExpr)
				if isPtr {
					ft = ft.(*ast.StarExpr).X
				}
				// encoding/json ignores the option for a type that marshals itself.
				id, isIdent := ft.(*ast.Ident)
				if _, basic := basicTS[m.pkg.basicKind(ft)]; basic && !(isIdent && m.pkg.marshals(id.Name)) {
					ts = "string"
					if isPtr {
						ts = "string | null"
					}
				}
			}
			*out = append(*out, tsField{
				name:     cmp.Or(name, fieldName),
				tagged:   name != "",
				index:    idx,
				ts:       ts,
				optional: optional || hasOpt(opts, "omitempty") || hasOpt(opts, "omitzero"),
			})
		}
	}
	return ""
}

func hasOpt(opts, opt string) bool {
	return slices.Contains(strings.Split(opts, ","), opt)
}

// genCallMethods makes the state of a route with calls a mux.Caller. Its Call
// runs only the calls the template declares, each with its arguments decoded
// by mux.DecodeArgs.
func genCallMethods(buf *gobuf.GoBuf, rPath string, calls []*node.SsrCall, reactive bool) {
	if !reactive {
		buf.WriteString("const routeKey = ")
		buf.WriteQuotedString(routeKeyFor(rPath), "\n")
		buf.WriteStringLn("func (c *ssrState) RouteKey() string { return routeKey }")
		buf.WriteString("\n")
	}
	buf.WriteStringLn("var _ mux.Caller = (*ssrState)(nil)")
	buf.WriteString("\n")
	buf.WriteStringLn("func (c *ssrState) Call(ctx context.Context, r *mux.Request, name string, args json.RawMessage) (any, error) {")
	buf.WriteStringLn("switch name {")
	for _, call := range calls {
		buf.WriteString("case ")
		buf.WriteQuotedString(call.Name, ":\n")
		buf.WriteStringLn("var in " + call.In)
		buf.WriteStringLn("if err := mux.DecodeArgs(args, &in); err != nil {")
		buf.WriteStringLn("	return nil, err")
		buf.WriteStringLn("}")
		buf.WriteStringLn("return c.dp." + callMethod(call.Name) + "(ctx, r, in)")
	}
	buf.WriteStringLn("}")
	buf.WriteString("return nil, mux.NewHttpError(http.StatusNotFound, ")
	buf.WriteQuotedString(callNotFound, ")\n")
	buf.WriteStringLn("}")
	buf.WriteString("\n")
}
