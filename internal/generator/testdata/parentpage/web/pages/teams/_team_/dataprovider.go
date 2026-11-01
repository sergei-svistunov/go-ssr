package _team_

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// Guarded is the team the last Guard call saw.
var Guarded string

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Guard(_ context.Context, r *mux.Request) error {
	Guarded = r.URLParam("team")
	return nil
}

func (*DP) Data(context.Context, *mux.Request, mux.ResponseWriter, *RouteData) error { return nil }
