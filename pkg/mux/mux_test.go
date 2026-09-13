package mux

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- test helpers ---

type testDataContext struct {
	body string
}

func (d *testDataContext) Write(w io.Writer) error {
	_, err := w.Write([]byte(d.body))
	return err
}

func (d *testDataContext) WriteAssets(w io.Writer, written map[string]struct{}) error {
	return nil
}

type testRoute struct {
	body            string
	defaultSubRoute string
	err             error
}

func (r testRoute) GetDataContext(_ context.Context, req *Request, _ ResponseWriter, child DataContext) (DataContext, error) {
	if r.err != nil {
		return nil, r.err
	}
	if child != nil {
		return child, nil
	}
	return &testDataContext{body: r.body}, nil
}

func (r testRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return r.defaultSubRoute, nil
}

func newMux(routes map[string]Route, opts Options) *Mux {
	return New(routes, opts)
}

func doGet(m *Mux, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	m.ServeHTTP(w, r)
	return w
}

// --- tests ---

func TestMux_StaticRoute(t *testing.T) {
	m := newMux(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	w := doGet(m, "/home")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "home page" {
		t.Fatalf("expected 'home page', got %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html content type, got %q", ct)
	}
}

func TestMux_NotFound(t *testing.T) {
	m := newMux(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	w := doGet(m, "/nonexistent")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMux_DynamicParam(t *testing.T) {
	paramRoute := &paramCapturingRoute{body: "user page"}
	m := newMux(map[string]Route{
		"/users/_userId_": paramRoute,
	}, Options{})

	w := doGet(m, "/users/john42")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if paramRoute.capturedParam != "john42" {
		t.Fatalf("expected param 'john42', got %q", paramRoute.capturedParam)
	}
}

type paramCapturingRoute struct {
	body          string
	capturedParam string
}

func (r *paramCapturingRoute) GetDataContext(_ context.Context, req *Request, _ ResponseWriter, child DataContext) (DataContext, error) {
	r.capturedParam = req.URLParam("userId")
	return &testDataContext{body: r.body}, nil
}

func (r *paramCapturingRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_DefaultSubRoute_Redirect(t *testing.T) {
	m := newMux(map[string]Route{
		"/users":      testRoute{defaultSubRoute: "list"},
		"/users/list": testRoute{body: "user list"},
	}, Options{})

	w := doGet(m, "/users")

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/users/list" {
		t.Fatalf("expected redirect to /users/list, got %q", loc)
	}
}

func TestMux_TrailingSlashStripped(t *testing.T) {
	m := newMux(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	w := doGet(m, "/home/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMux_RouteError_DefaultHandler(t *testing.T) {
	m := newMux(map[string]Route{
		"/fail": testRoute{err: NewHttpError(http.StatusForbidden, "forbidden")},
	}, Options{})

	w := doGet(m, "/fail")

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestMux_RouteError_CustomHandler(t *testing.T) {
	var capturedErr error
	m := newMux(map[string]Route{
		"/fail": testRoute{err: errors.New("boom")},
	}, Options{
		ErrorHandler: func(w http.ResponseWriter, r *Request, err error) {
			capturedErr = err
			w.WriteHeader(http.StatusTeapot)
		},
	})

	w := doGet(m, "/fail")

	if w.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", w.Code)
	}
	if capturedErr == nil || capturedErr.Error() != "boom" {
		t.Fatalf("expected error 'boom', got %v", capturedErr)
	}
}

func TestMux_Redirect_DefaultHandler(t *testing.T) {
	m := newMux(map[string]Route{
		"/old": testRoute{err: Redirect(http.StatusMovedPermanently, "/new")},
	}, Options{})

	w := doGet(m, "/old")

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/new" {
		t.Fatalf("expected redirect to /new, got %q", loc)
	}
}

func TestMux_NestedRoutes(t *testing.T) {
	m := newMux(map[string]Route{
		"/users":      testRoute{body: "users", defaultSubRoute: "list"},
		"/users/_id_": testRoute{body: "user detail"},
	}, Options{})

	// Leaf route works
	w := doGet(m, "/users/42")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Parent with children redirects
	w = doGet(m, "/users")
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
}

func TestMux_RootPath(t *testing.T) {
	m := newMux(map[string]Route{
		"/": testRoute{body: "root"},
	}, Options{})

	w := doGet(m, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "root" {
		t.Fatalf("expected 'root', got %q", w.Body.String())
	}
}

func TestMux_MultipleDynamicSegments(t *testing.T) {
	route := &multiParamRoute{body: "post page"}
	m := newMux(map[string]Route{
		"/users/_userId_/posts/_postId_": route,
	}, Options{})

	w := doGet(m, "/users/alice/posts/99")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if route.capturedUserId != "alice" {
		t.Fatalf("expected userId 'alice', got %q", route.capturedUserId)
	}
	if route.capturedPostId != "99" {
		t.Fatalf("expected postId '99', got %q", route.capturedPostId)
	}
}

type multiParamRoute struct {
	body           string
	capturedUserId string
	capturedPostId string
}

func (r *multiParamRoute) GetDataContext(_ context.Context, req *Request, _ ResponseWriter, child DataContext) (DataContext, error) {
	r.capturedUserId = req.URLParam("userId")
	r.capturedPostId = req.URLParam("postId")
	return &testDataContext{body: r.body}, nil
}

func (r *multiParamRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_StaticRouteOverDynamic(t *testing.T) {
	m := newMux(map[string]Route{
		"/users/_id_":  testRoute{body: "dynamic user"},
		"/users/admin": testRoute{body: "admin page"},
	}, Options{})

	// Static match should take priority
	w := doGet(m, "/users/admin")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "admin page" {
		t.Fatalf("expected 'admin page', got %q", w.Body.String())
	}

	// Dynamic still works for other values
	w = doGet(m, "/users/john")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "dynamic user" {
		t.Fatalf("expected 'dynamic user', got %q", w.Body.String())
	}
}

func TestMux_GenericError_Returns500(t *testing.T) {
	m := newMux(map[string]Route{
		"/fail": testRoute{err: errors.New("unexpected")},
	}, Options{})

	w := doGet(m, "/fail")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestMux_ResponseWriter_HeadersFromRoute(t *testing.T) {
	route := &headerSettingRoute{body: "with headers"}
	m := newMux(map[string]Route{
		"/page": route,
	}, Options{})

	w := doGet(m, "/page")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if v := w.Header().Get("X-Custom"); v != "test-value" {
		t.Fatalf("expected X-Custom header 'test-value', got %q", v)
	}
}

type headerSettingRoute struct {
	body string
}

func (r *headerSettingRoute) GetDataContext(_ context.Context, _ *Request, w ResponseWriter, child DataContext) (DataContext, error) {
	w.Header().Set("X-Custom", "test-value")
	return &testDataContext{body: r.body}, nil
}

func (r *headerSettingRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

// captureRoute records the Request it was handed so tests can inspect what the
// mux attached to it.
type captureRoute struct {
	got **Request
}

func (r captureRoute) GetDataContext(_ context.Context, req *Request, _ ResponseWriter, _ DataContext) (DataContext, error) {
	*r.got = req
	return &testDataContext{body: "ok"}, nil
}

func (r captureRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_CookieAndOriginOptionsReachRoute(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		var got *Request
		patterns := []string{"dev.example:*"}
		m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{
			InsecureCookies: insecure,
			OriginPatterns:  patterns,
		})
		doGet(m, "/")
		if got == nil {
			t.Fatal("route was not called")
		}
		if got.InsecureCookies != insecure {
			t.Errorf("Options.InsecureCookies=%v, but Request.InsecureCookies=%v", insecure, got.InsecureCookies)
		}
		if len(got.OriginPatterns) != 1 || got.OriginPatterns[0] != patterns[0] {
			t.Errorf("Options.OriginPatterns=%v, but Request.OriginPatterns=%v", patterns, got.OriginPatterns)
		}
	}
}

// cacheRoute sets Cache-Control the way a layout's Data hook might, and
// keeps whatever data context the leaf produced.
type cacheRoute struct{ value string }

func (r cacheRoute) GetDataContext(_ context.Context, _ *Request, w ResponseWriter, child DataContext) (DataContext, error) {
	w.Header().Set("Cache-Control", r.value)
	if child != nil {
		return child, nil
	}
	return &testDataContext{body: "ok"}, nil
}

func (r cacheRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) { return "", nil }

// noStoreRoute is a leaf that marks the response uncacheable, as a route
// issuing a CSRF token does.
type noStoreRoute struct{}

func (noStoreRoute) GetDataContext(_ context.Context, req *Request, _ ResponseWriter, _ DataContext) (DataContext, error) {
	req.NoStore()
	return &testDataContext{body: "ok"}, nil
}

func (noStoreRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) { return "", nil }

func TestMux_NoStoreSurvivesLaterRoutes(t *testing.T) {
	// The leaf runs first and marks no-store; the layout runs after it and
	// sets a cacheable policy. The mark must win.
	m := newMux(map[string]Route{
		"/":        cacheRoute{value: "public, max-age=300"},
		"/contact": noStoreRoute{},
	}, Options{})
	w := doGet(m, "/contact")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestMux_CacheControlLeftAloneWithoutNoStore(t *testing.T) {
	m := newMux(map[string]Route{
		"/":        cacheRoute{value: "public, max-age=300"},
		"/contact": testRoute{body: "ok"},
	}, Options{})
	w := doGet(m, "/contact")
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want the route's own value", got)
	}
}

func doCrossSite(m *Mux, method string) (*httptest.ResponseRecorder, *http.Request) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	m.ServeHTTP(w, r)
	return w, r
}

func TestMux_RejectsCrossSiteUnsafeMethodsBeforeRouting(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		var got *Request
		m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{})
		w, _ := doCrossSite(m, method)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", method, w.Code)
		}
		if got != nil {
			t.Errorf("%s: route ran on a cross-site request", method)
		}
	}
}

func TestMux_SafeMethodsAreNotOriginChecked(t *testing.T) {
	var got *Request
	m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{})
	w, _ := doCrossSite(m, http.MethodGet)
	if w.Code != http.StatusOK || got == nil {
		t.Errorf("cross-site GET: status %d, route called %v", w.Code, got != nil)
	}
}

func TestMux_ListedOriginMayUseUnsafeMethods(t *testing.T) {
	var got *Request
	m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{OriginPatterns: []string{"evil.example"}})
	w, _ := doCrossSite(m, http.MethodPost)
	if w.Code != http.StatusOK || got == nil {
		t.Errorf("listed origin POST: status %d, route called %v", w.Code, got != nil)
	}
}

func doCrossSitePath(m *Mux, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	m.ServeHTTP(w, r)
	return w
}

func TestMux_CrossSiteToUnknownPathIs404(t *testing.T) {
	m := newMux(map[string]Route{"/": testRoute{body: "ok"}}, Options{})
	if w := doCrossSitePath(m, http.MethodPost, "/nope"); w.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 (routing before the origin gate)", w.Code)
	}
}

