package mcp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fixtureProject copies testdata/fixture-app into a temporary directory so tests
// can generate into it without touching the repository.
func fixtureProject(t *testing.T) string {
	t.Helper()

	dst := t.TempDir()
	src := filepath.Join("testdata", "fixture-app")

	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}

func TestDocsTool_ReturnsTopicAndSection(t *testing.T) {
	res, out, err := handleDocs(context.Background(), nil, docsInput{Topic: "template-syntax"})
	if err != nil {
		t.Fatalf("handleDocs: %v", err)
	}
	if out.Title == "" {
		t.Error("title is empty")
	}
	if !strings.Contains(out.Content, "<ssr:var>") {
		t.Error("template-syntax does not mention <ssr:var>")
	}
	if res.IsError {
		t.Error("IsError set on a successful call")
	}

	_, sec, err := handleDocs(context.Background(), nil, docsInput{Topic: "expressions", Section: "Operators"})
	if err != nil {
		t.Fatalf("handleDocs section: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(sec.Content), "## Operators") {
		t.Errorf("section content starts with %q, want the Operators heading", firstLine(sec.Content))
	}
	if strings.Contains(sec.Content, "## Literals") {
		t.Error("section leaked into the following section")
	}
}

func TestDocsTool_UnknownTopicNamesTheAlternatives(t *testing.T) {
	_, _, err := handleDocs(context.Background(), nil, docsInput{Topic: "does-not-exist"})
	if err == nil {
		t.Fatal("expected an error for an unknown topic")
	}
	if !strings.Contains(err.Error(), "template-syntax") {
		t.Errorf("error %q should list the available topics", err)
	}

	if _, _, err := handleDocs(context.Background(), nil, docsInput{Topic: "forms", Section: "nope"}); err == nil {
		t.Fatal("expected an error for an unknown section")
	}
}

func TestSearchDocsTool(t *testing.T) {
	_, out, err := handleSearchDocs(context.Background(), nil, searchDocsInput{Query: "ssr:bind", Limit: 5})
	if err != nil {
		t.Fatalf("handleSearchDocs: %v", err)
	}
	if len(out.Hits) == 0 {
		t.Fatal("no hits for ssr:bind")
	}
	if len(out.Hits) > 5 {
		t.Errorf("got %d hits, limit was 5", len(out.Hits))
	}
	for _, h := range out.Hits {
		if _, err := findTopic(h.Topic); err != nil {
			t.Errorf("hit names topic %q which does not resolve", h.Topic)
		}
	}

	_, empty, err := handleSearchDocs(context.Background(), nil, searchDocsInput{Query: "zzzznotpresent"})
	if err != nil {
		t.Fatalf("handleSearchDocs: %v", err)
	}
	if len(empty.Hits) != 0 {
		t.Errorf("expected no hits, got %d", len(empty.Hits))
	}
}

func TestRoutesTool_DescribesTheFixture(t *testing.T) {
	dir := fixtureProject(t)

	res, out, err := handleRoutes(context.Background(), nil, routesInput{ProjectDir: dir})
	if err != nil {
		t.Fatalf("handleRoutes: %v", err)
	}
	if !out.OK {
		t.Fatalf("fixture reported diagnostics: %+v", out.Diagnostics)
	}

	byPath := map[string]int{}
	for i, r := range out.Inventory.Routes {
		byPath[r.Path] = i
	}
	for _, want := range []string{"/", "/users", "/users/_userId_", "/contact"} {
		if _, ok := byPath[want]; !ok {
			t.Fatalf("route %s missing from inventory: %+v", want, byPath)
		}
	}

	root := out.Inventory.Routes[byPath["/"]]
	if root.ContentDefault != "/users" {
		t.Errorf("root ContentDefault = %q, want /users", root.ContentDefault)
	}

	users := out.Inventory.Routes[byPath["/users"]]
	if !users.Reactive {
		t.Error("/users should be reactive")
	}
	for _, want := range []string{"Data", "DefaultRoute", "Subscribe", "ValidateCount"} {
		if !slices.Contains(users.DataProviderMethods, want) {
			t.Errorf("/users methods %v missing %s", users.DataProviderMethods, want)
		}
	}

	param := out.Inventory.Routes[byPath["/users/_userId_"]]
	if !slices.Equal(param.URLParams, []string{"userId"}) {
		t.Errorf("/users/_userId_ URLParams = %v", param.URLParams)
	}

	contact := out.Inventory.Routes[byPath["/contact"]]
	if len(contact.Forms) != 1 || contact.Forms[0].InitMethod != "InitContact" {
		t.Fatalf("/contact forms = %+v", contact.Forms)
	}
	fields := map[string]string{}
	for _, f := range contact.Forms[0].Fields {
		fields[f.Name] = f.GoType + "/" + f.Kind
	}
	if fields["age"] != "uint8/input" || fields["message"] != "string/textarea" {
		t.Errorf("form fields = %v", fields)
	}

	// The WebSocket endpoint sits on the leaf below the reactive route.
	if !slices.Contains(out.Inventory.WSEndpoints, "/users/_userId_") {
		t.Errorf("WSEndpoints = %v, want /users/_userId_", out.Inventory.WSEndpoints)
	}

	summary := resultText(t, res)
	if !strings.Contains(summary, "/users/_userId_/__ws") {
		t.Errorf("text summary should name the websocket endpoint:\n%s", summary)
	}
}

func TestRoutesTool_FiltersToOneRoute(t *testing.T) {
	dir := fixtureProject(t)

	_, out, err := handleRoutes(context.Background(), nil, routesInput{ProjectDir: dir, RoutePath: "/contact"})
	if err != nil {
		t.Fatalf("handleRoutes: %v", err)
	}
	if len(out.Inventory.Routes) != 1 || out.Inventory.Routes[0].Path != "/contact" {
		t.Fatalf("expected only /contact, got %+v", out.Inventory.Routes)
	}
}

func TestValidateTool(t *testing.T) {
	dir := fixtureProject(t)

	res, out, err := handleValidate(context.Background(), nil, projectInput{ProjectDir: dir})
	if err != nil {
		t.Fatalf("handleValidate: %v", err)
	}
	if !out.OK || len(out.Diagnostics) != 0 {
		t.Fatalf("clean fixture reported %+v", out.Diagnostics)
	}
	if res.IsError {
		t.Error("IsError set for a clean project")
	}
	if !out.AssetsStale {
		t.Error("a project that never ran webpack should report stale assets")
	}

	// Break two routes in different ways.
	write(t, filepath.Join(dir, "internal", "web", "pages", "contact", "index.html"),
		"<div>\n<ssr:var name=\"x\"/>\n</div>")
	write(t, filepath.Join(dir, "internal", "web", "pages", "users", "_userId_", "index.html"),
		"<ssr:var name=\"n\" type=\"string\"/>\n<p ssr:nope=\"1\">{{ n }}</p>")

	res, out, err = handleValidate(context.Background(), nil, projectInput{ProjectDir: dir})
	if err != nil {
		t.Fatalf("handleValidate: %v", err)
	}
	if out.OK {
		t.Fatal("expected diagnostics after breaking two routes")
	}
	if !res.IsError {
		t.Error("a project with diagnostics should come back as a tool error")
	}
	if len(out.Diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(out.Diagnostics), out.Diagnostics)
	}
	for _, d := range out.Diagnostics {
		if d.File == "" || d.Line == 0 {
			t.Errorf("diagnostic without a location: %+v", d)
		}
		if filepath.IsAbs(d.File) {
			t.Errorf("diagnostic file %q should be relative to the project", d.File)
		}
	}
}

