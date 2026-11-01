package form

import "github.com/sergei-svistunov/go-ssr/pkg/mux"

const (
	MessageRequiredField = "This field is required"

	MultipartFormData = "multipart/form-data"

	// CSRFTokenName is the name of the hidden field every <ssr:form> renders.
	CSRFTokenName = mux.CSRFFieldName
)
