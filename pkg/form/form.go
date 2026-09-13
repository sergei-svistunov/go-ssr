package form

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

var MaxMultipartMemory int64 = 32 << 20 // 32 MB

// CSRFTokenName is the name of the hidden field every <ssr:form> renders.
const CSRFTokenName = "_csrf_token_"

// csrfCookieName is CSRFTokenName with the __Host- prefix. Browsers refuse to
// store a __Host- cookie unless it arrives over https with Path=/ and no
// Domain, which is what stops a sibling subdomain or a plain-http page from
// planting a cookie the double-submit check would accept.
const csrfCookieName = "__Host-" + CSRFTokenName

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

// processKey signs tokens for a Request that no mux built — direct use of
// this package, and tests. Through a mux the key comes from Options.CSRFKey.
var processKey = sync.OnceValue(func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("form: generating a CSRF key: " + err.Error())
	}
	return key
})

func signingKey(r *mux.Request) []byte {
	if k := r.CSRFKey(); len(k) > 0 {
		return k
	}
	return processKey()
}

func csrfMAC(r *mux.Request, payload []byte, session string) []byte {
	m := hmac.New(sha256.New, signingKey(r))
	m.Write(payload)
	m.Write([]byte{0})
	m.Write([]byte(session))
	return m.Sum(nil)
}

// mintToken returns a fresh signed token, bound to the session the user has
// now — which is not the one the request arrived with if a hook just logged
// them in.
func mintToken(r *mux.Request) (string, error) {
	raw := make([]byte, csrfTokenLen)
	if _, err := rand.Read(raw[:csrfNonceLen]); err != nil {
		return "", err
	}
	binary.BigEndian.PutUint64(raw[csrfNonceLen:], uint64(csrfNow().Unix()))
	copy(raw[csrfNonceLen+csrfTimeLen:], csrfMAC(r, raw[:csrfNonceLen+csrfTimeLen], r.CSRFSessionNow()))
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// tokenAge reports how long ago v was minted, if v was signed for this
// request's key and session and is still within csrfMaxAge. A token dated
// further ahead than csrfClockSkew is refused: this server cannot have
// issued it.
func tokenAge(r *mux.Request, v, session string) (time.Duration, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil || len(raw) != csrfTokenLen {
		return 0, false
	}
	payload, mac := raw[:csrfNonceLen+csrfTimeLen], raw[csrfNonceLen+csrfTimeLen:]
	if !hmac.Equal(mac, csrfMAC(r, payload, session)) {
		return 0, false
	}
	age := csrfNow().Sub(time.Unix(int64(binary.BigEndian.Uint64(payload[csrfNonceLen:])), 0))
	return age, age >= -csrfClockSkew && age <= csrfMaxAge
}

// validToken checks a token the request arrived with.
func validToken(r *mux.Request, v string) bool {
	_, ok := tokenAge(r, v, r.CSRFSession())
	return ok
}

// cookieName honours mux.Options.InsecureCookies, which the mux copies onto
// every request: without the Secure flag a __Host- cookie would be refused.
func cookieName(r *mux.Request) string {
	if r.InsecureCookies {
		return CSRFTokenName
	}
	return csrfCookieName
}

// prevCookieName holds the token a rotation replaced, so a form rendered
// before it still submits.
func prevCookieName(r *mux.Request) string { return cookieName(r) + "prev" }

type BaseFormValues struct {
	csrfToken string
	error     string
	validated bool
	elements  []Element
}

type Element interface {
	HasError() bool
}

func (f *BaseFormValues) SetElements(elements []Element) { f.elements = elements }
func (f *BaseFormValues) MarkValidated()                 { f.validated = true }
func (f *BaseFormValues) IsValidated() bool              { return f.validated }
func (f *BaseFormValues) GetCSRFToken() string           { return f.csrfToken }
func (f *BaseFormValues) SetError(err string)            { f.error = err }
func (f *BaseFormValues) GetError() string               { return f.error }

func (f *BaseFormValues) HasError() bool {
	if f.error != "" {
		return true
	}

	for _, e := range f.elements {
		if e.HasError() {
			return true
		}
	}

	return false
}

func IsMultipart(r *mux.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), MultipartFormData)
}