func TestGenerateTool_WritesStubsAndCanSkipAssets(t *testing.T) {
	dir := fixtureProject(t)

	res, out, err := handleGenerate(context.Background(), nil, generateInput{ProjectDir: dir, Assets: "skip"})
	if err != nil {
		t.Fatalf("handleGenerate: %v", err)
	}
	if !out.OK {
		t.Fatalf("generation failed: %+v (%s)", out.Diagnostics, out.Output)
	}
	if res.IsError {
		t.Error("IsError set on a successful generation")
	}
	if out.WebpackRan {
		t.Error("assets=skip must not run webpack")
	}
	if len(out.Routes) != 4 {
		t.Errorf("Routes = %v, want 4 routes", out.Routes)
	}
	if len(out.StubsCreated) != 4 {
		t.Errorf("StubsCreated = %v, want one per route", out.StubsCreated)
	}

	for _, rel := range []string{
		"internal/web/pages/ssrhandler_gen.go",
		"internal/web/pages/ssrroute_gen.go",
		"internal/web/pages/dataprovider.go",
		"internal/web/pages/users/ssrroute_gen.go",
		"internal/web/pages/users/__ssr_gen__.ts",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}

	// A second run creates no new stubs and leaves the hand-written provider alone.
	dp := filepath.Join(dir, "internal", "web", "pages", "users", "dataprovider.go")
	write(t, dp, "package users\n\n// hand written\n")

	_, out, err = handleGenerate(context.Background(), nil, generateInput{ProjectDir: dir, Assets: "skip"})
	if err != nil {
		t.Fatalf("handleGenerate: %v", err)
	}
	if len(out.StubsCreated) != 0 {
		t.Errorf("second run created stubs again: %v", out.StubsCreated)
	}
	if got := read(t, dp); !strings.Contains(got, "hand written") {
		t.Error("generation overwrote an existing dataprovider.go")
	}
}

