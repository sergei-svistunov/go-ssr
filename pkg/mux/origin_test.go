package mux

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCrossSite(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		host     string
		patterns []string
		want     bool
	}{
		{"no browser headers passes", nil, "app.example", nil, false},
		{"sec-fetch-site same-origin passes even with foreign Origin", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://other.example"}, "app.example", nil, false},
		{"sec-fetch-site none passes", map[string]string{"Sec-Fetch-Site": "none"}, "app.example", nil, false},
		{"same-site with Origin rejected", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://other.app.example"}, "app.example", nil, true},
		{"same-site without Origin rejected", map[string]string{"Sec-Fetch-Site": "same-site"}, "app.example", nil, true},
		{"cross-site with Origin rejected", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, "app.example", nil, true},
		{"cross-site listed origin passes", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://localhost:3000"}, "app.example", []string{"localhost:*"}, false},
		{"cross-site unlisted origin rejected", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, "app.example", []string{"localhost:*"}, true},
		{"cross-site without Origin rejected despite patterns", map[string]string{"Sec-Fetch-Site": "cross-site"}, "app.example", []string{"*"}, true},
		{"Origin matching Host passes", map[string]string{"Origin": "https://APP.example"}, "app.example", nil, false},
		{"Origin mismatching Host rejected", map[string]string{"Origin": "https://evil.example"}, "app.example", nil, true},
		{"Origin mismatching Host but listed passes", map[string]string{"Origin": "https://dev.example"}, "app.example", []string{"dev.example"}, false},
		{"Origin null is no signal", map[string]string{"Origin": "null"}, "app.example", nil, false},
		{"malformed Origin rejected", map[string]string{"Origin": "::not a url"}, "app.example", nil, true},
		{"Origin without host rejected", map[string]string{"Origin": "https://"}, "app.example", nil, true},
		{"default port on Host ignored", map[string]string{"Origin": "https://app.example"}, "app.example:443", nil, false},
		{"default port on Origin ignored", map[string]string{"Origin": "http://app.example:80"}, "app.example", nil, false},
		{"non-default port still compared", map[string]string{"Origin": "https://app.example:8443"}, "app.example", nil, true},
		{"pattern match is case-insensitive", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://dev.example:3000"}, "app.example", []string{"Dev.Example:*"}, false},
		{"scheme pattern pins the scheme", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://dev.example"}, "app.example", []string{"https://dev.example"}, true},
		{"scheme pattern matches its scheme", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://dev.example"}, "app.example", []string{"https://dev.example"}, false},
		{"malformed pattern is ignored", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://dev.example"}, "app.example", []string{"[", "dev.example"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/contact", nil)
			r.Host = tc.host
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := CrossSite(r, tc.patterns); got != tc.want {
				t.Errorf("CrossSite() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCrossSite_SchemeAware(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		host     string
		tls      bool
		xfpHTTPS bool
		want     bool
	}{
		{"http Origin for a TLS request is cross-site", "http://app.example", "app.example", true, false, true},
		{"http Origin for an X-Forwarded-Proto https request is cross-site", "http://app.example", "app.example", false, true, true},
		{"https Origin for a TLS request passes", "https://app.example", "app.example", true, false, false},
		{"https Origin with default port on Host passes over TLS", "https://app.example", "app.example:443", true, false, false},
		{"http Origin for a plain request passes", "http://app.example", "app.example", false, false, false},
		{":80 on Host is a default only for an http Origin", "https://app.example", "app.example:80", false, false, true},
		{":443 on Host is a default only for an https Origin", "http://app.example", "app.example:443", false, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/contact", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if tc.xfpHTTPS {
				r.Header.Set("X-Forwarded-Proto", "https")
			}
			if got := CrossSite(r, nil); got != tc.want {
				t.Errorf("CrossSite() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInvalidOriginPatternIsAnError(t *testing.T) {
	m, err := New(map[string]Route{}, Options{CSRFKey: testKey, OriginPatterns: []string{"dev.example:[3000"}})
	if m != nil || err == nil || !strings.Contains(err.Error(), `"dev.example:[3000"`) {
		t.Fatalf("New = %v, %v; want nil and an error naming the malformed OriginPatterns entry", m, err)
	}
}

func TestNew_AcceptsEscapedIPv6Pattern(t *testing.T) {
	m := newMux(map[string]Route{}, Options{OriginPatterns: []string{`\[::1\]:*`}})
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	r.Host = "app.example"
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "http://[::1]:5173")
	if CrossSite(r, m.options.originPatterns) {
		t.Error("escaped IPv6 literal pattern should match its origin")
	}
}

func TestRequest_CrossSiteUsesOwnPatterns(t *testing.T) {
	r := NewRequest(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/contact", nil))
	r.Host = "app.example"
	r.Header.Set("Origin", "https://dev.example")
	if !r.CrossSite() {
		t.Error("foreign Origin with no patterns should be cross-site")
	}
	r.OriginPatterns = []string{"dev.example"}
	if r.CrossSite() {
		t.Error("listed Origin should not be cross-site")
	}
}

func TestCrossSiteStrict_RefusesOpaqueOrigin(t *testing.T) {
	// A handshake has no token behind it, and a browser always sends a real
	// Origin on one; "null" is a sandboxed iframe or a data: document.
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/live/__ws", nil)
	r.Host = "app.example"
	r.Header.Set("Origin", "null")
	if CrossSite(r, nil) {
		t.Error("lenient CrossSite should tolerate Origin: null")
	}
	if !CrossSiteStrict(r, nil) {
		t.Error("strict CrossSite should refuse Origin: null")
	}
}

func TestCrossSiteStrict_RefusesOpaqueOriginEvenIfBrowserSaysSameOrigin(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/live/__ws", nil)
	r.Host = "app.example"
	r.Header.Set("Origin", "null")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if !CrossSiteStrict(r, nil) {
		t.Error("an opaque Origin is refused whatever Sec-Fetch-Site claims")
	}
}

func TestCrossSiteStrict_MatchesCrossSiteOtherwise(t *testing.T) {
	// Absent Origin stays allowed: that is a non-browser client, which the
	// WebSocket library itself permits.
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"absent Origin", nil, false},
		{"matching Origin", map[string]string{"Origin": "https://app.example"}, false},
		{"foreign Origin", map[string]string{"Origin": "https://evil.example"}, true},
		{"same-origin per browser", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://other.example"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/live/__ws", nil)
			r.Host = "app.example"
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := CrossSiteStrict(r, nil); got != tc.want {
				t.Errorf("CrossSiteStrict() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSecureRequest(t *testing.T) {
	plain := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if SecureRequest(plain) {
		t.Error("a plain http request is not secure")
	}
	proxied := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https")
	if !SecureRequest(proxied) {
		t.Error("X-Forwarded-Proto: https means the browser used TLS")
	}
	direct := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	direct.TLS = &tls.ConnectionState{}
	if !SecureRequest(direct) {
		t.Error("a TLS connection is secure")
	}
}

func TestNew_RefusesUnescapedIPv6Pattern(t *testing.T) {
	// path.Match parses "[::1]:*" happily as a character class: it never
	// matches the intended origin and does match unintended hosts, so the
	// mistake has to be caught at startup like any other bad pattern.
	for _, p := range []string{"[::1]:*", "[::1]", "https://[::1]:*"} {
		t.Run(p, func(t *testing.T) {
			m, err := New(map[string]Route{}, Options{CSRFKey: testKey, OriginPatterns: []string{p}})
			if m != nil || err == nil {
				t.Fatalf("New = %v, %v; want nil and an error for %q", m, err, p)
			}
			if !strings.Contains(err.Error(), `\[`) {
				t.Errorf("error %v should show the escaped form", err)
			}
		})
	}
}

func TestNew_AcceptsOrdinaryPatterns(t *testing.T) {
	for _, p := range []string{"dev.example", "dev.example:*", "https://dev.example", `\[::1\]:*`, "*.dev.example"} {
		if _, err := New(map[string]Route{}, Options{CSRFKey: testKey, OriginPatterns: []string{p}}); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
}

func TestSafeMethod(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		if !SafeMethod(m) {
			t.Errorf("%s should be safe", m)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, "FOO"} {
		if SafeMethod(m) {
			t.Errorf("%s should not be safe", m)
		}
	}
}

func TestSecureRequest_ForwardedProtoList(t *testing.T) {
	// Chained proxies append: "https, http".
	for _, v := range []string{"https", "https, http", " HTTPS ,http", "https,http"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("X-Forwarded-Proto", v)
		if !SecureRequest(r) {
			t.Errorf("X-Forwarded-Proto %q should count as secure", v)
		}
	}
	for _, v := range []string{"http", "http, https", ""} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("X-Forwarded-Proto", v)
		if SecureRequest(r) {
			t.Errorf("X-Forwarded-Proto %q should not count as secure", v)
		}
	}
}

func TestCrossSite_ListedHttpOriginIsHonouredOnATLSRequest(t *testing.T) {
	// The scheme guard must not pre-empt an origin the operator listed on
	// purpose, or the Sec-Fetch-Site path and the fallback disagree.
	patterns := []string{"http://dev.example:3000"}
	for _, headers := range []map[string]string{
		{"Origin": "http://dev.example:3000"},                                 // no Fetch Metadata
		{"Origin": "http://dev.example:3000", "Sec-Fetch-Site": "cross-site"}, // with it
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/contact", nil)
		r.Host = "app.example"
		r.TLS = &tls.ConnectionState{}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if CrossSite(r, patterns) {
			t.Errorf("listed http origin refused on a TLS request (headers %v)", headers)
		}
	}
}

func TestCrossSite_UnlistedHttpOriginStillRefusedOnATLSRequest(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/contact", nil)
	r.Host = "app.example"
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Origin", "http://app.example")
	if !CrossSite(r, nil) {
		t.Error("an http origin for a TLS app must still be refused when nothing lists it")
	}
}
