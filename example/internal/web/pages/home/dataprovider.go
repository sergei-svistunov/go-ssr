package home

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type DP struct{}

func NewDP(_ *model.Model) *DP {
	return &DP{}
}

func (p *DP) DefaultRoute(ctx context.Context, r *mux.Request) (string, error) {
	return "", nil
}

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	return nil
}
