package generator_test

import (
	"fmt"
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
			name:     "unknown attribute on ssr:form",
			template: "<div>\n\n<ssr:form name=\"f\" ssr:for=\"x in xs\"></ssr:form>\n</div>",
			wantLine: 3,
			wantMsg:  "unknown GoSSR attribute \"ssr:for\" on <ssr:form>",
		},
		{
			name:     "form in a live block",
			template: "<ssr:var name=\"on\" type=\"bool\" reactive=\"true\"/>\n<div>\n<ssr:form name=\"f\" ssr:if=\"on\"></ssr:form>\n</div>",
			wantLine: 3,
			wantMsg:  "a live update cannot render a form",
		},
		{
			name:     "reserved loop item",
			template: "<ssr:var name=\"xs\" type=\"[]int\"/>\n<ul>\n<li ssr:for=\"render in xs\">x</li>\n</ul>",
			wantLine: 3,
			wantMsg:  `the ssr:for item "render" is a name the generated code uses; pick another name`,
		},
		{
			name:     "form read in a live site",
			template: "<ssr:var name=\"on\" type=\"bool\" reactive=\"true\"/>\n<ssr:form name=\"f\">\n<p ssr:if=\"on\">{{ form.GetError() }}</p>\n</ssr:form>",
			wantLine: 3,
			wantMsg:  "a live update has no form values",
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
	d := &generator.Diagnostic{File: "internal/web/pages/users/index.html", Line: 14, Message: "boom"}
	if got, want := d.Error(), "internal/web/pages/users/index.html:14: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	coded := &generator.Diagnostic{File: "pages/index.html", Line: 3, Code: "E09", Message: "boom", Fix: "do this"}
	if got, want := coded.Error(), "pages/index.html:3: boom (E09)\n\tfix: do this"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	noPos := &generator.Diagnostic{Message: "boom"}
	if got := noPos.Error(); got != "boom" {
		t.Errorf("Error() = %q, want %q", got, "boom")
	}
}

// Every refusal of an unsafe context reaches the caller as a diagnostic with its
// code, its line and its fix, and every broken route is reported in one run.
func TestForbiddenContexts(t *testing.T) {
	cases := []struct {
		template string
		code     string
		line     int
		fix      string
	}{
		{"<div>\n<script>{{ x }}</script>\n</div>", "E08", 2, "pass the value through a data-* attribute and read it in the page's TypeScript"},
		{"<div>\n<a onclick=\"go()\">x</a>\n</div>", "E09", 2, "remove the attribute and add the listener in the page's TypeScript"},
		{"<div>\n\n<p style=\"color:red\">x</p>\n</div>", "E10", 3, "move the style into a stylesheet and switch classes instead"},
		{"<div>\n<a class={{ x }}>x</a>\n</div>", "E11", 2, `quote the value: class="{{ … }}"`},
		{"<div>\n<noscript>\n{{ x }}</noscript>\n</div>", "E12", 3, "move the value out of <noscript>; its text is shown undecoded"},
		{"<div>\n<a title=\"{{$ x }}\">x</a>\n</div>", "E13", 2, "use {{ }}, which escapes the value"},
		{"<div>\n<title>a\n\n{{$ x }}</title>\n</div>", "E13", 4, "use {{ }}, which escapes the value"},
		{"<div>\n<a href=\"javascript:alert(1)\">x</a>\n</div>", "E14", 2, "link to a page, or handle the click in the page's TypeScript"},
		{"<div>\n\n<iframe src=\"data:text/html,{{ x }}\"></iframe>\n</div>", "E14", 3, "link to a page, or handle the click in the page's TypeScript"},
		{"<div>\n<a href=\"{{ base }}/x\">x</a>\n</div>", "E15", 2, "start the attribute with a fixed scheme or path, or put the whole URL in one value"},
		{"<div>\n<script>\nalert(1)\n</script>\n</div>", "E16", 2, "move it into an asset next to the template; the Content-Security-Policy blocks inline code"},
		{"<div>\n\n<style></style>\n</div>", "E16", 3, "move it into an asset next to the template; the Content-Security-Policy blocks inline code"},
		{"<div>\n<a {{ x }}=\"1\">x</a>\n</div>", "E17", 2, "write the name literally"},
		{"<div>\n<script src=\"{{ x }}\"></script>\n</div>", "E18", 2, "write the attribute literally, or remove it"},
		{"<div>\n<iframe srcdoc=\"<p>hi</p>\"></iframe>\n</div>", "E18", 2, "write the attribute literally, or remove it"},
	}

	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><ssr:content default="r0"/></body></html>`)
	for i, c := range cases {
		writeTemplate(t, webDir, fmt.Sprintf("r%d", i), c.template)
	}

	g := makeGen(t, webDir)
	err := g.Analyze()
	if err == nil {
		err = g.Generate()
	}
	diags := generator.DiagnosticsOf(err)
	if len(diags) != len(cases) {
		t.Fatalf("got %d diagnostics, want %d: %v", len(diags), len(cases), err)
	}
	byRoute := map[string]generator.Diagnostic{}
	for _, d := range diags {
		byRoute[d.Route] = d
	}
	for i, c := range cases {
		route := fmt.Sprintf("/r%d", i)
		d, ok := byRoute[route]
		if !ok {
			t.Errorf("%s: no diagnostic", route)
			continue
		}
		if d.Code != c.code || d.Line != c.line || d.Fix != c.fix {
			t.Errorf("%s: %s %d %q, want %s %d %q", route, d.Code, d.Line, d.Fix, c.code, c.line, c.fix)
		}
		if want := fmt.Sprintf("pages/r%d/index.html", i); !strings.HasSuffix(filepath.ToSlash(d.File), want) {
			t.Errorf("%s: file %q, want %s", route, d.File, want)
		}
		if want := fmt.Sprintf(":%d: %s (%s)\n\tfix: %s", c.line, d.Message, c.code, c.fix); !strings.HasSuffix(d.Error(), want) {
			t.Errorf("%s: Error() = %q, want it to end with %q", route, d.Error(), want)
		}
	}
}
