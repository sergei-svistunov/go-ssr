package mux

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- test helpers ---

// hookRoute runs the given funcs as its Guard, Data and DefaultRoute (nil
// does nothing), owns every form, and writes body followed by its child.
type hookRoute struct {
	body  string
	guard func(*Request) error
	data  func(*Request, ResponseWriter) error
	def   func(*Request) (string, error)
}

func (h hookRoute) NewState() RouteState { return &hookState{route: h} }

// Layout is true: a hookRoute writes its child.
func (hookRoute) Layout() bool { return true }

type hookState struct {
	Frame
	route hookRoute
}

func (s *hookState) Guard(_ context.Context, r *Request) error {
	if s.route.guard == nil {
		return nil
	}
	return s.route.guard(r)
}

func (s *hookState) InitForms(context.Context, *Request, ResponseWriter) error { return nil }

func (s *hookState) SubmitForm(context.Context, *Request, ResponseWriter, string) (bool, error) {
	return true, nil
}

func (s *hookState) Data(_ context.Context, r *Request, w ResponseWriter) error {
	if s.route.data == nil {
		return nil
	}
	return s.route.data(r, w)
}

func (s *hookState) DefaultRoute(_ context.Context, r *Request) (string, error) {
	if s.route.def == nil {
		return "", nil
	}
	return s.route.def(r)
}

func (s *hookState) Write(w io.Writer) error {
	if _, err := io.WriteString(w, s.route.body); err != nil {
		return err
	}
	return s.WriteChild(w)
}

// testRoute renders body, fails its Data with err, and names
// defaultSubRoute as its default child.
type testRoute struct {
	body            string
	defaultSubRoute string
	err             error
}

func (testRoute) Layout() bool { return true }

func (r testRoute) NewState() RouteState {
	return hookRoute{
		body: r.body,
		data: func(*Request, ResponseWriter) error { return r.err },
		def:  func(*Request) (string, error) { return r.defaultSubRoute, nil },
	}.NewState()
}

// testKey is the CSRFKey newMux supplies when opts has none.
var testKey = []byte("test-key-test-key-test-key-test1")

func newMux(routes map[string]Route, opts Options) *Mux {
	if opts.CSRFKey == nil {
		opts.CSRFKey = testKey
	}
	m, err := New(routes, opts)
	if err != nil {
		panic(err)
	}
	return m
}

func doGet(t testing.TB, m *Mux, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	m.ServeHTTP(w, r)
	return w
}

// --- tests ---

