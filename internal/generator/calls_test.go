package generator_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator"
)

// TestPageCallsCompile generates a root layout and pages with calls, builds
// them and runs calls through the mux on their live connections.
func TestPageCallsCompile(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/calls"), "-test.run", "^TestCallsThroughTheMux$")
}

// TestCallCode runs the generated Call of the fixture's routes directly.
func TestCallCode(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/calls"), "-test.run", "^TestCallDispatch$")
}

// TestCallStub runs the Call<Name> of a data provider the generator wrote.
func TestCallStub(t *testing.T) {
	runFixture(t, buildFixture(t, "testdata/calls"), "-test.run", "^TestStubRefuses$")
}

func routeKeyOf(path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(path)))[:8]
}

func writeGoFile(t *testing.T, webDir, routePath, name, content string) {
	t.Helper()
	p := filepath.Join(webDir, "pages", filepath.FromSlash(routePath), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	fixE19 = `declare <ssr:call name="lowerCamel" in="Type" out="Type"/> once, at the template's top level`
	fixE20 = "use a struct with json tags, or struct{}"
)

func TestCallRefusals(t *testing.T) {
	const ok = `in="struct{}" out="struct{}"`
	for _, tc := range []struct {
		name, tmpl, goSrc, code, msg string
	}{
		{"unknown attribute", `<ssr:call name="x" ` + ok + ` extra="1"/>`, "", "E19", `unknown attribute "extra"`},
		{"ssr attribute", `<ssr:call name="x" ` + ok + ` ssr:if="true"/>`, "", "E19", `unknown attribute "ssr:if"`},
		{"no name", `<ssr:call ` + ok + `/>`, "", "E19", "has no name"},
		{"empty name", `<ssr:call name="" ` + ok + `/>`, "", "E19", "has no name"},
		{"upper name", `<ssr:call name="Refresh" ` + ok + `/>`, "", "E19", `name "Refresh"`},
		{"dash name", `<ssr:call name="do-it" ` + ok + `/>`, "", "E19", `name "do-it"`},
		{"underscore name", `<ssr:call name="do_it" ` + ok + `/>`, "", "E19", `name "do_it"`},
		{"digit name", `<ssr:call name="1x" ` + ok + `/>`, "", "E19", `name "1x"`},
		{"no in", `<ssr:call name="x" out="struct{}"/>`, "", "E19", "has no in type"},
		{"no out", `<ssr:call name="x" in="struct{}"/>`, "", "E19", "has no out type"},
		{"in not a type", `<ssr:call name="x" in="1+2" out="struct{}"/>`, "", "E19", `in type "1+2" is not a Go type`},
		{"out not a type", `<ssr:call name="x" in="struct{}" out="f()"/>`, "", "E19", `out type "f()" is not a Go type`},
		{"broken type", `<ssr:call name="x" in="struct{" out="struct{}"/>`, "", "E19", `in type "struct{" is not a Go type`},
		{"in ssr:if", "<div ssr:if=\"true\"><p><ssr:call name=\"x\" " + ok + "/></p></div>", "", "E19", "inside ssr:if, ssr:for or <ssr:form>"},
		{"in ssr:else", "<div ssr:if=\"true\"></div><div ssr:else><ssr:call name=\"x\" " + ok + "/></div>", "", "E19", "inside ssr:if, ssr:for or <ssr:form>"},
		{"in ssr:for", "<ul ssr:for=\"x in xs\"><ssr:call name=\"x\" " + ok + "/></ul>", "", "E19", "inside ssr:if, ssr:for or <ssr:form>"},
		{"in ssr:form", "<ssr:form name=\"f\"><ssr:call name=\"x\" " + ok + "/></ssr:form>", "", "E19", "inside ssr:if, ssr:for or <ssr:form>"},
		{"twice", "<ssr:call name=\"x\" " + ok + "/><ssr:call name=\"x\" " + ok + "/>", "", "E19", `<ssr:call name="x"> is declared twice`},
		{"func", `<ssr:call name="x" in="func()" out="struct{}"/>`, "", "E20", `in type "func()" cannot travel as JSON: a func`},
		{"chan", `<ssr:call name="x" in="struct{}" out="chan int"/>`, "", "E20", `out type "chan int" cannot travel as JSON: a chan`},
		{"complex", `<ssr:call name="x" in="[]complex128" out="struct{}"/>`, "", "E20", "a complex128"},
		{"unsafe", `<ssr:call name="x" in="struct{}" out="unsafe.Pointer"/>`, "", "E20", "an unsafe.Pointer"},
		{"mux", `<ssr:call name="x" in="*mux.Request" out="struct{}"/>`, "", "E20", "a mux.Request"},
		{"render", `<ssr:call name="x" in="struct{}" out="map[string]render.SafeHTML"/>`, "", "E20", "a render.SafeHTML"},
		{"reactive", `<ssr:call name="x" in="struct{ C *reactive.Conn }" out="struct{}"/>`, "", "E20", "a reactive.Conn in field C"},
		{"map key", `<ssr:call name="x" in="map[bool]int" out="struct{}"/>`, "", "E20", "a map with bool keys"},
		{"error", `<ssr:call name="x" in="error" out="struct{}"/>`, "", "E20", `in type "error" cannot travel as JSON: an error`},
		{"error field", `<ssr:call name="x" in="struct{}" out="struct{ Err error }"/>`, "", "E20", "an error in field Err"},
		{"interface", `<ssr:call name="x" in="struct{}" out="[]interface{ String() string }"/>`, "", "E20", "an interface with methods"},
		{"named interface", `<ssr:call name="x" in="Shape" out="struct{}"/>`, "type Shape interface{ Area() float64 }\n", "E20", "an interface with methods"},
		{"field", `<ssr:call name="x" in="Outer" out="struct{}"/>`,
			"type Outer struct{ Inner Inner `json:\"inner\"` }\n\ntype Inner struct {\n\tF func() `json:\"f\"`\n}\n", "E20", "a func in field Inner.F (web/pages/p/types.go:6)"},
		{"field of a package type", `<ssr:call name="x" in="struct{}" out="Out"/>`,
			"import \"github.com/sergei-svistunov/go-ssr/pkg/mux\"\n\ntype Out struct{ R *mux.Request }\n", "E20", "a mux.Request in field Out.R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			webDir := filepath.Join(t.TempDir(), "web")
			writeTemplate(t, webDir, "p", "<p>x</p>\n"+tc.tmpl)
			if tc.goSrc != "" {
				writeGoFile(t, webDir, "p", "types.go", "package p\n\n"+tc.goSrc)
			}
			diags, err := makeGen(t, webDir).Validate()
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics %v, want one", len(diags), diags)
			}
			d := diags[0]
			wantFix := fixE19
			if tc.code == "E20" {
				wantFix = fixE20
			}
			if d.Code != tc.code || d.Line != 2 || d.File != "web/pages/p/index.html" || d.Fix != wantFix || !strings.Contains(d.Message, tc.msg) {
				t.Errorf("got %s:%d %s %q fix %q, want web/pages/p/index.html:2 %s containing %q", d.File, d.Line, d.Code, d.Message, d.Fix, tc.code, tc.msg)
			}
		})
	}
}