func TestMux_AllowCrossSiteExemptsChosenRequests(t *testing.T) {
	var got *Request
	m := newMux(map[string]Route{
		"/callback": captureRoute{got: &got},
		"/other":    captureRoute{got: &got},
	}, Options{
		AllowCrossSite: func(r *Request) bool { return r.URL.Path == "/callback" },
	})
	if w := doCrossSitePath(m, http.MethodPost, "/callback"); w.Code != http.StatusOK || got == nil {
		t.Errorf("/callback: status %d, route called %v; want the hook to let it through", w.Code, got != nil)
	}
	got = nil
	if w := doCrossSitePath(m, http.MethodPost, "/other"); w.Code != http.StatusForbidden || got != nil {
		t.Errorf("/other: status %d, route called %v; want 403", w.Code, got != nil)
	}
}

func TestMux_CrossSiteErrorHandlerSeesURLParams(t *testing.T) {
	var seen map[string]string
	m := newMux(map[string]Route{"/users/_userId_": testRoute{body: "ok"}}, Options{
		ErrorHandler: func(w http.ResponseWriter, r *Request, err error) {
			seen = r.URLParams
			http.Error(w, err.Error(), http.StatusForbidden)
		},
	})
	doCrossSitePath(m, http.MethodPost, "/users/42")
	if seen["userId"] != "42" {
		t.Errorf("URLParams = %v, want userId=42 resolved before the origin gate", seen)
	}
}

