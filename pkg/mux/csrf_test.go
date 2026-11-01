package mux

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// through runs req through a mux built with opts and returns the *Request
// the route at req's path saw. The route's Guard stops the request, so the
// mux's own form check leaves the body to the test.
func through(t *testing.T, opts Options, req *http.Request) *Request {
	t.Helper()
	var got *Request
	m := newMux(map[string]Route{req.URL.Path: hookRoute{guard: func(r *Request) error {
		got = r
		return NewHttpError(http.StatusTeapot, "captured")
	}}}, opts)
	req.Host = "app.example"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	m.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("route was not reached")
	}
	return got
}

// withMuxOptions returns req as a Request carrying the options a mux built
// with opts attaches, for a method the mux answers before any hook.
func withMuxOptions(opts Options, req *http.Request) *Request {
	o := newMux(nil, opts).options
	o.noStore = new(atomic.Bool)
	o.csrfSession = &arrivalSession{}
	return NewRequest(withOptions(req, o))
}

// mintCSRF renders a GET through CSRFToken and returns the cookie it set
// and the token handed to the form.
func mintCSRF(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	return mintCSRFWith(t, false)
}

// mintCSRFWith is mintCSRF through a mux with the given InsecureCookies
// option.
func mintCSRFWith(t *testing.T, insecure bool) (*http.Cookie, string) {
	t.Helper()
	r := through(t, Options{InsecureCookies: insecure}, httptest.NewRequest(http.MethodGet, "/contact", nil))
	w := httptest.NewRecorder()
	token, err := r.CSRFToken(w)
	if err != nil {
		t.Fatalf("CSRFToken: %v", err)
	}
	setCookie := w.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("no Set-Cookie header on GET")
	}
	c, err := http.ParseSetCookie(setCookie)
	if err != nil {
		t.Fatalf("ParseSetCookie(%q): %v", setCookie, err)
	}
	return c, token
}

// postForm builds a urlencoded POST carrying the given cookie (nil for none)
// and _csrf_token_ field (empty string for none).
func postForm(t *testing.T, cookie *http.Cookie, field string) *Request {
	t.Helper()
	vals := url.Values{"name": {"x"}}
	if field != "" {
		vals.Set(CSRFTokenName, field)
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	return through(t, Options{}, req)
}

func wantHttpError(t *testing.T, err error, code int) {
	t.Helper()
	var he *HttpError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HttpError %d, got %v", code, err)
	}
	if he.Code != code {
		t.Fatalf("expected %d, got %d: %s", code, he.Code, he.Message)
	}
}

func TestNewRequiresCSRFKey(t *testing.T) {
	for _, tc := range []struct {
		key  []byte
		want string
	}{
		{nil, "mux: CSRFKey is empty"},
		{[]byte{}, "mux: CSRFKey is empty"},
		{bytes.Repeat([]byte("k"), 31), "mux: CSRFKey must be at least 32 bytes"},
	} {
		m, err := New(map[string]Route{}, Options{CSRFKey: tc.key})
		if m != nil || err == nil || err.Error() != tc.want {
			t.Errorf("%d-byte key: New = %v, %v; want nil, %q", len(tc.key), m, err, tc.want)
		}
	}
	if m, err := New(map[string]Route{}, Options{CSRFKey: bytes.Repeat([]byte("k"), 32)}); m == nil || err != nil {
		t.Errorf("32-byte key: New = %v, %v; want a mux", m, err)
	}
}

func TestCSRFWithoutMuxFails(t *testing.T) {
	r := NewRequest(httptest.NewRequest(http.MethodGet, "/contact", nil))
	w := httptest.NewRecorder()
	if _, err := r.CSRFToken(w); err == nil || err.Error() != "mux: no CSRF key" {
		t.Errorf("CSRFToken without a mux: err = %v, want mux: no CSRF key", err)
	}
	if sc := w.Header().Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("CSRFToken without a mux set cookies: %v", sc)
	}
}

