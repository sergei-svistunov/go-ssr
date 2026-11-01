package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
	"github.com/sergei-svistunov/go-ssr/pkg/render"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(_ context.Context, _ *mux.Request, _ mux.ResponseWriter, data *RouteData) error {
	data.Evil = `"><img src=x onerror=alert(1)>`
	data.Link = "javascript:alert(1)"
	data.Markup = render.HTMLConst("<b>ok</b>")
	return nil
}

func (*DP) InitF(_ context.Context, _ *mux.Request, _ mux.ResponseWriter, data *FormFValues) error {
	data.Q.SetValue(`"><img src=x onerror=alert(1)>`)
	data.T.SetValue(`</textarea><img src=x onerror=alert(1)>`)
	return nil
}

func (*DP) ProcessF(context.Context, *mux.Request, mux.ResponseWriter, *FormFValues) error {
	return nil
}
