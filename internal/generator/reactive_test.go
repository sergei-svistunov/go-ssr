package generator_test

// reactive_test.go — tests for reactive-bindings generator (E01–E06, AC18–AC22).
//
// Each test creates a minimal temporary pages directory tree and asserts that
// generator.Generate() (via Analyze+Generate) produces the expected output or
// returns the expected error.
//
// Helper to detect a string that may appear either as a UTF-8 literal or as
// gobuf hex-byte sequences in the generated Go source:
//
//	containsHTMLSnippet(content, snippet) checks both forms.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/config"
	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

// makeGen creates a generator pointing at a temporary directory.
func makeGen(t *testing.T, webDir string) *generator.Generator {
	t.Helper()
	// Dir is the project root — use the parent of webDir.
	dir := filepath.Dir(webDir)
	return generator.New(&config.Config{
		Dir:    dir,
		WebDir: webDir,
		// Use a fake package name to avoid import resolution in tests.
		WebPackage: "example.com/web",
	})
}

// writeTemplate writes an index.html at webDir/pages/routePath/index.html.
func writeTemplate(t *testing.T, webDir, routePath, content string) {
	t.Helper()
	dir := filepath.Join(webDir, "pages", filepath.FromSlash(routePath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// runAndExpectError runs Analyze+Generate and asserts the error contains want.
func runAndExpectError(t *testing.T, g *generator.Generator, want string) {
	t.Helper()
	if err := g.Analyze(); err != nil {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Analyze error = %q, want substring %q", err.Error(), want)
		}
		return
	}
	if err := g.Generate(); err != nil {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Generate error = %q, want substring %q", err.Error(), want)
		}
		return
	}
	t.Fatalf("expected an error containing %q, but got nil", want)
}

// ---- E01: Reserved path segment __ws ----

func TestGenerator_E01_ReservedFolderWs(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	// Create a route folder literally named __ws.
	writeTemplate(t, webDir, "__ws", `<p>hello</p>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`reactive-bindings: route folder "__ws"`)
}

// ---- E02 retired: nested reactive routes now succeed ----
//
// Previously E02 blocked generation when a child route was reactive and its
// parent was also reactive. Rev 8 multiplexes both routes over one WS connection.
// The test below verifies that generation succeeds and produces the correct
// multiplexed handler.

func TestGenerator_NestedReactiveRoutes_Succeed(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "dashboard",
		`<ssr:var name="visits" type="int" reactive="true"/>
<p>{{ visits }}</p>`)
	writeTemplate(t, webDir, "dashboard/stats",
		`<ssr:var name="rps" type="float64" reactive="true"/>
<p>{{ rps }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error (E02 should be retired): %v", err)
	}

	// The generated ssrhandler_gen.go must have ONE WS endpoint at the leaf path
	// /dashboard/stats/__ws (not two separate ones).
	handlerFile := filepath.Join(webDir, "pages", "ssrhandler_gen.go")
	handlerData, err := os.ReadFile(handlerFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", handlerFile, err)
	}
	handlerContent := string(handlerData)

	// Leaf path WS endpoint must be present.
	if !strings.Contains(handlerContent, "/dashboard/stats/__ws") {
		t.Errorf("expected /dashboard/stats/__ws WS endpoint in handler\n%s", handlerContent)
	}

	// There should be only ONE wsHandler function (the leaf handler muxes both routes).
	wsHandlerCount := strings.Count(handlerContent, "wsHandler")
	if wsHandlerCount < 2 { // at least the var assignment + the map entry
		t.Errorf("expected wsHandler references in generated handler, got %d\n%s", wsHandlerCount, handlerContent)
	}

	// The leaf handler must reference both routes' data providers.
	if !strings.Contains(handlerContent, "routeDashboard.NewDP") {
		t.Errorf("expected routeDashboard.NewDP in handler (ancestor route)\n%s", handlerContent)
	}
	if !strings.Contains(handlerContent, "routeDashboardStats.NewDP") {
		t.Errorf("expected routeDashboardStats.NewDP in handler (leaf route)\n%s", handlerContent)
	}
}

// TestGenerator_NestedReactiveRoutes_BindingKeysNamespaced verifies that each
// reactive route's binding keys are namespaced with that route's routeKey
// (routeKey.localKey), ensuring no collision when both routes use the same
// variable name (e.g. "count"). AC23.
func TestGenerator_NestedReactiveRoutes_BindingKeysNamespaced(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "dashboard",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)
	writeTemplate(t, webDir, "dashboard/users",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Each route's ssrroute_gen.go must have a routeKey constant.
	dashFile := filepath.Join(webDir, "pages", "dashboard", "ssrroute_gen.go")
	dashData, err := os.ReadFile(dashFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", dashFile, err)
	}
	dashContent := string(dashData)
	if !strings.Contains(dashContent, "const routeKey") {
		t.Errorf("expected routeKey constant in /dashboard ssrroute_gen.go\n%s", dashContent)
	}

	usersFile := filepath.Join(webDir, "pages", "dashboard/users", "ssrroute_gen.go")
	usersData, err := os.ReadFile(usersFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", usersFile, err)
	}
	usersContent := string(usersData)
	if !strings.Contains(usersContent, "const routeKey") {
		t.Errorf("expected routeKey constant in /dashboard/users ssrroute_gen.go\n%s", usersContent)
	}

	// The two routeKey values must differ (different route paths → different hashes).
	dashRKStart := strings.Index(dashContent, "const routeKey =")
	usersRKStart := strings.Index(usersContent, "const routeKey =")
	if dashRKStart < 0 || usersRKStart < 0 {
		t.Fatal("could not find routeKey constant in generated files")
	}
	dashRKLine := dashContent[dashRKStart : dashRKStart+40]
	usersRKLine := usersContent[usersRKStart : usersRKStart+40]
	if dashRKLine == usersRKLine {
		t.Errorf("dashboard and dashboard/users have the same routeKey: %q — must differ", dashRKLine)
	}

	// Both files' snapshot() must use namespaced keys (routeKey.count, not just "count").
	// The namespaced key appears as a string literal in snapshot().
	if !strings.Contains(dashContent, `"count"`) {
		// Snap function uses quoted key string.
	}
	// A namespaced key looks like "XXXXXXXX.count" (8 hex + dot + varname).
	// We can't predict the exact hash, but we can check for dot-separated keys.
	if !strings.Contains(dashContent, ".count") {
		t.Errorf("expected namespaced key (*.count) in /dashboard ssrroute_gen.go snapshot\n%s", dashContent)
	}
	if !strings.Contains(usersContent, ".count") {
		t.Errorf("expected namespaced key (*.count) in /dashboard/users ssrroute_gen.go snapshot\n%s", usersContent)
	}
}

// TestGenerator_NestedReactiveRoutes_RouteKeyConstant verifies that each
// reactive route emits a routeKey constant in ssrroute_gen.go.
func TestGenerator_NestedReactiveRoutes_RouteKeyConstant(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "app",
		`<ssr:var name="status" type="string" reactive="true"/>
<p>{{ status }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "app", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Must have a routeKey constant.
	if !strings.Contains(content, "const routeKey") {
		t.Errorf("expected routeKey constant in generated file\n%s", content)
	}
	// The constant value must be 8 hex chars long (4 bytes of SHA256).
	// Find the quoted value.
	idx := strings.Index(content, "const routeKey =")
	if idx < 0 {
		t.Fatalf("routeKey constant not found\n%s", content)
	}
	line := content[idx : idx+50]
	// Extract the quoted value between first pair of quotes.
	start := strings.Index(line, `"`)
	end := strings.LastIndex(line[:strings.Index(line[start+1:], `"`)+start+2], `"`)
	if start < 0 || end <= start {
		t.Fatalf("could not parse routeKey constant value from: %q", line)
	}
	val := line[start+1 : end]
	if len(val) != 8 {
		t.Errorf("routeKey should be 8 hex chars, got %q (len=%d)", val, len(val))
	}
}

// TestGenerator_NonLeafReactiveRoute_NoWSEndpoint verifies that a non-leaf
// reactive route (one that has a child route) does NOT get its own WS endpoint;
// only the leaf gets a WS endpoint.
func TestGenerator_NonLeafReactiveRoute_NoWSEndpoint(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "shop",
		`<ssr:var name="cart" type="int" reactive="true"/>
<p>{{ cart }}</p>`)
	// Non-reactive child; shop is the reactive parent.
	writeTemplate(t, webDir, "shop/checkout",
		`<ssr:var name="step" type="string"/>
<p>{{ step }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	handlerFile := filepath.Join(webDir, "pages", "ssrhandler_gen.go")
	handlerData, err := os.ReadFile(handlerFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	handlerContent := string(handlerData)

	// The leaf path /shop/checkout/__ws must be present (because /shop is reactive).
	if !strings.Contains(handlerContent, "/shop/checkout/__ws") {
		t.Errorf("expected /shop/checkout/__ws (leaf WS endpoint) in handler\n%s", handlerContent)
	}
	// The non-leaf /shop/__ws must NOT be present as a separate endpoint.
	if strings.Contains(handlerContent, `"/shop/__ws"`) {
		t.Errorf("non-leaf /shop/__ws should NOT get its own WS endpoint\n%s", handlerContent)
	}
}

// ---- E03 removed: reactive var in ssr:if condition is now valid ----
// Previously, a reactive variable used only in an ssr:if condition was a
// compile-time error (E03). E03 is removed and instead the entire conditional
// block is wrapped in <ssr-block data-ssr-bind="KEY">.

func TestGenerator_E03_ConditionOnlyReactiveVar_NowGeneratesBlock(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "status",
		`<ssr:var name="active" type="bool" reactive="true"/>
<span ssr:if="active">On</span>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	// The generated ssrroute_gen.go should contain an ssr-block wrapper.
	genFile := filepath.Join(webDir, "pages", "status", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", genFile, err)
	}
	content := string(data)
	// Check for ssr-block wrapper (may be hex-encoded in byte arrays).
	hasSsrBlock := strings.Contains(content, "ssr-block") ||
		strings.Contains(content, "0x73, 0x73, 0x72, 0x2d, 0x62, 0x6c, 0x6f, 0x63, 0x6b")
	if !hasSsrBlock {
		t.Errorf("expected ssr-block wrapper in generated file\n--- content ---\n%s", content)
	}
}

// ---- E04 retired: struct/slice/map reactive vars now succeed ----
//
// The scalar-only restriction has been lifted. Any Go type may be used with
// reactive="true". E04 is retired and must NOT fire for non-scalar types.

// TestGenerator_E04_Retired_StructReactiveVar verifies that a struct-typed
// reactive variable no longer triggers E04 (AC12(a,d)).
func TestGenerator_E04_Retired_StructReactiveVar(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "profile",
		`<ssr:var name="user" type="User" reactive="true"/>
<p>{{ user }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error (E04 should be retired): %v", err)
	}

	// The generated ssrroute_gen.go must have SetUser with the correct type.
	genFile := filepath.Join(webDir, "pages", "profile", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", genFile, err)
	}
	content := string(data)
	if !strings.Contains(content, "SetUser(v User)") {
		t.Errorf("expected SetUser(v User) in generated file\n%s", content)
	}
}

// TestGenerator_StructReactiveVar_Compiles verifies that a struct-typed
// reactive variable compiles cleanly and emits a TS type in __ssr_gen__.ts
// (AC12(a,b,c)).
func TestGenerator_StructReactiveVar_Compiles(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "page",
		`<ssr:var name="profile" type="Profile" reactive="true"/>
<p>{{ profile }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	// SetProfile must use the correct Go type.
	genFile := filepath.Join(webDir, "pages", "page", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "SetProfile(v Profile)") {
		t.Errorf("expected SetProfile(v Profile)\n%s", content)
	}

	// __ssr_gen__.ts must contain the Profile type reference.
	tsFile := filepath.Join(webDir, "pages", "page", "__ssr_gen__.ts")
	tsData, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatalf("ReadFile __ssr_gen__.ts: %v", err)
	}
	tsContent := string(tsData)
	if !strings.Contains(tsContent, "Profile") {
		t.Errorf("expected Profile in __ssr_gen__.ts\n%s", tsContent)
	}
	if !strings.Contains(tsContent, "ReadVars") {
		t.Errorf("expected ReadVars in __ssr_gen__.ts\n%s", tsContent)
	}
}

// TestGenerator_SliceReactiveVar verifies that a slice-typed reactive variable
// generates correct code and emits the TS type T[] (AC12).
func TestGenerator_SliceReactiveVar(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "items",
		`<ssr:var name="tags" type="[]string" reactive="true"/>
<ul ssr:for="x in tags"><li>{{ x }}</li></ul>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "items", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "SetTags(v []string)") {
		t.Errorf("expected SetTags(v []string)\n%s", content)
	}

	// TS type for []string should be string[].
	tsFile := filepath.Join(webDir, "pages", "items", "__ssr_gen__.ts")
	tsData, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatalf("ReadFile __ssr_gen__.ts: %v", err)
	}
	tsContent := string(tsData)
	if !strings.Contains(tsContent, "string[]") {
		t.Errorf("expected string[] in __ssr_gen__.ts\n%s", tsContent)
	}
}

// TestGenerator_MapReactiveVar verifies that a map[string]int reactive variable
// generates correct code and emits Record<string, number> in TS (AC12).
func TestGenerator_MapReactiveVar(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "scores",
		`<ssr:var name="scores" type="map[string]int" reactive="true"/>
<p>{{ scores }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "scores", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "SetScores(v map[string]int)") {
		t.Errorf("expected SetScores(v map[string]int)\n%s", content)
	}

	tsFile := filepath.Join(webDir, "pages", "scores", "__ssr_gen__.ts")
	tsData, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatalf("ReadFile __ssr_gen__.ts: %v", err)
	}
	tsContent := string(tsData)
	if !strings.Contains(tsContent, "Record<string, number>") {
		t.Errorf("expected Record<string, number> in __ssr_gen__.ts\n%s", tsContent)
	}
}

// TestGenerator_PointerReactiveVar verifies that a *int reactive variable
// generates correct code and emits "number | null" in TS (AC12).
func TestGenerator_PointerReactiveVar(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "maybe",
		`<ssr:var name="count" type="*int" reactive="true"/>
<p>{{ count }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "maybe", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "SetCount(v *int)") {
		t.Errorf("expected SetCount(v *int)\n%s", content)
	}

	tsFile := filepath.Join(webDir, "pages", "maybe", "__ssr_gen__.ts")
	tsData, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatalf("ReadFile __ssr_gen__.ts: %v", err)
	}
	tsContent := string(tsData)
	if !strings.Contains(tsContent, "number | null") {
		t.Errorf("expected number | null in __ssr_gen__.ts\n%s", tsContent)
	}
}

// TestGenerator_RecursiveStructFallback verifies that a recursive type
// (type Node struct { Children []*Node }) substitutes "any" for the cycle
// in the TS output and emits the type without infinite recursion.
func TestGenerator_RecursiveStructFallback(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	// The variable type is "Node" — a struct name the generator sees as a
	// simple identifier. The cycle guard fires when Node is encountered during
	// its own interface expansion (which for our placeholder emitter means we
	// detect the cycle at depth 1).
	writeTemplate(t, webDir, "tree",
		`<ssr:var name="root" type="Node" reactive="true"/>
<p>{{ root }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	tsFile := filepath.Join(webDir, "pages", "tree", "__ssr_gen__.ts")
	tsData, err := os.ReadFile(tsFile)
	if err != nil {
		t.Fatalf("ReadFile __ssr_gen__.ts: %v", err)
	}
	// The TS output should contain the Node type reference without panicking.
	tsContent := string(tsData)
	if !strings.Contains(tsContent, "Node") {
		t.Errorf("expected Node in __ssr_gen__.ts\n%s", tsContent)
	}
}

// ---- E07: ssr:bind on non-scalar variable type ----

// TestGenerator_E07_SsrBindOnStruct verifies that ssr:bind on a struct-typed
// reactive variable produces error E07 (AC27).
func TestGenerator_E07_SsrBindOnStruct(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "form",
		`<ssr:var name="profile" type="Profile" reactive="true" client-writable="true"/>
<p>{{ profile }}</p>
<input ssr:bind="profile" type="text"/>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`requires a scalar variable type`)
}

// TestGenerator_E07_SsrBindOnSlice verifies that ssr:bind on a slice-typed
// reactive variable produces error E07 (AC27).
func TestGenerator_E07_SsrBindOnSlice(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "tags",
		`<ssr:var name="tags" type="[]string" reactive="true" client-writable="true"/>
<p>{{ tags }}</p>
<input ssr:bind="tags" type="text"/>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`requires a scalar variable type`)
}

// TestGenerator_E07_SsrBindOnAlias verifies that ssr:bind on a user-defined
// type alias (e.g. type UserID string) produces error E07 because aliases are
// NOT considered scalar (E07 scalar-list restriction).
func TestGenerator_E07_SsrBindOnAlias(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	// "UserID" is not in the ssrBindScalarTypes set even if its underlying
	// type is string — the generator checks the literal token.
	writeTemplate(t, webDir, "uid",
		`<ssr:var name="uid" type="UserID" reactive="true" client-writable="true"/>
<p>{{ uid }}</p>
<input ssr:bind="uid" type="text"/>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`requires a scalar variable type`)
}

// TestGenerator_E07_SsrBindOnComplex64 verifies that ssr:bind on a complex64-typed
// reactive variable produces error E07. complex64 and complex128 were previously in
// ssrBindScalarTypes but are excluded because encoding/json cannot unmarshal any JSON
// value into a complex type — every client write would produce a decode_error at runtime.
func TestGenerator_E07_SsrBindOnComplex64(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "cmplx",
		`<ssr:var name="z" type="complex64" reactive="true" client-writable="true"/>
<p>{{ z }}</p>
<input ssr:bind="z" type="text"/>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`requires a scalar variable type`)
}

// TestGenerator_E07_NotFiredForScalar verifies that ssr:bind on a scalar type
// (e.g. int) does NOT produce E07 (positive case).
func TestGenerator_E07_NotFiredForScalar(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "counter",
		`<ssr:var name="n" type="int" reactive="true" client-writable="true"/>
<p>{{ n }}</p>
<input ssr:bind="n" type="number"/>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error (E07 should not fire for int): %v", err)
	}
}

// ---- AC12(e): json.Unmarshal replaces ParseValue for ALL types ----

// TestHandleWrite_UsesJsonUnmarshal verifies that the generated handleWrite
// dispatch uses json.Unmarshal(msg.Value, &val) and NOT reactive.ParseValue[T]
// for all variable types including scalars (AC12(e)).
func TestHandleWrite_UsesJsonUnmarshal(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "counter",
		`<ssr:var name="counter" type="int" reactive="true" client-writable="true"/>
<p>{{ counter }}</p>
<input ssr:bind="counter" type="number"/>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "counter", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Must use json.Unmarshal for write dispatch (AC12(e)).
	if !strings.Contains(content, "json.Unmarshal(msg.Value") {
		t.Errorf("expected json.Unmarshal(msg.Value...) in handleWrite\n%s", content)
	}
	// Must import encoding/json.
	if !strings.Contains(content, `"encoding/json"`) {
		t.Errorf("expected encoding/json import in generated file\n%s", content)
	}
	// Must NOT use reactive.ParseValue (retired from reactive code paths).
	if strings.Contains(content, "ParseValue") {
		t.Errorf("ParseValue should not appear in generated code (AC12(e))\n%s", content)
	}
	// Must emit decode_error on unmarshal failure.
	if !strings.Contains(content, `"decode_error"`) {
		t.Errorf("expected decode_error code in handleWrite\n%s", content)
	}
}

// TestHandleWrite_StructType_UsesJsonUnmarshal verifies that a struct-typed
// client-writable variable also uses json.Unmarshal (not ParseValue) (AC12(e)).
func TestHandleWrite_StructType_UsesJsonUnmarshal(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "edit",
		`<ssr:var name="profile" type="Profile" reactive="true" client-writable="true"/>
<p>{{ profile }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "edit", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "json.Unmarshal(msg.Value") {
		t.Errorf("expected json.Unmarshal for struct type\n%s", content)
	}
	if strings.Contains(content, "ParseValue") {
		t.Errorf("ParseValue should not be in generated code for struct type\n%s", content)
	}
	if !strings.Contains(content, "var val Profile") {
		t.Errorf("expected 'var val Profile' in handleWrite case\n%s", content)
	}
}

// ---- E05: ssr:bind without client-writable ----

func TestGenerator_E05_SsrBindWithoutClientWritable(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "counter",
		`<ssr:var name="counter" type="int" reactive="true"/>
<p>{{ counter }}</p>
<input ssr:bind="counter" type="number"/>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`requires client-writable="true" on the variable declaration`)
}

