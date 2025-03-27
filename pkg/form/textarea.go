package form

import "github.com/sergei-svistunov/go-ssr/pkg/mux"

type Textarea struct {
	value   string
	notNull bool
	error   string
}

func (e *Textarea) SetError(err string) { e.error = err }
func (e *Textarea) HasError() bool      { return e.error != "" }
func (e *Textarea) GetError() string    { return e.error }
func (e *Textarea) GetValue() string    { return e.value }
func (e *Textarea) IsNotNull() bool     { return e.notNull }
func (e *Textarea) SetValue(v string) {
	e.value = v
	e.notNull = true
}

func (e *Textarea) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
	var vs []string
	if isMultipart {
		vs = r.MultipartForm.Value[name]
	} else {
		vs = r.PostForm[name]
	}

	if len(vs) > 0 {
		e.value = vs[0]
		e.notNull = true
	}

	if isRequired && e.value == "" {
		e.error = MessageRequiredField
		return
	}
}
