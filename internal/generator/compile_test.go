package generator_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template"
)

// The generated code of every fixture, with the fixture's own Go files and tests, compiles
// and passes go vet.
func TestGeneratedFixturesCompile(t *testing.T) {
	fixtures, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		// The generator runs one at a time; go vet runs in parallel.
		app := generateFixture(t, filepath.Join("testdata", f.Name()))
		t.Run(f.Name(), func(t *testing.T) {
			t.Parallel()
			goCommand(t, app, "vet", "./...")
		})
	}
}

// Every name the generated code of the fixtures uses is either refused as the name of a
// template's value, loop item and loop index, or compiles as each of them wherever a template's
// names are in scope: in the page, in a live block, in a form and around <ssr:content/>.
func TestGeneratedNamesCompile(t *testing.T) {
	names := map[string]bool{"w": true, "c": true, "data": true}
	fixtures, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		for _, name := range generatedIdents(t, generateFixture(t, filepath.Join("testdata", f.Name()))) {
			names[name] = true
		}
	}

	var accepted []string
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for n := range names {
			if !strings.HasPrefix(n, "zz") && !yield(n) {
				return
			}
		}
	}) {
		declarations := []string{
			`<ssr:var name="` + name + `" type="int"/>`,
			`<ssr:var name="xs" type="[]int"/><p ssr:for="` + name + ` in xs">x</p>`,
		}
		if name != "_" {
			declarations = append(declarations, `<ssr:var name="xs" type="[]int"/><p ssr:for="`+name+`, x in xs">x</p>`)
		}
		var refused []bool
		for _, src := range declarations {
			err := analyzeTemplate(t, src)
			if err != nil && !strings.Contains(err.Error(), strconv.Quote(name)) {
				t.Errorf("%s: %v, want it to name %q", src, err, name)
			}
			refused = append(refused, err != nil)
		}
		if slices.Contains(refused, !refused[0]) {
			t.Errorf("%q is refused in some declarations and not in others: %v", name, refused)
			continue
		}
		if !refused[0] {
			accepted = append(accepted, name)
		}
	}

	app := filepath.Join(t.TempDir(), "app")
	writeTemplate(t, filepath.Join(app, "web"), ".", namesTemplate(accepted))
	g := appGenerator(app)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	goCommand(t, app, "vet", "./...")
}

// appGenerator returns a generator of the project in app, as module example.com/app.
func appGenerator(app string) *generator.Generator {
	g := generator.New(&config.Config{Dir: app, WebDir: filepath.Join(app, "web"), WebPackage: "example.com/app/web"})
	g.SetOutput(nil, io.Discard, io.Discard)
	return g
}

// generatedIdents returns the identifiers of the generated Go files of the project in app.
func generatedIdents(t *testing.T, app string) []string {
	t.Helper()
	var idents []string
	err := filepath.WalkDir(app, func(path string, _ fs.DirEntry, err error) error {
		if err != nil || !strings.HasSuffix(path, "_gen.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				idents = append(idents, id.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return idents
}

// analyzeTemplate analyzes a project whose only page is src.
func analyzeTemplate(t *testing.T, src string) error {
	t.Helper()
	app := t.TempDir()
	writeTemplate(t, filepath.Join(app, "web"), ".", src)
	return appGenerator(app).Analyze()
}

// namesTemplate returns a layout that declares each of names as a value and reads it in the
// page and in a live block, and declares it as a loop item and a loop index in a form, in a
// live block and around <ssr:content/> and <ssr:assets/>, each loop inside the one before.
func namesTemplate(names []string) string {
	var b strings.Builder
	b.WriteString(`<ssr:var name="zzItems" type="[]int"/><ssr:var name="zzLive" type="int" reactive="true"/>` + "\n")
	fields := map[string]bool{}
	var vars []string
	for _, n := range names {
		if f := template.ExportedName(n); !fields[f] {
			fields[f] = true
			vars = append(vars, n)
			fmt.Fprintf(&b, "<ssr:var name=%q type=\"int\"/>\n", n)
		}
	}
	for _, v := range vars {
		fmt.Fprintf(&b, "<p>{{ %s }}</p>\n", v)
	}
	b.WriteString(`<div ssr:if="zzLive > 0">`)
	for _, v := range vars {
		fmt.Fprintf(&b, "<p>{{ %s }}</p>\n", v)
	}
	b.WriteString("</div>\n")

	loops := func(index bool, inner string) {
		for _, n := range names {
			if index {
				fmt.Fprintf(&b, `<div ssr:for="%s, zzV in zzItems">{{ %s }}`+"\n", n, n)
			} else {
				fmt.Fprintf(&b, `<div ssr:for="%s in zzItems">{{ %s }}`+"\n", n, n)
			}
		}
		b.WriteString(inner)
		b.WriteString(strings.Repeat("</div>", len(names)) + "\n")
	}
	b.WriteString(`<ssr:form name="zzForm">{{ zzLive }}`)
	loops(false, `<ssr:input name="zzText" class="{{ form.IsValidated() ? input.HasError() ? 'a' : 'b' : '' }}"/>`+
		`<ssr:input name="zzBox" type="checkbox" gotype="int" value="1"/><ssr:input name="zzBox" type="checkbox" gotype="int" value="2"/>`+
		`<ssr:input name="zzRadio" type="radio" gotype="bool" value="true"/>`+
		`<ssr:select name="zzSelect"/><ssr:textarea name="zzArea" class="{{ textarea.HasError() ? 'a' : '' }}"/>`)
	loops(true, `<ssr:input name="zzIndexText" class="{{ input.HasError() ? 'a' : '' }}"/>`)
	b.WriteString("</ssr:form>\n")
	b.WriteString(`<div ssr:if="zzLive > 0">`)
	loops(false, `{{ zzLive }}`)
	loops(true, `{{ zzLive }}`)
	b.WriteString("</div>\n")
	loops(true, `<ssr:content/><ssr:assets/>`)
	return b.String()
}
