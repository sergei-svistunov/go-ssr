package pages

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type PingIn struct {
	Text string `json:"text"`
}

type PingOut struct {
	Echo string `json:"echo"`
	Path string `json:"path"`
}

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(context.Context, *mux.Request, mux.ResponseWriter, *RouteData) error { return nil }

func (*DP) CallPing(_ context.Context, r *mux.Request, in PingIn) (PingOut, error) {
	return PingOut{Echo: in.Text, Path: r.URL.Path}, nil
}
