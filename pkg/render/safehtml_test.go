package render

import (
	"encoding"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeHTML(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{HTMLConst("<b>hi</b>").String(), "<b>hi</b>"},
		{EscapeHTML(`<b>"x" & 'y'`).String(), "&lt;b&gt;&#34;x&#34; &amp; &#39;y&#39;"},
		{JoinHTML(HTMLConst("<i>a</i>"), EscapeHTML("<b>")).String(), "<i>a</i>&lt;b&gt;"},
		{SafeHTML{}.String(), ""},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// HTMLConst accepts a string constant; a string variable passed to it does not compile.
func TestHTMLConstRejectsVariables(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not in PATH")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	build := func(body string) (string, error) {
		dir := t.TempDir()
		files := map[string]string{
			"go.mod": "module probe\n\n" + goLine(t, root) + "\n\nrequire github.com/sergei-svistunov/go-ssr v0.0.0\n\n" +
				"replace github.com/sergei-svistunov/go-ssr => " + root + "\n",
			"go.sum":  string(sum),
			"main.go": "package main\n\nimport \"github.com/sergei-svistunov/go-ssr/pkg/render\"\n\nfunc main() {\n" + body + "\n}\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.CommandContext(t.Context(), "go", "build", "-o", os.DevNull, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := build(`_ = render.HTMLConst("<b>x</b>")`); err != nil {
		if strings.Contains(out, "GOPROXY=off") {
			t.Skip("the module cache lacks a dependency and the probe builds offline: run go mod download")
		}
		t.Fatalf("a constant does not compile: %v\n%s", err, out)
	}
	out, err := build("markup := \"<b>x</b>\"\n_ = render.HTMLConst(markup)")
	if err == nil {
		t.Fatal("a string variable compiles")
	}
	if !strings.Contains(out, "stringConstant") {
		t.Fatalf("unexpected build output:\n%s", out)
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

// decodeLive decodes a value the page sends for a live variable of type T, as live writes do.
func decodeLive[T any](data string) (T, error) {
	var v T
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}

// A live variable of type SafeHTML never takes markup from what the page sends.
func TestSafeHTMLDoesNotDecode(t *testing.T) {
	for _, data := range []string{`"<img src=x onerror=alert(1)>"`, `{"s":"<img src=x onerror=alert(1)>"}`} {
		if h, err := decodeLive[SafeHTML](data); err == nil {
			t.Errorf("%s decodes into SafeHTML as %q", data, h.String())
		}
	}
}

type box[T any] struct{ V T }

type embedded = box[SafeHTML]

// No path through a decoded value reaches a SafeHTML.
func TestSafeHTMLRefusesDecoding(t *testing.T) {
	const markup = `"<img src=x onerror=alert(1)>"`
	for name, decode := range map[string]func() error{
		"value":          func() error { _, err := decodeLive[SafeHTML](markup); return err },
		"pointer":        func() error { _, err := decodeLive[*SafeHTML](markup); return err },
		"slice":          func() error { _, err := decodeLive[[]SafeHTML](`[` + markup + `]`); return err },
		"map":            func() error { _, err := decodeLive[map[string]SafeHTML](`{"k":` + markup + `}`); return err },
		"field":          func() error { _, err := decodeLive[struct{ Body SafeHTML }](`{"Body":` + markup + `}`); return err },
		"embedded alias": func() error { _, err := decodeLive[struct{ embedded }](`{"V":` + markup + `}`); return err },
		"text": func() error {
			u, ok := any(new(SafeHTML)).(encoding.TextUnmarshaler)
			if !ok {
				return nil
			}
			return u.UnmarshalText([]byte("<b>"))
		},
		"text map key": func() error { _, err := decodeLive[map[SafeHTML]int](`{"<b>":1}`); return err },
	} {
		if err := decode(); err == nil || !strings.Contains(err.Error(), "markup cannot be decoded from input") {
			t.Errorf("%s: got %v, want a refusal", name, err)
		}
	}
}

// SafeHTML encodes as before: it has no marshaler and no exported field.
func TestSafeHTMLEncoding(t *testing.T) {
	for _, v := range []any{SafeHTML{}, &SafeHTML{}} {
		if _, ok := v.(json.Marshaler); ok {
			t.Errorf("%T is a json.Marshaler", v)
		}
		if _, ok := v.(encoding.TextMarshaler); ok {
			t.Errorf("%T is an encoding.TextMarshaler", v)
		}
	}
	if b, err := json.Marshal(struct{ H SafeHTML }{HTMLConst("<b>x</b>")}); err != nil || string(b) != `{"H":{}}` {
		t.Errorf("got %s, %v, want {\"H\":{}}", b, err)
	}
}
