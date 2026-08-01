package web

import (
	"net/http"

	ssrMux "github.com/sergei-svistunov/go-ssr/pkg/mux"

	"<PKG_NAME>/<WEB_DIR>/pages"
)

func New() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", pages.NewSsrHandler(ssrMux.Options{}))

	return mux
}
