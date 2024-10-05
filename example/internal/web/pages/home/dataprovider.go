package home

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &HomeDP{}

type HomeDP struct{}

func NewDP() *HomeDP {
	return &HomeDP{}
}

func (p *HomeDP) GetRouteHomeDefaultSubRoute(ctx context.Context, r *mux.Request) (string, error) {
	return "", nil
}

func (p *HomeDP) GetRouteHomeData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	return nil
}
