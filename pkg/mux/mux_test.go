package mux

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
