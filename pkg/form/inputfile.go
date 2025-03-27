package form

import (
	"mime/multipart"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type FileHeader struct {
	*multipart.FileHeader
}

type File struct {
	value *FileHeader
	error string
}

func (e *File) SetError(err string)   { e.error = err }
func (e *File) HasError() bool        { return e.error != "" }
func (e *File) GetError() string      { return e.error }
func (e *File) GetValue() *FileHeader { return e.value }
func (e *File) IsNotNull() bool       { return e.value != nil }

func (e *File) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
	if isMultipart {
		if files := r.MultipartForm.File[name]; len(files) > 0 {
			e.value = &FileHeader{r.MultipartForm.File[name][0]}
		}
	}

	if isRequired && e.value == nil {
		e.error = MessageRequiredField
		return
	}
}

type FileMultiple struct {
	value []*FileHeader
	error string
}

func (e *FileMultiple) SetError(err string)     { e.error = err }
func (e *FileMultiple) HasError() bool          { return e.error != "" }
func (e *FileMultiple) GetError() string        { return e.error }
func (e *FileMultiple) GetValue() []*FileHeader { return e.value }
func (e *FileMultiple) IsNotNull() bool         { return e.value != nil }

func (e *FileMultiple) Process(r *mux.Request, name string, isMultipart, isRequired bool) {
	if isMultipart {
		e.value = make([]*FileHeader, len(r.MultipartForm.File[name]))
		for i, f := range r.MultipartForm.File[name] {
			e.value[i] = &FileHeader{f}
		}
	}

	if isRequired && len(e.value) == 0 {
		e.error = MessageRequiredField
		return
	}
}
