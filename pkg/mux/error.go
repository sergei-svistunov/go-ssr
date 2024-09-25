package mux

import (
	"fmt"
	"net/http"
)

type HttpError struct {
	Code    int
	Message string
}

func (e *HttpError) Error() string {
	message := e.Message
	if message == "" {
		message = http.StatusText(e.Code)
	}
	return fmt.Sprintf("%d: %s", e.Code, message)
}

type HttpRedirect struct {
	Code int
	Url  string
}

func (h *HttpRedirect) Error() string {
	return fmt.Sprintf("redirect to %s (code: %d)", h.Url, h.Code)
}

func NewHttpError(code int, message string) *HttpError {
	return &HttpError{code, message}
}

func Redirect(code int, location string) *HttpRedirect {
	return &HttpRedirect{code, location}
}
