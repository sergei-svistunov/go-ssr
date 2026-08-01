package route

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template"
)

type Route struct {
	dir      string
	template *template.Template
}

func FromDir(dir string, imageResolver func(string) string) (*Route, error) {
	r := &Route{
		dir: dir,
	}

	var err error
	r.template, err = parseTemplate(dir, imageResolver)
	if err != nil {
		// The parser only knows the template's base name. Re-point the error at
		// the full path so callers can report a location the user can open, and
		// keep the error typed so the generator can turn it into a diagnostic.
		indexPath := filepath.Join(dir, "index.html")

		var syntaxErr *template.SyntaxError
		if errors.As(err, &syntaxErr) {
			return nil, &template.SyntaxError{Filename: indexPath, Line: syntaxErr.Line, Message: syntaxErr.Message}
		}

		var bindErr *template.SsrBindOnPrimitiveError
		if errors.As(err, &bindErr) {
			return nil, &template.SsrBindOnPrimitiveError{File: indexPath, Line: bindErr.Line, Element: bindErr.Element}
		}

		return nil, fmt.Errorf("%s: %w", indexPath, err)
	}

	return r, nil
}

func (r *Route) Template() *template.Template { return r.template }

func parseTemplate(dir string, imageResolver func(string) string) (*template.Template, error) {
	template, err := template.Parse(filepath.Join(dir, "index.html"), imageResolver)
	if err != nil {
		return nil, err
	}

	return template, nil
}
