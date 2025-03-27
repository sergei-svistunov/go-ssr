package form

import "github.com/sergei-svistunov/go-ssr/pkg/mux"

type Input[T ElementValueType] struct {
	value     T
	notNull   bool
	formValue string
	error     string
}

func (e *Input[T]) SetError(err string)  { e.error = err }
func (e *Input[T]) HasError() bool       { return e.error != "" }
func (e *Input[T]) GetError() string     { return e.error }
func (e *Input[T]) GetValue() T          { return e.value }
func (e *Input[T]) GetFormValue() string { return e.formValue }
func (e *Input[T]) IsNotNull() bool      { return e.notNull }
func (e *Input[T]) SetValue(v T) {
	e.value = v
	e.notNull = true
}

func (e *Input[T]) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
	var vs []string
	if isMultipart {
		vs = r.MultipartForm.Value[name]
	} else {
		vs = r.PostForm[name]
	}

	if len(vs) > 0 {
		e.formValue = vs[0]
		e.notNull = true
	}

	if isRequired && e.formValue == "" {
		e.error = MessageRequiredField
		return
	}

	if e.formValue != "" {
		parseValue(e.formValue, &e.value, &e.error)
	}
}

type InputMultiple[T ElementValueType] struct {
	value     map[T]struct{}
	formValue string
	error     string
}

func (e *InputMultiple[T]) SetError(err string)       { e.error = err }
func (e *InputMultiple[T]) HasError() bool            { return e.error != "" }
func (e *InputMultiple[T]) GetError() string          { return e.error }
func (e *InputMultiple[T]) GetValue() map[T]struct{}  { return e.value }
func (e *InputMultiple[T]) GetFormValue() string      { return e.formValue }
func (e *InputMultiple[T]) IsNotNull() bool           { return e.value != nil }
func (e *InputMultiple[T]) SetValue(v map[T]struct{}) { e.value = v }

func (e *InputMultiple[T]) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
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
