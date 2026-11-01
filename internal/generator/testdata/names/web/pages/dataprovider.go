package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(_ context.Context, _ *mux.Request, _ mux.ResponseWriter, data *RouteData) error {
	data.W = []string{"a", "b"}
	data.C = 1
	data.Data = 2
	return nil
}

func (*DP) Subscribe(context.Context, *mux.Request, *ReactiveState) error { return nil }

func (*DP) InitNote(context.Context, *mux.Request, mux.ResponseWriter, *FormNoteValues) error {
	return nil
}

func (*DP) ProcessNote(context.Context, *mux.Request, mux.ResponseWriter, *FormNoteValues) error {
	return nil
}
