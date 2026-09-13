package form

import (
	"bytes"
	"crypto/tls"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// mintCSRF renders a GET through SetCSRFToken and returns the cookie it set
// and the token handed to the form.
func mintCSRF(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	return mintCSRFWith(t, false)
}

// mintCSRFWith is mintCSRF on a request the mux flagged with the given
// InsecureCookies option.
func mintCSRFWith(t *testing.T, insecure bool) (*http.Cookie, string) {
	t.Helper()
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/contact", nil))
	r.InsecureCookies = insecure
	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(r, w, &f); err != nil {
		t.Fatalf("SetCSRFToken: %v", err)
	}
	setCookie := w.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("no Set-Cookie header on GET")
	}
	c, err := http.ParseSetCookie(setCookie)
	if err != nil {
		t.Fatalf("ParseSetCookie(%q): %v", setCookie, err)
	}
	return c, f.GetCSRFToken()
}

// postForm builds a urlencoded POST carrying the given cookie (nil for none)
// and _csrf_token_ field (empty string for none).
func postForm(cookie *http.Cookie, field string) *mux.Request {
	vals := url.Values{"name": {"x"}}
	if field != "" {
		vals.Set(CSRFTokenName, field)
	}
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	return mux.NewRequest(req)
}

func wantHttpError(t *testing.T, err error, code int) {
	t.Helper()
	var he *mux.HttpError
	if !errors.As(err, &he) {
		t.Fatalf("expected *mux.HttpError %d, got %v", code, err)
	}
	if he.Code != code {
		t.Fatalf("expected %d, got %d: %s", code, he.Code, he.Message)
	}
}