func TestReadFormWithoutMuxRefusesEveryToken(t *testing.T) {
	// Without a key the HMAC is public: anyone could sign a token.
	raw := make([]byte, csrfTokenLen)
	binary.BigEndian.PutUint64(raw[csrfNonceLen:], uint64(time.Now().Unix()))
	copy(raw[csrfNonceLen+csrfTimeLen:], csrfMAC(nil, raw[:csrfNonceLen+csrfTimeLen], ""))
	forged := base64.RawURLEncoding.EncodeToString(raw)

	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(CSRFTokenName+"=contact:"+forged))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: forged})
	_, err := readForm(NewRequest(req))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestCSRFToken_CookieIsHostPrefixedAndHardened(t *testing.T) {
	c, token := mintCSRF(t)
	if c.Name != "__Host-_csrf_token_" {
		t.Errorf("cookie name = %q, want __Host-_csrf_token_", c.Name)
	}
	if c.Value != token {
		t.Errorf("cookie value %q != form token %q", c.Value, token)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want none", c.Domain)
	}
	if !c.Secure {
		t.Error("cookie is not Secure")
	}
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.MaxAge <= 0 {
		t.Errorf("MaxAge = %d, want positive", c.MaxAge)
	}
}

func TestCSRFToken_InsecureCookiesDropsHostPrefixAndSecure(t *testing.T) {
	c, _ := mintCSRFWith(t, true)
	if c.Name != "_csrf_token_" {
		t.Errorf("cookie name = %q, want _csrf_token_", c.Name)
	}
	if c.Secure {
		t.Error("insecure cookie must not be Secure")
	}
	if c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("other attributes must stay hardened: %s", c.String())
	}
}