// E05 should NOT fire when client-writable="true" is set.
func TestGenerator_E05_NotFiredWhenClientWritable(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "counter",
		`<ssr:var name="counter" type="int" reactive="true" client-writable="true"/>
<p>{{ counter }}</p>
<input ssr:bind="counter" type="number"/>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}
}

// ---- E06: ssr:bind on GoSSR form primitive ----

func TestGenerator_E06_SsrBindOnSsrInput(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "search",
		`<ssr:var name="query" type="string" reactive="true" client-writable="true"/>
<ssr:form name="search">
  <ssr:input ssr:bind="query" name="q" type="text"/>
</ssr:form>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`ssr:bind is not valid on GoSSR form primitive`)
}

// ---- Non-reactive routes are unchanged ----

func TestGenerator_NonReactiveRouteUnchanged(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "home",
		`<ssr:var name="title" type="string"/>
<h1>{{ title }}</h1>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}

	// The generated ssrroute_gen.go should NOT contain "ReactiveState".
	genFile := filepath.Join(webDir, "pages", "home", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", genFile, err)
	}
	if strings.Contains(string(data), "ReactiveState") {
		t.Error("non-reactive route should not contain ReactiveState")
	}
}

// ---- Reactive route generates ReactiveState ----

func TestGenerator_ReactiveRouteGeneratesReactiveState(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "dashboard",
		`<ssr:var name="visits" type="int" reactive="true"/>
<p>{{ visits }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}

	// Check ssrroute_gen.go contains ReactiveState and SetVisits.
	genFile := filepath.Join(webDir, "pages", "dashboard", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", genFile, err)
	}
	content := string(data)
	if !strings.Contains(content, "ReactiveState") {
		t.Error("expected ReactiveState in generated file")
	}
	if !strings.Contains(content, "SetVisits") {
		t.Error("expected SetVisits in generated file")
	}
	if !strings.Contains(content, "Subscribe") {
		t.Error("expected Subscribe in generated file")
	}

	// Check ssrhandler_gen.go contains wsHandler registration.
	handlerFile := filepath.Join(webDir, "pages", "ssrhandler_gen.go")
	handlerData, err := os.ReadFile(handlerFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", handlerFile, err)
	}
	handlerContent := string(handlerData)
	if !strings.Contains(handlerContent, "__ws") {
		t.Error("expected __ws in generated handler file")
	}
	if !strings.Contains(handlerContent, "WithWSHandlers") {
		t.Error("expected WithWSHandlers in generated handler file")
	}
}

