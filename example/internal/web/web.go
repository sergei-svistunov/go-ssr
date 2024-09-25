//go:generate go run github.com/sergei-svistunov/go-ssr -dir . -package github.com/sergei-svistunov/go-ssr/example/internal/web
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

func New(m *model.Model) http.Handler {
	mux := http.NewServeMux()

	ssrHandler := pages.NewSsrHandler(NewDataProvider(m), ssrMux.Options{
		ErrorHandler: handleError,
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ssrHandler.ServeHTTP(w, r.WithContext(ctxdata.ToContext(r.Context(), &ctxdata.Data{})))
	}))

	mux.Handle("/static/", http.FileServer(http.Dir("example/internal/web"))) // ToDo: disable directory listing

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