// issuedInResponse returns the token an earlier SetCSRFToken call already
// set on this response — a layout and a leaf both rendering a form — or "".
func issuedInResponse(w mux.ResponseWriter, name string) string {
	for _, sc := range w.Header().Values("Set-Cookie") {
		if c, err := http.ParseSetCookie(sc); err == nil && c.Name == name {
			return c.Value
		}
	}
	return ""
}

func SetCSRFToken(r *mux.Request, w mux.ResponseWriter, forms ...*BaseFormValues) error {
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
				return err
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
	for _, form := range forms {
		form.csrfToken = csrfToken
	}

	return nil
}

func setCSRFCookie(w mux.ResponseWriter, name, token string, maxAge int, insecure bool) {
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
func dropCookie(w mux.ResponseWriter, name string) {
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
func cookieToken(r *mux.Request, name, session string) (string, time.Duration, bool) {
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
func missingCookie(r *mux.Request) error {
	if !mux.SecureRequest(r.Request) && !r.InsecureCookies {
		// The same sentence every time: a configuration diagnostic, not a
		// per-request event, and the path is remote input on a route anyone
		// can drive.
		insecureCookieHint.Do(func() {
			log.Printf("form: CSRF cookie missing on a plain-http request (first seen: %s %q). Browsers keep the "+
				"Secure cookie only over https, or on localhost in Chrome and Firefox (not Safari); serve https, "+
				"forward X-Forwarded-Proto from your proxy, or set mux.Options.InsecureCookies for development",
				r.Method, r.URL.Path)
		})
	}
	return mux.NewHttpError(http.StatusBadRequest, "Missed CSRF token")
}

// insecureCookieHint keeps the diagnostic above to one line per process.
var insecureCookieHint sync.Once

// Parse identifies the submitted form and checks its CSRF token against the
// cookie. Cross-site submissions never get here: the mux refuses them before
// the route's hooks run.
func Parse(r *mux.Request) (string, error) {
	// Checked before the body is read, so a cookie-less POST buffers nothing.
	// The grace cookie a rotation left behind counts: a form rendered before
	// the rotation is still the user's own.
	cookies := append(r.CookiesNamed(cookieName(r)), r.CookiesNamed(prevCookieName(r))...)
	if len(cookies) == 0 {
		return "", missingCookie(r)
	}

	var csrfToken string
	if IsMultipart(r) {
		if err := r.ParseMultipartForm(MaxMultipartMemory); err != nil {
			return "", mux.NewHttpError(http.StatusBadRequest, err.Error())
		}
		if vs := r.MultipartForm.Value[CSRFTokenName]; len(vs) > 0 {
			csrfToken = vs[0]
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return "", mux.NewHttpError(http.StatusBadRequest, err.Error())
		}
		if vs := r.PostForm[CSRFTokenName]; len(vs) > 0 {
			csrfToken = vs[0]
		}
	}

	formName, token, ok := strings.Cut(csrfToken, ":")
	// The signature is checked before the cookie comparison, so a value the
	// server never issued is refused even if it was also planted as a cookie.
	if !ok || !validToken(r, token) {
		return "", mux.NewHttpError(http.StatusBadRequest, "Invalid CSRF token")
	}
	// Every cookie of that name is tried, for the reason cookieToken gives.
	matched := false
	for _, c := range cookies {
		if subtle.ConstantTimeCompare([]byte(token), []byte(c.Value)) == 1 {
			matched = true
		}
	}
	if !matched {
		return "", mux.NewHttpError(http.StatusBadRequest, "Invalid CSRF token")
	}

	return formName, nil
}