// captureRefusalLog captures the log and rearms the throttle, which is
// package state shared by every test in this package.
func captureRefusalLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	routedRefusals = logThrottle{}
	handshakeRefusals = logThrottle{}
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		routedRefusals = logThrottle{}
		handshakeRefusals = logThrottle{}
	})
	return &buf
}

func TestMux_CrossSiteRejectionIsLogged(t *testing.T) {
	buf := captureRefusalLog(t)
	m := newMux(map[string]Route{"/": testRoute{body: "ok"}}, Options{})
	doCrossSitePath(m, http.MethodPost, "/")
	if !strings.Contains(buf.String(), "cross-site") || !strings.Contains(buf.String(), "evil.example") {
		t.Errorf("expected a log line naming the origin, got %q", buf.String())
	}
}

// redirectingCacheRoute is a layout that makes the response cacheable and
// then redirects, after the leaf already issued a token.
type redirectingCacheRoute struct{}

func (redirectingCacheRoute) GetDataContext(_ context.Context, _ *Request, w ResponseWriter, _ DataContext) (DataContext, error) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	return nil, Redirect(http.StatusSeeOther, "/login")
}

func (redirectingCacheRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_NoStoreSurvivesRedirectFromLaterRoute(t *testing.T) {
	m := newMux(map[string]Route{
		"/":        redirectingCacheRoute{},
		"/contact": noStoreRoute{},
	}, Options{})
	w := doGet(m, "/contact")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q on the redirect, want no-store", got)
	}
}

