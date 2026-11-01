package _userId_

import (
	"context"
	"net/http"
	"strings"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type User struct {
	Age   uint8
	Name  string
	Login string
}

type DP struct {
	model *model.Model
}

func NewDP(d *model.Model) *DP {
	return &DP{model: d}
}

// Guard answers 404 for an unknown user before any route below this one runs.
func (p *DP) Guard(ctx context.Context, r *mux.Request) error {
	if p.model.GetUserByLogin(r.URLParam("userId")) == nil {
		return mux.NewHttpError(http.StatusNotFound, "user wasn't found")
	}
	return nil
}

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	dbUser := p.model.GetUserByLogin(r.URLParam("userId"))

	data.User.Login = dbUser.Login
	data.User.Name = dbUser.Name
	data.User.Age = dbUser.Age

	data.UserTabClass = func(rPath string) string {
		if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), rPath) {
			return "active"
		}
		return ""
	}

	return nil
}