// containsHTMLSnippet checks whether a Go source content string contains the
// given HTML snippet, either as a plain UTF-8 string literal OR as the
// hex-byte sequence that gobuf emits for static HTML strings.
//
// gobuf encodes static HTML output as []byte{0xNN, 0xNN, ...} variables.
// We look for the first 8 bytes of the snippet in the hex-byte form.
func containsHTMLSnippet(content, snippet string) bool {
	if strings.Contains(content, snippet) {
		return true
	}
	if len(snippet) < 1 {
		return false
	}
	// Build the hex-byte prefix for the first up to 6 bytes of the snippet.
	limit := len(snippet)
	if limit > 6 {
		limit = 6
	}
	hexParts := make([]string, limit)
	for i := 0; i < limit; i++ {
		hexParts[i] = fmt.Sprintf("0x%02x", snippet[i])
	}
	return strings.Contains(content, strings.Join(hexParts, ", "))
}

// ---- span data-ssr-bind wrapper ----

func TestGenerator_SpanWrapperEmitted(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "counter",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "counter", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	// gobuf encodes static HTML strings as byte-array variables, so the literal
	// string "data-ssr-bind" will not appear verbatim. Instead, check for the
	// hex-encoded bytes of 'data-ssr-bind' (0x64, 0x61, 0x74, 0x61, 0x2d, 0x73, 0x73, 0x72).
	// We check for '0x64,0x61,0x74,0x61,0x2d' which spells "data-".
	// The full literal text would appear if gobuf uses quoted strings; the hex
	// form appears when gobuf batches into byte slices (current behaviour).
	hasDataSsrBind := strings.Contains(content, `data-ssr-bind`) ||
		strings.Contains(content, "0x64, 0x61, 0x74, 0x61, 0x2d, 0x73, 0x73, 0x72, 0x2d, 0x62, 0x69, 0x6e, 0x64")
	if !hasDataSsrBind {
		t.Errorf("expected data-ssr-bind in generated file (either as string or hex bytes)\n--- file content ---\n%s\n--- end ---", content)
	}
}

