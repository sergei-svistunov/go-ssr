package pages

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type errDataContext struct {
	err error
}

func (c *errDataContext) Write(w io.Writer) error {
	if _, err := w.Write([]byte(`<div class="container"><div class="alert alert-danger mt-3"><strong>Error:&nbsp;</strong>`)); err != nil {
		return err
	}
	if _, err := w.Write([]byte(c.err.Error())); err != nil {
		return err
	}
	if _, err := w.Write([]byte(`</div></div>`)); err != nil {
		return err
	}
	return nil
}

func (e *errDataContext) WriteAssets(w io.Writer, writen map[string]struct{}) error {
	return nil
}

type errRouteDataProvider struct{}

func (errRouteDataProvider) GetRouteRootData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	data.Title = func() string { return "Error" }
	return nil
}

func WriteError(w http.ResponseWriter, r *mux.Request, err error) {
	dc, err := Route[RouteDataProvider]{}.GetDataContext(r.Context(), r, w, errRouteDataProvider{}, &errDataContext{err})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Print(err)
		return
	}

	if err := dc.Write(w); err != nil {
		log.Print(err)
	}
}
