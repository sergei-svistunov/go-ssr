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
		var syntaxErr *template.SyntaxError
		if errors.As(err, &syntaxErr) {
			return nil, fmt.Errorf("%s/index.html:%d: %s", dir, syntaxErr.Line, syntaxErr.Message)
		}
		return nil, fmt.Errorf("%s/index.html: %w", dir, err)
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