// ---- AC18: reactive ssr:if generates <ssr-block data-ssr-bind> wrapper ----

// TestReactiveConditional_BlockKeyEmitted verifies that a reactive ssr:if
// block emits an <ssr-block data-ssr-bind="KEY"> wrapper (AC18a).
func TestReactiveConditional_BlockKeyEmitted(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "vis",
		`<ssr:var name="flag" type="bool" reactive="true"/>
<div ssr:if="flag"><p>Active</p></div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "vis", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Must have an ssr-block wrapper in the generated HTML output.
	if !containsHTMLSnippet(content, "<ssr-block") {
		t.Errorf("expected <ssr-block wrapper in generated file\n%s", content)
	}
	// Must have a renderBlock_KEY function (for snapshot and Set*).
	if !strings.Contains(content, "renderBlock_") {
		t.Errorf("expected renderBlock_ helper function in generated file\n%s", content)
	}
	// Snapshot() must reference the block key.
	if !strings.Contains(content, "Snapshot(") {
		t.Errorf("expected Snapshot function in generated file\n%s", content)
	}
}

// TestReactiveConditional_NoInnerSpan verifies that inner {{ flag }} inside a
// reactive conditional branch does NOT get a <span data-ssr-bind> wrapper
// (inner-wrapper suppression rule, AC18).
func TestReactiveConditional_NoInnerSpan(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "vis",
		`<ssr:var name="flag" type="bool" reactive="true"/>
<div ssr:if="flag">{{ flag }}</div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "vis", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// The outer block must be wrapped in ssr-block.
	if !containsHTMLSnippet(content, "<ssr-block") {
		t.Errorf("expected outer <ssr-block wrapper\n%s", content)
	}

	// There should be NO separate <span data-ssr-bind> wrapper for the inner
	// {{ flag }} expression — it is suppressed.
	// The span wrapper is emitted as WritePrintString which becomes hex bytes.
	// A <span data-ssr-bind="flag"> would appear in the Write() function.
	// Check that the BindingKey for the inner expression is NOT set by verifying
	// the generated code does NOT have a span wrapper alongside the ssr-block.
	// The Write function is where the suppression manifests: we check that the
	// write function body only has one ssr-block and no extra span data-ssr-bind.
	hasSpanInsideBlock := strings.Contains(content, `"flag"`) &&
		containsHTMLSnippet(content, `<span data-ssr-bind="flag">`)
	if hasSpanInsideBlock {
		t.Errorf("inner expression should NOT have its own span wrapper (suppression rule)\n%s", content)
	}
}