func TestGenerateTool_ReportsDiagnosticsInsteadOfWriting(t *testing.T) {
	dir := fixtureProject(t)
	write(t, filepath.Join(dir, "internal", "web", "pages", "contact", "index.html"),
		"<ssr:form name=\"a\">\n<ssr:input name=\"x\" gotype=\"complex128\"/>\n</ssr:form>")

	res, out, err := handleGenerate(context.Background(), nil, generateInput{ProjectDir: dir, Assets: "skip"})
	if err != nil {
		t.Fatalf("handleGenerate: %v", err)
	}
	if out.OK || len(out.Diagnostics) == 0 {
		t.Fatal("expected diagnostics for an unknown gotype")
	}
	if !res.IsError {
		t.Error("expected a tool error")
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "web", "pages", "ssrhandler_gen.go")); err == nil {
		t.Error("nothing should be generated while a template is broken")
	}
}

func TestGenerateTool_RejectsUnknownAssetPolicy(t *testing.T) {
	dir := fixtureProject(t)
	if _, _, err := handleGenerate(context.Background(), nil, generateInput{ProjectDir: dir, Assets: "sometimes"}); err == nil {
		t.Fatal("expected an error for an unknown assets policy")
	}
}

func TestScaffoldRouteTool(t *testing.T) {
	dir := fixtureProject(t)

	_, out, err := handleScaffoldRoute(context.Background(), nil, scaffoldRouteInput{
		ProjectDir: dir,
		RoutePath:  "users/_userId_/orders",
		WithTs:     true,
		WithScss:   true,
		Assets:     "skip",
	})
	if err != nil {
		t.Fatalf("handleScaffoldRoute: %v", err)
	}
	if !out.OK {
		t.Fatalf("scaffolding reported %+v (%s)", out.Diagnostics, out.Output)
	}
	if out.RoutePath != "/users/_userId_/orders" {
		t.Errorf("RoutePath = %q", out.RoutePath)
	}
	if !slices.Equal(out.URLParams, []string{"userId"}) {
		t.Errorf("URLParams = %v", out.URLParams)
	}

	for _, rel := range []string{
		"internal/web/pages/users/_userId_/orders/index.html",
		"internal/web/pages/users/_userId_/orders/index.ts",
		"internal/web/pages/users/_userId_/orders/styles.scss",
	} {
		if !slices.Contains(out.Created, rel) {
			t.Errorf("Created = %v, missing %s", out.Created, rel)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s not written: %v", rel, err)
		}
	}

	if out.DataProvider == "" {
		t.Error("no dataprovider.go reported")
	}
	if !slices.Contains(out.DataProviderMethods, "Data") {
		t.Errorf("DataProviderMethods = %v", out.DataProviderMethods)
	}

	// Scaffolding again must not clobber the route.
	if _, _, err := handleScaffoldRoute(context.Background(), nil, scaffoldRouteInput{
		ProjectDir: dir, RoutePath: "/users/_userId_/orders", Assets: "skip",
	}); err == nil {
		t.Error("expected an error when the route already exists")
	}
}

