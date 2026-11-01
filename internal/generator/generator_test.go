package generator_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

func TestGenerator_Generate(t *testing.T) {
	g := generator.New(&config.Config{
		Dir:         "../../example",
		WebDir:      "../../example/internal/web",
		WebPackage:  "github.com/sergei-svistunov/go-ssr/example/internal/web",
		DepsPackage: "github.com/sergei-svistunov/go-ssr/example/internal/model",
		DepsType:    "Model",
		GoRunArgs:   ".",
		Env:         nil,
	})

	//if err := g.Webpack(); err != nil {
	//	t.Fatal(err)
	//}

	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}

	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
}

// TestGeneratedRouteRunsRootToLeaf serves a generated root layout and child,
// each with a form and a Guard; testdata/rootleaf/rootleaf_test.go asserts the
// order of their hooks.
func TestGeneratedRouteRunsRootToLeaf(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/rootleaf"))
}

// TestGeneratedParentRoutes generates a team page without <ssr:content/> that
// has a members page below it, under a root layout.
func TestGeneratedParentRoutes(t *testing.T) {
	bin := buildFixture(t, "testdata/parentpage")
	// Only a template with <ssr:content/> generates a route whose Layout() is
	// true.
	t.Run("LayoutFlag", func(t *testing.T) { runFixture(t, bin, "-test.run", "^TestLayoutFlag$") })
	// Each page renders inside the root layout on its own, and the team page's
	// Guard covers the members page.
	t.Run("ParentPage", func(t *testing.T) { runFixture(t, bin, "-test.run", "^TestParentPage$") })
	// The page writes the webpack runtime once, before the entries of its
	// routes, so their modules share one runtime.
	t.Run("Scripts", func(t *testing.T) { runFixture(t, bin, "-test.run", "^TestPageScripts$") })
}

// TestGeneratedPageEscapes serves a generated page that writes one hostile
// value as text, in a class attribute and in a URL path, a javascript: URL as
// a whole href, and safe markup through {{$ }}.
func TestGeneratedPageEscapes(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/escapes"))
}

// TestGeneratedTextareaValues serves a generated page whose textareas hold values with and
// without a leading newline.
func TestGeneratedTextareaValues(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/textarea"))
}

// TestGeneratedLiveValues serves a generated page with live values and checks
// that every value its live connection sends is the markup the first render
// wrote for the same site: escaped text for {{ }}, the value as is for {{$ }},
// and a block's markup rendered with the same escapers; and that an int,
// float64 or bool variable takes the text an ssr:bind input writes; and, built
// with -race, that the Sets of two variables of one site do not race.
func TestGeneratedLiveValues(t *testing.T) {
	bin := buildFixture(t, "testdata/live", "-race")
	for _, name := range []string{"TestLiveTextValueIsEscaped", "TestLiveSafeHTMLValue", "TestBlockValueIsEscapedMarkup", "TestRawOutputWritesAnyValue", "TestScalarWritesTakeText", "TestSetsOfOneSiteDoNotRace"} {
		t.Run(name, func(t *testing.T) { runFixture(t, bin, "-test.run", "^"+name+"$") })
	}
}

// TestGeneratedFormCondition serves a generated page whose form has ssr:if
// and is followed by an element with ssr:else: the form and its token render
// only when the condition holds, and the form is processed either way.
func TestGeneratedFormCondition(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/formif"))
}

// The markup the generated code of every fixture writes holds no ssr:
// attribute other than ssr:bind, which the client runtime reads.
func TestGeneratedMarkupHasNoSsrAttributes(t *testing.T) {
	fixtures, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	ssrAttr := regexp.MustCompile(`\sssr:[A-Za-z-]+`)
	for _, f := range fixtures {
		app := generateFixture(t, filepath.Join("testdata", f.Name()))
		err := filepath.WalkDir(app, func(path string, _ fs.DirEntry, err error) error {
			if err != nil || !strings.HasSuffix(path, "_gen.go") {
				return err
			}
			for _, attr := range ssrAttr.FindAllString(generatedMarkup(t, path), -1) {
				if attr[1:] != "ssr:bind" {
					t.Errorf("%s writes %s", filepath.Join(f.Name(), strings.TrimPrefix(path, app)), attr[1:])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// generatedMarkup returns the literal markup the generated Go file at path
// writes, which the generator keeps in []byte variables of hex literals.
func generatedMarkup(t *testing.T, path string) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var markup strings.Builder
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, e := range lit.Elts {
			if b, ok := e.(*ast.BasicLit); ok && b.Kind == token.INT && strings.HasPrefix(b.Value, "0x") {
				v, err := strconv.ParseUint(b.Value[2:], 16, 8)
				if err != nil {
					t.Fatal(err)
				}
				markup.WriteByte(byte(v))
			}
		}
		markup.WriteByte('\n')
		return true
	})
	return markup.String()
}

// generateFixture copies the project in dir and generates it as module
// example.com/app, returning its directory.
func generateFixture(t *testing.T, dir string) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "app")
	if err := os.CopyFS(app, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}

	g := generator.New(&config.Config{Dir: app, WebDir: filepath.Join(app, "web"), WebPackage: "example.com/app/web"})
	var diag bytes.Buffer
	g.SetOutput(nil, io.Discard, &diag)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v\n%s", err, diag.String())
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v\n%s", err, diag.String())
	}
	return app
}

// buildFixture generates the project in dir with generateFixture, built
// against this repository, and compiles the tests of its root package, with
// the extra go test flags, into the binary it returns.
func buildFixture(t *testing.T, dir string, flags ...string) string {
	t.Helper()
	app := generateFixture(t, dir)
	bin := filepath.Join(app, "app.test")
	goCommand(t, app, append([]string{"test", "-c", "-o", bin}, append(flags, ".")...)...)
	return bin
}

// goCommand runs the go command with args in the generated project app, as module
// example.com/app built against this repository.
func goCommand(t *testing.T, app string, args ...string) {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	goMod := fmt.Sprintf("module example.com/app\n\n%s\n\nrequire github.com/sergei-svistunov/go-ssr v0.0.0\n\nreplace github.com/sergei-svistunov/go-ssr => %s\n", goLine(t, repoRoot), repoRoot)
	if err := os.WriteFile(filepath.Join(app, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mod", "tidy"}, args} {
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = app
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v:\n%s", args, out)
		}
	}
}

// goLine returns the go directive of the go.mod in root.
func goLine(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "go ") {
			return line
		}
	}
	t.Fatal("go.mod has no go directive")
	return ""
}

// runFixture runs a binary from buildFixture with args.
func runFixture(t *testing.T, bin string, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("%v:\n%s", args, out)
	}
}

// TestGeneratedNames serves a generated page whose values, loop items and loop indexes take
// the names the generated code used for its own values, and a live value inside a form.
func TestGeneratedNames(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/names"))
}

// TestGeneratedFormStub serves a page with a form whose data provider the generator wrote.
func TestGeneratedFormStub(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/formstub"))
}
