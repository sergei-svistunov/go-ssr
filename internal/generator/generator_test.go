package generator_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

// TestGeneratedPageEscapes serves a generated page that writes one hostile
// value as text, in a class attribute and in a URL path, a javascript: URL as
// a whole href, and safe markup through {{$ }}.
func TestGeneratedPageEscapes(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/escapes"))
}

// TestGeneratedLiveValues serves a generated page with live values and checks
// that every value its live connection sends is the markup the first render
// wrote for the same site: escaped text for {{ }}, the value as is for {{$ }},
// and a block's markup rendered with the same escapers; and that an int,
// float64 or bool variable takes the text an ssr:bind input writes.
func TestGeneratedLiveValues(t *testing.T) {
	bin := buildFixture(t, "testdata/live")
	for _, name := range []string{"TestLiveTextValueIsEscaped", "TestLiveSafeHTMLValue", "TestBlockValueIsEscapedMarkup", "TestRawOutputWritesAnyValue", "TestScalarWritesTakeText"} {
		t.Run(name, func(t *testing.T) { runFixture(t, bin, "-test.run", "^"+name+"$") })
	}
}

// buildFixture copies the project in dir, generates it as module
// example.com/app built against this repository, and compiles the tests of its
// root package into the binary it returns.
func buildFixture(t *testing.T, dir string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(t.TempDir(), "app")
	if err := os.CopyFS(app, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}

	g := generator.New(&config.Config{Dir: app, WebDir: filepath.Join(app, "web"), WebPackage: "example.com/app/web"})
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	goMod := fmt.Sprintf("module example.com/app\n\ngo 1.25.0\n\nrequire github.com/sergei-svistunov/go-ssr v0.0.0\n\nreplace github.com/sergei-svistunov/go-ssr => %s\n", repoRoot)
	if err := os.WriteFile(filepath.Join(app, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(app, "app.test")
	for _, args := range [][]string{{"mod", "tidy"}, {"test", "-c", "-o", bin, "."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = app
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v:\n%s", args, out)
		}
	}
	return bin
}

// runFixture runs a binary from buildFixture with args.
func runFixture(t *testing.T, bin string, args ...string) {
	t.Helper()
	if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("%v:\n%s", args, out)
	}
}