func TestMux_StaticRoute(t *testing.T) {
	m := newMux(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	w := doGet(t, m, "/home")

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

	w := doGet(t, m, "/nonexistent")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMux_DynamicParam(t *testing.T) {
	var capturedParam string
	m := newMux(map[string]Route{
		"/users/_userId_": hookRoute{body: "user page", data: func(r *Request, _ ResponseWriter) error {
			capturedParam = r.URLParam("userId")
			return nil
		}},
	}, Options{})

	w := doGet(t, m, "/users/john42")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedParam != "john42" {
		t.Fatalf("expected param 'john42', got %q", capturedParam)
	}
}

func TestMux_DefaultSubRoute_Redirect(t *testing.T) {
	m := newMux(map[string]Route{
		"/users":      testRoute{defaultSubRoute: "list"},
		"/users/list": testRoute{body: "user list"},
	}, Options{})

	w := doGet(t, m, "/users")

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

	w := doGet(t, m, "/home/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMux_RouteError_DefaultHandler(t *testing.T) {
	m := newMux(map[string]Route{
		"/fail": testRoute{err: NewHttpError(http.StatusForbidden, "forbidden")},
	}, Options{})

	w := doGet(t, m, "/fail")

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

	w := doGet(t, m, "/fail")

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

	w := doGet(t, m, "/old")

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
	w := doGet(t, m, "/users/42")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Parent with children redirects
	w = doGet(t, m, "/users")
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
}

func TestMux_RootPath(t *testing.T) {
	m := newMux(map[string]Route{
		"/": testRoute{body: "root"},
	}, Options{})

	w := doGet(t, m, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "root" {
		t.Fatalf("expected 'root', got %q", w.Body.String())
	}
}

func TestMux_MultipleDynamicSegments(t *testing.T) {
	var capturedUserId, capturedPostId string
	m := newMux(map[string]Route{
		"/users/_userId_/posts/_postId_": hookRoute{body: "post page", data: func(r *Request, _ ResponseWriter) error {
			capturedUserId = r.URLParam("userId")
			capturedPostId = r.URLParam("postId")
			return nil
		}},
	}, Options{})

	w := doGet(t, m, "/users/alice/posts/99")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedUserId != "alice" {
		t.Fatalf("expected userId 'alice', got %q", capturedUserId)
	}
	if capturedPostId != "99" {
		t.Fatalf("expected postId '99', got %q", capturedPostId)
	}
}

func TestMux_StaticRouteOverDynamic(t *testing.T) {
	m := newMux(map[string]Route{
		"/users/_id_":  testRoute{body: "dynamic user"},
		"/users/admin": testRoute{body: "admin page"},
	}, Options{})

	// Static match should take priority
	w := doGet(t, m, "/users/admin")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "admin page" {
		t.Fatalf("expected 'admin page', got %q", w.Body.String())
	}

	// Dynamic still works for other values
	w = doGet(t, m, "/users/john")
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

	w := doGet(t, m, "/fail")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestMux_ResponseWriter_HeadersFromRoute(t *testing.T) {
	m := newMux(map[string]Route{
		"/page": hookRoute{body: "with headers", data: func(_ *Request, w ResponseWriter) error {
			w.Header().Set("X-Custom", "test-value")
			return nil
		}},
	}, Options{})

	w := doGet(t, m, "/page")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if v := w.Header().Get("X-Custom"); v != "test-value" {
		t.Fatalf("expected X-Custom header 'test-value', got %q", v)
	}
}

// captureRoute records the Request its Guard was handed so tests can inspect
// what the mux attached to it.
type captureRoute struct {
	got **Request
}

func (captureRoute) Layout() bool { return true }

func (r captureRoute) NewState() RouteState {
	return hookRoute{body: "ok", guard: func(req *Request) error {
		*r.got = req
		return nil
	}}.NewState()
}

func TestMux_CookieAndOriginOptionsReachRoute(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		var got *Request
		patterns := []string{"dev.example:*"}
		m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{
			InsecureCookies: insecure,
			OriginPatterns:  patterns,
		})
		doGet(t, m, "/")
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

// cacheRoute sets Cache-Control the way a route's Data hook might.
func cacheRoute(value string) Route {
	return hookRoute{body: "ok", data: func(_ *Request, w ResponseWriter) error {
		w.Header().Set("Cache-Control", value)
		return nil
	}}
}

// noStoreRoute marks the response uncacheable, as a route issuing a CSRF
// token does.
var noStoreRoute = hookRoute{body: "ok", data: func(r *Request, _ ResponseWriter) error {
	r.NoStore()
	return nil
}}

func TestMux_NoStoreSurvivesLaterRoutes(t *testing.T) {
	// The layout runs first and marks no-store; the leaf runs after it and
	// sets a cacheable policy. The mark must win.
	m := newMux(map[string]Route{
		"/":        noStoreRoute,
		"/contact": cacheRoute("public, max-age=300"),
	}, Options{})
	w := doGet(t, m, "/contact")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestMux_CacheControlLeftAloneWithoutNoStore(t *testing.T) {
	m := newMux(map[string]Route{
		"/":        cacheRoute("public, max-age=300"),
		"/contact": testRoute{body: "ok"},
	}, Options{})
	w := doGet(t, m, "/contact")
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want the route's own value", got)
	}
}

func doCrossSite(t testing.TB, m *Mux, method string) (*httptest.ResponseRecorder, *http.Request) {
	w := httptest.NewRecorder()
	r := formRequest(t, method, "/", "form", "")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	m.ServeHTTP(w, r)
	return w, r
}

func TestMux_RejectsCrossSiteUnsafeMethodsBeforeRouting(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		var got *Request
		m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{})
		w, _ := doCrossSite(t, m, method)
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
	w, _ := doCrossSite(t, m, http.MethodGet)
	if w.Code != http.StatusOK || got == nil {
		t.Errorf("cross-site GET: status %d, route called %v", w.Code, got != nil)
	}
}

func TestMux_ListedOriginMayUseUnsafeMethods(t *testing.T) {
	var got *Request
	m := newMux(map[string]Route{"/": captureRoute{got: &got}}, Options{OriginPatterns: []string{"evil.example"}})
	w, _ := doCrossSite(t, m, http.MethodPost)
	if w.Code != http.StatusOK || got == nil {
		t.Errorf("listed origin POST: status %d, route called %v", w.Code, got != nil)
	}
}

// doCrossSitePath sends a cross-site request with a valid form token, so
// only the origin check can refuse it.
func doCrossSitePath(t testing.TB, m *Mux, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := formRequest(t, method, path, "form", "")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	m.ServeHTTP(w, r)
	return w
}

func TestMux_CrossSiteToUnknownPathIs404(t *testing.T) {
	m := newMux(map[string]Route{"/": testRoute{body: "ok"}}, Options{})
	if w := doCrossSitePath(t, m, http.MethodPost, "/nope"); w.Code != http.StatusNotFound {
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
	if w := doCrossSitePath(t, m, http.MethodPost, "/callback"); w.Code != http.StatusOK || got == nil {
		t.Errorf("/callback: status %d, route called %v; want the hook to let it through", w.Code, got != nil)
	}
	got = nil
	if w := doCrossSitePath(t, m, http.MethodPost, "/other"); w.Code != http.StatusForbidden || got != nil {
		t.Errorf("/other: status %d, route called %v; want 403", w.Code, got != nil)
	}
}

func TestMux_AllowCrossSiteKeepsTheTokenCheck(t *testing.T) {
	log := &[]string{}
	m := newMux(map[string]Route{
		"/callback": &fakeRoute{name: "callback", log: log, forms: []string{"form"}},
		"/other":    &fakeRoute{name: "other", log: log, forms: []string{"form"}},
	}, Options{
		AllowCrossSite: func(r *Request) bool { return r.URL.Path == "/callback" },
	})
	for _, tc := range []struct {
		name, path, cookie string
		status             int
		body, hooks        string
	}{
		{"exempt, no cookie, no token", "/callback", "", http.StatusBadRequest, "Missed CSRF token", "callback.guard"},
		{"exempt, cookie, no token", "/callback", "planted", http.StatusBadRequest, "Invalid CSRF token", "callback.guard"},
		{"not exempt", "/other", "", http.StatusForbidden, "Cross-site request", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			*log = nil
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://app.example"+tc.path, strings.NewReader("a=b"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("Origin", "https://evil.example")
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: tc.cookie})
			}
			if w := serve(m, r); w.Code != tc.status || w.Body.String() != tc.body {
				t.Errorf("status %d, body %q; want %d %q", w.Code, w.Body.String(), tc.status, tc.body)
			}
			wantLog(t, log, tc.hooks)
		})
	}
}

func TestMux_ErrorMessageIsPlainText(t *testing.T) {
	m := newMux(map[string]Route{"/": hookRoute{guard: func(*Request) error {
		return NewHttpError(http.StatusForbidden, "<b>x</b>")
	}}}, Options{})
	w := doGet(t, m, "/")
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q, want text/plain; charset=utf-8", got)
	}
	if w.Code != http.StatusForbidden || w.Body.String() != "<b>x</b>" {
		t.Errorf("status %d, body %q; want 403 and the message verbatim", w.Code, w.Body.String())
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
	doCrossSitePath(t, m, http.MethodPost, "/users/42")
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
		log.SetOutput(io.Discard)
		routedRefusals = logThrottle{}
		handshakeRefusals = logThrottle{}
	})
	return &buf
}