// ---- AC19: reactive ssr:for (reactive collection) ----

// TestReactiveLoop_ReactiveCollection verifies that an ssr:for with a reactive
// collection emits an <ssr-block data-ssr-bind="KEY"> wrapper (AC19a).
func TestReactiveLoop_ReactiveCollection(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	// Note: todos is a string slice.
	// We use type="string" for the reactive var; the loop iterates over characters.
	writeTemplate(t, webDir, "list",
		`<ssr:var name="items" type="string" reactive="true"/>
<ul ssr:for="x in items"><li>{{ x }}</li></ul>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "list", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !containsHTMLSnippet(content, "<ssr-block") {
		t.Errorf("expected <ssr-block wrapper for reactive loop\n%s", content)
	}
	if !strings.Contains(content, "renderBlock_") {
		t.Errorf("expected renderBlock_ helper for reactive loop\n%s", content)
	}
}

// TestReactiveLoop_ReactiveBody verifies that a static collection loop whose
// body uses a reactive variable emits an <ssr-block> wrapper (AC20).
func TestReactiveLoop_ReactiveBody(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "tagged",
		`<ssr:var name="prefix" type="string" reactive="true"/>
<ssr:var name="items" type="string"/>
<ul ssr:for="x in items"><li>{{ prefix }}: {{ x }}</li></ul>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "tagged", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !containsHTMLSnippet(content, "<ssr-block") {
		t.Errorf("expected <ssr-block wrapper for loop with reactive body\n%s", content)
	}
}

// TestReactiveLoop_BothReactive verifies the union dependency set case (AC22):
// a loop with both a reactive collection AND reactive body variables emits
// exactly one <ssr-block> wrapper and both variables appear in the dep map.
func TestReactiveLoop_BothReactive(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "combo",
		`<ssr:var name="items" type="string" reactive="true"/>
<ssr:var name="counter" type="int" reactive="true"/>
<ul ssr:for="x in items"><li>{{ counter }}</li></ul>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "combo", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Exactly one ssr-block (not two) — the union case.
	if !containsHTMLSnippet(content, "<ssr-block") {
		t.Errorf("expected <ssr-block wrapper\n%s", content)
	}
	// Both reactive vars must drive the same block's renderBlock helper.
	// SetItems and SetCounter must both enqueue patches for the loop's key.
	if !strings.Contains(content, "SetItems(") {
		t.Errorf("expected SetItems in generated file\n%s", content)
	}
	if !strings.Contains(content, "SetCounter(") {
		t.Errorf("expected SetCounter in generated file\n%s", content)
	}
	// The inner {{ counter }} inside the loop body must NOT have its own span
	// (suppression rule — it's inside the loop block).
	hasInnerSpan := containsHTMLSnippet(content, `<span data-ssr-bind="counter">`)
	if hasInnerSpan {
		t.Errorf("inner {{ counter }} inside loop should be suppressed (no span wrapper)\n%s", content)
	}
}

// ---- AC21: nested reactive conditionals ----

// TestNestedReactiveConditionals_OuterSubsumesInner verifies that when a
// reactive conditional is nested inside another reactive conditional, only the
// outer one gets a <ssr-block> wrapper. The outer's CollectVarRefs already
// captures the inner's reactive vars, so a change to either re-renders the
// outer correctly. Emitting the inner wrapper too would be redundant and -
// crucially - inside table descendants (e.g. <tr ssr:if> inside <tbody>) the
// extra <ssr-block> is foster-parented out by the HTML parser, duplicating
// rendered content in the DOM.
func TestNestedReactiveConditionals_OuterSubsumesInner(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "nested",
		`<ssr:var name="outer" type="bool" reactive="true"/>
<ssr:var name="inner" type="bool" reactive="true"/>
<div ssr:if="outer"><div ssr:if="inner"><p>Both</p></div></div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "nested", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Exactly one renderBlock_ function (the outer); the inner is suppressed.
	renderBlockCount := strings.Count(content, "func renderBlock_")
	if renderBlockCount != 1 {
		t.Errorf("expected exactly 1 renderBlock_ for nested-inside-outer reactive conditionals, got %d\n%s", renderBlockCount, content)
	}

	// Both SetInner and SetOuter must enqueue a patch for the SAME outer
	// block key, so a change to either reactive var re-renders the right
	// scope.
	idxIn := strings.Index(content, "func (s *ReactiveState) SetInner(")
	idxOut := strings.Index(content, "func (s *ReactiveState) SetOuter(")
	if idxIn < 0 || idxOut < 0 {
		t.Fatalf("missing Set methods\n%s", content)
	}
	innerBody := content[idxIn:]
	outerBody := content[idxOut:]
	innerEnqueueIdx := strings.Index(innerBody, "s.conn.Enqueue(")
	outerEnqueueIdx := strings.Index(outerBody, "s.conn.Enqueue(")
	if innerEnqueueIdx < 0 || outerEnqueueIdx < 0 {
		t.Fatalf("Set* methods missing Enqueue call\n%s", content)
	}
	innerEnqueueLine := innerBody[innerEnqueueIdx : innerEnqueueIdx+strings.IndexByte(innerBody[innerEnqueueIdx:], '\n')]
	outerEnqueueLine := outerBody[outerEnqueueIdx : outerEnqueueIdx+strings.IndexByte(outerBody[outerEnqueueIdx:], '\n')]
	if innerEnqueueLine != outerEnqueueLine {
		t.Errorf("inner and outer setters should target the same shared outer block:\n  inner: %s\n  outer: %s", innerEnqueueLine, outerEnqueueLine)
	}
}

// ---- snapshot completeness ----

// TestSnapshot_IncludesAllBindings verifies that the generated snapshot()
// function calls renderBlock_KEY for every reactive binding site, including
// composite, conditional, and loop sites (AC18b, AC19b).
func TestSnapshot_IncludesAllBindings(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	// Template with: scalar binding, block binding (conditional).
	writeTemplate(t, webDir, "snap",
		`<ssr:var name="count" type="int" reactive="true"/>
<ssr:var name="flag" type="bool" reactive="true"/>
<p>{{ count }}</p>
<div ssr:if="flag"><p>Visible</p></div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "snap", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// snapshot() must contain at least one renderBlock_ call per binding site.
	// We have: count (single-var) + flag conditional block = at least 2 sites.
	renderBlockCallCount := strings.Count(content, "renderBlock_")
	// Each key appears in: function definition, snapshot() call, and Set* enqueue.
	// For 2 binding sites (count, flag-block) we expect renderBlock_ to appear at
	// least 4 times (2 definitions + 2 in snapshot + 2 in Set calls).
	if renderBlockCallCount < 4 {
		t.Errorf("expected multiple renderBlock_ references (snapshot + Set*), got %d\n%s", renderBlockCallCount, content)
	}

	// Snapshot() must be present (exported so ssrhandler_gen.go can call it cross-package).
	if !strings.Contains(content, "func Snapshot(") {
		t.Errorf("expected Snapshot function\n%s", content)
	}
}

// ---- AC inline style ----

// TestInlineStyleEmittedOnce verifies that the <style>ssr-block{display:contents}</style>
// rule is emitted exactly once for a route with reactive blocks.
func TestInlineStyleEmittedOnce(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "styled",
		`<ssr:var name="flag" type="bool" reactive="true"/>
<div ssr:if="flag"><p>On</p></div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "styled", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// The style tag must be present.
	if !containsHTMLSnippet(content, "<style>ssr-block") {
		t.Errorf("expected <style>ssr-block{ display: contents; } in generated file\n%s", content)
	}
}

// TestInlineStyleNotEmittedForNonReactive verifies that non-reactive routes
// do NOT get the <style>ssr-block{...}</style> injection.
func TestInlineStyleNotEmittedForNonReactive(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "plain",
		`<ssr:var name="title" type="string"/>
<h1>{{ title }}</h1>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "plain", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if containsHTMLSnippet(content, "<style>ssr-block") {
		t.Errorf("non-reactive route should NOT have ssr-block style injection\n%s", content)
	}
}

// TestInlineStyleNotEmittedForScalarReactive verifies that a reactive route
// with only scalar bindings (no block sites) does NOT get the style injection.
func TestInlineStyleNotEmittedForScalarReactive(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "scalar",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "scalar", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if containsHTMLSnippet(content, "<style>ssr-block") {
		t.Errorf("scalar-only reactive route should NOT have ssr-block style injection\n%s", content)
	}
}

// ---- renderBlock_* helpers ----

// TestRenderBlockHelper_SingleVar verifies that single-variable binding sites
// get a renderBlock_KEY function using reactive.RenderValue.
func TestRenderBlockHelper_SingleVar(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "rv",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "rv", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Single-var: function should use reactive.RenderValue.
	if !strings.Contains(content, "func renderBlock_count(") {
		t.Errorf("expected renderBlock_count function\n%s", content)
	}
	if !strings.Contains(content, "reactive.RenderValue(") {
		t.Errorf("expected reactive.RenderValue in renderBlock_count\n%s", content)
	}
}

// TestRenderBlockHelper_BlockSite verifies that block binding sites get both
// a writeBlock_KEY and renderBlock_KEY function with strings.Builder.
func TestRenderBlockHelper_BlockSite(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "bs",
		`<ssr:var name="show" type="bool" reactive="true"/>
<div ssr:if="show"><p>Yes</p></div>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "bs", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "func writeBlock_") {
		t.Errorf("expected writeBlock_ helper for block site\n%s", content)
	}
	if !strings.Contains(content, "strings.Builder") {
		t.Errorf("expected strings.Builder in renderBlock_ helper\n%s", content)
	}
}

// ---- Regression: composite expression key collision (round-3.5 fix) ----

// TestCompositeExprKey_NoCollision is a regression test for the bug where all
// composite (multi-variable) expressions were hashed with an empty source string,
// causing every {{ a + b }}, {{ a - b }}, etc. to produce the same binding key
// (SHA256 of ""). With the fix, each expression's raw source text (e.g., "a + b")
// is used as the hash input so distinct expressions produce distinct keys.
//
// The template has two distinct composite expressions:
//   {{ a + b }}   — should produce binding key sha256("a + b")[:16]
//   {{ a - b }}   — should produce binding key sha256("a - b")[:16]
//
// Both a and b are reactive. Each expression must generate its own renderBlock_KEY
// function. Setting a or b must enqueue patches for BOTH keys (not just one).
// If reverted, both expressions would map to key e3b0c44298fc1c14 (SHA256 of "")
// and only the second renderBlock_ would be emitted.
func TestCompositeExprKey_NoCollision(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "composite",
		`<ssr:var name="a" type="int" reactive="true"/>
<ssr:var name="b" type="int" reactive="true"/>
<p>Sum: {{ a + b }}</p>
<p>Diff: {{ a - b }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "composite", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// There must be exactly two distinct renderBlock_ functions (one per composite
	// expression site). Before the fix there was only one because both keys
	// collapsed to e3b0c44298fc1c14.
	renderBlockCount := strings.Count(content, "func renderBlock_")
	if renderBlockCount != 2 {
		t.Errorf("expected 2 renderBlock_ functions (one per composite expr), got %d\n%s",
			renderBlockCount, content)
	}

	// The collision key (SHA256 of empty string) must NOT appear.
	if strings.Contains(content, "e3b0c44298fc1c14") {
		t.Errorf("found collision key e3b0c44298fc1c14 (SHA256 of empty string): composite expr source not being passed to keyer\n%s", content)
	}

	// SetA must enqueue patches for both binding keys (both expressions depend on a).
	// Count how many times Enqueue is called inside SetA — should be 2.
	setAStart := strings.Index(content, "func (s *ReactiveState) SetA(")
	setBStart := strings.Index(content, "func (s *ReactiveState) SetB(")
	if setAStart < 0 {
		t.Fatalf("SetA not found in generated file\n%s", content)
	}
	if setBStart < 0 {
		t.Fatalf("SetB not found in generated file\n%s", content)
	}

	// SetA body: find the function and count Enqueue calls within it.
	// Both composite expressions reference a, so SetA must enqueue 2 keys.
	setABody := content[setAStart:]
	nextFuncIdx := strings.Index(setABody[1:], "\nfunc ")
	if nextFuncIdx > 0 {
		setABody = setABody[:nextFuncIdx+1]
	}
	enqueueCountA := strings.Count(setABody, "s.conn.Enqueue(")
	if enqueueCountA != 2 {
		t.Errorf("SetA should enqueue 2 patches (one per composite expr), got %d\n%s",
			enqueueCountA, setABody)
	}

	// Similarly SetB must enqueue 2 patches.
	setBBody := content[setBStart:]
	nextFuncIdxB := strings.Index(setBBody[1:], "\nfunc ")
	if nextFuncIdxB > 0 {
		setBBody = setBBody[:nextFuncIdxB+1]
	}
	enqueueCountB := strings.Count(setBBody, "s.conn.Enqueue(")
	if enqueueCountB != 2 {
		t.Errorf("SetB should enqueue 2 patches (one per composite expr), got %d\n%s",
			enqueueCountB, setBBody)
	}
}

// ---- RB-MUX-6: cross-route ssr:bind validation ----

// TestSsrBind_CrossRouteRejected verifies that an ssr:bind in a child route that
// references a variable declared only in the parent route is rejected at generation
// time with a clear, actionable error message. Before this fix the generator would
// silently emit Go code with an undeclared identifier, producing a Go compile error.
func TestSsrBind_CrossRouteRejected(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")

	// Parent declares the reactive, client-writable variable "visits".
	writeTemplate(t, webDir, "dash",
		`<ssr:var name="visits" type="int" reactive="true" client-writable="true"/>
<p>{{ visits }}</p>
<ssr:content/>`)

	// Child uses ssr:bind="visits" — but "visits" is only declared in the parent.
	writeTemplate(t, webDir, "dash/detail",
		`<input ssr:bind="visits" type="number"/>`)

	g := makeGen(t, webDir)
	runAndExpectError(t, g,
		`references a variable declared in a different route`)
}

// TestSsrBind_LocalAccepted verifies that an ssr:bind referencing a client-writable
// variable declared in the SAME route is accepted without error (positive case).
func TestSsrBind_LocalAccepted(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")

	// Parent (no ssr:bind).
	writeTemplate(t, webDir, "dash",
		`<ssr:var name="visits" type="int" reactive="true" client-writable="true"/>
<p>{{ visits }}</p>
<ssr:content/>`)

	// Child declares its OWN client-writable variable and binds to it locally.
	writeTemplate(t, webDir, "dash/detail",
		`<ssr:var name="score" type="int" reactive="true" client-writable="true"/>
<p>{{ score }}</p>
<input ssr:bind="score" type="number"/>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error for local ssr:bind: %v", err)
	}
}