func TestCallsAccepted(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "p", `<html><body><div>
<ssr:call name="a" in="struct{}" out="Out"/>
<ssr:call name="b2" in="map[string]int" out="[]*Out"/>
<ssr:call name="c" in="Out" out="struct{}"/>
<ssr:call name="d" in="map[K]int" out="struct{}"/>
<ssr:call name="e" in="any" out="interface{}"/>
<ssr:call name="f" in="[]any" out="struct{ V any; M map[string]interface{} }"/>
</div></body></html>`)
	writeGoFile(t, webDir, "p", "types.go", "package p\n\ntype Out struct {\n\tF func() `json:\"-\"`\n\tf func()\n\tFn Fn\n}\n\ntype Fn func()\n\nfunc (Fn) MarshalJSON() ([]byte, error) { return []byte(\"1\"), nil }\n\ntype K (int)\n")
	diags, err := makeGen(t, webDir).Validate()
	if err != nil || len(diags) != 0 {
		t.Fatalf("Validate = %v, %v; want no diagnostics", diags, err)
	}
}

const callTSTypes = "package q\n\n" + `import (
	"encoding/json"
	"time"
)

type Base struct {
	ID      int       ` + "`json:\"id\"`" + `
	Created time.Time ` + "`json:\"created\"`" + `
}

type Stamp struct{ v int64 }

func (s Stamp) MarshalJSON() ([]byte, error) { return json.Marshal(s.v) }

type Level int

type Code string

func (c Code) MarshalText() ([]byte, error) { return []byte(c), nil }

type Query struct {
	Base
	Name   string         ` + "`json:\"name\"`" + `
	Note   string         ` + "`json:\"note,omitempty\"`" + `
	Parent *Query         ` + "`json:\"parent\"`" + `
	Data   []byte         ` + "`json:\"data\"`" + `
	Counts map[string]int ` + "`json:\"counts\"`" + `
	When   time.Time      ` + "`json:\"when\"`" + `
	Stamp  Stamp          ` + "`json:\"stamp\"`" + `
	Level  Level
	Tags   []*string ` + "`json:\"tags\"`" + `
	hidden int
	Skip   int    ` + "`json:\"-\"`" + `
	Big    int64  ` + "`json:\"big,string\"`" + `
	Inline struct {
		X int ` + "`json:\"x\"`" + `
	} ` + "`json:\"inline\"`" + `
	Code  Code  ` + "`json:\"code\"`" + `
	Seat  Seat  ` + "`json:\"seat\"`" + `
	Pos   Pos   ` + "`json:\"pos\"`" + `
	Cents Money ` + "`json:\"cents,string\"`" + `
}

type Rank int

func (Rank) MarshalText() ([]byte, error) { return nil, nil }

type Seat Rank

type Point struct{ X int }

func (Point) MarshalText() ([]byte, error) { return nil, nil }

type Pos Point

type Money int64

func (Money) MarshalJSON() ([]byte, error) { return nil, nil }
`

