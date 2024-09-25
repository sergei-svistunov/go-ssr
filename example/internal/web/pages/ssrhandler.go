package pages

import (
	"net/http"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"

	routeHome "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/home"
	routeUsers "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users"
	routeUsers_userId_ "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_"
	routeUsers_userId_Contacts "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_/contacts"
	routeUsers_userId_Info "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/_userId_/info"
	routeUsersAdd "github.com/sergei-svistunov/go-ssr/example/internal/web/pages/users/add"
)

type DataProvider interface {
	RouteDataProvider
	routeUsers.RouteDataProvider
	routeUsers_userId_.RouteDataProvider
	routeUsers_userId_Contacts.RouteDataProvider
	routeUsers_userId_Info.RouteDataProvider
	routeUsersAdd.RouteDataProvider
}

func NewSsrHandler(dp DataProvider, opts mux.Options) http.Handler {
	return mux.New(dp, map[string]mux.Route[DataProvider]{
		"/":                        Route[DataProvider]{},
		"/home":                    routeHome.Route[DataProvider]{},
		"/users":                   routeUsers.Route[DataProvider]{},
		"/users/_userId_":          routeUsers_userId_.Route[DataProvider]{},
		"/users/_userId_/contacts": routeUsers_userId_Contacts.Route[DataProvider]{},
		"/users/_userId_/info":     routeUsers_userId_Info.Route[DataProvider]{},
		"/users/add":               routeUsersAdd.Route[DataProvider]{},
	}, opts)
}
