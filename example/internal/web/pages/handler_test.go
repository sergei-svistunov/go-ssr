package pages_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web"
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
	ssrHandler = ctxMiddleware{
		pages.NewSsrHandler(
			web.NewDataProvider(&model.Model{}), mux.Options{},
		),
	}
	recorder *httptest.ResponseRecorder
	req1     = httptest.NewRequest(http.MethodGet, "/home", nil)
	req2     = httptest.NewRequest(http.MethodGet, "/users/johndoe123/info", nil)
	headers  = map[string][]string{}
)

var dw = DiscardWriter{}

func BenchmarkSsrHandler(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ssrHandler.ServeHTTP(dw, req2)
	}
}
