package app_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"example.com/app/web/pages"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// The page names its values w, c and data, as the generated code once named its writer,
// its state and its route data, and renders them.
func TestNames(t *testing.T) {
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d %q", w.Code, body)
	}
	for _, want := range []string{
		`<ul id="pairs"><li>0=a</li><li>1=b</li></ul>`,
		`<p id="stars"><b>*</b><b>*</b><i>*</i><i>*</i></p>`,
		`2 &gt; 1</p></ssr-block>`,
		`<input name="text" class="a ">`,
		`<input name="text" class="b ">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET / does not write %s:\n%s", want, body)
		}
	}

	// A live value inside a form is a live site, as it is anywhere else.
	m := regexp.MustCompile(`<p id="live"><span data-ssr-bind="([^"]+)">2</span></p>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("GET / does not write the live value in the form as a live site:\n%s", body)
	}
	if got := pages.Snapshot(&pages.RouteData{Data: 3})[m[1]]; got != "3" {
		t.Errorf("the live value of %s is %q, want 3", m[1], got)
	}
}
