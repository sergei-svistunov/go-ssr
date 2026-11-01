package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"example.com/app/web/pages"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// The data provider the generator writes for a page with a form renders the form, whose only
// live site is inside it, and refuses every submission of the form.
func TestFormStub(t *testing.T) {
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	m := regexp.MustCompile(`name="_csrf_token_" value="note:([^"]+)"`).FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || m == nil || !strings.Contains(w.Body.String(), `<p id="count"><span data-ssr-bind=`) {
		t.Fatalf("GET / = %d, want the form with its live site:\n%s", w.Code, w.Body.String())
	}

	fields := url.Values{"text": {"x"}, "_csrf_token_": {"note:" + m[1]}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range w.Result().Cookies() {
		req.AddCookie(c)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("POST / = %d, want 403", w.Code)
	}
}
