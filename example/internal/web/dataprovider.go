package web

import (
	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/pages"
	route_ "github.com/sergei-svistunov/go-ssr/example/internal/web/pages"
	routeHome "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/home"
	routeUsers "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users"
	routeUsers_userId_ "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_"
	routeUsers_userId_Contacts "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_/contacts"
	routeUsers_userId_Info "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_/info"
	routeUsersAdd "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/add"
)

func NewDataProvider(m *model.Model) pages.DataProvider {
	return &struct {
		*route_.DPRoot
		*routeHome.DPHome
		*routeUsers.DPUsers
		*routeUsers_userId_.DPUsers_userId_
		*routeUsers_userId_Contacts.DPUsers_userId_Contacts
		*routeUsers_userId_Info.DPUsers_userId_Info
		*routeUsersAdd.DPUsersAdd
	}{
		route_.NewDP(m),
		routeHome.NewDP(),
		routeUsers.NewDP(m),
		routeUsers_userId_.NewDP(m),
		routeUsers_userId_Contacts.NewDP(m),
		routeUsers_userId_Info.NewDP(m),
		routeUsersAdd.NewDP(m),
	}
}