func TestCallTS(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "q", `<ssr:call name="find" in="Query" out="Level"/>
<ssr:call name="other" in="struct{}" out="Missing"/>`)
	writeGoFile(t, webDir, "q", "types.go", callTSTypes)

	g := makeGen(t, webDir)
	var stderr bytes.Buffer
	g.SetOutput(nil, &bytes.Buffer{}, &stderr)
	if err := g.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ts := readGenerated(t, webDir, "q", "__ssr_gen__.ts")
	for _, want := range []string{
		`export type Code = string;

export type Level = number;

export type Missing = unknown;

export type Money = unknown;

export interface Pos {
  X: number;
}

export interface Query {
  id: number;
  created: string;
  name: string;
  note?: string;
  parent: Query | null;
  data: string;
  counts: Record<string, number>;
  when: string;
  stamp: Stamp;
  Level: Level;
  tags: (string | null)[];
  big: string;
  inline: { x: number };
  code: Code;
  seat: Seat;
  pos: Pos;
  cents: Money;
}

export type Seat = number;

export type Stamp = unknown;
`,
		`export type Calls = {
  find: { in: Query; out: Level };
  other: { in: Record<string, never>; out: Missing };
}`,
		"createSsrClient<ReadVars, WriteVars, Calls>(",
		"routeKey: '" + routeKeyOf("/q") + "'",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("__ssr_gen__.ts lacks\n%s\n---\n%s", want, ts)
		}
	}
	for _, want := range []string{
		"web/pages/q/types.go:13: warning: Stamp has a MarshalJSON method; its TypeScript type is unknown\n",
		"web/pages/q/index.html:2: warning: Missing is not declared in the route package; its TypeScript type is unknown\n",
		"web/pages/q/types.go:58: warning: Money has a MarshalJSON method; its TypeScript type is unknown\n",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("warnings lack %q:\n%s", want, stderr.String())
		}
	}
	if n := strings.Count(stderr.String(), "warning:"); n != 3 {
		t.Errorf("%d warnings, want 3:\n%s", n, stderr.String())
	}
}

func TestRouteWithoutCallsHasNoCalls(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "live", `<ssr:var name="n" type="int" reactive="true"/><p>{{ n }}</p>`)
	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	ts := readGenerated(t, webDir, "live", "__ssr_gen__.ts")
	if !strings.Contains(ts, "export type Calls = {};\n") || !strings.Contains(ts, "createSsrClient<ReadVars, WriteVars, Calls>(") {
		t.Errorf("a route without calls must pass an explicit empty Calls type:\n%s", ts)
	}
	if strings.Contains(readGenerated(t, webDir, "live", "ssrroute_gen.go"), "mux.Caller") {
		t.Error("a route without calls implements mux.Caller")
	}
}

var tsDecl = regexp.MustCompile(`(?s)export interface Item \{.*?\n\}\n`)

