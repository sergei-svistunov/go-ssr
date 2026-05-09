package mux

import (
	"net/http"
	"net/http/httptest"
	"testing"
)


// TestWithWSHandlers_RoutesUnchanged verifies that adding WithWSHandlers does
// not break existing non-reactive routes (AC7 — mux.New signature unchanged).
func TestWithWSHandlers_RoutesUnchanged(t *testing.T) {
	m := New(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	// Apply the WS handlers option.
	WithWSHandlers(map[string]http.Handler{
		"/home/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusSwitchingProtocols)
		}),
	})(m)

	// Normal SSR GET request still works.
	w := doGet(m, "/home")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "home page" {
		t.Fatalf("expected 'home page', got %q", w.Body.String())
	}
}

// TestWithWSHandlers_WSRequestDispatched verifies that a request with
// Upgrade: websocket is routed to the WS handler and does NOT fall through to
// the SSR rendering path.
func TestWithWSHandlers_WSRequestDispatched(t *testing.T) {
	wsHandlerCalled := false

	m := New(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	WithWSHandlers(map[string]http.Handler{
		"/home/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wsHandlerCalled = true
			w.WriteHeader(http.StatusSwitchingProtocols)
		}),
	})(m)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/home/__ws", nil)
	r.Header.Set("Upgrade", "websocket")
	m.ServeHTTP(w, r)

	if !wsHandlerCalled {
		t.Fatal("WS handler was not called")
	}
	if w.Code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", w.Code)
	}
}

// TestWithWSHandlers_NonWSRequestFallsThrough verifies that a non-WS request
// to a WS endpoint path falls through to the SSR routing logic (gets 404 since
// __ws is not an SSR route).
func TestWithWSHandlers_NonWSRequestFallsThrough(t *testing.T) {
	m := New(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	WithWSHandlers(map[string]http.Handler{
		"/home/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("WS handler should not have been called for a non-WS request")
		}),
	})(m)

	// Request WITHOUT Upgrade header to the __ws path → falls through to SSR → 404.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/home/__ws", nil)
	m.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-WS request, got %d", w.Code)
	}
}

// TestWithWSHandlers_NoWSHandlers_MuxUnchanged verifies that a Mux without any
// WS handlers behaves identically to before (no regression for existing routes).
func TestWithWSHandlers_NoWSHandlers_MuxUnchanged(t *testing.T) {
	m := New(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})
	// No WithWSHandlers call.

	w := doGet(m, "/home")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestWithWSHandlers_MultipleRoutes verifies that WS handlers for different
// routes are dispatched correctly.
func TestWithWSHandlers_MultipleRoutes(t *testing.T) {
	fooWsCalled := false
	barWsCalled := false

	m := New(map[string]Route{
		"/foo": testRoute{body: "foo"},
		"/bar": testRoute{body: "bar"},
	}, Options{})

	WithWSHandlers(map[string]http.Handler{
		"/foo/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fooWsCalled = true
		}),
		"/bar/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			barWsCalled = true
		}),
	})(m)

	doWSRequest := func(path string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Upgrade", "websocket")
		m.ServeHTTP(w, r)
	}

	doWSRequest("/foo/__ws")
	doWSRequest("/bar/__ws")

	if !fooWsCalled {
		t.Error("foo WS handler was not called")
	}
	if !barWsCalled {
		t.Error("bar WS handler was not called")
	}
}

// TestWSHandler_URLParamsExtracted_Static verifies that a WS request to a
// static (no dynamic segments) path results in an empty URLParams map — the
// existing behaviour is preserved and URLParamsFromContext returns nil.
func TestWSHandler_URLParamsExtracted_Static(t *testing.T) {
	m := New(map[string]Route{
		"/home": testRoute{body: "home page"},
	}, Options{})

	var capturedParams map[string]string
	WithWSHandlers(map[string]http.Handler{
		"/home/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedParams = URLParamsFromContext(r.Context())
			w.WriteHeader(http.StatusSwitchingProtocols)
		}),
	})(m)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/home/__ws", nil)
	req.Header.Set("Upgrade", "websocket")
	m.ServeHTTP(rec, req)

	// Static paths have no dynamic segments — context should not be populated.
	if capturedParams != nil {
		t.Fatalf("expected nil URLParams for static path, got %v", capturedParams)
	}
}

// TestWSHandler_URLParamsExtracted_SingleDynamic verifies that a WS request to
// a path with a single dynamic segment populates URLParams correctly.
// Pattern: /users/_userId_/__ws, request: /users/u123/__ws → userId = "u123".
func TestWSHandler_URLParamsExtracted_SingleDynamic(t *testing.T) {
	m := New(map[string]Route{
		"/users/_userId_": testRoute{body: "user"},
	}, Options{})

	var capturedParams map[string]string
	WithWSHandlers(map[string]http.Handler{
		"/users/_userId_/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedParams = URLParamsFromContext(r.Context())
			w.WriteHeader(http.StatusSwitchingProtocols)
		}),
	})(m)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/u123/__ws", nil)
	req.Header.Set("Upgrade", "websocket")
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", rec.Code)
	}
	if capturedParams == nil {
		t.Fatal("URLParamsFromContext returned nil; expected map with userId")
	}
	if got := capturedParams["userId"]; got != "u123" {
		t.Fatalf("expected userId=u123, got %q", got)
	}
}

// TestWSHandler_URLParamsExtracted_MultipleDynamic verifies that a WS request
// to a path with multiple dynamic segments extracts all params.
// Pattern: /orgs/_orgId_/users/_userId_/__ws with /orgs/o7/users/u123/__ws
// → orgId = "o7", userId = "u123".
func TestWSHandler_URLParamsExtracted_MultipleDynamic(t *testing.T) {
	m := New(map[string]Route{
		"/orgs/_orgId_/users/_userId_": testRoute{body: "org user"},
	}, Options{})

	var capturedParams map[string]string
	WithWSHandlers(map[string]http.Handler{
		"/orgs/_orgId_/users/_userId_/__ws": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedParams = URLParamsFromContext(r.Context())
			w.WriteHeader(http.StatusSwitchingProtocols)
		}),
	})(m)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orgs/o7/users/u123/__ws", nil)
	req.Header.Set("Upgrade", "websocket")
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", rec.Code)
	}
	if capturedParams == nil {
		t.Fatal("URLParamsFromContext returned nil; expected map with orgId and userId")
	}
	if got := capturedParams["orgId"]; got != "o7" {
		t.Fatalf("expected orgId=o7, got %q", got)
	}
	if got := capturedParams["userId"]; got != "u123" {
		t.Fatalf("expected userId=u123, got %q", got)
	}
}
