package form

import "github.com/sergei-svistunov/go-ssr/pkg/mux"

type Select[T ElementValueType] struct {
	value   T
	notNull bool
	error   string
	Options []SelectOptionElement[T]
}

func (e *Select[T]) SetError(err string) { e.error = err }
func (e *Select[T]) HasError() bool      { return e.error != "" }
func (e *Select[T]) GetError() string    { return e.error }
func (e *Select[T]) GetValue() T         { return e.value }
func (e *Select[T]) IsNotNull() bool     { return e.notNull }
func (e *Select[T]) SetValue(v T) {
	e.value = v
	e.notNull = true
}

func (e *Select[T]) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
	var vs []string
	if isMultipart {
		vs = r.MultipartForm.Value[name]
	} else {
		vs = r.PostForm[name]
	}

	var formValue = ""
	if len(vs) > 0 {
		formValue = vs[0]
		e.notNull = true
	}

	if isRequired && formValue == "" {
		e.error = MessageRequiredField
		return
	}

	if formValue != "" {
		parseValue(formValue, &e.value, &e.error)
	}
}

type SelectMultiple[T ElementValueType] struct {
	value   map[T]struct{}
	error   string
	options []SelectOptionElement[T]
}

func (e *SelectMultiple[T]) SetError(err string)       { e.error = err }
func (e *SelectMultiple[T]) HasError() bool            { return e.error != "" }
func (e *SelectMultiple[T]) GetError() string          { return e.error }
func (e *SelectMultiple[T]) GetValue() map[T]struct{}  { return e.value }
func (e *SelectMultiple[T]) IsNotNull() bool           { return e.value != nil }
func (e *SelectMultiple[T]) SetValue(v map[T]struct{}) { e.value = v }

func (e *SelectMultiple[T]) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
	var vs []string
	if isMultipart {
		vs = r.MultipartForm.Value[name]
	} else {
		vs = r.PostForm[name]
	}

	if len(vs) > 0 {
		e.value = make(map[T]struct{})
	}

	for _, formValue := range vs {
		var (
			parsedValue T
			err         string
		)
		parseValue(formValue, &parsedValue, &err)
		if err != "" {
			e.error = err
			return
		}
		e.value[parsedValue] = struct{}{}
	}

	if isRequired && len(e.value) == 0 {
		e.error = MessageRequiredField
		return
	}
}