// wsProbe stands in for a WebSocket handler: it records what the mux
// dispatched to it and what NewRequest derives from that request.
type wsProbe struct {
	got     **Request
	checked *bool
}

func (p wsProbe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	*p.got = NewRequest(r)
	*p.checked = OriginChecked(r.Context())
	w.WriteHeader(http.StatusSwitchingProtocols)
}

func newWSMux(opts Options, got **Request, checked *bool) *Mux {
	m := New(map[string]Route{}, opts)
	WithWSHandlers(map[string]http.Handler{"/live/__ws": wsProbe{got: got, checked: checked}})(m)
	return m
}

func doUpgrade(m *Mux, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/live/__ws", nil)
	r.Host = "app.example"
	r.Header.Set("Upgrade", "websocket")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	m.ServeHTTP(w, r)
	return w
}

func TestMux_WSDispatchRefusesCrossSite(t *testing.T) {
	var got *Request
	var checked bool
	m := newWSMux(Options{}, &got, &checked)
	w := doUpgrade(m, map[string]string{"Origin": "https://evil.example"})
	if w.Code != http.StatusForbidden || got != nil {
		t.Errorf("status %d, handler called %v; want 403 before dispatch", w.Code, got != nil)
	}
}

func TestMux_WSDispatchMarksOriginCheckedAndPassesOptions(t *testing.T) {
	var got *Request
	var checked bool
	m := newWSMux(Options{InsecureCookies: true, OriginPatterns: []string{"dev.example:*"}}, &got, &checked)
	w := doUpgrade(m, map[string]string{"Origin": "http://dev.example:3000"})
	if w.Code != http.StatusSwitchingProtocols || got == nil {
		t.Fatalf("status %d, handler called %v; want the listed origin dispatched", w.Code, got != nil)
	}
	if !checked {
		t.Error("handler should see the request marked as origin-checked")
	}
	if !got.InsecureCookies || len(got.OriginPatterns) != 1 {
		t.Errorf("NewRequest inside the handler: InsecureCookies=%v OriginPatterns=%v; want the mux options", got.InsecureCookies, got.OriginPatterns)
	}
}

