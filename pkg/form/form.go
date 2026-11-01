package form

import (
	"strings"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type BaseFormValues struct {
	csrfToken string
	error     string
	validated bool
	elements  []Element
}

type Element interface {
	HasError() bool
}

func (f *BaseFormValues) SetElements(elements []Element) { f.elements = elements }
func (f *BaseFormValues) MarkValidated()                 { f.validated = true }
func (f *BaseFormValues) IsValidated() bool              { return f.validated }
func (f *BaseFormValues) GetCSRFToken() string           { return f.csrfToken }
func (f *BaseFormValues) SetError(err string)            { f.error = err }
func (f *BaseFormValues) GetError() string               { return f.error }

func (f *BaseFormValues) HasError() bool {
	if f.error != "" {
		return true
	}

	for _, e := range f.elements {
		if e.HasError() {
			return true
		}
	}

	return false
}

func IsMultipart(r *mux.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), MultipartFormData)
}

// SetCSRFToken hands every form the token r.CSRFToken issues for this
// response.
func SetCSRFToken(r *mux.Request, w mux.ResponseWriter, forms ...*BaseFormValues) error {
	csrfToken, err := r.CSRFToken(w)
	if err != nil {
		return err
	}
	for _, form := range forms {
		form.csrfToken = csrfToken
	}
	return nil
}
