package web

import (
	"errors"
	"log"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/pages"
	ssrMux "github.com/sergei-svistunov/go-ssr/pkg/mux"
)

func New(d *model.Model) http.Handler {
	mux := http.NewServeMux()

	ssrHandler := pages.NewSsrHandler(d, ssrMux.Options{
		ErrorHandler: handleError,
		// The CSRF cookie is Secure-only. Uncomment for plain-http
		// development on a host other than localhost, or in Safari (which
		// drops Secure cookies on http://localhost too); never enable in
		// production.
		// InsecureCookies: true,

		// CSRFKey signs the CSRF tokens. Left unset, a key is generated at
		// startup, so every restart invalidates the tokens on pages users
		// already have open and no two instances accept each other's. Before
		// running more than one instance, or deploying without draining, set
		// it to at least 32 bytes from your secret store — and fail startup
		// if that value is missing, because an empty one silently falls back
		// to the generated key.
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ssrHandler.ServeHTTP(w, r.WithContext(ctxdata.ToContext(r.Context(), &ctxdata.Data{})))
	}))

	return mux
}

func handleError(w http.ResponseWriter, r *ssrMux.Request, err error) {
	var (
		httpErr     *ssrMux.HttpError
		redirectErr *ssrMux.HttpRedirect
	)
	if errors.As(err, &httpErr) {
		w.WriteHeader(httpErr.Code)
		pages.WriteError(w, r, err)
		return
	} else if errors.As(err, &redirectErr) {
		http.Redirect(w, r.Request, redirectErr.Url, redirectErr.Code)
		return
	}

	w.WriteHeader(http.StatusInternalServerError)
	pages.WriteError(w, r, errors.New("internal server error"))
	log.Println(err)
}