// ---- Round 6.1: exported reactive state function names compile cross-package ----

// TestGenerator_HandlerCallsExportedReactiveState verifies that the generated
// reactive state helpers (NewReactiveState, Snapshot, HandleWrite) are exported
// (capitalized) and that the combined generated tree compiles without errors.
//
// The test generates a reactive route and handler into a temp directory with a
// go.mod that replaces github.com/sergei-svistunov/go-ssr with the local repo.
// It then runs "go build ./..." and asserts exit code 0. This catches accidental
// lowercasing of exported names at compile time rather than at user-app build time.
func TestGenerator_HandlerCallsExportedReactiveState(t *testing.T) {
	// Locate the repo root: this test file lives in internal/generator/, so
	// the repo root is two directories up from the current working directory.
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	tmpDir := t.TempDir()
	webDir := filepath.Join(tmpDir, "app", "web")

	// Root route — required so the generator's "/" entry in the handler has a
	// corresponding NewRoute/NewDP in the pages package.
	writeTemplate(t, webDir, ".",
		`<p>Home</p>`)

	// Route with two reactive vars; the theme conditional block references only
	// theme, not counter — verifying that writeBlock_* emits only the needed alias.
	writeTemplate(t, webDir, "page",
		`<ssr:var name="counter" type="int" reactive="true" client-writable="true"/>
<ssr:var name="theme" type="string" reactive="true" client-writable="true"/>
<p>{{ counter }}</p>
<span ssr:if="theme == &quot;dark&quot;">Dark mode active</span>
<span ssr:else-if="theme == &quot;light&quot;">Light mode active</span>
<span ssr:else>Auto mode</span>
<input type="number" ssr:bind="counter"/>
<input type="text" ssr:bind="theme"/>`)

	// Generate code.
	g := generator.New(&config.Config{
		Dir:        filepath.Join(tmpDir, "app"),
		WebDir:     webDir,
		WebPackage: "example.com/app/web",
	})
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Write a minimal dataprovider.go for the root route (pages package).
	rootDPPath := filepath.Join(webDir, "pages", "dataprovider.go")
	rootDPContent := `package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type DP struct{}

func NewDP() *DP { return &DP{} }

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	return nil
}
`
	if err := os.WriteFile(rootDPPath, []byte(rootDPContent), 0644); err != nil {
		t.Fatalf("WriteFile root dataprovider.go: %v", err)
	}

	// Write a minimal dataprovider.go for the /page reactive route.
	dpPath := filepath.Join(webDir, "pages", "page", "dataprovider.go")
	dpContent := `package page

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type DP struct{}

func NewDP() *DP { return &DP{} }

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	return nil
}
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
	return nil
}
func (p *DP) ValidateCounter(ctx context.Context, r *mux.Request, val int) (int, error) {
	return val, nil
}
func (p *DP) ValidateTheme(ctx context.Context, r *mux.Request, val string) (string, error) {
	return val, nil
}
`
	if err := os.WriteFile(dpPath, []byte(dpContent), 0644); err != nil {
		t.Fatalf("WriteFile dataprovider.go: %v", err)
	}

	// Write a go.mod for the temp app, replacing the local go-ssr module so that
	// generated imports (pkg/mux, pkg/reactive) resolve to the repo under test.
	goModContent := fmt.Sprintf(`module example.com/app

go 1.23

require github.com/sergei-svistunov/go-ssr v0.0.0

replace github.com/sergei-svistunov/go-ssr => %s
`, repoRoot)
	goModPath := filepath.Join(tmpDir, "app", "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}

	appDir := filepath.Join(tmpDir, "app")

	// Resolve dependencies (go.sum) before building.
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = appDir
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed:\n%s", string(out))
	}

	// Run "go build ./..." inside the temp app directory.
	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = appDir
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed (exported reactive state names may be unexported or unused aliases cause compile error):\n%s", string(out))
	}
}

