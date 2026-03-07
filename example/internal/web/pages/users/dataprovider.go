package users

import (
	"context"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DP{}

type User struct {
	Name        string
	Login       string
	Image       string
	NavTabClass string
}

type DP struct {
	model *model.Model
}

func NewDP(d *model.Model) *DP {
	return &DP{model: d}
}

func (p *DP) DefaultRoute(ctx context.Context, r *mux.Request) (string, error) {
	return p.model.GetUsers()[0].Login, nil
}

func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	ctxdata.FromContext(ctx).PageTitle += " | User"

	curUserLogin := r.URLParam("userId")
	for _, user := range p.model.GetUsers() {
		vavTabClass := ""
		if user.Login == curUserLogin {
			vavTabClass = "active"
		}
		data.Users = append(data.Users, User{
			Name:        user.Name,
			Login:       user.Login,
			Image:       user.ImageUrl,
			NavTabClass: vavTabClass,
		})
	}

	return nil
}