func TestScaffoldRouteTool_LayoutGetsContentSlot(t *testing.T) {
	dir := fixtureProject(t)

	_, out, err := handleScaffoldRoute(context.Background(), nil, scaffoldRouteInput{
		ProjectDir: dir, RoutePath: "/admin", HasChildren: true, Assets: "skip",
	})
	if err != nil {
		t.Fatalf("handleScaffoldRoute: %v", err)
	}
	body := read(t, filepath.Join(dir, "internal", "web", "pages", "admin", "index.html"))
	if !strings.Contains(body, "<ssr:content/>") {
		t.Errorf("a layout route should get a content slot:\n%s", body)
	}
	if !slices.Contains(out.DataProviderMethods, "DefaultRoute") {
		t.Errorf("a content slot without a default requires DefaultRoute, got %v", out.DataProviderMethods)
	}
}

func TestScaffoldRouteTool_RejectsUnsafePaths(t *testing.T) {
	dir := fixtureProject(t)

	cases := map[string]string{
		"traversal":          "/users/../../etc",
		"reserved ws":        "/__ws",
		"reserved embed":     "/static_embed",
		"nested reserved":    "/users/__ws/x",
		"empty":              "   ",
		"root":               "/",
		"backslash":          `users\orders`,
		"invalid characters": "/users/a:b",
		// A directory name is also a Go package name and an import alias.
		"hyphen":        "/user-profile",
		"leading digit": "/2fa",
		"keyword":       "/range",
		// The router only captures a parameter of the form _name_, so a segment
		// with an inner underscore would silently be matched literally.
		"parameter with an inner underscore": "/users/_user_id_",
	}

	for name, routePath := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := handleScaffoldRoute(context.Background(), nil, scaffoldRouteInput{
				ProjectDir: dir, RoutePath: routePath,
			})
			if err == nil {
				t.Fatalf("routePath %q was accepted", routePath)
			}
		})
	}

	// Nothing was created outside pages/.
	if _, err := os.Stat(filepath.Join(dir, "internal", "web", "pages", "__ws")); err == nil {
		t.Error("a reserved directory was created")
	}
	// A rejected path must not leave a directory behind either: the generator
	// would fail on it from then on, for every later run.
	if _, err := os.Stat(filepath.Join(dir, "internal", "web", "pages", "user-profile")); err == nil {
		t.Error("a rejected route directory was created anyway")
	}
}

