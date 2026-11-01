package pages_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/pages"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type ctxMiddleware struct {
	h http.Handler
}

func (mw ctxMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mw.h.ServeHTTP(w, r.WithContext(ctxdata.ToContext(context.Background(), &ctxdata.Data{})))
}

type DiscardWriter struct{}

func (d DiscardWriter) Header() http.Header { return headers }

func (d DiscardWriter) Write(bytes []byte) (int, error) {
	return len(bytes), nil
}

func (d DiscardWriter) WriteHeader(int) {}

var (
	ssrHandler = ctxMiddleware{newSsrHandler()}
	req1       = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/home", nil)
	req2       = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/johndoe123/info", nil)
	headers    = map[string][]string{}
	dw         = DiscardWriter{}
)

func newSsrHandler() http.Handler {
	h, err := pages.NewSsrHandler(&model.Model{}, mux.Options{CSRFKey: []byte("bench-key-bench-key-bench-key-32")})
	if err != nil {
		panic(err)
	}
	return h
}

func BenchmarkSsrHandlerSimple(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ssrHandler.ServeHTTP(dw, req1)
	}
}

func BenchmarkSsrHandlerDeep(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ssrHandler.ServeHTTP(dw, req2)
	}
}

func TestExampleUserGuard(t *testing.T) {
	for path, want := range map[string]int{
		"/users/nosuchuser/info": http.StatusNotFound,
		"/users/johndoe123/info": http.StatusOK,
	} {
		w := httptest.NewRecorder()
		ssrHandler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("GET %s = %d, want %d", path, w.Code, want)
		}
	}
}

func TestErrorPageEscapesTheError(t *testing.T) {
	w := httptest.NewRecorder()
	pages.WriteError(w, mux.NewRequest(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)), errors.New("<b>x</b>"))
	if body := w.Body.String(); !strings.Contains(body, "&lt;b&gt;x&lt;/b&gt;") || strings.Contains(body, "<b>x</b>") {
		t.Errorf("error page body = %q, want the error text escaped", body)
	}
}
