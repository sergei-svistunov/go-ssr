package mux

import "net/http"

type ResponseWriter interface {
	Header() http.Header
}

// NoopResponseWriter is a ResponseWriter that discards all header writes.
// It is used by the WebSocket handler when calling Data() during reconnect,
// where response headers must not be set.
type NoopResponseWriter struct{}

func (NoopResponseWriter) Header() http.Header { return http.Header{} }
