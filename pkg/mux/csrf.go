package mux

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxMultipartMemory is how much of a multipart form body the mux keeps in
// memory; the rest goes to temporary files.
var MaxMultipartMemory int64 = 32 << 20

// CSRFFieldName is the name of the hidden field every <ssr:form> renders.
const CSRFFieldName = "_csrf_token_"

// minCSRFKeyLen is the shortest Options.CSRFKey New accepts: the size of the
// HMAC-SHA256 output it keys.
const minCSRFKeyLen = 32

// csrfCookieName is CSRFFieldName with the __Host- prefix. Browsers refuse to
// store a __Host- cookie unless it arrives over https with Path=/ and no
// Domain, which is what stops a sibling subdomain or a plain-http page from
// planting a cookie the double-submit check would accept.
const csrfCookieName = "__Host-" + CSRFFieldName

// csrfMaxAge is how long a token stays valid after it was minted. It is
// enforced from the issued-at the token carries, not from the cookie, so
// re-issuing the cookie to keep an active user's session alive cannot extend
// a token's life.
const csrfMaxAge = 24 * time.Hour

const csrfCookieMaxAge = int(csrfMaxAge / time.Second)

// csrfRotateAfter is the age at which a render replaces the token instead of
// reusing it, so the one handed to a browser always has at least this much
// life left to fill a form in. The token it replaces stays acceptable until
// its own expiry, or every form already open would stop submitting.
const csrfRotateAfter = csrfMaxAge / 2

// csrfClockSkew is how far ahead another instance's clock may be before its
// tokens are refused. Instances sharing Options.CSRFKey never agree exactly;
// far-future values are still rejected.
const csrfClockSkew = time.Minute

// remainingLife is how long a token of the given age has left, in seconds,
// so the cookie carrying it expires when it does. A negative age — a peer
// instance whose clock runs ahead, within csrfClockSkew — cannot buy the
// cookie more life than a freshly minted token gets.
func remainingLife(age time.Duration) int {
	if age < 0 {
		return csrfCookieMaxAge
	}
	return int((csrfMaxAge - age) / time.Second)
}

// A token is nonce || issuedAt || HMAC-SHA256(key, nonce||issuedAt||session).
// Signing is what makes a cookie the server never issued useless: without the
// key it cannot be produced, so a planted value is replaced rather than
// adopted, and it cannot validate a submission.
const (
	csrfNonceLen = 16
	csrfTimeLen  = 8
	csrfMACLen   = sha256.Size
	csrfTokenLen = csrfNonceLen + csrfTimeLen + csrfMACLen
)

// csrfNow is time.Now, replaced in tests.
var csrfNow = time.Now

var errNoCSRFKey = errors.New("mux: no CSRF key")

func csrfMAC(key, payload []byte, session string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	m.Write([]byte{0})
	m.Write([]byte(session))
	return m.Sum(nil)
}

