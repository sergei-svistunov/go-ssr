package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"example.com/app/web/pages"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// A browser parsing the page reads every textarea's value as the page set it, a leading
// newline included, and a literal textarea as its template reads.
func TestTextareaValues(t *testing.T) {
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d %q", w.Code, w.Body.String())
	}
	doc, err := html.Parse(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "textarea" {
			continue
		}
		var text strings.Builder
		for c := range n.ChildNodes() {
			text.WriteString(c.Data)
		}
		for _, a := range n.Attr {
			if a.Key == "name" || a.Key == "id" {
				got[a.Val] = text.String()
			}
		}
	}
	for key, want := range map[string]string{"lead": "\nabc", "plain": "abc", "value": "\nabc", "literal": "abc"} {
		if got[key] != want {
			t.Errorf("textarea %s = %q, want %q:\n%s", key, got[key], want, w.Body.String())
		}
	}
}
