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
	wsHandlers   map[string]http.Handler
}

// WithWSHandlers returns a functional option that registers WebSocket handlers
// for specific paths. When a request with "Upgrade: websocket" matches a key
// in the handlers map, the call is delegated to the registered handler and the
// normal SSR rendering path is bypassed.
//
// This option is additive and non-breaking: callers that do not use WebSocket
// functionality pass no options and mux.New's signature is unchanged.
func WithWSHandlers(handlers map[string]http.Handler) func(*Mux) {
	return func(m *Mux) {
		m.wsHandlers = handlers
	}
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
	// Check WebSocket upgrade requests first when WS handlers are registered.
	if len(m.wsHandlers) > 0 && r.Header.Get("Upgrade") == "websocket" {
		if h, pattern := m.matchWSHandler(r.URL.Path); h != nil {
			// Extract URL params from the matched WS pattern so that
			// r.UrlParam(…) works inside the WS handler / Subscribe.
			params := extractWSURLParams(pattern, r.URL.Path)
			if len(params) > 0 {
				r = r.WithContext(context.WithValue(r.Context(), urlParamsKey{}, params))
			}
			h.ServeHTTP(w, r)
			return
		}
	}

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

// matchWSHandler finds the WS handler for the given request path. It supports
// dynamic segments (using the same _paramName_ convention as SSR routes).
// Returns (nil, "") if no handler matches.
// Returns (handler, pattern) on a match so the caller can extract URL params.
func (m *Mux) matchWSHandler(requestPath string) (http.Handler, string) {
	// Fast path: exact match (no dynamic segments, no extraction needed).
	if h, ok := m.wsHandlers[requestPath]; ok {
		return h, requestPath
	}
	// Slow path: pattern match (for dynamic segments like _userId_).
	reDynParam := reDynParamGlobal
	reqParts := strings.Split(strings.TrimSuffix(requestPath, "/"), "/")
	for pattern, h := range m.wsHandlers {
		patParts := strings.Split(strings.TrimSuffix(pattern, "/"), "/")
		if len(patParts) != len(reqParts) {
			continue
		}
		match := true
		for i, pp := range patParts {
			if reDynParam.MatchString(pp) {
				continue // dynamic segment matches anything
			}
			if pp != reqParts[i] {
				match = false
				break
			}
		}
		if match {
			return h, pattern
		}
	}
	return nil, ""
}

// extractWSURLParams walks the matched WS pattern and the actual request path
// in parallel, extracting values for each dynamic segment (_paramName_).
// Returns an empty map (not nil) when there are no dynamic segments.
func extractWSURLParams(pattern, requestPath string) map[string]string {
	params := map[string]string{}
	patParts := strings.Split(strings.TrimSuffix(pattern, "/"), "/")
	reqParts := strings.Split(strings.TrimSuffix(requestPath, "/"), "/")
	if len(patParts) != len(reqParts) {
		return params
	}
	reDynParam := reDynParamGlobal
	for i, pp := range patParts {
		if reDynParam.MatchString(pp) {
			// pp is "_paramName_"; strip the underscores to get the key.
			key := pp[1 : len(pp)-1]
			params[key] = reqParts[i]
		}
	}
	return params
}

var reDynParamGlobal = regexp.MustCompile("^_[^_]+_$")

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
