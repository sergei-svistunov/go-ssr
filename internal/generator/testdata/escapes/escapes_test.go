package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/app/web/pages"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

func TestPageEscapes(t *testing.T) {
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
		`<p id="text">&#34;&gt;&lt;img src=x onerror=alert(1)&gt;</p>`,
		`<p id="attr" class="c &#34;&gt;&lt;img src=x onerror=alert(1)&gt;">`,
		`<a id="part" href="/u/%22%3e%3cimg%20src=x%20onerror=alert%281%29%3e">`,
		`<a id="abs" href="https://h.example/u/%22%3e%3cimg%20src=x%20onerror=alert%281%29%3e">`,
		`<a id="whole" href="about:invalid#gossr-unsafe-url">`,
		`<div id="markup"><b>ok</b></div>`,
		`<input name="q" type="text" value="&#34;&gt;&lt;img src=x onerror=alert(1)&gt;">`,
		`<textarea name="t">&lt;/textarea&gt;&lt;img src=x onerror=alert(1)&gt;</textarea>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<img") {
		t.Errorf("a value became markup:\n%s", body)
	}
}
