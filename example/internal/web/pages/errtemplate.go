package pages

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
	"github.com/sergei-svistunov/go-ssr/pkg/render"
)

type errChild struct {
	err error
}

func (c errChild) Write(w io.Writer) error {
	if _, err := io.WriteString(w, `<div class="container"><div class="alert alert-danger mt-3"><strong>Error:&nbsp;</strong>`); err != nil {
		return err
	}
	if err := render.Text(w, c.err.Error()); err != nil {
		return err
	}
	_, err := io.WriteString(w, `</div></div>`)
	return err
}

type errRouteDataProvider struct{}

func (errRouteDataProvider) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	data.Title = func() string { return "Error" }
	return nil
}

func (errRouteDataProvider) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
	<-ctx.Done()
	return nil
}

// WriteError renders err inside the root layout.
func WriteError(w http.ResponseWriter, r *mux.Request, err error) {
	page := NewRoute(errRouteDataProvider{}).NewState()
	if dataErr := page.Data(r.Context(), r, w); dataErr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Print(dataErr)
		return
	}
	page.SetChild(errChild{err})
	if err := page.Write(w); err != nil {
		log.Print(err)
	}
}
