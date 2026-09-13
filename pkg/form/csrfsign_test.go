package form

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

// captureRoute hands the test the *mux.Request the mux built, so a Request
// carrying real Options (key, session hook) can be exercised.
type captureRoute struct {
	got  **mux.Request
	then func(*mux.Request)
}

func (c captureRoute) GetDataContext(_ context.Context, r *mux.Request, _ mux.ResponseWriter, _ mux.DataContext) (mux.DataContext, error) {
	*c.got = r
	if c.then != nil {
		c.then(r)
	}
	return nil, mux.NewHttpError(http.StatusTeapot, "captured")
}

func (c captureRoute) GetDefaultRoute(_ context.Context, _ *mux.Request) (string, error) {
	return "", nil
}

// testKey pins the signing key: every mux.New with no CSRFKey generates its
// own, which would make cross-mux tests pass for the wrong reason.
var testKey = []byte("test-key-test-key-test-key-test1")

func withTestKey(opts mux.Options) mux.Options {
	if len(opts.CSRFKey) == 0 {
		opts.CSRFKey = testKey
	}
	return opts
}

// requestVia runs one request through a mux built with opts and returns the
// *mux.Request the route saw.
func requestVia(t *testing.T, opts mux.Options, method, path string, cookies ...*http.Cookie) *mux.Request {
	t.Helper()
	opts = withTestKey(opts)
	var got *mux.Request
	m := mux.New(map[string]mux.Route{"/": captureRoute{got: &got}}, opts)
	req := httptest.NewRequest(method, path, nil)
	req.Host = "app.example"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	m.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("route was not reached")
	}
	return got
}

func cookieNameFor(r *mux.Request) string { return cookieName(r) }

// renderToken runs SetCSRFToken and returns the token and the Set-Cookie it emitted.
func renderToken(t *testing.T, r *mux.Request) (string, *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(r, w, &f); err != nil {
		t.Fatalf("SetCSRFToken: %v", err)
	}
	var set *http.Cookie
	if raw := w.Header().Get("Set-Cookie"); raw != "" {
		c, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatalf("ParseSetCookie: %v", err)
		}
		set = c
	}
	return f.GetCSRFToken(), set
}

func TestSign_PlantedCookieIsNotAdopted(t *testing.T) {
	// A value the server never signed must be replaced, not carried into the
	// rendered form.
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	planted := base64.RawURLEncoding.EncodeToString(make([]byte, csrfTokenLen))
	r.AddCookie(&http.Cookie{Name: cookieNameFor(r), Value: planted})

	tok, set := renderToken(t, r)
	if tok == planted {
		t.Error("an unsigned cookie value was adopted into the form")
	}
	if set == nil || set.Value != tok {
		t.Errorf("a replacement token must be issued as a cookie, got %v", set)
	}
}

func TestSign_PlantedCookieCannotValidateASubmission(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	planted := base64.RawURLEncoding.EncodeToString(make([]byte, csrfTokenLen))
	post := postSigned(t, mux.Options{}, "contact:"+planted, &http.Cookie{Name: cookieNameFor(r), Value: planted})
	if _, err := Parse(post); err == nil {
		t.Error("a submission backed only by an unsigned cookie was accepted")
	}
}

func TestSign_TamperedTokenIsRejected(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	tok, set := renderToken(t, r)
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, csrfNonceLen, csrfTokenLen - 1} {
		tampered := append([]byte(nil), raw...)
		tampered[i] ^= 0xff
		v := base64.RawURLEncoding.EncodeToString(tampered)
		post := postSigned(t, mux.Options{}, "contact:"+v, &http.Cookie{Name: set.Name, Value: v})
		if _, err := Parse(post); err == nil {
			t.Errorf("a token tampered at byte %d was accepted", i)
		}
	}
}

// postSigned builds a POST through a mux with opts, carrying the field and cookies.
func postSigned(t *testing.T, opts mux.Options, field string, cookies ...*http.Cookie) *mux.Request {
	t.Helper()
	opts = withTestKey(opts)
	var got *mux.Request
	m := mux.New(map[string]mux.Route{"/": captureRoute{got: &got}}, opts)
	body := "name=x&" + CSRFTokenName + "=" + field
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Host = "app.example"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	m.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("route was not reached")
	}
	return got
}