func TestOriginChecked_FalseOutsideMux(t *testing.T) {
	if OriginChecked(context.Background()) {
		t.Error("a bare context must not count as origin-checked")
	}
}

func TestMux_WSDispatchRefusesOpaqueOrigin(t *testing.T) {
	var got *Request
	var checked bool
	m := newWSMux(Options{}, &got, &checked)
	w := doUpgrade(m, map[string]string{"Origin": "null"})
	if w.Code != http.StatusForbidden || got != nil {
		t.Errorf("status %d, handler called %v; want 403 for an opaque Origin", w.Code, got != nil)
	}
}

func TestMux_WSDispatchHonoursAllowCrossSite(t *testing.T) {
	var got *Request
	var checked bool
	var sawParams map[string]string
	m := New(map[string]Route{}, Options{
		AllowCrossSite: func(r *Request) bool {
			sawParams = r.URLParams
			return r.URL.Path == "/widget/42/__ws"
		},
	})
	WithWSHandlers(map[string]http.Handler{"/widget/_id_/__ws": wsProbe{got: &got, checked: &checked}})(m)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/widget/42/__ws", nil)
	r.Host = "app.example"
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Origin", "https://partner.example")
	m.ServeHTTP(w, r)

	if w.Code != http.StatusSwitchingProtocols || got == nil {
		t.Errorf("status %d, handler called %v; want the hook to let the handshake through", w.Code, got != nil)
	}
	if sawParams["id"] != "42" {
		t.Errorf("AllowCrossSite saw URLParams %v, want id=42", sawParams)
	}
}

func TestMux_WSDispatchAcceptsUppercaseUpgradeToken(t *testing.T) {
	var got *Request
	var checked bool
	m := newWSMux(Options{}, &got, &checked)
	w := doUpgrade(m, map[string]string{"Upgrade": "WebSocket", "Origin": "http://app.example"})
	if w.Code != http.StatusSwitchingProtocols || got == nil {
		t.Errorf("status %d, handler called %v; Upgrade is a case-insensitive token", w.Code, got != nil)
	}
}

func TestMux_CrossSiteLogQuotesThePath(t *testing.T) {
	buf := captureRefusalLog(t)
	m := newMux(map[string]Route{"/": testRoute{body: "ok"}, "/x\nmux: forged": testRoute{body: "ok"}}, Options{})
	doCrossSitePath(m, http.MethodPost, "/x%0Amux:%20forged")
	if n := strings.Count(strings.TrimSuffix(buf.String(), "\n"), "\n"); n != 0 {
		t.Errorf("a path with a newline forged %d extra log line(s): %q", n, buf.String())
	}
}

// noStoreDefaultRoute marks the response and then redirects to its default
// child, the way an auth-aware section index might.
type noStoreDefaultRoute struct{}

func (noStoreDefaultRoute) GetDataContext(_ context.Context, _ *Request, _ ResponseWriter, child DataContext) (DataContext, error) {
	return child, nil
}

func (noStoreDefaultRoute) GetDefaultRoute(_ context.Context, r *Request) (string, error) {
	r.NoStore()
	return "info", nil
}