func TestWriteVarAndCallTypesAgree(t *testing.T) {
	const item = "type Item struct {\n\tName string `json:\"name\"`\n\tTags []string `json:\"tags,omitempty\"`\n\tNext *Item\n}\n"
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "a", `<ssr:var name="item" type="Item" reactive="true" client-writable="true"/><p>{{ item.Name }}</p>
<ssr:call name="save" in="Item" out="[]Item"/>`)
	writeGoFile(t, webDir, "a", "types.go", "package a\n\n"+item)
	writeTemplate(t, webDir, "b", `<ssr:call name="load" in="struct{}" out="Item"/>`)
	writeGoFile(t, webDir, "b", "types.go", "package b\n\n"+item)

	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	a, b := readGenerated(t, webDir, "a", "__ssr_gen__.ts"), readGenerated(t, webDir, "b", "__ssr_gen__.ts")
	const want = "export interface Item {\n  name: string;\n  tags?: string[];\n  Next: Item | null;\n}\n"
	if got := tsDecl.FindAllString(a, -1); len(got) != 1 || got[0] != want {
		t.Errorf("route a declares Item as %q, want once %q", got, want)
	}
	if got := tsDecl.FindAllString(b, -1); len(got) != 1 || got[0] != want {
		t.Errorf("route b declares Item as %q, want once %q", got, want)
	}
	for _, part := range []string{
		"export type ReadVars = {\n  item: string;\n}",
		"export type WriteVars = {\n  item: Item;\n}",
		"  save: { in: Item; out: Item[] };\n",
	} {
		if !strings.Contains(a, part) {
			t.Errorf("route a lacks %q:\n%s", part, a)
		}
	}
}

// TestReadVarsAreStrings checks that every live variable reads as the markup
// string the server rendered, so a type only a read-only variable uses is not
// declared, and that a client-writable struct has its own type.
func TestReadVarsAreStrings(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "p", `<ssr:var name="profile" type="Profile" reactive="true"/>
<ssr:var name="n" type="int" reactive="true" client-writable="true"/>
<ssr:var name="post" type="Post" reactive="true" client-writable="true"/>
<p>{{ profile.Name }} {{ n }} {{ post.Title }}</p>`)
	writeGoFile(t, webDir, "p", "types.go", "package p\n\ntype Profile struct{ Name string }\n\ntype Post struct {\n\tTitle string\n\tTags  []string\n}\n")

	g := makeGen(t, webDir)
	var stderr bytes.Buffer
	g.SetOutput(nil, &bytes.Buffer{}, &stderr)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	ts := readGenerated(t, webDir, "p", "__ssr_gen__.ts")
	for _, part := range []string{
		"export type ReadVars = {\n  n: string;\n  post: string;\n  profile: string;\n}",
		"export type WriteVars = {\n  n: number;\n  post: Post;\n}",
		"export interface Post {\n  Title: string;\n  Tags: string[];\n}",
	} {
		if !strings.Contains(ts, part) {
			t.Errorf("__ssr_gen__.ts lacks %q:\n%s", part, ts)
		}
	}
	if strings.Contains(ts, "Profile") {
		t.Errorf("a type only a read-only variable uses is declared:\n%s", ts)
	}
	if stderr.Len() != 0 {
		t.Errorf("warnings:\n%s", stderr.String())
	}
}

// TestLiveFixtureHasNoWarnings generates testdata/live, whose read-only live
// variables include a render.SafeHTML, and expects no warning.
func TestLiveFixtureHasNoWarnings(t *testing.T) {
	app := filepath.Join(t.TempDir(), "app")
	if err := os.CopyFS(app, os.DirFS("testdata/live")); err != nil {
		t.Fatal(err)
	}
	g := makeGen(t, filepath.Join(app, "web"))
	var stderr bytes.Buffer
	g.SetOutput(nil, &bytes.Buffer{}, &stderr)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Errorf("warnings:\n%s", stderr.String())
	}
}

func TestInventoryListsCalls(t *testing.T) {
	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, ".", `<html><body><ssr:content default="p"/></body></html>`)
	writeTemplate(t, webDir, "p", `<ssr:call name="zeta" in="struct{}" out="struct{}"/>
<ssr:call name="alpha" in="struct{}" out="int"/>`)
	writeTemplate(t, webDir, "q", `<p>q</p>`)
	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	inv, err := g.Inventory("")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inv.WSEndpoints, []string{"/p"}) {
		t.Errorf("WSEndpoints = %v, want [/p]", inv.WSEndpoints)
	}
	for _, r := range inv.Routes {
		switch r.Path {
		case "/p":
			if !slices.Equal(r.Calls, []string{"alpha", "zeta"}) || r.Reactive || r.WSEndpoint != "/p/__ws" {
				t.Errorf("/p: Calls %v, Reactive %v, WSEndpoint %q", r.Calls, r.Reactive, r.WSEndpoint)
			}
			if !slices.Equal(r.DataProviderMethods, []string{"Data", "CallAlpha", "CallZeta"}) {
				t.Errorf("/p: DataProviderMethods = %v", r.DataProviderMethods)
			}
		default:
			if r.Calls != nil {
				t.Errorf("%s: Calls = %v, want none", r.Path, r.Calls)
			}
		}
	}
}

