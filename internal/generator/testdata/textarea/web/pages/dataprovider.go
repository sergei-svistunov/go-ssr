package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(_ context.Context, _ *mux.Request, _ mux.ResponseWriter, data *RouteData) error {
	data.Lead = "\nabc"
	return nil
}

func (*DP) InitF(_ context.Context, _ *mux.Request, _ mux.ResponseWriter, data *FormFValues) error {
	data.Lead.SetValue("\nabc")
	data.Plain.SetValue("abc")
	return nil
}

func (*DP) ProcessF(context.Context, *mux.Request, mux.ResponseWriter, *FormFValues) error {
	return nil
}