func TestMux_NoStoreAppliesToDefaultRouteRedirect(t *testing.T) {
	m := newMux(map[string]Route{
		"/users":      noStoreDefaultRoute{},
		"/users/info": testRoute{body: "ok"},
	}, Options{})
	w := doGet(m, "/users")
	if w.Code != http.StatusFound {
		t.Fatalf("status %d, want 302", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q on the default-route redirect, want no-store", got)
	}
}

func TestMux_RequestOriginPatternsCannotAliasTheMux(t *testing.T) {
	shared := make([]string, 0, 4)
	shared = append(shared, "dev.example")
	var got *Request
	m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{OriginPatterns: shared})
	doGet(m, "/")
	if got == nil {
		t.Fatal("route was not called")
	}
	if cap(got.OriginPatterns) != len(got.OriginPatterns) {
		t.Errorf("cap %d > len %d: appending in a hook would write into the shared array",
			cap(got.OriginPatterns), len(got.OriginPatterns))
	}
	got.OriginPatterns = append(got.OriginPatterns, "leaked.example")
	var second *Request
	m2 := newMux(map[string]Route{"/": captureRoute{got: &second}}, Options{OriginPatterns: shared})
	doGet(m2, "/")
	if len(second.OriginPatterns) != 1 {
		t.Errorf("a hook's append leaked into another request: %v", second.OriginPatterns)
	}
}

// contextRoute rebuilds a Request the way a hook spawning work would.
type contextRoute struct{ rebuilt **Request }

func (c contextRoute) GetDataContext(_ context.Context, r *Request, _ ResponseWriter, _ DataContext) (DataContext, error) {
	*c.rebuilt = NewRequest(r.Request)
	return &testDataContext{body: "ok"}, nil
}

func (c contextRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) { return "", nil }

func TestMux_RoutedRequestCarriesOptionsInItsContext(t *testing.T) {
	var rebuilt *Request
	m := newMux(map[string]Route{"/": contextRoute{rebuilt: &rebuilt}}, Options{
		InsecureCookies: true,
		OriginPatterns:  []string{"dev.example"},
	})
	doGet(m, "/")
	if rebuilt == nil {
		t.Fatal("route was not called")
	}
	if !rebuilt.InsecureCookies || len(rebuilt.OriginPatterns) != 1 {
		t.Errorf("NewRequest on the routed path lost the options: InsecureCookies=%v OriginPatterns=%v",
			rebuilt.InsecureCookies, rebuilt.OriginPatterns)
	}
}

// rebuildingNoStoreRoute marks the response through a Request it rebuilt
// itself, the way generated reactive code rebuilds one.
type rebuildingNoStoreRoute struct{}

func (rebuildingNoStoreRoute) GetDataContext(_ context.Context, r *Request, _ ResponseWriter, _ DataContext) (DataContext, error) {
	NewRequest(r.Request).NoStore()
	return &testDataContext{body: "ok"}, nil
}

func (rebuildingNoStoreRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_NoStoreSurvivesARebuiltRequest(t *testing.T) {
	m := newMux(map[string]Route{
		"/":        cacheRoute{value: "public, max-age=300"},
		"/contact": rebuildingNoStoreRoute{},
	}, Options{})
	w := doGet(m, "/contact")
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: the mark was lost with the rebuilt Request", got)
	}
}

func TestRequest_NoStoreOutsideMuxIsANoop(t *testing.T) {
	// Must not panic when the Request was not built by the mux.
	NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)).NoStore()
}

// sessionProbeRoute rebuilds the Request and reports the session each one
// sees, after the hook's answer has changed.
type sessionProbeRoute struct {
	rotate          func()
	direct, rebuilt *string
}

