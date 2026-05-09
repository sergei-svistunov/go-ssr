package mux

import (
	"context"
	"net/http"
)

type Request struct {
	*http.Request
	URLParams map[string]string
}

func NewRequest(r *http.Request) *Request {
	return &Request{
		Request:   r,
		URLParams: map[string]string{},
	}
}

func (r *Request) URLParam(key string) string {
	return r.URLParams[key]
}

// urlParamsKey is the unexported context key used to stash URL params for WS
// upgrade requests. Using a package-local type prevents collisions with keys
// from other packages.
type urlParamsKey struct{}

// URLParamsFromContext retrieves the URL params map stored in ctx by the mux
// WS dispatch path. Returns nil if the context was not populated (e.g., the
// handler was not called through the mux).
func URLParamsFromContext(ctx context.Context) map[string]string {
	v, _ := ctx.Value(urlParamsKey{}).(map[string]string)
	return v
}
