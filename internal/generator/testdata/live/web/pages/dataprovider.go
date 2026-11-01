package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
	"github.com/sergei-svistunov/go-ssr/pkg/render"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(_ context.Context, _ *mux.Request, _ mux.ResponseWriter, data *RouteData) error {
	data.Title = `<b>x</b> & "y"`
	data.Text = `<b>x</b> & "y"`
	data.S = "<b>ok</b>"
	data.Markup = render.HTMLConst("<b>ok</b>")
	data.Name = "<script>"
	return nil
}

// Subscribe sets every live variable again, so each sends a patch.
func (*DP) Subscribe(ctx context.Context, _ *mux.Request, state *ReactiveState) error {
	state.SetText(`<b>x</b> & "y"`)
	state.SetS("<b>ok</b>")
	state.SetMarkup(render.HTMLConst("<b>ok</b>"))
	state.SetName("<script>")
	state.SetCount(0)
	state.SetRatio(0)
	state.SetOn(false)
	<-ctx.Done()
	return nil
}

func (*DP) ValidateText(_ context.Context, _ *mux.Request, v string) (string, error) { return v, nil }

func (*DP) ValidateCount(_ context.Context, _ *mux.Request, v int) (int, error) { return v, nil }

func (*DP) ValidateRatio(_ context.Context, _ *mux.Request, v float64) (float64, error) {
	return v, nil
}

func (*DP) ValidateOn(_ context.Context, _ *mux.Request, v bool) (bool, error) { return v, nil }