func (s sessionProbeRoute) GetDataContext(_ context.Context, r *Request, _ ResponseWriter, _ DataContext) (DataContext, error) {
	_ = r.CSRFSession() // the arrival session is resolved here
	s.rotate()
	*s.direct = r.CSRFSession()
	*s.rebuilt = NewRequest(r.Request).CSRFSession()
	return &testDataContext{body: "ok"}, nil
}

func (s sessionProbeRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_ArrivalSessionSurvivesARebuiltRequest(t *testing.T) {
	session := "anonymous"
	var direct, rebuilt string
	m := newMux(map[string]Route{"/": sessionProbeRoute{
		rotate:  func() { session = "user-1" },
		direct:  &direct,
		rebuilt: &rebuilt,
	}}, Options{CSRFSession: func(*Request) string { return session }})
	doGet(m, "/")

	if direct != "anonymous" {
		t.Errorf("arrival session on the mux's own Request = %q, want anonymous", direct)
	}
	if rebuilt != direct {
		t.Errorf("a rebuilt Request reports %q, want the arrival session %q", rebuilt, direct)
	}
}

func TestLogThrottle_CollapsesRepeats(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	th := logThrottle{interval: time.Hour}
	for i := 0; i < 5; i++ {
		th.logf("refused %d", i)
	}
	if n := strings.Count(buf.String(), "refused"); n != 1 {
		t.Errorf("wrote %d lines for 5 refusals, want 1", n)
	}

	th.last = time.Time{} // the interval elapses
	th.logf("refused again")
	if !strings.Contains(buf.String(), "4 more suppressed") {
		t.Errorf("the next line should account for what it swallowed, got %q", buf.String())
	}
}

func TestMux_CrossSiteFloodDoesNotFloodTheLog(t *testing.T) {
	buf := captureRefusalLog(t)
	routedRefusals.interval = time.Hour

	m := newMux(map[string]Route{"/": testRoute{body: "ok"}}, Options{})
	for i := 0; i < 20; i++ {
		doCrossSitePath(m, http.MethodPost, "/")
	}
	if n := strings.Count(buf.String(), "refused as cross-site"); n != 1 {
		t.Errorf("20 refusals wrote %d log lines; an unauthenticated client must not drive log volume", n)
	}
}

// arrivalProbeRoute reports the arrival session, having changed the hook's
// answer before anything asked for it.
type arrivalProbeRoute struct {
	rotate func()
	seen   *string
}

func (a arrivalProbeRoute) GetDataContext(_ context.Context, r *Request, _ ResponseWriter, _ DataContext) (DataContext, error) {
	a.rotate()
	*a.seen = r.CSRFSession()
	return &testDataContext{body: "ok"}, nil
}

func (a arrivalProbeRoute) GetDefaultRoute(_ context.Context, _ *Request) (string, error) {
	return "", nil
}

func TestMux_ArrivalSessionIsResolvedBeforeAnyHookRuns(t *testing.T) {
	// A hook that establishes a session before anything asks for the arrival
	// value must not become the arrival value.
	session := "anonymous"
	var seen string
	m := newMux(map[string]Route{"/": arrivalProbeRoute{
		rotate: func() { session = "user-1" },
		seen:   &seen,
	}}, Options{CSRFSession: func(*Request) string { return session }})
	doGet(m, "/")
	if seen != "anonymous" {
		t.Errorf("arrival session = %q, want the one the request came in with", seen)
	}
}

func TestRequest_SharedStateIsSafeAcrossGoroutines(t *testing.T) {
	// Generated WebSocket code hands one Request to the Subscribe goroutine
	// and the write-dispatch goroutine; -race is what makes this meaningful.
	var got *Request
	m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{
		CSRFSession: func(*Request) string { return "user-1" },
	})
	doGet(m, "/")
	if got == nil {
		t.Fatal("route was not called")
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shared := NewRequest(got.Request)
			shared.NoStore()
			if s := shared.CSRFSession(); s != "user-1" {
				t.Errorf("CSRFSession = %q", s)
			}
		}()
	}
	wg.Wait()
}
