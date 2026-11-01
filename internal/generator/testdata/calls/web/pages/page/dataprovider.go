package page

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type AddIn struct {
	A int `json:"a"`
	B int `json:"b"`
}

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Data(context.Context, *mux.Request, mux.ResponseWriter, *RouteData) error { return nil }

func (*DP) CallAdd(_ context.Context, _ *mux.Request, in AddIn) (int, error) { return in.A + in.B, nil }

// CallSecret is not declared in the template, so no call reaches it.
func (*DP) CallSecret(context.Context, *mux.Request, struct{}) (string, error) { return "secret", nil }
