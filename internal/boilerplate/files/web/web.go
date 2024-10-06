package web

import (
	"embed"
	"net/http"

	ssrMux "github.com/sergei-svistunov/go-ssr/pkg/mux"

	"<PKG_NAME>/internal/web/pages"
)

//go:embed static
var staticFiles embed.FS

func New() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", pages.NewSsrHandler(NewDataProvider(), ssrMux.Options{}))

	mux.Handle("/static/", http.FileServer(http.Dir("internal/web"))) // ToDo: disable directory listing

	return mux
}
