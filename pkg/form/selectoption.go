package form

import (
	"io"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var (
	_ SelectOptionElement[string] = SelectOption[string]{}
	_ SelectOptionElement[string] = SelectOptionGroup[string]{}
)

type SelectOptionElement[T ElementValueType] interface {
	WriteHtml(w io.Writer, isSelected func(v T) bool) error
}

type SelectOption[T ElementValueType] struct {
	Value    T
	Label    string
	Disabled bool
}

func (o SelectOption[T]) WriteHtml(w io.Writer, isSelected func(v T) bool) error {
	if _, err := io.WriteString(w, `<option value="`); err != nil {
		return err
	}

	if _, err := mux.WriteHtmlEscaped(w, o.Value); err != nil {
		return err
	}

	if _, err := io.WriteString(w, `"`); err != nil {
		return err
	}

	if o.Disabled {
		if _, err := io.WriteString(w, " disabled"); err != nil {
			return err
		}
	}

	if isSelected(o.Value) {
		if _, err := io.WriteString(w, " selected"); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, `>`); err != nil {
		return err
	}

	if o.Label != "" {
		if _, err := mux.WriteHtmlEscaped(w, o.Label); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, `</option>`); err != nil {
		return err
	}

	return nil
}

type SelectOptionGroup[T ElementValueType] struct {
	Label    string
	Disabled bool
	Options  []SelectOptionElement[T]
}

func (o SelectOptionGroup[T]) WriteHtml(w io.Writer, isSelected func(v T) bool) error {
	if _, err := io.WriteString(w, `<optgroup label="`); err != nil {
		return err
	}

	if _, err := mux.WriteHtmlEscaped(w, o.Label); err != nil {
		return err
	}

	if _, err := io.WriteString(w, `"`); err != nil {
		return err
	}

	if o.Disabled {
		if _, err := io.WriteString(w, " disabled"); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, `>`); err != nil {
		return err
	}

	for _, opt := range o.Options {
		if err := opt.WriteHtml(w, isSelected); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, `</optgroup>`); err != nil {
		return err
	}

	return nil
}
