package info

import (
	"context"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &Users_userId_InfoDP{}

type Users_userId_InfoDP struct {
	model *model.Model
}

func NewDP(m *model.Model) *Users_userId_InfoDP {
	return &Users_userId_InfoDP{m}
}

func (p *Users_userId_InfoDP) GetRouteUsers_userId_InfoData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	ctxdata.FromContext(ctx).PageTitle += " | User info"

	dbUser := p.model.GetUserByLogin(r.URLParam("userId"))
	if dbUser == nil {
		return mux.NewHttpError(http.StatusNotFound, "user wasn't found")
	}

	data.User.Age = dbUser.Age
	data.User.Info = dbUser.Info

	return nil
}
