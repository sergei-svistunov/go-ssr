package web

import (
	"github.com/sergei-svistunov/go-ssr/example/internal/model"
	"github.com/sergei-svistunov/go-ssr/example/internal/web/pages"
	routeUser "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users"
	routeUsers_userId_ "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_"
	routeUsers_userId_Contacts "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_/contacts"
	routeUsers_userId_Info "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_/info"
	routeUsersAdd "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/add"
)

var _ pages.DataProvider = &TestDataProvider{}

type TestDataProvider struct {
	*pages.RootDP
	*routeUser.UsersDP
	*routeUsers_userId_.UsersUserDP
	*routeUsers_userId_Contacts.UsersUserContactsDP
	*routeUsers_userId_Info.UsersUserInfoDP
	*routeUsersAdd.UsersAddDP
}

func NewDataProvider(m *model.Model) *TestDataProvider {
	return &TestDataProvider{
		RootDP:              pages.NewDP(m),
		UsersDP:             routeUser.NewDP(m),
		UsersUserDP:         routeUsers_userId_.NewDP(m),
		UsersUserContactsDP: routeUsers_userId_Contacts.NewDP(m),
		UsersUserInfoDP:     routeUsers_userId_Info.NewDP(m),
		UsersAddDP:          routeUsersAdd.NewDP(m),
	}
}