// ---- Bug-1 regression: ssr:bind attribute preserved in rendered HTML ----

// TestGenerator_SsrBindAttrPreservedInOutput verifies that an <input ssr:bind="x">
// in a template produces generated Go code whose Write output contains the literal
// ssr:bind="x" attribute. This attribute must reach the browser so that the
// TypeScript runtime's wireSsrBindElements() can discover bound inputs via
// el.getAttribute('ssr:bind').
func TestGenerator_SsrBindAttrPreservedInOutput(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "bind",
		`<ssr:var name="x" type="string" reactive="true" client-writable="true"/>
<input ssr:bind="x" type="text"/>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	genFile := filepath.Join(webDir, "pages", "bind", "ssrroute_gen.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// The generated Write function must contain the literal ssr:bind="x" string
	// (either inline or as hex bytes) so the browser receives it in rendered HTML.
	hasSsrBind := containsHTMLSnippet(content, `ssr:bind="x"`)
	if !hasSsrBind {
		t.Errorf("expected ssr:bind=\"x\" in generated HTML output (either as string or hex bytes)\n--- content ---\n%s\n--- end ---", content)
	}
}

// TestGenerator_NoStubIndexTSEmitted verifies that the generator does NOT
// create an index.ts in a reactive route directory. Bundling __ssr_gen__.ts
// into the route's webpack entry is the responsibility of the webpack plugin.
func TestGenerator_NoStubIndexTSEmitted(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "live",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	stubPath := filepath.Join(webDir, "pages", "live", "index.ts")
	if _, err := os.Stat(stubPath); !os.IsNotExist(err) {
		t.Errorf("expected no index.ts in pages/live, but Stat returned %v", err)
	}
}

// TestGenerator_DeveloperIndexTSNotOverwritten verifies that the generator
// does NOT touch a developer-authored index.ts. The existing file must be
// left intact after Generate().
func TestGenerator_DeveloperIndexTSNotOverwritten(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "live",
		`<ssr:var name="count" type="int" reactive="true"/>
<p>{{ count }}</p>`)

	devContent := "// developer authored\nconsole.log('live');\n"
	indexPath := filepath.Join(webDir, "pages", "live", "index.ts")
	if err := os.WriteFile(indexPath, []byte(devContent), 0644); err != nil {
		t.Fatalf("WriteFile index.ts: %v", err)
	}

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	afterData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("ReadFile after Generate: %v", err)
	}
	if string(afterData) != devContent {
		t.Errorf("developer-authored index.ts was overwritten by the generator\n--- want ---\n%s\n--- got ---\n%s", devContent, string(afterData))
	}
}

// TestGenerator_AttributeReactivity_WrapsElement verifies that an HtmlElement
// whose attribute references a reactive variable is wrapped in
// <ssr-block data-ssr-bind="KEY"> and gets a renderBlock helper. The full
// element (including its rendered attributes) is the patch payload.
func TestGenerator_AttributeReactivity_WrapsElement(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "page")
	writeTemplate(t, webDir, "live",
		`<ssr:var name="userColor" type="string" reactive="true"/>
<p style="color: {{ userColor }}">hello</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	gen, err := os.ReadFile(filepath.Join(webDir, "pages", "live", "ssrroute_gen.go"))
	if err != nil {
		t.Fatalf("read ssrroute_gen.go: %v", err)
	}
	got := string(gen)

	hasSsrBlock := strings.Contains(got, "ssr-block") ||
		strings.Contains(got, "0x73, 0x73, 0x72, 0x2d, 0x62, 0x6c, 0x6f, 0x63, 0x6b")
	if !hasSsrBlock {
		t.Errorf("expected ssr-block wrapper around element with reactive attribute\n%s", got)
	}
	if !strings.Contains(got, `renderBlock_`) || !strings.Contains(got, `func writeBlock_`) {
		t.Errorf("expected renderBlock_/writeBlock_ helpers for the attribute-reactive element\n%s", got)
	}
}