func TestSetCSRFToken_CookieIsHostPrefixedAndHardened(t *testing.T) {
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

func TestSetCSRFToken_InsecureCookiesDropsHostPrefixAndSecure(t *testing.T) {
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

func TestSetCSRFToken_MarksResponseNoStore(t *testing.T) {
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/contact", nil))
	w := httptest.NewRecorder()
	if err := SetCSRFToken(r, w); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestSetCSRFToken_OverridesEarlierCacheControl(t *testing.T) {
	// A page carrying a token is never cacheable, whatever ran before.
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/contact", nil))
	w := httptest.NewRecorder()
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := SetCSRFToken(r, w); err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestSetCSRFToken_PostWithoutAValidCookieMints(t *testing.T) {
	// Rendering must not fail: Parse is what rejects a bad submission, and
	// by the time a render happens the session may have rotated under it.
	r := postForm(nil, "contact:abc")
	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(r, w, &f); err != nil {
		t.Fatalf("rendering after a POST should mint, not fail: %v", err)
	}
	if !validToken(r, f.GetCSRFToken()) || w.Header().Get("Set-Cookie") == "" {
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

func TestParse_MissingCookieOverPlainHttpIsExplainedInLog(t *testing.T) {
	// The most likely reason for a missing cookie on plain http is the
	// browser refusing the Secure cookie. The operator's log says how to fix
	// it; the public 400 body does not advertise a dev-only option.
	logs := captureLog(t)
	_, err := Parse(postForm(nil, "contact:abc"))
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

func TestParse_MissingCookieHintNotLoggedWhenNotApplicable(t *testing.T) {
	logs := captureLog(t)
	r := postForm(nil, "contact:abc")
	r.InsecureCookies = true
	_, _ = Parse(r)
	if logs.Len() != 0 {
		t.Errorf("option already on, nothing to hint; got log %q", logs.String())
	}

	r = postForm(nil, "contact:abc")
	r.TLS = &tls.ConnectionState{}
	_, _ = Parse(r)
	if logs.Len() != 0 {
		t.Errorf("over TLS, nothing to hint; got log %q", logs.String())
	}
}

func TestParse_RoundTrip(t *testing.T) {
	c, token := mintCSRF(t)
	name, err := Parse(postForm(c, "contact:"+token))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if name != "contact" {
		t.Errorf("form name = %q, want contact", name)
	}
}

func TestParse_InsecureCookiesRoundTrip(t *testing.T) {
	c, token := mintCSRFWith(t, true)
	r := postForm(c, "contact:"+token)
	r.InsecureCookies = true
	name, err := Parse(r)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if name != "contact" {
		t.Errorf("form name = %q, want contact", name)
	}
}

func TestParse_InsecureCookieNotAcceptedWhenOptionOff(t *testing.T) {
	c, token := mintCSRFWith(t, true)
	_, err := Parse(postForm(c, "contact:"+token))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestParse_RejectsMismatchedToken(t *testing.T) {
	c, _ := mintCSRF(t)
	_, other := mintCSRF(t)
	_, err := Parse(postForm(c, "contact:"+other))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestParse_RejectsMissingCookie(t *testing.T) {
	_, token := mintCSRF(t)
	_, err := Parse(postForm(nil, "contact:"+token))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestParse_RejectsMissingField(t *testing.T) {
	c, _ := mintCSRF(t)
	_, err := Parse(postForm(c, ""))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestParse_RejectsEmptyTokenHalf(t *testing.T) {
	c, _ := mintCSRF(t)
	c.Value = ""
	_, err := Parse(postForm(c, "contact:"))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestParse_RejectsFieldWithoutFormName(t *testing.T) {
	c, token := mintCSRF(t)
	_, err := Parse(postForm(c, token))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestParse_MissingCookieHintNotLoggedBehindTLSProxy(t *testing.T) {
	logs := captureLog(t)
	r := postForm(nil, "contact:abc")
	r.Header.Set("X-Forwarded-Proto", "https, http")
	_, err := Parse(r)
	wantHttpError(t, err, http.StatusBadRequest)
	if logs.Len() != 0 {
		t.Errorf("behind a TLS-terminating proxy, nothing to hint; got log %q", logs.String())
	}
}

func TestParse_AcceptsAnyCookieOfThatName(t *testing.T) {
	// A pre-upgrade default-path cookie is sent before the Path=/ one and
	// r.Cookie would return it; the valid one must still be found.
	c, token := mintCSRFWith(t, true)
	stale := &http.Cookie{Name: c.Name, Value: "stale-value-from-before-the-upgrade"}
	r := postForm(stale, "contact:"+token)
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	r.InsecureCookies = true
	if _, err := Parse(r); err != nil {
		t.Fatalf("valid cookie shadowed by a stale one of the same name: %v", err)
	}
}

func TestParse_MissingCookieRejectedBeforeBodyIsParsed(t *testing.T) {
	// A multipart body without a boundary cannot be parsed; the cookie check
	// must come first so a cookie-less POST never buffers the body.
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data")
	_, err := Parse(mux.NewRequest(req))
	wantHttpError(t, err, http.StatusBadRequest)
	if !strings.Contains(err.Error(), "Missed CSRF token") {
		t.Errorf("expected the missing-cookie error, got %q", err)
	}
}

func TestSetCSRFToken_ReusesTokenAlreadyIssuedInResponse(t *testing.T) {
	// A layout and a leaf that both render a form share one response; the
	// second call must hand out the token the first one set, not a new one.
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/users/add", nil))
	w := httptest.NewRecorder()
	var leaf, layout BaseFormValues
	if err := SetCSRFToken(r, w, &leaf); err != nil {
		t.Fatal(err)
	}
	if err := SetCSRFToken(r, w, &layout); err != nil {
		t.Fatal(err)
	}
	if leaf.GetCSRFToken() != layout.GetCSRFToken() {
		t.Errorf("leaf token %q != layout token %q", leaf.GetCSRFToken(), layout.GetCSRFToken())
	}
	if n := len(w.Header().Values("Set-Cookie")); n != 1 {
		t.Errorf("expected one Set-Cookie, got %d: %v", n, w.Header().Values("Set-Cookie"))
	}
}

func TestSetCSRFToken_ReusesValidCookieAcrossRenders(t *testing.T) {
	// Opening a second form page must not invalidate a form already open in
	// another tab: a valid incoming cookie is kept and only refreshed.
	c, token := mintCSRF(t)
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/users/add", nil))
	r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(r, w, &f); err != nil {
		t.Fatal(err)
	}
	if f.GetCSRFToken() != token {
		t.Errorf("second render handed out %q, want the existing token %q", f.GetCSRFToken(), token)
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

func TestSetCSRFToken_MintsWhenIncomingCookieIsMalformed(t *testing.T) {
	for _, bad := range []string{"short", strings.Repeat("x", 44), "not base64 at all, but forty-four bytes!"} {
		r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/contact", nil))
		r.AddCookie(&http.Cookie{Name: "__Host-" + CSRFTokenName, Value: bad})
		w := httptest.NewRecorder()
		var f BaseFormValues
		if err := SetCSRFToken(r, w, &f); err != nil {
			t.Fatal(err)
		}
		if f.GetCSRFToken() == bad {
			t.Errorf("malformed cookie %q was reused instead of replaced", bad)
		}
		if !validToken(r, f.GetCSRFToken()) {
			t.Errorf("replacement token %q is not a valid signed token", f.GetCSRFToken())
		}
	}
}

func TestParse_FormOpenedBeforeAnotherRenderStillSubmits(t *testing.T) {
	// Tab A renders /contact, tab B renders /users/add, tab A submits.
	cookieA, tokenA := mintCSRF(t)
	rB := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/users/add", nil))
	rB.AddCookie(&http.Cookie{Name: cookieA.Name, Value: cookieA.Value})
	wB := httptest.NewRecorder()
	if err := SetCSRFToken(rB, wB); err != nil {
		t.Fatal(err)
	}
	// Tab B may re-issue the cookie, but must not change its value.
	if sc, err := http.ParseSetCookie(wB.Header().Get("Set-Cookie")); err == nil && sc.Value != cookieA.Value {
		t.Errorf("tab B replaced the token: %q -> %q", cookieA.Value, sc.Value)
	}
	if _, err := Parse(postForm(cookieA, "contact:"+tokenA)); err != nil {
		t.Fatalf("tab A's form failed after tab B rendered: %v", err)
	}
}

func TestSetCSRFToken_OnPostPrefersTheValidCookie(t *testing.T) {
	// A stale default-path cookie is sent first; the re-rendered form must
	// carry the valid token, not the stale value.
	c, token := mintCSRF(t)
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: c.Name, Value: "stale-value-from-before-the-upgrade"})
	req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	r := mux.NewRequest(req)

	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(r, w, &f); err != nil {
		t.Fatal(err)
	}
	if f.GetCSRFToken() != token {
		t.Errorf("re-rendered form carries %q, want the valid token %q", f.GetCSRFToken(), token)
	}
}

func TestParse_OnPostRejectsWhenNoCookieIsValid(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(
		CSRFTokenName+"=contact:not-a-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "__Host-" + CSRFTokenName, Value: "not-a-token"})
	_, err := Parse(mux.NewRequest(req))
	wantHttpError(t, err, http.StatusBadRequest)
}

func TestSetCSRFToken_EmptyCookieValueIsReplaced(t *testing.T) {
	r := mux.NewRequest(httptest.NewRequest(http.MethodGet, "/contact", nil))
	r.AddCookie(&http.Cookie{Name: "__Host-" + CSRFTokenName, Value: ""})
	w := httptest.NewRecorder()
	var leaf, layout BaseFormValues
	if err := SetCSRFToken(r, w, &leaf); err != nil {
		t.Fatal(err)
	}
	if err := SetCSRFToken(r, w, &layout); err != nil {
		t.Fatal(err)
	}
	if leaf.GetCSRFToken() == "" || leaf.GetCSRFToken() != layout.GetCSRFToken() {
		t.Errorf("empty cookie should be replaced once and shared: leaf %q layout %q", leaf.GetCSRFToken(), layout.GetCSRFToken())
	}
	if n := len(w.Header().Values("Set-Cookie")); n != 1 {
		t.Errorf("expected one Set-Cookie, got %d", n)
	}
}

func TestSetCSRFToken_UnsafeMethodWithNoValidCookieMints(t *testing.T) {
	// Nothing valid is being displaced here — any other tab's token is dead
	// too — and the render needs a token the next submit can use.
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/contact", nil)
		r := mux.NewRequest(req)
		w := httptest.NewRecorder()
		var f BaseFormValues
		if err := SetCSRFToken(r, w, &f); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if !validToken(r, f.GetCSRFToken()) || w.Header().Get("Set-Cookie") == "" {
			t.Errorf("%s should mint and issue a token", method)
		}
	}
}

func TestSetCSRFToken_UnsafeMethodWithValidCookieReuses(t *testing.T) {
	c, token := mintCSRF(t)
	req := httptest.NewRequest(http.MethodPut, "/contact", nil)
	req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	var f BaseFormValues
	if err := SetCSRFToken(mux.NewRequest(req), httptest.NewRecorder(), &f); err != nil {
		t.Fatal(err)
	}
	if f.GetCSRFToken() != token {
		t.Errorf("PUT handed out %q, want the existing token", f.GetCSRFToken())
	}
}

func TestMissingCookie_ConfigurationHintIsLoggedOnce(t *testing.T) {
	logs := captureLog(t)
	for i := 0; i < 5; i++ {
		_, _ = Parse(postForm(nil, "contact:abc"))
	}
	if n := strings.Count(logs.String(), "InsecureCookies"); n > 1 {
		t.Errorf("configuration hint logged %d times; it repeats verbatim and should be logged once", n)
	}
}
