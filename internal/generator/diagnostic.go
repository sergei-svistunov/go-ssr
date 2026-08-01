package generator

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template"
)

// Diagnostic is a single problem found while analyzing or generating a project.
// It carries a location so that both a person reading the CLI output and a tool
// consuming the generator programmatically can jump straight to the line that
// needs fixing.
//
// Diagnostic implements error, so validation code can return one directly.
type Diagnostic struct {
	Route   string `json:"route,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func (d *Diagnostic) Error() string {
	var b strings.Builder
	if d.File != "" {
		b.WriteString(d.File)
		if d.Line > 0 {
			fmt.Fprintf(&b, ":%d", d.Line)
			if d.Col > 0 {
				fmt.Fprintf(&b, ":%d", d.Col)
			}
		}
		b.WriteString(": ")
	}
	b.WriteString(d.Message)
	return b.String()
}

// Diagnostics is a set of problems reported together, so a project with
// mistakes in several routes can be fixed in one pass instead of one
// regeneration per mistake.
type Diagnostics []Diagnostic

func (ds Diagnostics) Error() string {
	msgs := make([]string, len(ds))
	for i := range ds {
		msgs[i] = ds[i].Error()
	}
	return strings.Join(msgs, "\n")
}

// ErrorOrNil returns nil when there are no diagnostics, avoiding the non-nil
// error interface holding an empty slice.
func (ds Diagnostics) ErrorOrNil() error {
	if len(ds) == 0 {
		return nil
	}
	return ds
}

// DiagnosticsOf extracts structured diagnostics from an error returned by
// Analyze, Generate or Validate. Errors that are not diagnostics (I/O failures,
// for example) yield a single diagnostic carrying the message.
func DiagnosticsOf(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var ds Diagnostics
	if errors.As(err, &ds) {
		return ds
	}
	var d *Diagnostic
	if errors.As(err, &d) {
		return []Diagnostic{*d}
	}
	return []Diagnostic{{Message: err.Error()}}
}

// diagnostic converts an error raised while parsing or validating a route into a
// Diagnostic anchored at that route.
func (g *Generator) diagnostic(routePath, indexPath string, err error) Diagnostic {
	d := Diagnostic{Route: routePath, File: g.relPath(indexPath), Message: err.Error()}

	var own *Diagnostic
	if errors.As(err, &own) {
		out := *own
		if out.Route == "" {
			out.Route = routePath
		}
		if out.File == "" {
			out.File = d.File
		}
		return out
	}

	var syntaxErr *template.SyntaxError
	if errors.As(err, &syntaxErr) {
		d.File = g.relPath(syntaxErr.Filename)
		d.Line = syntaxErr.Line
		d.Message = syntaxErr.Message
		return d
	}

	var bindErr *template.SsrBindOnPrimitiveError
	if errors.As(err, &bindErr) {
		d.File = g.relPath(bindErr.File)
		d.Line = bindErr.Line
		d.Col = 1
		d.Code = "E06"
		d.Message = bindErr.Message()
		return d
	}

	return d
}

// relPath renders p relative to the project root when possible, so reported
// locations stay short and stable across machines.
func (g *Generator) relPath(p string) string {
	if p == "" {
		return ""
	}
	if g.dir != "" {
		if rel, err := filepath.Rel(g.dir, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
	}
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	return p
}

// routeIndexPath returns the template path for a route path such as "/users".
func (g *Generator) routeIndexPath(routePath string) string {
	return filepath.Join(g.webDir, "pages", filepath.FromSlash(strings.TrimPrefix(routePath, "/")), "index.html")
}