func TestMux_CrossSiteRejectionIsLogged(t *testing.T) {
	buf := captureRefusalLog(t)
	m := newMux(map[string]Route{"/": testRoute{body: "ok"}}, Options{})
	doCrossSitePath(t, m, http.MethodPost, "/")
	if !strings.Contains(buf.String(), "cross-site") || !strings.Contains(buf.String(), "evil.example") {
		t.Errorf("expected a log line naming the origin, got %q", buf.String())
	}
}

// redirectingCacheRoute is a leaf that makes the response cacheable and
// then redirects, after the layout already issued a token.
var redirectingCacheRoute = hookRoute{data: func(_ *Request, w ResponseWriter) error {
	w.Header().Set("Cache-Control", "public, max-age=60")
	return Redirect(http.StatusSeeOther, "/login")
}}

func TestMux_NoStoreSurvivesRedirectFromLaterRoute(t *testing.T) {
	m := newMux(map[string]Route{
		"/":        noStoreRoute,
		"/contact": redirectingCacheRoute,
	}, Options{})
	w := doGet(t, m, "/contact")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q on the redirect, want no-store", got)
	}
}

func TestMux_CrossSiteLogQuotesThePath(t *testing.T) {
	buf := captureRefusalLog(t)
	m := newMux(map[string]Route{"/": testRoute{body: "ok"}, "/x\nmux: forged": testRoute{body: "ok"}}, Options{})
	doCrossSitePath(t, m, http.MethodPost, "/x%0Amux:%20forged")
	if n := strings.Count(strings.TrimSuffix(buf.String(), "\n"), "\n"); n != 0 {
		t.Errorf("a path with a newline forged %d extra log line(s): %q", n, buf.String())
	}
}

