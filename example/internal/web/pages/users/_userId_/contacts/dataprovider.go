package contacts

import (
	"context"
	"net/http"

	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/ctxdata"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var _ RouteDataProvider = &DPUsers_userId_Contacts{}

type DPUsers_userId_Contacts struct {
	model *model.Model
}

func NewDP(m *model.Model) *DPUsers_userId_Contacts {
	return &DPUsers_userId_Contacts{m}
}

func (p *DPUsers_userId_Contacts) GetRouteUsers_userId_ContactsData(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
	ctxdata.FromContext(ctx).PageTitle += " | User info"

	dbUser := p.model.GetUserByLogin(r.URLParam("userId"))
	if dbUser == nil {
		return mux.NewHttpError(http.StatusNotFound, "user wasn't found")
	}

	data.Emails = dbUser.Emails
	data.Phones = dbUser.Phones

	return nil
}
