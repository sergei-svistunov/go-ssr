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

// New returns the site's handler. csrfKey signs the CSRF tokens: at least 32
// bytes, the same on every instance that serves the site.
func New(d *model.Model, csrfKey []byte) (http.Handler, error) {
	ssrHandler, err := pages.NewSsrHandler(d, ssrMux.Options{
		ErrorHandler: handleError,
		// The CSRF cookie is Secure-only. Uncomment for plain-http
		// development on a host other than localhost, or in Safari (which
		// drops Secure cookies on http://localhost too); never enable in
		// production.
		// InsecureCookies: true,

		CSRFKey: csrfKey,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ssrHandler.ServeHTTP(w, r.WithContext(ctxdata.ToContext(r.Context(), &ctxdata.Data{})))
	}))

	return mux, nil
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
