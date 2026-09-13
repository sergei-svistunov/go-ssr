package mux

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Route interface {
	GetDataContext(ctx context.Context, r *Request, w ResponseWriter, child DataContext) (DataContext, error)
	GetDefaultRoute(ctx context.Context, r *Request) (string, error)
}

type Mux struct {
	rootRoute      *muxRoute
	errorHandler   ErrorHandler
	wsHandlers     map[string]http.Handler
	options        requestOptions
	allowCrossSite func(*Request) bool
}

// allowsCrossSite reports whether the application exempts this request from
// the cross-site refusal.
func (m *Mux) allowsCrossSite(r *Request) bool {
	return m.allowCrossSite != nil && m.allowCrossSite(r)
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
	// InsecureCookies drops the Secure flag and the __Host- prefix from the
	// cookies the framework sets, so forms work over plain http on hosts other
	// than localhost (Chrome and Firefox already treat http://localhost as
	// secure; Safari does not). Never enable it in production.
	InsecureCookies bool
	// OriginPatterns lists additional origins allowed to submit forms and open
	// reactive WebSockets. Empty means the browser's Origin must match the
	// request Host. Each pattern is matched (path.Match) against the Origin's
	// host as the browser sends it, which omits default ports: "dev.example"
	// matches https://dev.example, "dev.example:*" matches any explicit port
	// such as http://dev.example:3000, and "https://dev.example" also pins the
	// scheme. The mux refuses cross-site POST/PUT/PATCH/DELETE requests before
	// a route's hooks run and cross-site WebSocket handshakes before
	// dispatching them; AllowCrossSite exempts either kind.
	OriginPatterns []string
	// AllowCrossSite lets a request through the cross-site refusal that
	// OriginPatterns describes — a routed request or a WebSocket handshake —
	// for a route that receives third-party browser traffic by design: an
	// OpenID Connect form_post or SAML callback, a payment return. The route
	// must then validate that request itself. nil exempts nothing.
	AllowCrossSite func(r *Request) bool
	// CSRFKey signs the CSRF tokens pkg/form mints, so a token the server
	// did not issue is rejected instead of being trusted because it happens
	// to match a cookie. Empty means a key generated when the handler is
	// built: zero configuration, but tokens minted by one process are not
	// accepted by another, so set it explicitly when more than one instance
	// serves the same site or when tokens must survive a restart.
	CSRFKey []byte
	// CSRFSession binds each token to the session it was minted for, so a
	// token captured before a login is not accepted after one. Return the
	// session identifier, or "" for an anonymous request. It is consulted
	// once per request for the session the request arrived with, and again
	// on every render that issues or reuses a token — so it must be cheap:
	// read an already-loaded session rather than querying. nil binds nothing.
	CSRFSession func(r *Request) string
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
	validateOriginPatterns(opts.OriginPatterns)

	return &Mux{
		rootRoute:    rootRoute,
		errorHandler: opts.ErrorHandler,
		// Clipped so a hook appending to Request.OriginPatterns reallocates
		// instead of writing into an array other requests are reading.
		options: requestOptions{
			insecureCookies: opts.InsecureCookies,
			originPatterns:  slices.Clip(slices.Clone(opts.OriginPatterns)),
			csrfKey:         csrfKey(opts.CSRFKey),
			csrfSessionFn:   opts.CSRFSession,
		},
		allowCrossSite: opts.AllowCrossSite,
	}
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every Request built from this request — here, in a WebSocket handler,
	// or by a hook rebuilding one — reads its settings from the context, so
	// they cannot drift and a NoStore mark survives the rebuild.
	options := m.options
	options.noStore = new(atomic.Bool)
	options.csrfSession = &arrivalSession{}
	r = withOptions(r, options)

	// Check WebSocket upgrade requests first when WS handlers are registered.
	// Upgrade is a case-insensitive token (RFC 6455).
	if len(m.wsHandlers) > 0 && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if h, pattern := m.matchWSHandler(r.URL.Path); h != nil {
			// Extract URL params from the matched WS pattern so that
			// r.UrlParam(…) works inside the WS handler / Subscribe.
			params := extractWSURLParams(pattern, r.URL.Path)
			if len(params) > 0 {
				r = r.WithContext(context.WithValue(r.Context(), urlParamsKey{}, params))
			}
			// The handshake carries the browser's cookies, so the rule that
			// guards form submissions guards it too, for every handler
			// registered here — and more strictly, since no token follows it.
			if CrossSiteStrict(r, m.options.originPatterns) {
				wsRequest := NewRequest(r)
				wsRequest.URLParams = params
				if !m.allowsCrossSite(wsRequest) {
					handshakeRefusals.logf("mux: WebSocket handshake for %q refused as cross-site (Origin %q, Host %q)",
						r.URL.Path, r.Header.Get("Origin"), r.Host)
					http.Error(w, "cross-site WebSocket handshake", http.StatusForbidden)
					return
				}
			}
			checked := options
			checked.originChecked = true
			h.ServeHTTP(w, withOptions(r, checked))
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

	// A state-changing request from another origin is refused once the route
	// is known and before any of its hooks run; the CSRF cookie/token
	// comparison in form.Parse stays as the second line of defence.
	if !SafeMethod(r.Method) && muxRequest.CrossSite() && !m.allowsCrossSite(muxRequest) {
		routedRefusals.logf("mux: %s %q refused as cross-site (Origin %q, Host %q)",
			r.Method, r.URL.Path, r.Header.Get("Origin"), r.Host)
		m.errorHandler(w, muxRequest, NewHttpError(http.StatusForbidden, "Cross-site request"))
		return
	}

	// Resolved before any hook runs, so a hook that establishes a session
	// cannot become the session the request is judged to have arrived with.
	if options.csrfSessionFn != nil {
		_ = muxRequest.CSRFSession()
	}

	// Once a route has had a say, whatever the outcome — page, redirect or
	// error — a response marked NoStore leaves with that header; a later
	// route in the stack may have set a cacheable policy of its own.
	noStore := func() {
		if options.noStore.Load() {
			w.Header().Set("Cache-Control", "no-store")
		}
	}

	if len(currentRoute.children) > 0 {
		if currentRoute.route == nil {
			// The path only groups child routes; there is no template of its own
			// to render and nothing to redirect to.
			m.errorHandler(w, muxRequest, NewHttpError(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
			return
		}

		subRoute, err := currentRoute.route.GetDefaultRoute(r.Context(), muxRequest)
		if err != nil {
			noStore()
			m.errorHandler(w, muxRequest, err)
			return
		}

		noStore()
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
			noStore()
			m.errorHandler(w, muxRequest, err)
			return
		}
		dataContext = dc
	}

	if dataContext == nil {
		noStore()
		m.errorHandler(w, muxRequest, NewHttpError(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
		return
	}

	noStore()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := dataContext.Write(w); err != nil {
		log.Println(err)
	}
}

// csrfKey returns the configured key, or one generated for this handler.
// Generating is what makes the default configuration work with no setup; the
// cost is that tokens do not survive a restart or span instances, which
// Options.CSRFKey documents.
func csrfKey(configured []byte) []byte {
	if len(configured) > 0 {
		return slices.Clip(slices.Clone(configured))
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("mux: generating a CSRF key: " + err.Error())
	}
	return key
}

// logThrottle keeps a diagnostic any unauthenticated client can trigger from
// becoming a log amplifier: one line per interval, saying how many it stood
// in for. The refusal itself is never suppressed, only its log line.
type logThrottle struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	dropped  int
}

func (t *logThrottle) logf(format string, args ...any) {
	interval := t.interval
	if interval == 0 {
		interval = time.Second
	}

	t.mu.Lock()
	now := time.Now()
	if !t.last.IsZero() && now.Sub(t.last) < interval {
		t.dropped++
		t.mu.Unlock()
		return
	}
	t.last = now
	dropped := t.dropped
	t.dropped = 0
	t.mu.Unlock()

	if dropped > 0 {
		format += fmt.Sprintf(" (%d more suppressed)", dropped)
	}
	log.Printf(format, args...)
}

var (
	routedRefusals    logThrottle
	handshakeRefusals logThrottle
)

// SafeMethod reports whether the method is defined not to change state, so
// a cross-site request with it needs no origin check.
func SafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
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
