package mux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Mux struct {
	rootRoute      *muxRoute
	errorHandler   ErrorHandler
	options        requestOptions
	allowCrossSite func(*Request) bool
	live           liveConns
	liveMaxAge     time.Duration
}

// allowsCrossSite reports whether the application exempts this request from
// the cross-site refusal.
func (m *Mux) allowsCrossSite(r *Request) bool {
	return m.allowCrossSite != nil && m.allowCrossSite(r)
}

type muxRoute struct {
	route    Route
	paramId  string
	children map[string]*muxRoute
}

type Options struct {
	ErrorHandler ErrorHandler
	// InsecureCookies drops the Secure flag and the __Host- prefix from the
	// cookies the mux sets, so forms work over plain http on hosts other
	// than localhost (Chrome and Firefox already treat http://localhost as
	// secure; Safari does not). Never enable it in production.
	InsecureCookies bool
	// OriginPatterns lists additional origins allowed to submit forms and open
	// live connections. Empty means the browser's Origin must match the
	// request Host. Each pattern is matched (path.Match) against the Origin's
	// host as the browser sends it, which omits default ports: "dev.example"
	// matches https://dev.example, "dev.example:*" matches any explicit port
	// such as http://dev.example:3000, and "https://dev.example" also pins the
	// scheme. The mux refuses cross-site POST/PUT/PATCH/DELETE requests and
	// cross-site live connection handshakes before a route's hooks run;
	// AllowCrossSite exempts either kind.
	OriginPatterns []string
	// AllowCrossSite lets a request through the cross-site refusal that
	// OriginPatterns describes — a routed request or a live connection
	// handshake, which it sees at its own address, ending in /__ws. It lifts
	// only that refusal: an exempt POST to a page still needs the CSRF cookie
	// and a token the mux signed, and is refused with 400 without them. Use
	// it when which routes another origin may reach depends on the request,
	// not on one list for the whole site. A third-party POST that carries no
	// token, such as a sign-in or payment callback, belongs in an
	// http.Handler outside the mux. nil exempts nothing.
	AllowCrossSite func(r *Request) bool
	// CSRFKey signs the CSRF tokens the mux mints, so a token the server did
	// not issue is rejected instead of being trusted because it happens to
	// match a cookie. It is required and must be at least 32 bytes; New
	// refuses anything shorter. Every instance serving the same site needs
	// the same key, and tokens survive a restart only if the key does.
	CSRFKey []byte
	// CSRFSession binds each token to the session it was minted for, so a
	// token captured before a login is not accepted after one. Return the
	// session identifier, or "" for an anonymous request. It is consulted
	// once per request for the session the request arrived with, and again
	// on every render that issues or reuses a token — so it must be cheap:
	// read an already-loaded session rather than querying. nil binds nothing.
	CSRFSession func(r *Request) string
	// MaxLiveConnsPerSession caps the live connections open at once for one
	// CSRFSession value; a handshake beyond it is refused with 429. A request
	// without a session is not counted. 0 means no limit.
	MaxLiveConnsPerSession int
	// CSPSources adds sources to directives of the Content-Security-Policy
	// the mux sends with every response, after the directive's defaults;
	// nothing removes a default. The directives that take sources are
	// default-src, script-src, style-src, img-src, font-src, connect-src,
	// form-action, media-src, worker-src, frame-src and manifest-src. New
	// refuses any other directive, and a source that is empty, contains
	// whitespace, ';', ',' or a control character, is "*" or has "*" as its
	// whole host (https://*, *:443), is a quoted keyword containing unsafe-
	// ('unsafe-inline', 'unsafe-eval', 'unsafe-hashes', 'wasm-unsafe-eval')
	// or is 'strict-dynamic'. In script-src it also refuses a scheme on its
	// own, such as https: or data:.
	CSPSources map[string][]string
}

type ErrorHandler func(w http.ResponseWriter, r *Request, err error)

