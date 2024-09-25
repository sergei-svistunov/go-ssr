package _userId_

import (
	"context"
	"net/http"
	"strings"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &UsersUserDP{}

type User struct {
	Age   uint8
	Name  string
	Login string
	Image string
}

type UsersUserDP struct {
	model *model.Model
}

func NewDP(m *model.Model) *UsersUserDP {
	return &UsersUserDP{m}
}

func (p *UsersUserDP) GetRouteUsers_userId_Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	dbUser := p.model.GetUserByLogin(r.URLParam("userId"))
	if dbUser == nil {
		return mux.NewHttpError(http.StatusNotFound, "user wasn't found")
	}

	data.User.Login = dbUser.Login
	data.User.Name = dbUser.Name
	data.User.Age = dbUser.Age
	data.User.Image = "https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcTUW0u5Eiiy3oM6wcpeEE6sXCzlh8G-tX1_Iw&s"

	data.UserTabClass = func(rPath string) string {
		if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), rPath) {
			return "active"
		}
		return ""
	}

	return nil
}
