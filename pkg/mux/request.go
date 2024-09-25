package mux

import "net/http"

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
