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
		*route_.RootDP
		*routeHome.HomeDP
		*routeUsers.UsersDP
		*routeUsers_userId_.Users_userId_DP
		*routeUsers_userId_Contacts.Users_userId_ContactsDP
		*routeUsers_userId_Info.Users_userId_InfoDP
		*routeUsersAdd.UsersAddDP
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
