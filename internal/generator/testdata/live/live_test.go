package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"example.com/app/web/pages"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var (
	siteRe = regexp.MustCompile(`<span data-ssr-bind="([^"]+)">(.*?)</span>|<ssr-block data-ssr-bind="([^"]+)">(.*?)</ssr-block>`)
	idRe   = regexp.MustCompile(`id="(\w+)"><(?:span|ssr-block) data-ssr-bind="([^"]+)"`)
)

// livePage is a page's first render and what its live connection sent.
type livePage struct {
	body     string
	routeKey string
	key      map[string]string // element id → binding key of the site inside it
	sites    map[string]string // binding key → markup the first render wrote for it
	init     map[string]string
	patches  map[string]string
	conn     *websocket.Conn
}

type frame struct {
	T        string            `json:"t"`
	RouteKey string            `json:"routeKey"`
	Bindings map[string]string `json:"bindings"`
	Key      string            `json:"key"`
	HTML     string            `json:"html"`
	Var      string            `json:"var"`
	Code     string            `json:"code"`
}

// openPage renders the page, opens its live connection, reads the init frame and the
// patch that Subscribe sends for every site, and checks that each site's live value is
// byte-equal to what the first render wrote for it.
func openPage(t *testing.T) *livePage {
	t.Helper()
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d %v\n%s", resp.StatusCode, err, b)
	}
	p := &livePage{body: string(b), key: map[string]string{}, sites: map[string]string{}, patches: map[string]string{}}
	for _, m := range siteRe.FindAllStringSubmatch(p.body, -1) {
		k, markup := m[1]+m[3], m[2]+m[4]
		if prev, ok := p.sites[k]; ok && prev != markup {
			t.Errorf("sites with the key %s rendered %q and %q", k, prev, markup)
		}
		p.sites[k] = markup
	}
	for _, m := range idRe.FindAllStringSubmatch(p.body, -1) {
		p.key[m[1]] = m[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	p.conn, _, err = websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/__ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.conn.CloseNow() })
	init := p.read(t, "init")
	p.routeKey, p.init = init.RouteKey, init.Bindings
	for len(p.patches) < len(p.init) {
		f := p.read(t, "patch")
		p.patches[f.Key] = f.HTML
	}

	if len(p.sites) != len(p.init) {
		t.Errorf("the page has sites %v, the init frame %v", p.sites, p.init)
	}
	for k, want := range p.sites {
		if p.init[k] != want || p.patches[k] != want {
			t.Errorf("site %s: first render %q, init %q, patch %q", k, want, p.init[k], p.patches[k])
		}
	}
	return p
}

// read reads the next frame, which must be of type typ.
func (p *livePage) read(t *testing.T, typ string) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var f frame
	if err := wsjson.Read(ctx, p.conn, &f); err != nil {
		t.Fatalf("reading a %s frame: %v", typ, err)
	}
	if f.T != typ {
		t.Fatalf("got frame %+v, want %s", f, typ)
	}
	return f
}

// site checks that the site inside the element id sends want, as the first render wrote it.
func (p *livePage) site(t *testing.T, id, want string) string {
	t.Helper()
	k := p.key[id]
	if k == "" {
		t.Fatalf("no live site inside #%s:\n%s", id, p.body)
	}
	if p.sites[k] != want || p.init[k] != want || p.patches[k] != want {
		t.Errorf("#%s: first render %q, init %q, patch %q, want %q", id, p.sites[k], p.init[k], p.patches[k], want)
	}
	return k
}

func TestLiveTextValueIsEscaped(t *testing.T) {
	const want = `&lt;b&gt;x&lt;/b&gt; &amp; &#34;y&#34;`
	p := openPage(t)
	if !strings.Contains(p.body, "<title>"+want+"</title>") {
		t.Errorf("the title is not escaped text:\n%s", p.body)
	}
	k := p.site(t, "text", want)

	value, _ := json.Marshal(`<b>x</b> & "y"`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, p.conn, map[string]any{"t": "write", "routeKey": p.routeKey, "var": "text", "value": json.RawMessage(value)}); err != nil {
		t.Fatal(err)
	}
	if f := p.read(t, "ack"); f.Var != "text" {
		t.Errorf("ack for %q", f.Var)
	}
	if f := p.read(t, "patch"); f.Key != k || f.HTML != want {
		t.Errorf("echo of the write: %+v, want %s = %q", f, k, want)
	}
}

func TestLiveSafeHTMLValue(t *testing.T) {
	openPage(t).site(t, "markup", "<b>ok</b>")
}

func TestBlockValueIsEscapedMarkup(t *testing.T) {
	openPage(t).site(t, "block", "<p>&lt;script&gt;</p>")
}

func TestRawOutputWritesAnyValue(t *testing.T) {
	p := openPage(t)
	if p.site(t, "raw", "<b>ok</b>") == p.site(t, "esc", "&lt;b&gt;ok&lt;/b&gt;") {
		t.Error("the {{$ s }} and {{ s }} sites share a binding key")
	}
}

// TestScalarWritesTakeText writes the JSON strings an ssr:bind input sends to
// int, float64 and bool variables: a value of the type is taken, anything else
// is refused with validation_failed and leaves the variable as it was.
func TestScalarWritesTakeText(t *testing.T) {
	p := openPage(t)
	for _, tc := range []struct {
		v, value, want string // want is the patch, or "" for a refusal
	}{
		{"count", `"42"`, "42"},
		{"count", `7`, "7"},
		{"count", `"-3"`, "-3"},
		{"count", `"x"`, ""},
		{"count", `""`, ""},
		{"count", `"1.5"`, ""},
		{"count", `" 4"`, ""},
		{"count", `"99999999999999999999"`, ""},
		{"ratio", `"2.5"`, "2.5"},
		{"ratio", `0.25`, "0.25"},
		{"ratio", `"abc"`, ""},
		{"ratio", `""`, ""},
		{"ratio", `"NaN"`, ""},
		{"ratio", `"Inf"`, ""},
		{"on", `"true"`, "true"},
		{"on", `false`, "false"},
		{"on", `"maybe"`, ""},
		{"on", `""`, ""},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := wsjson.Write(ctx, p.conn, map[string]any{"t": "write", "routeKey": p.routeKey, "var": tc.v, "value": json.RawMessage(tc.value)})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == "" {
			if f := p.read(t, "err"); f.Var != tc.v || f.Code != "validation_failed" {
				t.Errorf("%s = %s: %+v, want a validation_failed err", tc.v, tc.value, f)
			}
			continue
		}
		if f := p.read(t, "ack"); f.Var != tc.v {
			t.Errorf("%s = %s: ack for %q", tc.v, tc.value, f.Var)
		}
		if f := p.read(t, "patch"); f.Key != p.key[tc.v] || f.HTML != tc.want {
			t.Errorf("%s = %s: patch %+v, want %s = %q", tc.v, tc.value, f, p.key[tc.v], tc.want)
		}
	}
}
