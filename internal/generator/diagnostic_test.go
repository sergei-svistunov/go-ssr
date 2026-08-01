package generator_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

// A project with mistakes in two routes must report both, so they can be fixed
// in one pass instead of one regeneration per mistake.
func TestAnalyze_CollectsOneDiagnosticPerRoute(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><ssr:content/></body></html>`)
	writeTemplate(t, webDir, "users", `<div>
  <ssr:var name="count"/>
</div>`)
	writeTemplate(t, webDir, "contact", `<div>
  <ssr:input name="email"/>
</div>`)

	g := makeGen(t, webDir)
	err := g.Analyze()
	if err == nil {
		t.Fatal("Analyze: expected diagnostics, got nil")
	}

	diags := generator.DiagnosticsOf(err)
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %v", len(diags), err)
	}

	byRoute := map[string]generator.Diagnostic{}
	for _, d := range diags {
		byRoute[d.Route] = d
	}

	users, ok := byRoute["/users"]
	if !ok {
		t.Fatalf("no diagnostic for /users: %+v", diags)
	}
	if users.Line != 2 {
		t.Errorf("/users line = %d, want 2", users.Line)
	}
	if !strings.Contains(users.Message, "type attribute") {
		t.Errorf("/users message = %q, want it to mention the missing type attribute", users.Message)
	}
	if !strings.HasSuffix(filepath.ToSlash(users.File), "pages/users/index.html") {
		t.Errorf("/users file = %q, want it to point at the route template", users.File)
	}

	contact, ok := byRoute["/contact"]
	if !ok {
		t.Fatalf("no diagnostic for /contact: %+v", diags)
	}
	if contact.Line != 2 {
		t.Errorf("/contact line = %d, want 2", contact.Line)
	}
	if !strings.Contains(contact.Message, "<ssr:form>") {
		t.Errorf("/contact message = %q, want it to mention the missing form", contact.Message)
	}
}

// Every parse failure must carry a position: a bare sentence forces the reader
// to search the pages tree for the offending line.
func TestAnalyze_ParseErrorsCarryPositions(t *testing.T) {
	cases := []struct {
		name     string
		template string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "unknown tag",
			template: "<div>\n<ssr:unknown/>\n</div>",
			wantLine: 2,
			wantMsg:  "unknown GoSSR tag",
		},
		{
			name:     "unknown attribute",
			template: "<div>\n\n<p ssr:whatever=\"1\">x</p>\n</div>",
			wantLine: 3,
			wantMsg:  "unknown GoSSR attribute",
		},
		{
			name:     "misplaced else",
			template: "<div>\n<p ssr:else>x</p>\n</div>",
			wantLine: 2,
			wantMsg:  "must directly follow an element with ssr:if",
		},
		{
			name:     "bad loop expression",
			template: "<ul>\n<li ssr:for=\"phones\">x</li>\n</ul>",
			wantLine: 2,
			wantMsg:  "invalid ssr:for expression",
		},
		{
			name:     "unknown gotype",
			template: "<ssr:form name=\"f\">\n<ssr:input name=\"a\" gotype=\"complex128\"/>\n</ssr:form>",
			wantLine: 2,
			wantMsg:  "unknown gotype",
		},
		{
			name:     "missing var name",
			template: "<div>\n<ssr:var type=\"string\"/>\n</div>",
			wantLine: 2,
			wantMsg:  "missing the required name attribute",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			webDir := filepath.Join(t.TempDir(), "web")
			writeTemplate(t, webDir, ".", tc.template)

			g := makeGen(t, webDir)
			err := g.Analyze()
			if err == nil {
				t.Fatal("Analyze: expected a diagnostic, got nil")
			}

			diags := generator.DiagnosticsOf(err)
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(diags), err)
			}
			d := diags[0]
			if d.Line != tc.wantLine {
				t.Errorf("line = %d, want %d (%s)", d.Line, tc.wantLine, d.Error())
			}
			if !strings.Contains(d.Message, tc.wantMsg) {
				t.Errorf("message = %q, want substring %q", d.Message, tc.wantMsg)
			}
			if d.File == "" {
				t.Error("file is empty, want the template path")
			}
		})
	}
}

// A project whose pages directory is missing must be reported, not crash: a
// panic here would take down a caller that stays alive across many projects.
func TestAnalyze_MissingPagesDirectoryIsAnError(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	if err := os.MkdirAll(webDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err := makeGen(t, webDir).Analyze()
	if err == nil {
		t.Fatal("Analyze: expected an error for a missing pages directory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should say what is missing: %v", err)
	}
}

// A directory that only holds child routes has no template of its own. Treating
// it as a route puts an entry in the generated handler for a package with no Go
// files, and the project stops compiling.
func TestAnalyze_DirectoryWithoutTemplateIsNotARoute(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><ssr:content/></body></html>`)
	writeTemplate(t, webDir, "users/_userId_", `<div>user</div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	got := g.RoutePaths()
	want := []string{"/", "/users/_userId_"}
	if !slices.Equal(got, want) {
		t.Errorf("routes = %v, want %v", got, want)
	}
}

// Reactive binding failures are reported with their code so a caller can react
// to the class of problem, not just the prose.
func TestGenerate_ReactiveDiagnosticsCarryCodes(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body>
<ssr:var name="count" type="int" reactive="true"/>
<input ssr:bind="count"/>
</body></html>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	err := g.Generate()
	if err == nil {
		t.Fatal("Generate: expected a diagnostic, got nil")
	}

	diags := generator.DiagnosticsOf(err)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %v", len(diags), err)
	}
	if diags[0].Code != "E05" {
		t.Errorf("code = %q, want E05 (%s)", diags[0].Code, diags[0].Error())
	}
	if diags[0].Line != 3 {
		t.Errorf("line = %d, want 3", diags[0].Line)
	}
	if !strings.HasSuffix(filepath.ToSlash(diags[0].File), "pages/index.html") {
		t.Errorf("file = %q, want the route template path", diags[0].File)
	}
}

func TestDiagnostic_ErrorFormat(t *testing.T) {
	d := &generator.Diagnostic{File: "internal/web/pages/users/index.html", Line: 14, Col: 1, Message: "boom"}
	if got, want := d.Error(), "internal/web/pages/users/index.html:14:1: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	noPos := &generator.Diagnostic{Message: "boom"}
	if got := noPos.Error(); got != "boom" {
		t.Errorf("Error() = %q, want %q", got, "boom")
	}
}