func TestMux_NoStoreAppliesToDefaultRouteRedirect(t *testing.T) {
	// The section index marks the response and then redirects to its default
	// child, the way an auth-aware one might.
	m := newMux(map[string]Route{
		"/users": hookRoute{def: func(r *Request) (string, error) {
			r.NoStore()
			return "info", nil
		}},
		"/users/info": testRoute{body: "ok"},
	}, Options{})
	w := doGet(t, m, "/users")
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
	doGet(t, m, "/")
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
	doGet(t, m2, "/")
	if len(second.OriginPatterns) != 1 {
		t.Errorf("a hook's append leaked into another request: %v", second.OriginPatterns)
	}
}

func TestMux_RoutedRequestCarriesOptionsInItsContext(t *testing.T) {
	// The route rebuilds a Request the way a hook spawning work would.
	var rebuilt *Request
	m := newMux(map[string]Route{"/": hookRoute{data: func(r *Request, _ ResponseWriter) error {
		rebuilt = NewRequest(r.Request)
		return nil
	}}}, Options{
		InsecureCookies: true,
		OriginPatterns:  []string{"dev.example"},
	})
	doGet(t, m, "/")
	if rebuilt == nil {
		t.Fatal("route was not called")
	}
	if !rebuilt.InsecureCookies || len(rebuilt.OriginPatterns) != 1 {
		t.Errorf("NewRequest on the routed path lost the options: InsecureCookies=%v OriginPatterns=%v",
			rebuilt.InsecureCookies, rebuilt.OriginPatterns)
	}
}

func TestMux_NoStoreSurvivesARebuiltRequest(t *testing.T) {
	// The leaf marks the response through a Request it rebuilt itself, the
	// way generated reactive code rebuilds one.
	m := newMux(map[string]Route{
		"/": cacheRoute("public, max-age=300"),
		"/contact": hookRoute{body: "ok", data: func(r *Request, _ ResponseWriter) error {
			NewRequest(r.Request).NoStore()
			return nil
		}},
	}, Options{})
	w := doGet(t, m, "/contact")
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: the mark was lost with the rebuilt Request", got)
	}
}

func TestRequest_NoStoreOutsideMuxIsANoop(t *testing.T) {
	// Must not panic when the Request was not built by the mux.
	NewRequest(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)).NoStore()
}

func TestMux_ArrivalSessionSurvivesARebuiltRequest(t *testing.T) {
	// The route rebuilds the Request and reports the session each one sees,
	// after the hook's answer has changed.
	session := "anonymous"
	var direct, rebuilt string
	m := newMux(map[string]Route{"/": hookRoute{data: func(r *Request, _ ResponseWriter) error {
		_ = r.CSRFSession() // the arrival session is resolved here
		session = "user-1"
		direct = r.CSRFSession()
		rebuilt = NewRequest(r.Request).CSRFSession()
		return nil
	}}}, Options{CSRFSession: func(*Request) string { return session }})
	doGet(t, m, "/")

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
	t.Cleanup(func() { log.SetOutput(io.Discard) })

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
		doCrossSitePath(t, m, http.MethodPost, "/")
	}
	if n := strings.Count(buf.String(), "refused as cross-site"); n != 1 {
		t.Errorf("20 refusals wrote %d log lines; an unauthenticated client must not drive log volume", n)
	}
}

func TestMux_ArrivalSessionIsResolvedBeforeAnyHookRuns(t *testing.T) {
	// A hook that establishes a session before anything asks for the arrival
	// value must not become the arrival value. Guard is the first hook.
	session := "anonymous"
	var seen string
	m := newMux(map[string]Route{"/": hookRoute{guard: func(r *Request) error {
		session = "user-1"
		seen = r.CSRFSession()
		return nil
	}}}, Options{CSRFSession: func(*Request) string { return session }})
	doGet(t, m, "/")
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
	doGet(t, m, "/")
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
