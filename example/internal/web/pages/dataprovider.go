package pages

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DPRoot{}

type DPRoot struct {
	model *model.Model
}

func NewDP(m *model.Model) *DPRoot {
	return &DPRoot{m}
}

func (D *DPRoot) GetRouteRootData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) > 1 {
		data.RoutePath = "/" + pathParts[1]
	}

	data.Title = func() string {
		return "Page test title<>\"" + ctxdata.FromContext(ctx).PageTitle
	}

	data.User.Image = "https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcTUW0u5Eiiy3oM6wcpeEE6sXCzlh8G-tX1_Iw&s"
	data.User.Name = "User<>\"Name"

	w.Header().Set("X-Custom-Header", fmt.Sprint(rand.Int63()))

	return nil
}
