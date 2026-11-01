package mux

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testCSP is the default policy for a plain-http request to example.test.
const testCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
	"connect-src 'self' ws://example.test; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

func wantSecurityHeaders(t *testing.T, h http.Header, csp string) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy":      csp,
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "same-origin",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for name, value := range want {
		if got := h.Values(name); len(got) != 1 || got[0] != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func hostRequest(t *testing.T, method, path, host string) *http.Request {
	r := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	r.Host = host
	return r
}

func TestSecurityHeaders(t *testing.T) {
	hooked := http.Header{
		"Content-Security-Policy":      {"script-src *"},
		"X-Content-Type-Options":       {"hook"},
		"Referrer-Policy":              {"unsafe-url"},
		"Cross-Origin-Opener-Policy":   {"unsafe-none"},
		"Cross-Origin-Resource-Policy": {"cross-origin"},
	}
	m, _ := site(t, func(r map[string]*fakeRoute) {
		r["/home"].header = hooked
		r["/notes"].header = hooked
		r["/notes"].fail = map[string]error{"data": NewHttpError(http.StatusForbidden, "Forbidden")}
	})
	handshake := hostRequest(t, http.MethodGet, "/home/__ws", "example.test")
	handshake.Header.Set("Connection", "Upgrade")
	handshake.Header.Set("Upgrade", "websocket")
	handshake.Header.Set("Origin", "https://evil.test")

	for name, tc := range map[string]struct {
		req    *http.Request
		status int
	}{
		"page":              {hostRequest(t, http.MethodGet, "/home", "example.test"), http.StatusOK},
		"error":             {hostRequest(t, http.MethodGet, "/missing", "example.test"), http.StatusNotFound},
		"error after hook":  {hostRequest(t, http.MethodGet, "/notes", "example.test"), http.StatusForbidden},
		"redirect":          {hostRequest(t, http.MethodGet, "/", "example.test"), http.StatusFound},
		"refused handshake": {handshake, http.StatusForbidden},
	} {
		for _, secure := range []bool{false, true} {
			req := tc.req.Clone(tc.req.Context())
			csp := testCSP
			if secure {
				req.Header.Set("X-Forwarded-Proto", "https")
				csp = strings.Replace(csp, "ws://", "wss://", 1)
			}
			w := serve(m, req)
			if w.Code != tc.status {
				t.Fatalf("%s: status %d, want %d", name, w.Code, tc.status)
			}
			wantSecurityHeaders(t, w.Header(), csp)
		}
	}
}

func TestCSPHostIsNotEchoed(t *testing.T) {
	m, _ := site(t, nil)
	bare := strings.Replace(testCSP, " ws://example.test", "", 1)
	for _, host := range []string{"evil.test; script-src *", "a b", "evil.test,x", `"x"`, "[abc]"} {
		csp := serve(m, hostRequest(t, http.MethodGet, "/home", host)).Header().Get("Content-Security-Policy")
		if csp != bare {
			t.Errorf("Host %q: CSP %q, want %q", host, csp, bare)
		}
		if n := len(strings.Split(csp, "; ")); n != 10 {
			t.Errorf("Host %q: %d directives, want 10", host, n)
		}
	}

	csp := serve(m, hostRequest(t, http.MethodGet, "/home", "[::1]:8080")).Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "; connect-src 'self' ws://[::1]:8080; ") {
		t.Errorf("Host [::1]:8080: CSP %q", csp)
	}
}

func TestCSPSources(t *testing.T) {
	m := newMux(map[string]Route{"/": &fakeRoute{name: "root", log: &[]string{}}}, Options{CSPSources: map[string][]string{
		"img-src":     {"https://img.example", "https://*.cdn.example", "img.example:*"},
		"connect-src": {"https://api.example"},
		"media-src":   {"blob:"},
		"worker-src":  {"'self'"},
	}})
	csp := serve(m, hostRequest(t, http.MethodGet, "/", "example.test")).Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"; img-src 'self' data: https://img.example https://*.cdn.example img.example:*; ",
		"; connect-src 'self' ws://example.test https://api.example; ",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q does not contain %q", csp, want)
		}
	}
	if !strings.HasSuffix(csp, "; form-action 'self'; media-src blob:; worker-src 'self'") {
		t.Errorf("CSP %q does not end with media-src and worker-src after form-action", csp)
	}

	for _, tc := range []struct{ directive, source, named string }{
		{"object-src", "https://a.example", "object-src"},
		{"frame-ancestors", "https://a.example", "frame-ancestors"},
		{"script-src", "'unsafe-inline'", "'unsafe-inline'"},
		{"script-src", "*", "*"},
		{"img-src", "a;b", "a;b"},
		{"img-src", "a,b", "a,b"},
		{"img-src", "a b", "a b"},
		{"img-src", "a\x00b", `a\x00b`},
		{"img-src", "", `""`},
		{"script-src", "'UNSAFE-EVAL'", "'UNSAFE-EVAL'"},
		{"style-src", "'unsafe-hashes'", "'unsafe-hashes'"},
		{"script-src", "'wasm-unsafe-eval'", "'wasm-unsafe-eval'"},
		{"script-src", "'strict-dynamic'", "'strict-dynamic'"},
		{"default-src", "'Strict-Dynamic'", "'Strict-Dynamic'"},
		{"img-src", "*", "*"},
		{"connect-src", "https://*", "https://*"},
		{"img-src", "*:443", "*:443"},
		{"frame-src", "wss://*/path", "wss://*/path"},
		{"script-src", "http:", "http:"},
		{"script-src", "HTTPS:", "HTTPS:"},
		{"script-src", "data:", "data:"},
		{"script-src", "blob:", "blob:"},
		{"script-src", "filesystem:", "filesystem:"},
		{"script-src-elem", "https://a.example", "script-src-elem"},
	} {
		_, err := New(nil, Options{CSRFKey: testKey, CSPSources: map[string][]string{tc.directive: {tc.source}}})
		if err == nil || !strings.Contains(err.Error(), tc.directive) || !strings.Contains(err.Error(), tc.named) {
			t.Errorf("New with %s %q: error %v, want one naming %q and %q", tc.directive, tc.source, err, tc.directive, tc.named)
		}
	}
}
