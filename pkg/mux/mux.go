package mux

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path"
	"regexp"
	"strings"
)

type Route[DataProvider any] interface {
	GetDataContext(ctx context.Context, r *Request, w ResponseWriter, dp DataProvider, child DataContext) (DataContext, error)
	GetDefaultSubRoute(ctx context.Context, r *Request, dp DataProvider) (string, error)
}

type Mux[DataProvider any] struct {
	dataProvider DataProvider
	rootRoute    *muxRoute[DataProvider]
	errorHandler ErrorHandler
}

type muxRoute[DataProvider any] struct {
	route    Route[DataProvider]
	paramId  string
	children map[string]*muxRoute[DataProvider]
}

type Options struct {
	ErrorHandler ErrorHandler
}

type ErrorHandler func(w http.ResponseWriter, r *Request, err error)

func New[DataProvider any](dataProvider DataProvider, routes map[string]Route[DataProvider], opts Options) *Mux[DataProvider] {
	rootRoute := &muxRoute[DataProvider]{
		children: map[string]*muxRoute[DataProvider]{},
	}

	reDynParam := regexp.MustCompile("^_[^_]+_$")

	for rPath, r := range routes {
		pathParts := strings.Split(strings.TrimSuffix(rPath, "/"), "/")[1:]
		currentRoute := rootRoute
		for _, p := range pathParts {
			if currentRoute.children[p] == nil {
				currentRoute.children[p] = &muxRoute[DataProvider]{
					children: map[string]*muxRoute[DataProvider]{},
				}
			}

			if reDynParam.MatchString(p) {
				currentRoute.paramId = p[1 : len(p)-1]
			}
			currentRoute = currentRoute.children[p]

		}
		currentRoute.route = r
	}

	if opts.ErrorHandler == nil {
		opts.ErrorHandler = defaultErrorHandler
	}

	return &Mux[DataProvider]{
		dataProvider: dataProvider,
		rootRoute:    rootRoute,
		errorHandler: opts.ErrorHandler,
	}
}

func (m *Mux[DataProvider]) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	routePath := r.URL.Path
	if len(routePath) > 0 && routePath[len(routePath)-1] == '/' {
		routePath = routePath[:len(routePath)-1]
	}

	routePathParts := strings.Split(routePath, "/")[1:]

	currentRoute := m.rootRoute
	routesStack := []Route[DataProvider]{currentRoute.route}
	muxRequest := NewRequest(r)
	for _, routePathPart := range routePathParts {
		child := currentRoute.children[routePathPart]
		if child == nil && currentRoute.paramId != "" {
			muxRequest.URLParams[currentRoute.paramId] = routePathPart
			child = currentRoute.children["_"+currentRoute.paramId+"_"]
		}

		if child == nil {
			m.errorHandler(w, muxRequest, NewHttpError(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
			return
		}

		currentRoute = child
		routesStack = append(routesStack, currentRoute.route)
	}

	if len(currentRoute.children) > 0 {
		subRoute, err := currentRoute.route.GetDefaultSubRoute(r.Context(), muxRequest, m.dataProvider)
		if err != nil {
			m.errorHandler(w, muxRequest, err)
			return
		}

		http.Redirect(w, r, path.Join(r.URL.Path, subRoute), http.StatusFound)
		return
	}

	var dataContext DataContext = nil
	for len(routesStack) > 0 {
		route := routesStack[len(routesStack)-1]
		routesStack = routesStack[:len(routesStack)-1]

		dc, err := route.GetDataContext(r.Context(), muxRequest, w, m.dataProvider, dataContext)
		if err != nil {
			m.errorHandler(w, muxRequest, err)
			return
		}
		dataContext = dc
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := dataContext.Write(w); err != nil {
		log.Println(err)
	}
}

func defaultErrorHandler(w http.ResponseWriter, r *Request, err error) {
	var (
		httpErr     *HttpError
		redirectErr *HttpRedirect
	)
	if errors.As(err, &httpErr) {
		w.WriteHeader(httpErr.Code)
		if _, err := w.Write([]byte(httpErr.Message)); err != nil {
			log.Println(err)
		}
		return
	} else if errors.As(err, &redirectErr) {
		http.Redirect(w, r.Request, redirectErr.Url, redirectErr.Code)
		return
	}

	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	log.Println(err)
}
