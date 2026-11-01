package mux

import "net/http"

type ResponseWriter interface {
	Header() http.Header
}

// NoopResponseWriter is a ResponseWriter that discards all header writes.
// The mux passes it to Data on a live connection, which sends no headers.
type NoopResponseWriter struct{}

func (NoopResponseWriter) Header() http.Header { return http.Header{} }
