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

type Route interface {
	GetDataContext(ctx context.Context, r *Request, w ResponseWriter, child DataContext) (DataContext, error)
	GetDefaultRoute(ctx context.Context, r *Request) (string, error)
}

type Mux struct {
	rootRoute    *muxRoute
	errorHandler ErrorHandler
}

type muxRoute struct {
	route    Route
	paramId  string
	children map[string]*muxRoute
}

type Options struct {
	ErrorHandler ErrorHandler
}

type ErrorHandler func(w http.ResponseWriter, r *Request, err error)

func New(routes map[string]Route, opts Options) *Mux {
	rootRoute := &muxRoute{
		children: map[string]*muxRoute{},
	}

	reDynParam := regexp.MustCompile("^_[^_]+_$")

	for rPath, r := range routes {
		pathParts := strings.Split(strings.TrimSuffix(rPath, "/"), "/")[1:]
		currentRoute := rootRoute
		for _, p := range pathParts {
			if currentRoute.children[p] == nil {
				currentRoute.children[p] = &muxRoute{
					children: map[string]*muxRoute{},
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

	return &Mux{
		rootRoute:    rootRoute,
		errorHandler: opts.ErrorHandler,
	}
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	routePath := r.URL.Path
	if len(routePath) > 0 && routePath[len(routePath)-1] == '/' {
		routePath = routePath[:len(routePath)-1]
	}

	routePathParts := strings.Split(routePath, "/")[1:]

	currentRoute := m.rootRoute
	routesStack := []Route{currentRoute.route}
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
		subRoute, err := currentRoute.route.GetDefaultRoute(r.Context(), muxRequest)
		if err != nil {
			m.errorHandler(w, muxRequest, err)
			return
		}

		http.Redirect(w, r, path.Join(r.URL.Path, subRoute), http.StatusFound)
		return
	}

	var dataContext DataContext
	for len(routesStack) > 0 {
		route := routesStack[len(routesStack)-1]
		routesStack = routesStack[:len(routesStack)-1]

		if route == nil {
			continue
		}

		dc, err := route.GetDataContext(r.Context(), muxRequest, w, dataContext)
		if err != nil {
			m.errorHandler(w, muxRequest, err)
			return
		}
		dataContext = dc
	}

	if dataContext == nil {
		m.errorHandler(w, muxRequest, NewHttpError(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
		return
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
