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

var tokenField = regexp.MustCompile(`<input type="hidden" name="_csrf_token_" value="note:([^"]+)">`)

func TestFormIf(t *testing.T) {
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?show=1", nil))
	body := w.Body.String()
	m := tokenField.FindStringSubmatch(body)
	if w.Code != http.StatusOK || m == nil || !strings.Contains(body, `<form method="post" enctype="application/x-www-form-urlencoded" class="note">`) || strings.Contains(body, "No form.") {
		t.Fatalf("GET /?show=1 = %d, want the form and its token without the else branch:\n%s", w.Code, body)
	}
	if strings.Contains(body, "ssr:") {
		t.Errorf("GET /?show=1 writes an ssr: attribute:\n%s", body)
	}
	cookie, token := w.Result().Cookies()[0], m[1]

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body = w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(body, "<form") || strings.Contains(body, "_csrf_token_") || !strings.Contains(body, `<p id="none">No form.</p>`) {
		t.Fatalf("GET / = %d, want the else branch without the form or its token:\n%s", w.Code, body)
	}
	if strings.Contains(body, "ssr:") {
		t.Errorf("GET / writes an ssr: attribute:\n%s", body)
	}

	// The condition decides what renders; the form is processed whether it holds or not.
	fields := url.Values{"text": {"x"}, "_csrf_token_": {"note:" + token}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || pages.Processed != 1 {
		t.Errorf("POST / = %d with %d runs of ProcessNote, want 200 and 1", w.Code, pages.Processed)
	}
}