func TestCSRFToken_MarksResponseNoStore(t *testing.T) {
	r := through(t, Options{}, httptest.NewRequest(http.MethodGet, "/contact", nil))
	w := httptest.NewRecorder()
	if _, err := r.CSRFToken(w); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestCSRFToken_OverridesEarlierCacheControl(t *testing.T) {
	// A page carrying a token is never cacheable, whatever ran before.
	r := through(t, Options{}, httptest.NewRequest(http.MethodGet, "/contact", nil))
	w := httptest.NewRecorder()
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := r.CSRFToken(w); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestCSRFToken_PostWithoutAValidCookieMints(t *testing.T) {
	// Rendering must not fail: readForm is what rejects a bad submission,
	// and by the time a render happens the session may have rotated under it.
	r := postForm(t, nil, "contact:abc")
	w := httptest.NewRecorder()
	token, err := r.CSRFToken(w)
	if err != nil {
		t.Fatalf("rendering after a POST should mint, not fail: %v", err)
	}
	if !validToken(r, token) || w.Header().Get("Set-Cookie") == "" {
		t.Error("the minted token must be valid and issued as a cookie")
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	// The configuration hint is one-per-process; rearm it so each test sees
	// its own first occurrence.
	insecureCookieHint = sync.Once{}
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestReadForm_MissingCookieOverPlainHttpIsExplainedInLog(t *testing.T) {
	// The most likely reason for a missing cookie on plain http is the
	// browser refusing the Secure cookie. The operator's log says how to fix
	// it; the public 400 body does not advertise a dev-only option.
	logs := captureLog(t)
	_, err := readForm(postForm(t, nil, "contact:abc"))
	wantHttpError(t, err, http.StatusBadRequest)
	if strings.Contains(err.Error(), "InsecureCookies") {
		t.Errorf("response %q must not carry the configuration hint", err)
	}
	for _, want := range []string{"InsecureCookies", "Safari", "/contact"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q should mention %q", logs.String(), want)
		}
	}
}

func TestReadForm_MissingCookieHintNotLoggedWhenNotApplicable(t *testing.T) {
	logs := captureLog(t)
	r := postForm(t, nil, "contact:abc")
	r.InsecureCookies = true
	_, _ = readForm(r)
	if logs.Len() != 0 {
		t.Errorf("option already on, nothing to hint; got log %q", logs.String())
	}

	r = postForm(t, nil, "contact:abc")
	r.TLS = &tls.ConnectionState{}
	_, _ = readForm(r)
	if logs.Len() != 0 {
		t.Errorf("over TLS, nothing to hint; got log %q", logs.String())
	}
}

func TestReadForm_RoundTrip(t *testing.T) {
	c, token := mintCSRF(t)
	name, err := readForm(postForm(t, c, "contact:"+token))
	if err != nil {
		t.Fatalf("readForm: %v", err)
	}
	if name != "contact" {
		t.Errorf("form name = %q, want contact", name)
	}
}

func TestReadForm_InsecureCookiesRoundTrip(t *testing.T) {
	c, token := mintCSRFWith(t, true)
	r := postForm(t, c, "contact:"+token)
	r.InsecureCookies = true
	name, err := readForm(r)
	if err != nil {
		t.Fatalf("readForm: %v", err)
	}
	if name != "contact" {
		t.Errorf("form name = %q, want contact", name)
	}
}

func TestReadForm_InsecureCookieNotAcceptedWhenOptionOff(t *testing.T) {
	c, token := mintCSRFWith(t, true)
	_, err := readForm(postForm(t, c, "contact:"+token))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestReadForm_RejectsMismatchedToken(t *testing.T) {
	c, _ := mintCSRF(t)
	_, other := mintCSRF(t)
	_, err := readForm(postForm(t, c, "contact:"+other))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestReadForm_RejectsMissingCookie(t *testing.T) {
	_, token := mintCSRF(t)
	_, err := readForm(postForm(t, nil, "contact:"+token))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestReadForm_RejectsMissingField(t *testing.T) {
	c, _ := mintCSRF(t)
	_, err := readForm(postForm(t, c, ""))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestReadForm_RejectsEmptyTokenHalf(t *testing.T) {
	c, _ := mintCSRF(t)
	c.Value = ""
	_, err := readForm(postForm(t, c, "contact:"))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestReadForm_RejectsFieldWithoutFormName(t *testing.T) {
	c, token := mintCSRF(t)
	_, err := readForm(postForm(t, c, token))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestReadForm_MissingCookieHintNotLoggedBehindTLSProxy(t *testing.T) {
	logs := captureLog(t)
	r := postForm(t, nil, "contact:abc")
	r.Header.Set("X-Forwarded-Proto", "https, http")
	_, err := readForm(r)
	wantHttpError(t, err, http.StatusBadRequest)
	if logs.Len() != 0 {
		t.Errorf("behind a TLS-terminating proxy, nothing to hint; got log %q", logs.String())
	}
}

func TestReadForm_AcceptsAnyCookieOfThatName(t *testing.T) {
	// A pre-upgrade default-path cookie is sent before the Path=/ one and
	// r.Cookie would return it; the valid one must still be found.
	c, token := mintCSRFWith(t, true)
	stale := &http.Cookie{Name: c.Name, Value: "stale-value-from-before-the-upgrade"}
	r := postForm(t, stale, "contact:"+token)
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	r.InsecureCookies = true
	if _, err := readForm(r); err != nil {
		t.Fatalf("valid cookie shadowed by a stale one of the same name: %v", err)
	}
}

func TestReadForm_MissingCookieRejectedBeforeBodyIsParsed(t *testing.T) {
	// A multipart body without a boundary cannot be parsed; the cookie check
	// must come first so a cookie-less POST never buffers the body.
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data")
	_, err := readForm(through(t, Options{}, req))
	wantHttpError(t, err, http.StatusBadRequest)
	if !strings.Contains(err.Error(), "Missed CSRF token") {
		t.Errorf("expected the missing-cookie error, got %q", err)
	}
}

func TestCSRFToken_ReusesTokenAlreadyIssuedInResponse(t *testing.T) {
	// A layout and a leaf that both render a form share one response; the
	// second call must hand out the token the first one set, not a new one.
	r := through(t, Options{}, httptest.NewRequest(http.MethodGet, "/users/add", nil))
	w := httptest.NewRecorder()
	leaf, err := r.CSRFToken(w)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := r.CSRFToken(w)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != layout {
		t.Errorf("leaf token %q != layout token %q", leaf, layout)
	}
	if n := len(w.Header().Values("Set-Cookie")); n != 1 {
		t.Errorf("expected one Set-Cookie, got %d: %v", n, w.Header().Values("Set-Cookie"))
	}
}

func TestCSRFToken_ReusesValidCookieAcrossRenders(t *testing.T) {
	// Opening a second form page must not invalidate a form already open in
	// another tab: a valid incoming cookie is kept and only refreshed.
	c, token := mintCSRF(t)
	req := httptest.NewRequest(http.MethodGet, "/users/add", nil)
	req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	r := through(t, Options{}, req)
	w := httptest.NewRecorder()
	got, err := r.CSRFToken(w)
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Errorf("second render handed out %q, want the existing token %q", got, token)
	}
	// The cookie is re-issued so an active user does not fall off its edge.
	// That cannot extend the token: its lifetime runs from the issued-at it
	// carries, which TestSign_ExpiredTokenIsRejectedAndReplaced pins.
	sc, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatalf("the cookie should be re-issued: %v", err)
	}
	if sc.Value != token || sc.MaxAge <= 0 {
		t.Errorf("re-issued cookie should carry the same token and a fresh Max-Age, got %s", sc)
	}
}

func TestCSRFToken_MintsWhenIncomingCookieIsMalformed(t *testing.T) {
	for _, bad := range []string{"short", strings.Repeat("x", 44), "not base64 at all, but forty-four bytes!"} {
		req := httptest.NewRequest(http.MethodGet, "/contact", nil)
		req.AddCookie(&http.Cookie{Name: "__Host-" + CSRFTokenName, Value: bad})
		r := through(t, Options{}, req)
		token, err := r.CSRFToken(httptest.NewRecorder())
		if err != nil {
			t.Fatal(err)
		}
		if token == bad {
			t.Errorf("malformed cookie %q was reused instead of replaced", bad)
		}
		if !validToken(r, token) {
			t.Errorf("replacement token %q is not a valid signed token", token)
		}
	}
}

func TestReadForm_FormOpenedBeforeAnotherRenderStillSubmits(t *testing.T) {
	// Tab A renders /contact, tab B renders /users/add, tab A submits.
	cookieA, tokenA := mintCSRF(t)
	reqB := httptest.NewRequest(http.MethodGet, "/users/add", nil)
	reqB.AddCookie(&http.Cookie{Name: cookieA.Name, Value: cookieA.Value})
	rB := through(t, Options{}, reqB)
	wB := httptest.NewRecorder()
	if _, err := rB.CSRFToken(wB); err != nil {
		t.Fatal(err)
	}
	// Tab B may re-issue the cookie, but must not change its value.
	if sc, err := http.ParseSetCookie(wB.Header().Get("Set-Cookie")); err == nil && sc.Value != cookieA.Value {
		t.Errorf("tab B replaced the token: %q -> %q", cookieA.Value, sc.Value)
	}
	if _, err := readForm(postForm(t, cookieA, "contact:"+tokenA)); err != nil {
		t.Fatalf("tab A's form failed after tab B rendered: %v", err)
	}
}

func TestCSRFToken_OnPostPrefersTheValidCookie(t *testing.T) {
	// A stale default-path cookie is sent first; the re-rendered form must
	// carry the valid token, not the stale value.
	c, token := mintCSRF(t)
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: c.Name, Value: "stale-value-from-before-the-upgrade"})
	req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	r := through(t, Options{}, req)

	got, err := r.CSRFToken(httptest.NewRecorder())
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Errorf("re-rendered form carries %q, want the valid token %q", got, token)
	}
}

func TestReadForm_OnPostRejectsWhenNoCookieIsValid(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(
		CSRFTokenName+"=contact:not-a-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "__Host-" + CSRFTokenName, Value: "not-a-token"})
	_, err := readForm(through(t, Options{}, req))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestCSRFToken_EmptyCookieValueIsReplaced(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/contact", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-" + CSRFTokenName, Value: ""})
	r := through(t, Options{}, req)
	w := httptest.NewRecorder()
	leaf, err := r.CSRFToken(w)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := r.CSRFToken(w)
	if err != nil {
		t.Fatal(err)
	}
	if leaf == "" || leaf != layout {
		t.Errorf("empty cookie should be replaced once and shared: leaf %q layout %q", leaf, layout)
	}
	if n := len(w.Header().Values("Set-Cookie")); n != 1 {
		t.Errorf("expected one Set-Cookie, got %d", n)
	}
}

func TestCSRFToken_UnsafeMethodWithNoValidCookieMints(t *testing.T) {
	// Nothing valid is being displaced here — any other tab's token is dead
	// too — and the render needs a token the next submit can use.
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		r := withMuxOptions(Options{}, httptest.NewRequest(method, "/contact", nil))
		w := httptest.NewRecorder()
		token, err := r.CSRFToken(w)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if !validToken(r, token) || w.Header().Get("Set-Cookie") == "" {
			t.Errorf("%s should mint and issue a token", method)
		}
	}
}

func TestCSRFToken_UnsafeMethodWithValidCookieReuses(t *testing.T) {
	c, token := mintCSRF(t)
	req := httptest.NewRequest(http.MethodPut, "/contact", nil)
	req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	got, err := withMuxOptions(Options{}, req).CSRFToken(httptest.NewRecorder())
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Errorf("PUT handed out %q, want the existing token", got)
	}
}

func TestMissingCookie_ConfigurationHintIsLoggedOnce(t *testing.T) {
	logs := captureLog(t)
	for i := 0; i < 5; i++ {
		_, _ = readForm(postForm(t, nil, "contact:abc"))
	}
	if n := strings.Count(logs.String(), "InsecureCookies"); n > 1 {
		t.Errorf("configuration hint logged %d times; it repeats verbatim and should be logged once", n)
	}
}
