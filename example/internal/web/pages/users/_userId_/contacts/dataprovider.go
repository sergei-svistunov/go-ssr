package contacts

import (
	"context"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &UsersUserContactsDP{}

type UsersUserContactsDP struct {
	model *model.Model
}

func NewDP(m *model.Model) *UsersUserContactsDP {
	return &UsersUserContactsDP{m}
}

func (p *UsersUserContactsDP) GetRouteUsers_userId_ContactsData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	ctxdata.FromContext(ctx).PageTitle += " | User info"

	dbUser := p.model.GetUserByLogin(r.URLParam("userId"))
	if dbUser == nil {
		return mux.NewHttpError(http.StatusNotFound, "user wasn't found")
	}

	data.Emails = dbUser.Emails
	data.Phones = dbUser.Phones

	return nil
}