func TestClientWritableRenderTypeRefused(t *testing.T) {
	const render = "import \"github.com/sergei-svistunov/go-ssr/pkg/render\"\n\n"
	for _, tc := range []struct{ name, typ, goSrc, holds string }{
		{"render type", "render.SafeHTML", "", "a render.SafeHTML"},
		{"pointer", "*render.SafeHTML", "", "a render.SafeHTML"},
		{"slice", "[]render.SafeHTML", "", "a render.SafeHTML"},
		{"map", "map[string]render.SafeHTML", "", "a render.SafeHTML"},
		{"aliased import", "H", "import r \"github.com/sergei-svistunov/go-ssr/pkg/render\"\n\ntype H r.SafeHTML\n", "a render.SafeHTML"},
		{"defined type", "H", render + "type H render.SafeHTML\n", "a render.SafeHTML"},
		{"alias", "H", render + "type H = render.SafeHTML\n", "a render.SafeHTML"},
		{"field", "Post", render + "type Post struct {\n\tTitle string\n\tBody  render.SafeHTML\n}\n", "a render.SafeHTML in field Post.Body (web/pages/p/types.go:7)"},
		{"field through pointer, slice and map", "Post", render + "type Post struct{ Parts map[string][]*Part }\n\ntype Part struct{ Body render.SafeHTML }\n", "a render.SafeHTML in field Part.Body"},
		{"type that marshals itself", "Post", render + "type Post struct{ Body render.SafeHTML }\n\nfunc (Post) MarshalJSON() ([]byte, error) { return nil, nil }\n", "a render.SafeHTML in field Post.Body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			webDir := filepath.Join(t.TempDir(), "web")
			writeTemplate(t, webDir, "p", "<p>x</p>\n<ssr:var name=\"h\" type=\""+tc.typ+"\" reactive=\"true\" client-writable=\"true\"/>")
			if tc.goSrc != "" {
				writeGoFile(t, webDir, "p", "types.go", "package p\n\n"+tc.goSrc)
			}
			diags, err := makeGen(t, webDir).Validate()
			if err != nil {
				t.Fatal(err)
			}
			want := "<ssr:var name=\"h\"> is client-writable, but its type " + tc.typ + " holds " + tc.holds
			if len(diags) != 1 || diags[0].File != "web/pages/p/index.html" || diags[0].Line != 2 || !strings.Contains(diags[0].Message, want) {
				t.Errorf("got %v, want %q at web/pages/p/index.html:2", diags, want)
			}
		})
	}
}