// New builds the handler for routes. It refuses an Options.CSRFKey shorter
// than 32 bytes, a malformed Options.OriginPatterns entry and an
// Options.CSPSources entry that would weaken the policy.
func New(routes map[string]Route, opts Options) (*Mux, error) {
	switch {
	case len(opts.CSRFKey) == 0:
		return nil, errors.New("mux: CSRFKey is empty")
	case len(opts.CSRFKey) < minCSRFKeyLen:
		return nil, errors.New("mux: CSRFKey must be at least 32 bytes")
	}
	if err := validateOriginPatterns(opts.OriginPatterns); err != nil {
		return nil, err
	}
	csp, err := newCSP(opts.CSPSources)
	if err != nil {
		return nil, err
	}

	rootRoute := &muxRoute{
		children: map[string]*muxRoute{},
	}

	for rPath, r := range routes {
		pathParts := strings.Split(strings.TrimSuffix(rPath, "/"), "/")[1:]
		currentRoute := rootRoute
		for _, p := range pathParts {
			if currentRoute.children[p] == nil {
				currentRoute.children[p] = &muxRoute{
					children: map[string]*muxRoute{},
				}
			}

			if reDynParamGlobal.MatchString(p) {
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
		// Clipped so a hook appending to Request.OriginPatterns reallocates
		// instead of writing into an array other requests are reading.
		options: requestOptions{
			insecureCookies: opts.InsecureCookies,
			originPatterns:  slices.Clip(slices.Clone(opts.OriginPatterns)),
			csrfKey:         slices.Clip(slices.Clone(opts.CSRFKey)),
			csrfSessionFn:   opts.CSRFSession,
			csp:             csp,
		},
		allowCrossSite: opts.AllowCrossSite,
		live:           liveConns{max: opts.MaxLiveConnsPerSession},
		liveMaxAge:     liveMaxAge,
	}, nil
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every Request built from this request — here, on a live connection,
	// or by a hook rebuilding one — reads its settings from the context, so
	// they cannot drift and a NoStore mark survives the rebuild.
	options := m.options
	options.noStore = new(atomic.Bool)
	options.csrfSession = &arrivalSession{}
	r = withOptions(r, options)
	setHeaders(w.Header(), r)

	if page, ok := liveAddress(r.URL.Path); ok {
		m.serveLive(w, r, page)
		return
	}

	muxRequest := NewRequest(r)
	routes, end, err := m.match(muxRequest)
	if err != nil {
		m.errorHandler(w, muxRequest, err)
		return
	}
	parent := end.route.Layout() && len(end.children) > 0

	// A state-changing request from another origin is refused once the route
	// is known and before any of its hooks run; the CSRF cookie/token
	// comparison in readForm stays as the second line of defence.
	if !SafeMethod(r.Method) && muxRequest.CrossSite() && !m.allowsCrossSite(muxRequest) {
		routedRefusals.logf("mux: %s %q refused as cross-site (Origin %q, Host %q)",
			r.Method, r.URL.Path, r.Header.Get("Origin"), r.Host)
		m.errorHandler(w, muxRequest, NewHttpError(http.StatusForbidden, "Cross-site request"))
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost:
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		m.errorHandler(w, muxRequest, NewHttpError(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)))
		return
	}

	// Resolved before any hook runs, so a hook that establishes a session
	// cannot become the session the request is judged to have arrived with.
	if options.csrfSessionFn != nil {
		_ = muxRequest.CSRFSession()
	}

	// Once a route has had a say, whatever the outcome — page, redirect or
	// error — the response leaves with the security headers whatever a hook
	// set, and one marked NoStore with that header too; a later route in the
	// stack may have set a cacheable policy of its own.
	finish := func() {
		setHeaders(w.Header(), r)
		if options.noStore.Load() {
			w.Header().Set("Cache-Control", "no-store")
		}
	}
	fail := func(err error) {
		finish()
		m.errorHandler(w, muxRequest, err)
	}

	ctx := r.Context()
	states, err := guard(ctx, muxRequest, routes)
	if err != nil {
		fail(err)
		return
	}

	post := r.Method == http.MethodPost
	if parent {
		if post {
			w.Header().Set("Allow", "GET, HEAD")
			fail(NewHttpError(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)))
			return
		}
		subRoute, err := states[len(states)-1].DefaultRoute(ctx, muxRequest)
		if err != nil {
			fail(err)
			return
		}
		self := path.Join("/", r.URL.Path)
		target := path.Join(self, subRoute)
		if target == self {
			fail(NotFound())
			return
		}
		finish()
		http.Redirect(w, r, (&url.URL{Path: target}).EscapedPath(), http.StatusFound)
		return
	}

	states = rendered(routes, states)

	var formID string
	if post {
		id, err := readForm(muxRequest)
		if err != nil {
			fail(err)
			return
		}
		formID = id
	}
	for _, s := range states {
		if err := s.InitForms(ctx, muxRequest, w); err != nil {
			fail(err)
			return
		}
	}
	if post {
		owned := false
		for _, s := range states {
			ok, err := s.SubmitForm(ctx, muxRequest, w, formID)
			if err != nil {
				fail(err)
				return
			}
			if ok {
				owned = true
				break
			}
		}
		if !owned {
			fail(NewHttpError(http.StatusBadRequest, "This form is not on this page."))
			return
		}
	}
	for _, s := range states {
		if err := s.Data(ctx, muxRequest, w); err != nil {
			fail(err)
			return
		}
	}

	for i := 0; i+1 < len(states); i++ {
		states[i].SetChild(states[i+1])
	}
	// Rendered whole before anything is sent, so a failing template leaves
	// the error handler a clean response instead of half a page.
	var body bytes.Buffer
	if err := states[0].Write(&body); err != nil {
		fail(err)
		return
	}

	finish()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := body.WriteTo(w); err != nil {
		log.Println(err)
	}
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

// match finds the routes on r's path, root first, and the node the path ends
// at, recording URL parameters in r.URLParams. A path that names no route is
// NotFound.
func (m *Mux) match(r *Request) ([]Route, *muxRoute, error) {
	current := m.rootRoute
	var routes []Route
	if current.route != nil {
		routes = append(routes, current.route)
	}
	for _, part := range strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")[1:] {
		// An empty segment names no route and is never a parameter's value.
		if part == "" {
			return nil, nil, NotFound()
		}
		// A segment shaped like a parameter folder's name is a value, never
		// that folder.
		var child *muxRoute
		if !reDynParamGlobal.MatchString(part) {
			child = current.children[part]
		}
		if child == nil && current.paramId != "" {
			r.URLParams[current.paramId] = part
			child = current.children["_"+current.paramId+"_"]
		}
		if child == nil {
			return nil, nil, NotFound()
		}
		current = child
		if current.route != nil {
			routes = append(routes, current.route)
		}
	}
	// A path that only groups child routes has nothing of its own to render
	// or redirect from.
	if current.route == nil {
		return nil, nil, NotFound()
	}
	return routes, current, nil
}

// guard makes a fresh state for every route and runs their Guards, root
// first, stopping at the first error.
func guard(ctx context.Context, r *Request, routes []Route) ([]RouteState, error) {
	states := make([]RouteState, len(routes))
	for i, route := range routes {
		states[i] = route.NewState()
	}
	for _, s := range states {
		if err := s.Guard(ctx, r); err != nil {
			return nil, err
		}
	}
	return states, nil
}

// rendered returns the states that render the page at the end of the path:
// every layout on the path and the last route. The gates above it have had
// their Guard and take no further part.
func rendered(routes []Route, states []RouteState) []RouteState {
	page := make([]RouteState, 0, len(states))
	for i, route := range routes {
		if i == len(routes)-1 || route.Layout() {
			page = append(page, states[i])
		}
	}
	return page
}

var reDynParamGlobal = regexp.MustCompile("^_[^_]+_$")

func defaultErrorHandler(w http.ResponseWriter, r *Request, err error) {
	var (
		httpErr     *HttpError
		redirectErr *HttpRedirect
	)
	if errors.As(err, &httpErr) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
