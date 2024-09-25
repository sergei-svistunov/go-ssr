package route

import (
	"fmt"
	"os"
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
		return nil, fmt.Errorf("%s/index.html: %w", dir, err)
	}

	return r, nil
}

func (r *Route) Template() *template.Template { return r.template }

func parseTemplate(dir string, imageResolver func(string) string) (*template.Template, error) {
	tplFile, err := os.Open(filepath.Join(dir, "index.html"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not open template file: %w", err)
	}
	defer tplFile.Close()

	template, err := template.Parse(tplFile, imageResolver)
	if err != nil {
		return nil, fmt.Errorf("could not parse HTML template: %w", err)
	}

	return template, nil
}