func TestRoutesTool_ReportsABrokenRouteInsteadOfCallingItUnknown(t *testing.T) {
	dir := fixtureProject(t)
	write(t, filepath.Join(dir, "internal", "web", "pages", "users", "index.html"),
		`<ssr:var name="broken"/>`)

	res, out, err := handleRoutes(context.Background(), nil, routesInput{ProjectDir: dir, RoutePath: "/users"})
	if err != nil {
		t.Fatalf("a route whose template does not parse must not come back as an unknown route: %v", err)
	}
	if !res.IsError || out.OK {
		t.Error("a broken route must be reported as a failure")
	}
	if len(out.Diagnostics) == 0 {
		t.Fatal("the diagnostic explaining the breakage was dropped")
	}
	if !strings.Contains(out.Diagnostics[0].Message, "type attribute") {
		t.Errorf("unexpected diagnostic: %+v", out.Diagnostics[0])
	}

	// The whole-project listing must flag the failure too, not just the payload.
	res, _, err = handleRoutes(context.Background(), nil, routesInput{ProjectDir: dir})
	if err != nil {
		t.Fatalf("handleRoutes: %v", err)
	}
	if !res.IsError {
		t.Error("a project with a broken template must not report a successful call")
	}
}

func TestScaffoldRouteTool_ReportsCreatedFilesWhenRegenerationFails(t *testing.T) {
	dir := fixtureProject(t)
	// Another route is already broken, so the regeneration that follows the
	// scaffolding cannot succeed. The new route's files exist by then, and a
	// caller that is not told so would retry and be refused.
	write(t, filepath.Join(dir, "internal", "web", "pages", "contact", "index.html"),
		`<ssr:var name="broken"/>`)

	res, out, err := handleScaffoldRoute(context.Background(), nil, scaffoldRouteInput{
		ProjectDir: dir, RoutePath: "/admin", Assets: "skip",
	})
	if err != nil {
		t.Fatalf("handleScaffoldRoute: %v", err)
	}
	if !res.IsError || out.OK {
		t.Error("a failed regeneration must be reported as a failure")
	}
	if !slices.Contains(out.Created, "internal/web/pages/admin/index.html") {
		t.Errorf("the caller must be told which files exist now, got %v", out.Created)
	}
}

func TestProjectTools_ReportAMissingProjectClearly(t *testing.T) {
	empty := t.TempDir()

	_, _, err := handleRoutes(context.Background(), nil, routesInput{ProjectDir: empty})
	if err == nil {
		t.Fatal("expected an error outside a project")
	}
	if !strings.Contains(err.Error(), "gossr.yaml") || !strings.Contains(err.Error(), "gossr_init") {
		t.Errorf("error should name the config and the way out: %v", err)
	}
}

func TestInitTool_ValidatesInput(t *testing.T) {
	if _, _, err := handleInit(context.Background(), nil, initInput{ModuleName: "example.com/x"}); err == nil {
		t.Error("expected an error without dir")
	}
	if _, _, err := handleInit(context.Background(), nil, initInput{Dir: t.TempDir()}); err == nil {
		t.Error("expected an error without moduleName")
	}

	// A non-empty directory is refused before anything is written.
	dir := t.TempDir()
	write(t, filepath.Join(dir, "README.md"), "hello")
	if _, _, err := handleInit(context.Background(), nil, initInput{Dir: dir, ModuleName: "example.com/x"}); err == nil {
		t.Error("expected an error for a non-empty directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "gossr.yaml")); err == nil {
		t.Error("nothing should be written into a non-empty directory")
	}
}

func TestCleanRoutePath(t *testing.T) {
	cases := []struct {
		in, canonical, relative string
	}{
		{"users", "/users", "users"},
		{"/users/", "/users", "users"},
		{"/users/_userId_/orders", "/users/_userId_/orders", "users/_userId_/orders"},
		{"users//orders", "/users/orders", "users/orders"},
		{"./users/./orders", "/users/orders", "users/orders"},
	}
	for _, tc := range cases {
		canonical, relative, err := cleanRoutePath(tc.in)
		if err != nil {
			t.Errorf("cleanRoutePath(%q): %v", tc.in, err)
			continue
		}
		if canonical != tc.canonical || relative != tc.relative {
			t.Errorf("cleanRoutePath(%q) = %q, %q; want %q, %q", tc.in, canonical, relative, tc.canonical, tc.relative)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}
	return tc.Text
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