func TestSign_KeysAreNotInterchangeable(t *testing.T) {
	a := mux.Options{CSRFKey: []byte("key-a-key-a-key-a-key-a-key-a-32b")}
	b := mux.Options{CSRFKey: []byte("key-b-key-b-key-b-key-b-key-b-32b")}
	r := requestVia(t, a, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	if _, err := Parse(postSigned(t, a, "contact:"+tok, set)); err != nil {
		t.Fatalf("its own key should accept the token: %v", err)
	}
	if _, err := Parse(postSigned(t, b, "contact:"+tok, set)); err == nil {
		t.Error("a token signed with another key was accepted")
	}
}

func TestSign_TokenIsBoundToTheSession(t *testing.T) {
	session := "user-1"
	opts := mux.Options{CSRFSession: func(*mux.Request) string { return session }}
	r := requestVia(t, opts, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	if _, err := Parse(postSigned(t, opts, "contact:"+tok, set)); err != nil {
		t.Fatalf("same session should accept its own token: %v", err)
	}
	session = "user-2" // a login, a logout, a privilege change
	if _, err := Parse(postSigned(t, opts, "contact:"+tok, set)); err == nil {
		t.Error("a token minted for another session was accepted")
	}
}

func TestSign_ExpiredTokenIsRejectedAndReplaced(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	csrfNow = func() time.Time { return time.Now().Add(csrfMaxAge + time.Minute) }
	t.Cleanup(func() { csrfNow = time.Now })

	if _, err := Parse(postSigned(t, mux.Options{}, "contact:"+tok, set)); err == nil {
		t.Error("a token past its absolute lifetime was accepted")
	}
	r2 := requestVia(t, mux.Options{}, http.MethodGet, "/", set)
	fresh, freshSet := renderToken(t, r2)
	if fresh == tok || freshSet == nil {
		t.Error("an expired token should be replaced on the next render")
	}
}

func TestSign_FutureDatedTokenIsRejected(t *testing.T) {
	csrfNow = func() time.Time { return time.Now().Add(2 * time.Hour) }
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	tok, set := renderToken(t, r)
	csrfNow = time.Now
	t.Cleanup(func() { csrfNow = time.Now })

	if _, err := Parse(postSigned(t, mux.Options{}, "contact:"+tok, set)); err == nil {
		t.Error("a token dated in the future was accepted")
	}
}

func TestSign_ReuseKeepsTheValueAndRefreshesTheCookie(t *testing.T) {
	// The token value is stable so other tabs keep working; the cookie is
	// re-issued so an active user does not fall off the 24h edge. The
	// absolute lifetime is enforced from the signed issued-at, not the cookie.
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	r2 := requestVia(t, mux.Options{}, http.MethodGet, "/", set)
	tok2, set2 := renderToken(t, r2)
	if tok2 != tok {
		t.Errorf("token changed on reuse: %q -> %q", tok, tok2)
	}
	if set2 == nil || set2.Value != tok || set2.MaxAge <= 0 {
		t.Errorf("the cookie should be re-issued with a fresh Max-Age, got %v", set2)
	}
}

func TestSign_TokenPastHalfLifeRotatesWithGrace(t *testing.T) {
	// A token handed to a browser must have enough life left to fill the
	// form in, so it rotates past half its life — and the one it replaces
	// keeps working until it expires, or every already-open tab would break.
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	old, oldSet := renderToken(t, r)

	csrfNow = func() time.Time { return time.Now().Add(csrfMaxAge/2 + time.Minute) }
	t.Cleanup(func() { csrfNow = time.Now })

	r2 := requestVia(t, mux.Options{}, http.MethodGet, "/", oldSet)
	fresh, _ := renderToken(t, r2)
	if fresh == old {
		t.Fatal("a token past half its life should be rotated")
	}

	w := httptest.NewRecorder()
	var f BaseFormValues
	r3 := requestVia(t, mux.Options{}, http.MethodGet, "/", oldSet)
	if err := SetCSRFToken(r3, w, &f); err != nil {
		t.Fatal(err)
	}
	var current, prev *http.Cookie
	for _, raw := range w.Header().Values("Set-Cookie") {
		c, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatal(err)
		}
		switch c.Name {
		case cookieNameFor(r3):
			current = c
		case prevCookieName(r3):
			prev = c
		}
	}
	if current == nil || prev == nil {
		t.Fatalf("rotation should set both cookies, got %v", w.Header().Values("Set-Cookie"))
	}
	if prev.Value != old {
		t.Errorf("the grace cookie should carry the replaced token, got %q", prev.Value)
	}
	// A tab that rendered before the rotation still submits.
	if _, err := Parse(postSigned(t, mux.Options{}, "contact:"+old, current, prev)); err != nil {
		t.Errorf("a form rendered before the rotation was rejected: %v", err)
	}
	// And the new one works too.
	if _, err := Parse(postSigned(t, mux.Options{}, "contact:"+current.Value, current, prev)); err != nil {
		t.Errorf("the rotated-in token was rejected: %v", err)
	}
}

func TestSign_TokenWithinHalfLifeIsNotRotated(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	csrfNow = func() time.Time { return time.Now().Add(csrfMaxAge/2 - time.Minute) }
	t.Cleanup(func() { csrfNow = time.Now })

	r2 := requestVia(t, mux.Options{}, http.MethodGet, "/", set)
	again, _ := renderToken(t, r2)
	if again != tok {
		t.Errorf("a token within half its life should be reused, got %q", again)
	}
}

func TestSign_GraceTokenStillExpiresOnItsOwnSchedule(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	old, oldSet := renderToken(t, r)

	csrfNow = func() time.Time { return time.Now().Add(csrfMaxAge + time.Minute) }
	t.Cleanup(func() { csrfNow = time.Now })

	prev := &http.Cookie{Name: prevCookieName(r), Value: old}
	if _, err := Parse(postSigned(t, mux.Options{}, "contact:"+old, oldSet, prev)); err == nil {
		t.Error("the grace cookie must not outlive the token's absolute lifetime")
	}
}

func TestSign_ToleratesSmallClockSkew(t *testing.T) {
	// Instances sharing a CSRFKey never have identical clocks.
	csrfNow = func() time.Time { return time.Now().Add(5 * time.Second) }
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	tok, set := renderToken(t, r)
	csrfNow = time.Now
	t.Cleanup(func() { csrfNow = time.Now })

	if _, err := Parse(postSigned(t, mux.Options{}, "contact:"+tok, set)); err != nil {
		t.Errorf("a token minted 5s ahead by another instance was rejected: %v", err)
	}
}

func TestSign_SessionRotatedDuringAPostIsPickedUp(t *testing.T) {
	// A login handler that re-renders instead of redirecting: Parse runs with
	// the old session, the hook rotates it, then the form is rendered.
	session := "anonymous"
	opts := mux.Options{CSRFSession: func(*mux.Request) string { return session }}
	r := requestVia(t, opts, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	post := postSigned(t, opts, "contact:"+tok, set)
	if _, err := Parse(post); err != nil {
		t.Fatalf("pre-login submission should validate: %v", err)
	}
	session = "user-1" // the hook logs the user in

	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(post, w, &f); err != nil {
		t.Fatalf("re-rendering after a session change failed: %v", err)
	}
	if f.GetCSRFToken() == tok {
		t.Error("the re-rendered form still carries the pre-login token")
	}
	newSet, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatalf("the new token must be issued as a cookie, or the next submit fails: %v", err)
	}
	// The point of the new token: the next request, which arrives with the
	// new session, accepts it.
	if _, err := Parse(postSigned(t, opts, "contact:"+f.GetCSRFToken(), newSet)); err != nil {
		t.Errorf("the re-rendered token was rejected on the next request: %v", err)
	}
}

func TestSign_AncestorParseSurvivesASessionRotationByTheLeaf(t *testing.T) {
	// Generated code runs form.Parse for every route in the stack that has a
	// form — leaf first, ancestors after, with each route's Process in
	// between. A login on the leaf must not make the ancestor's Parse reject
	// the very submission that performed it.
	session := "anonymous"
	opts := mux.Options{CSRFSession: func(*mux.Request) string { return session }}
	r := requestVia(t, opts, http.MethodGet, "/")
	tok, set := renderToken(t, r)

	post := postSigned(t, opts, "contact:"+tok, set)
	if _, err := Parse(post); err != nil {
		t.Fatalf("leaf Parse: %v", err)
	}
	session = "user-1" // the leaf's Process logs the user in
	if _, err := Parse(post); err != nil {
		t.Fatalf("ancestor Parse after the rotation: %v", err)
	}

	// The render that follows still has to hand out a token for the new session.
	w := httptest.NewRecorder()
	var f BaseFormValues
	if err := SetCSRFToken(post, w, &f); err != nil {
		t.Fatal(err)
	}
	if f.GetCSRFToken() == tok {
		t.Error("the re-rendered form still carries the pre-login token")
	}
}

func TestSign_GraceCookieExpiresWithItsToken(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	_, oldSet := renderToken(t, r)

	csrfNow = func() time.Time { return time.Now().Add(csrfMaxAge/2 + time.Minute) }
	t.Cleanup(func() { csrfNow = time.Now })

	w := httptest.NewRecorder()
	r2 := requestVia(t, mux.Options{}, http.MethodGet, "/", oldSet)
	if err := SetCSRFToken(r2, w, &BaseFormValues{}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range w.Header().Values("Set-Cookie") {
		c, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatal(err)
		}
		if c.Name != prevCookieName(r2) {
			continue
		}
		// The token inside has at most half a lifetime left; a 24h cookie
		// would keep being sent long after it stopped verifying.
		if want := int((csrfMaxAge - csrfRotateAfter) / time.Second); c.MaxAge > want {
			t.Errorf("grace cookie Max-Age %d exceeds the token's remaining life %d", c.MaxAge, want)
		}
		if c.MaxAge <= 0 {
			t.Errorf("grace cookie should still be usable, got Max-Age %d", c.MaxAge)
		}
		return
	}
	t.Fatal("no grace cookie was set")
}

func TestSign_AncestorLoginDoesNotReuseThePreLoginToken(t *testing.T) {
	// Routes render leaf→root in one response. If an ancestor's Process logs
	// the user in, its own render must not reuse the token the leaf minted
	// for the anonymous session — that token no longer verifies.
	session := "anonymous"
	opts := mux.Options{CSRFSession: func(*mux.Request) string { return session }}
	r := requestVia(t, opts, http.MethodGet, "/")

	w := httptest.NewRecorder()
	var leaf, layout BaseFormValues
	if err := SetCSRFToken(r, w, &leaf); err != nil {
		t.Fatal(err)
	}
	session = "user-1" // the ancestor's Process logs the user in
	if err := SetCSRFToken(r, w, &layout); err != nil {
		t.Fatal(err)
	}
	if layout.GetCSRFToken() == leaf.GetCSRFToken() {
		t.Fatal("the ancestor reused a token minted for the previous session")
	}

	set, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(w.Header().Values("Set-Cookie")); n != 1 {
		t.Errorf("one cookie of a name should be issued, got %d: %v", n, w.Header().Values("Set-Cookie"))
	}
	if set.Value != layout.GetCSRFToken() {
		t.Errorf("the cookie carries %q, want the token the page renders %q", set.Value, layout.GetCSRFToken())
	}
	if _, err := Parse(postSigned(t, opts, "contact:"+layout.GetCSRFToken(), set)); err != nil {
		t.Errorf("the post-login token was rejected on the next request: %v", err)
	}
}

func TestSign_ReusedCookieExpiresWithItsToken(t *testing.T) {
	r := requestVia(t, mux.Options{}, http.MethodGet, "/")
	_, set := renderToken(t, r)

	elapsed := csrfRotateAfter - time.Minute
	csrfNow = func() time.Time { return time.Now().Add(elapsed) }
	t.Cleanup(func() { csrfNow = time.Now })

	w := httptest.NewRecorder()
	r2 := requestVia(t, mux.Options{}, http.MethodGet, "/", set)
	if err := SetCSRFToken(r2, w, &BaseFormValues{}); err != nil {
		t.Fatal(err)
	}
	again, err := http.ParseSetCookie(w.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatal(err)
	}
	// A cookie outliving the token it carries hides a genuinely missing
	// cookie behind a generic "Invalid CSRF token".
	if want := int((csrfMaxAge - elapsed) / time.Second); again.MaxAge > want {
		t.Errorf("reused cookie Max-Age %d exceeds the token's remaining life %d", again.MaxAge, want)
	}
}

func TestSign_ReMintDropsAGraceCookieAnEarlierRouteLeft(t *testing.T) {
	// A leaf rotates (leaving a grace cookie), then an ancestor's login
	// forces a fresh mint. The grace cookie is bound to the old session and
	// can never be accepted, so it must not be left on the response.
	session := "anonymous"
	opts := mux.Options{CSRFSession: func(*mux.Request) string { return session }}
	r := requestVia(t, opts, http.MethodGet, "/")
	_, oldSet := renderToken(t, r)

	csrfNow = func() time.Time { return time.Now().Add(csrfRotateAfter + time.Minute) }
	t.Cleanup(func() { csrfNow = time.Now })

	r2 := requestVia(t, opts, http.MethodGet, "/", oldSet)
	w := httptest.NewRecorder()
	if err := SetCSRFToken(r2, w, &BaseFormValues{}); err != nil { // leaf: rotates
		t.Fatal(err)
	}
	var sawPrev bool
	for _, raw := range w.Header().Values("Set-Cookie") {
		if c, _ := http.ParseSetCookie(raw); c != nil && c.Name == prevCookieName(r2) {
			sawPrev = true
		}
	}
	if !sawPrev {
		t.Fatal("the leaf's rotation should have left a grace cookie to begin with")
	}

	session = "user-1" // an ancestor's Process logs the user in
	if err := SetCSRFToken(r2, w, &BaseFormValues{}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range w.Header().Values("Set-Cookie") {
		if c, _ := http.ParseSetCookie(raw); c != nil && c.Name == prevCookieName(r2) {
			t.Errorf("a grace cookie for the previous session survived the re-mint: %s", raw)
		}
	}
}