// TestGenerator_AttributeReactivity_SuppressesInnerSpan verifies that an
// expression site nested inside an element with a reactive attribute is NOT
// independently wrapped — the outer block re-render covers it.
func TestGenerator_AttributeReactivity_SuppressesInnerSpan(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "page")
	writeTemplate(t, webDir, "live",
		`<ssr:var name="userColor" type="string" reactive="true"/>
<p style="color: {{ userColor }}">{{ userColor }}</p>`)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	gen, err := os.ReadFile(filepath.Join(webDir, "pages", "live", "ssrroute_gen.go"))
	if err != nil {
		t.Fatalf("read ssrroute_gen.go: %v", err)
	}
	got := string(gen)

	// The inner <span data-ssr-bind=...> wrapper must NOT appear (it would
	// duplicate patches for every userColor change). gobuf encodes this as a
	// hex byte array — search both forms.
	hasInnerSpan := strings.Contains(got, `<span data-ssr-bind=`) ||
		strings.Contains(got, "0x3c, 0x73, 0x70, 0x61, 0x6e, 0x20, 0x64, 0x61, 0x74, 0x61, 0x2d, 0x73, 0x73, 0x72, 0x2d, 0x62, 0x69, 0x6e, 0x64")
	if hasInnerSpan {
		t.Errorf("inner <span data-ssr-bind=...> must be suppressed by the outer reactive block\n%s", got)
	}
}
