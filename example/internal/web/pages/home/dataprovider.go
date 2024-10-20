package home

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DPHome{}

type DPHome struct{}

func NewDP() *DPHome {
	return &DPHome{}
}

func (p *DPHome) GetRouteHomeDefaultSubRoute(ctx context.Context, r *mux.Request) (string, error) {
	return "", nil
}

func (p *DPHome) GetRouteHomeData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	return nil
}
