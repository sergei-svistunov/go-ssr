package mux

import "net/http"

type ResponseWriter interface {
	Header() http.Header
}
