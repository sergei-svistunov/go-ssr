package template

import "fmt"

// CodedError is a template refusal with a stable code and a one-line fix.
type CodedError struct {
	File    string
	Line    int
	Code    string
	Message string
	Fix     string
}

func (e *CodedError) Error() string {
	return fmt.Sprintf("%s:%d: %s (%s)\n\tfix: %s", e.File, e.Line, e.Message, e.Code, e.Fix)
}
