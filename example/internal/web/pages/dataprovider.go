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

var _ RouteDataProvider = &RootDP{}

type RootDP struct {
	model *model.Model
}

func NewDP(m *model.Model) *RootDP {
	return &RootDP{m}
}

func (D *RootDP) GetRouteRootData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	data.TabClass = func(rPath string) string {
		currentPath := r.URL.Path

		if currentPath == rPath || strings.HasPrefix(currentPath, rPath+"/") {
			return "active"
		}

		return ""
	}

	data.Title = func() string {
		return "Page test title<>\"" + ctxdata.FromContext(ctx).PageTitle
	}

	data.User.Image = "https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcTUW0u5Eiiy3oM6wcpeEE6sXCzlh8G-tX1_Iw&s"
	data.User.Name = "User<>\"Name"

	w.Header().Set("X-Custom-Header", fmt.Sprint(rand.Int63()))

	return nil
}
