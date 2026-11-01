package pages

import (
	"context"

	"example.com/app/calls"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Guard(_ context.Context, r *mux.Request) error {
	calls.Add("root.guard")
	if r.URL.Query().Has("refuse") {
		return mux.NotFound()
	}
	return nil
}

func (*DP) InitNote(context.Context, *mux.Request, mux.ResponseWriter, *FormNoteValues) error {
	calls.Add("root.init")
	return nil
}

func (*DP) ProcessNote(context.Context, *mux.Request, mux.ResponseWriter, *FormNoteValues) error {
	calls.Add("root.process")
	return nil
}

func (*DP) Data(context.Context, *mux.Request, mux.ResponseWriter, *RouteData) error {
	calls.Add("root.data")
	return nil
}
