package render

import (
	"html"
	"net/url"
	"strings"
	"testing"
)

func FuzzAttr(f *testing.F) {
	f.Add(`"><script>alert(1)</script>`)
	f.Add("&amp;\x00'")
	f.Fuzz(func(t *testing.T, s string) {
		var b strings.Builder
		if err := Attr(&b, s); err != nil {
			t.Fatal(err)
		}
		out := b.String()
		if strings.ContainsAny(out, `<>"'`) {
			t.Fatalf("Attr(%q) = %q", s, out)
		}
		if got, want := html.UnescapeString(out), strings.ReplaceAll(s, "\x00", "\uFFFD"); got != want {
			t.Fatalf("Attr(%q) decodes to %q, want %q", s, got, want)
		}
	})
}

// checkURL fails unless u parses and is relative or has an allowed scheme, or is UnsafeURL.
func checkURL(t *testing.T, u string) {
	t.Helper()
	for i := 0; i < len(u); i++ {
		if u[i] <= 0x20 || u[i] >= 0x7f {
			t.Fatalf("byte %#x in %q", u[i], u)
		}
	}
	if u == UnsafeURL {
		return
	}
	p, err := url.Parse(u)
	if err != nil {
		t.Fatalf("%q does not parse: %v", u, err)
	}
	switch p.Scheme {
	case "", "http", "https", "mailto":
	default:
		t.Fatalf("%q has the scheme %q", u, p.Scheme)
	}
}

func FuzzURL(f *testing.F) {
	for _, s := range []string{"javascript:alert(1)", "https://x.example/?a=b", " java\nscript:x", "/p", "/100%", "http://[::1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var b strings.Builder
		if err := URL(&b, s); err != nil {
			t.Fatal(err)
		}
		checkURL(t, html.UnescapeString(b.String()))
	})
}

func FuzzURLPathStart(f *testing.F) {
	for _, s := range []string{"/evil.example", `\\evil`, "", "notes/1", "a#%z"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var b strings.Builder
		if err := URLPathStart(&b, s); err != nil {
			t.Fatal(err)
		}
		out := html.UnescapeString(b.String())
		if out == "" || out[0] == '/' || out[0] == '\\' {
			t.Fatalf("URLPathStart(%q) makes the URL /%s", s, out)
		}
		checkURL(t, "/"+out)
	})
}
