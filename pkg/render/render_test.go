package render

import (
	"strings"
	"testing"
)

func write(t *testing.T, f func(*strings.Builder) error) string {
	t.Helper()
	var b strings.Builder
	if err := f(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestFormat(t *testing.T) {
	for v, want := range map[any]string{
		nil: "", "s": "s", true: "true", 42: "42", int8(-3): "-3", int16(5): "5", int32(6): "6", int64(7): "7",
		uint(8): "8", uint8(9): "9", uint16(10): "10", uint32(11): "11", uint64(12): "12",
		1.5: "1.5", float32(0.1): "0.1", HTMLConst("<b>"): "<b>",
	} {
		if got := Format(v); got != want {
			t.Errorf("Format(%T %v) = %q, want %q", v, v, got, want)
		}
	}
}

func TestTextAndAttrEscape(t *testing.T) {
	for in, want := range map[string]string{
		`<script>alert("x")</script>`:    "&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;",
		`"><img src=x onerror=alert(1)>`: "&#34;&gt;&lt;img src=x onerror=alert(1)&gt;",
		"a & 'b'":                        "a &amp; &#39;b&#39;",
		"nul\x00":                        "nul\uFFFD",
		"plain":                          "plain",
	} {
		if got := write(t, func(b *strings.Builder) error { return Text(b, in) }); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
		if got := write(t, func(b *strings.Builder) error { return Attr(b, in) }); got != want {
			t.Errorf("Attr(%q) = %q, want %q", in, got, want)
		}
	}
	if got := write(t, func(b *strings.Builder) error { return Text(b, HTMLConst("<b>")) }); got != "&lt;b&gt;" {
		t.Errorf("Text(HTMLConst) = %q", got)
	}
}

func TestURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/a?b=c&d=e": "https://example.com/a?b=c&amp;d=e",
		"/users/42":                     "/users/42",
		"mailto:a@b.example":            "mailto:a@b.example",
		"HTTP://X.example":              "HTTP://X.example",
		"page?x=1":                      "page?x=1",
		"//cdn.example/x":               "//cdn.example/x",
		"/a:b":                          "/a:b",
		`/a"><script>`:                  "/a%22%3e%3cscript%3e",
		"/a b":                          "/a%20b",
		"/100%":                         "/100%25",
		"/a%zz":                         "/a%25zz",
		"/a%41":                         "/a%41",
		"javascript:alert(1)":           UnsafeURL,
		"JaVaScRiPt:alert(1)":           UnsafeURL,
		" javascript:alert(1)":          UnsafeURL,
		"java\tscript:alert(1)":         UnsafeURL,
		"data:text/html,<script>":       UnsafeURL,
		"vbscript:x":                    UnsafeURL,
		"a:b":                           UnsafeURL,
		"http://[::1/x":                 UnsafeURL,
		"https://x.example:port/":       UnsafeURL,
	} {
		if got := write(t, func(b *strings.Builder) error { return URL(b, in) }); got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURLParts(t *testing.T) {
	for _, c := range []struct {
		f        func(b *strings.Builder, v any) error
		in, want string
	}{
		{func(b *strings.Builder, v any) error { return URLPart(b, v) }, "a b/c?d&e", "a%20b/c?d&amp;e"},
		{func(b *strings.Builder, v any) error { return URLPart(b, v) }, "javascript:x", "javascript:x"},
		{func(b *strings.Builder, v any) error { return URLPart(b, v) }, "/evil.example", "/evil.example"},
		{func(b *strings.Builder, v any) error { return URLPart(b, v) }, "100%", "100%25"},
		{func(b *strings.Builder, v any) error { return URLQuery(b, v) }, "a b&c=d/é", "a%20b%26c%3dd%2f%c3%a9"},
	} {
		if got := write(t, func(b *strings.Builder) error { return c.f(b, c.in) }); got != c.want {
			t.Errorf("%q gives %q, want %q", c.in, got, c.want)
		}
	}
}

// A value right after a URL's leading "/" never makes the URL start with "//" or "/\", which
// browsers read as another site.
func TestURLPathStart(t *testing.T) {
	for in, want := range map[string]string{
		"notes/1":        "notes/1",
		"a b?c":          "a%20b?c",
		"/evil.example":  "%2fevil.example",
		"//evil.example": "%2f/evil.example",
		`\evil.example`:  "%5cevil.example",
		"\t/evil":        "%09/evil",
		"":               ".",
	} {
		if got := write(t, func(b *strings.Builder) error { return URLPathStart(b, in) }); got != want {
			t.Errorf("URLPathStart(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSON(t *testing.T) {
	out := write(t, func(b *strings.Builder) error { return JSON(b, "</script><!--&\u2028") })
	if want := `"\u003c/script\u003e\u003c!--\u0026\u2028"`; out != want {
		t.Errorf("JSON = %s, want %s", out, want)
	}
	var b strings.Builder
	if err := JSON(&b, func() {}); err == nil {
		t.Error("JSON of a func: no error")
	}
}

func TestHTML(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{HTMLConst("<b>x</b>"), "<b>x</b>"},
		{"<b>x</b> & y", "<b>x</b> & y"},
		{7, "7"},
		{nil, ""},
	} {
		if got := write(t, func(b *strings.Builder) error { return HTML(b, c.v) }); got != c.want {
			t.Errorf("HTML(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestTextEscapes(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"plain text", "hello", "hello"},
		{"ampersand", "a&b", "a&amp;b"},
		{"less than", "a<b", "a&lt;b"},
		{"greater than", "a>b", "a&gt;b"},
		{"double quote", `a"b`, "a&#34;b"},
		{"single quote", "a'b", "a&#39;b"},
		{"carriage return", "a\rb", "a&#13;b"},
		{"multiple specials", `<script>alert("xss")</script>`, "&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;"},
		{"empty string", "", ""},
		{"no specials", "hello world 123", "hello world 123"},
		{"all specials together", "&'<>\"\r", "&amp;&#39;&lt;&gt;&#34;&#13;"},
		{"int", 42, "42"},
		{"int negative", -7, "-7"},
		{"int8", int8(8), "8"},
		{"int16", int16(16), "16"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"uint", uint(10), "10"},
		{"uint8", uint8(8), "8"},
		{"uint16", uint16(16), "16"},
		{"uint32", uint32(32), "32"},
		{"uint64", uint64(64), "64"},
		{"float32", float32(3.14), "3.14"},
		{"float64", float64(2.718), "2.718"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := write(t, func(b *strings.Builder) error { return Text(b, tc.input) }); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
	if got := write(t, func(b *strings.Builder) error { return HTML(b, HTMLConst("<b>bold</b>")) }); got != "<b>bold</b>" {
		t.Fatalf("expected raw HTML, got %q", got)
	}
}
