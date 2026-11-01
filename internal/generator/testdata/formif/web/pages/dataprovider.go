package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// Processed counts the runs of ProcessNote.
var Processed int

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) InitNote(context.Context, *mux.Request, mux.ResponseWriter, *FormNoteValues) error {
	return nil
}

func (*DP) ProcessNote(context.Context, *mux.Request, mux.ResponseWriter, *FormNoteValues) error {
	Processed++
	return nil
}

func (*DP) Data(_ context.Context, r *mux.Request, _ mux.ResponseWriter, data *RouteData) error {
	data.Show = r.URL.Query().Has("show")
	return nil
}