func TestClientWritableGenericTypeRefused(t *testing.T) {
	const render = "import \"github.com/sergei-svistunov/go-ssr/pkg/render\"\n\n"
	const box = "type Box[T any] struct{ V T }\n"
	for _, tc := range []struct{ name, typ, goSrc, uses string }{
		{"instantiation", "Box[render.SafeHTML]", render + box, "the generic type Box[render.SafeHTML]"},
		{"alias", "H", render + box + "type H = Box[render.SafeHTML]\n", "the generic type Box[render.SafeHTML]"},
		{"generic with a render field", "Box[string]", render + "type Box[T any] struct {\n\tV    T\n\tBody render.SafeHTML\n}\n", "the generic type Box[string]"},
		{"field", "Post", render + box + "type Post struct{ B Box[Part] }\n\ntype Part struct{ Body render.SafeHTML }\n", "the generic type Box[Part] in field Post.B (web/pages/p/types.go:6)"},
		{"plain fields only", "Box[string]", box, "the generic type Box[string]"},
		{"two type arguments", "Pair[string, int]", "type Pair[K, V any] struct{ K K; V V }\n", "the generic type Pair[string, int]"},
		{"embedded unexported", "Post", box + "type Post struct{ box[string] }\n\ntype box[T any] struct{ V T }\n", "the generic type box[string] in field Post.box (web/pages/p/types.go:4)"},
		{"embedded unexported alias", "Post", render + "type Post struct{ h }\n\ntype h = box[render.SafeHTML]\n\ntype box[T any] struct{ V T }\n", "the generic type box[render.SafeHTML] in field Post.h (web/pages/p/types.go:5)"},
		{"embedded unexported defined type", "Post", render + "type Post struct{ h }\n\ntype h box[render.SafeHTML]\n\ntype box[T any] struct{ V T }\n", "the generic type box[render.SafeHTML] in field Post.h (web/pages/p/types.go:5)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			webDir := filepath.Join(t.TempDir(), "web")
			writeTemplate(t, webDir, "p", "<p>x</p>\n<ssr:var name=\"h\" type=\""+tc.typ+"\" reactive=\"true\" client-writable=\"true\"/>")
			writeGoFile(t, webDir, "p", "types.go", "package p\n\n"+tc.goSrc)
			diags, err := makeGen(t, webDir).Validate()
			if err != nil {
				t.Fatal(err)
			}
			want := "<ssr:var name=\"h\"> is client-writable, but its type " + tc.typ + " uses " + tc.uses +
				"; client-writable variables cannot use generic types, so declare a concrete struct instead"
			if len(diags) != 1 || diags[0].File != "web/pages/p/index.html" || diags[0].Line != 2 || !strings.Contains(diags[0].Message, want) {
				t.Errorf("got %v, want %q at web/pages/p/index.html:2", diags, want)
			}
		})
	}
}

func TestUnresolvedQualifierRefused(t *testing.T) {
	for _, tc := range []struct{ typ, pkg string }{{"tme.Time", "tme"}, {"[]model.Item", "model"}} {
		t.Run(tc.typ, func(t *testing.T) {
			webDir := filepath.Join(t.TempDir(), "web")
			writeTemplate(t, webDir, "p", "<p>x</p>\n<ssr:call name=\"x\" in=\""+tc.typ+"\" out=\"struct{}\"/>")
			diags, err := makeGen(t, webDir).Validate()
			if err != nil {
				t.Fatal(err)
			}
			want := "names the package " + tc.pkg + ", which no Go file of the route imports and the standard library does not have"
			if len(diags) != 1 || diags[0].File != "web/pages/p/index.html" || diags[0].Line != 2 || !strings.Contains(diags[0].Message, want) {
				t.Errorf("got %v, want %q at web/pages/p/index.html:2", diags, want)
			}
		})
	}
}

// TestGoEnvFailure resolves a standard library qualifier in a generator that
// does not know its GOROOT, with a go command that fails.
func TestGoEnvFailure(t *testing.T) {
	saved := build.Default.GOROOT
	build.Default.GOROOT = ""
	t.Cleanup(func() { build.Default.GOROOT = saved })
	t.Cleanup(generator.SetGoEnvCommand(func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "no-go"), "env", "GOROOT")
	}))

	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "p", "<p>x</p>\n<ssr:call name=\"x\" in=\"time.Time\" out=\"struct{}\"/>")
	diags, err := makeGen(t, webDir).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].File != "web/pages/p/index.html" || diags[0].Line != 2 ||
		!strings.Contains(diags[0].Message, "GOROOT could not be found") || !strings.Contains(diags[0].Message, "set the GOROOT environment variable") {
		t.Errorf("got %v, want a GOROOT error at web/pages/p/index.html:2", diags)
	}
}

// TestStdlibImportWithoutGOROOT generates a call of a standard library type,
// which no file of the route imports, in a generator that does not know its
// GOROOT, as one built with -trimpath.
func TestStdlibImportWithoutGOROOT(t *testing.T) {
	saved := build.Default.GOROOT
	build.Default.GOROOT = ""
	t.Cleanup(func() { build.Default.GOROOT = saved })

	webDir := filepath.Join(t.TempDir(), "web")
	writeTemplate(t, webDir, "p", `<ssr:call name="at" in="time.Time" out="struct{}"/>`)
	g := makeGen(t, webDir)
	if err := g.Analyze(); err != nil {
		t.Fatal(err)
	}
	if err := g.Generate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ssrroute_gen.go", "dataprovider.go"} {
		if src := readGenerated(t, webDir, "p", name); !strings.Contains(src, `time "time"`) {
			t.Errorf("%s does not import time:\n%s", name, src)
		}
	}
}
