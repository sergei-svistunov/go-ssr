package child

import (
	"context"

	"example.com/app/calls"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

type DP struct{}

func NewDP() *DP { return &DP{} }

func (*DP) Guard(context.Context, *mux.Request) error {
	calls.Add("child.guard")
	return nil
}

func (*DP) InitItem(context.Context, *mux.Request, mux.ResponseWriter, *FormItemValues) error {
	calls.Add("child.init")
	return nil
}

func (*DP) ProcessItem(context.Context, *mux.Request, mux.ResponseWriter, *FormItemValues) error {
	calls.Add("child.process")
	return nil
}

func (*DP) Data(context.Context, *mux.Request, mux.ResponseWriter, *RouteData) error {
	calls.Add("child.data")
	return nil
}
