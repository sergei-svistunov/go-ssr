package mux

import (
	"io"
)

type DataContext interface {
	Write(w io.Writer) error
	WriteAssets(w io.Writer, writen map[string]struct{}) error
}

type RouteDataContext struct {
	Child  DataContext
	Assets []string
}

func (r RouteDataContext) Write(w io.Writer) error {
	return r.Child.Write(w)
}

func (r RouteDataContext) WriteAssets(w io.Writer, writen map[string]struct{}) error {
	for _, a := range r.Assets {
		if _, ok := writen[a]; ok {
			continue
		}
		writen[a] = struct{}{}
		if _, err := w.Write([]byte(a)); err != nil {
			return err
		}
	}

	if r.Child != nil {
		return r.Child.WriteAssets(w, writen)
	}

	return nil
}
