package plain

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(context.Context, *mux.Request, mux.ResponseWriter, *RouteData) error { return nil }