// mintToken returns a fresh signed token, bound to the session the user has
// now — which is not the one the request arrived with if a hook just logged
// them in.
func mintToken(r *Request) (string, error) {
	raw := make([]byte, csrfTokenLen)
	if _, err := rand.Read(raw[:csrfNonceLen]); err != nil {
		return "", err
	}
	now := csrfNow().Unix()
	if now < 0 {
		return "", errors.New("mux: the clock is before 1970")
	}
	binary.BigEndian.PutUint64(raw[csrfNonceLen:], uint64(now))
	copy(raw[csrfNonceLen+csrfTimeLen:], csrfMAC(r.csrfKey, raw[:csrfNonceLen+csrfTimeLen], r.CSRFSessionNow()))
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// tokenAge reports how long ago v was minted, if v was signed for this
// request's key and session and is still within csrfMaxAge. A token dated
// further ahead than csrfClockSkew is refused: this server cannot have
// issued it. Without a key nothing verifies: an HMAC with an empty key is
// one anybody can compute.
func tokenAge(r *Request, v, session string) (time.Duration, bool) {
	if len(r.csrfKey) == 0 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil || len(raw) != csrfTokenLen {
		return 0, false
	}
	payload, mac := raw[:csrfNonceLen+csrfTimeLen], raw[csrfNonceLen+csrfTimeLen:]
	if !hmac.Equal(mac, csrfMAC(r.csrfKey, payload, session)) {
		return 0, false
	}
	issued := binary.BigEndian.Uint64(payload[csrfNonceLen:])
	if issued > math.MaxInt64 {
		return 0, false
	}
	age := csrfNow().Sub(time.Unix(int64(issued), 0))
	return age, age >= -csrfClockSkew && age <= csrfMaxAge
}

// validToken checks a token the request arrived with.
func validToken(r *Request, v string) bool {
	_, ok := tokenAge(r, v, r.CSRFSession())
	return ok
}

// cookieName honours Options.InsecureCookies, which the mux copies onto
// every request: without the Secure flag a __Host- cookie would be refused.
func cookieName(r *Request) string {
	if r.InsecureCookies {
		return CSRFFieldName
	}
	return csrfCookieName
}

// prevCookieName holds the token a rotation replaced, so a form rendered
// before it still submits.
func prevCookieName(r *Request) string { return cookieName(r) + "prev" }

// issuedInResponse returns the token an earlier CSRFToken call already set
// on this response — a layout and a leaf both rendering a form — or "".
func issuedInResponse(w ResponseWriter, name string) string {
	for _, sc := range w.Header().Values("Set-Cookie") {
		if c, err := http.ParseSetCookie(sc); err == nil && c.Name == name {
			return c.Value
		}
	}
	return ""
}

// CSRFToken returns the token a form rendered into this response carries,
// and sets the cookie it is checked against. It fails on a Request no mux
// built, which has no key to sign with.
func (r *Request) CSRFToken(w ResponseWriter) (string, error) {
	if len(r.csrfKey) == 0 {
		return "", errNoCSRFKey
	}
	name := cookieName(r)
	// A token is reused for as long as it is good rather than minted per
	// render, so a form open in one tab survives another page rendering in a
	// second tab. Reuse is judged against the session the user has now, so a
	// hook that just logged them in gets a token bound to the new session.
	session := r.CSRFSessionNow()
	csrfToken := issuedInResponse(w, name)
	// A token another route already issued in this response is only good if
	// it still verifies: an ancestor whose Process logged the user in must
	// not hand out what a descendant minted for the session before it.
	if csrfToken != "" {
		if _, ok := tokenAge(r, csrfToken, session); !ok {
			csrfToken = ""
		}
	}
	if csrfToken == "" {
		current, age, ok := cookieToken(r, name, session)
		switch {
		case ok && age <= csrfRotateAfter:
			// The cookie is re-issued so an active user does not fall off its
			// edge, and dies with the token it carries so a dead one cannot
			// mask a genuinely missing cookie.
			csrfToken = current
			setCSRFCookie(w, name, csrfToken, remainingLife(age), r.InsecureCookies)
		default:
			var err error
			if csrfToken, err = mintToken(r); err != nil {
				return "", err
			}
			setCSRFCookie(w, name, csrfToken, csrfCookieMaxAge, r.InsecureCookies)
			if ok {
				// Rotating: keep the replaced token acceptable until it
				// expires on its own, so forms already rendered still submit.
				// The cookie dies with it — a dead one left behind would mask
				// the diagnostic for a genuinely missing cookie.
				if remaining := remainingLife(age); remaining > 0 {
					setCSRFCookie(w, prevCookieName(r), current, remaining, r.InsecureCookies)
				}
			} else {
				// Not a rotation: nothing that came in is acceptable any
				// more, so a grace cookie an earlier route in this response
				// left behind is meaningless and would only mask the
				// diagnostic for a genuinely missing cookie.
				dropCookie(w, prevCookieName(r))
			}
		}
	}
	// The token is a per-response secret; a shared cache must not hand it
	// out. The mux re-applies this once every route in the stack has run.
	w.Header().Set("Cache-Control", "no-store")
	r.NoStore()
	return csrfToken, nil
}

func setCSRFCookie(w ResponseWriter, name, token string, maxAge int, insecure bool) {
	c := http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   !insecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	dropCookie(w, name)
	w.Header().Add("Set-Cookie", c.String())
}

// dropCookie removes any Set-Cookie for name already staged on the response.
// A later route may replace what an earlier one issued; leaving both behind
// would make issuedInResponse ambiguous.
func dropCookie(w ResponseWriter, name string) {
	h := w.Header()
	if prior := h.Values("Set-Cookie"); len(prior) > 0 {
		kept := make([]string, 0, len(prior))
		for _, sc := range prior {
			if pc, err := http.ParseSetCookie(sc); err == nil && pc.Name == name {
				continue
			}
			kept = append(kept, sc)
		}
		h.Del("Set-Cookie")
		for _, sc := range kept {
			h.Add("Set-Cookie", sc)
		}
	}
}

// cookieToken returns the first still-valid token among the request's
// cookies of that name with its age. Every one is examined because a stale
// cookie left at a default path by an older build is sent ahead of the
// current Path=/ one.
func cookieToken(r *Request, name, session string) (string, time.Duration, bool) {
	for _, c := range r.CookiesNamed(name) {
		if age, ok := tokenAge(r, c.Value, session); ok {
			return c.Value, age, true
		}
	}
	return "", 0, false
}

// missingCookie is the 400 for a POST without the CSRF cookie. When the
// request could not have carried a Secure cookie, the likeliest cause is the
// browser refusing it; that goes to the operator's log, not into a public
// response body that would advertise a development-only option.
func missingCookie(r *Request) error {
	if !SecureRequest(r.Request) && !r.InsecureCookies {
		// The same sentence every time: a configuration diagnostic, not a
		// per-request event, and the path is remote input on a route anyone
		// can drive.
		insecureCookieHint.Do(func() {
			log.Printf("mux: CSRF cookie missing on a plain-http request (first seen: %s %s). Browsers keep the "+
				"Secure cookie only over https, or on localhost in Chrome and Firefox (not Safari); serve https, "+
				"forward X-Forwarded-Proto from your proxy, or set mux.Options.InsecureCookies for development",
				strconv.Quote(r.Method), strconv.Quote(r.URL.Path))
		})
	}
	return NewHttpError(http.StatusBadRequest, "Missed CSRF token")
}

// bodyTooLarge answers a POST whose body is over Options.MaxBodyBytes.
func bodyTooLarge() error {
	return NewHttpError(http.StatusRequestEntityTooLarge, "Request body too large.")
}

// formError is the answer to a body the form parser could not read.
func formError(err error) error {
	if mbe := (*http.MaxBytesError)(nil); errors.As(err, &mbe) {
		return bodyTooLarge()
	}
	return NewHttpError(http.StatusBadRequest, err.Error())
}

// insecureCookieHint keeps the diagnostic above to one line per process.
var insecureCookieHint sync.Once

// readForm identifies the submitted form and checks its CSRF token against
// the cookie. Cross-site submissions never get here: the mux refuses them
// before the route's hooks run.
func readForm(r *Request) (formID string, err error) {
	// Checked before the body is read, so a cookie-less POST buffers nothing.
	// The grace cookie a rotation left behind counts: a form rendered before
	// the rotation is still the user's own.
	cookies := append(r.CookiesNamed(cookieName(r)), r.CookiesNamed(prevCookieName(r))...)
	if len(cookies) == 0 {
		return "", missingCookie(r)
	}

	var csrfToken string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(MaxMultipartMemory); err != nil {
			return "", formError(err)
		}
		if vs := r.MultipartForm.Value[CSRFFieldName]; len(vs) > 0 {
			csrfToken = vs[0]
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return "", formError(err)
		}
		if vs := r.PostForm[CSRFFieldName]; len(vs) > 0 {
			csrfToken = vs[0]
		}
	}

	formName, token, ok := strings.Cut(csrfToken, ":")
	// The signature is checked before the cookie comparison, so a value the
	// server never issued is refused even if it was also planted as a cookie.
	if !ok || !validToken(r, token) {
		return "", NewHttpError(http.StatusBadRequest, "Invalid CSRF token")
	}
	// Every cookie of that name is tried, for the reason cookieToken gives.
	matched := false
	for _, c := range cookies {
		if subtle.ConstantTimeCompare([]byte(token), []byte(c.Value)) == 1 {
			matched = true
		}
	}
	if !matched {
		return "", NewHttpError(http.StatusBadRequest, "Invalid CSRF token")
	}

	return formName, nil
}
