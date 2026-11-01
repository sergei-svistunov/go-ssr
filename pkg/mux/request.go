package mux

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
)

type Request struct {
	*http.Request
	URLParams map[string]string
	// InsecureCookies and OriginPatterns mirror the same Options fields for
	// the packages that set cookies or check origins on behalf of a route.
	// They are what the handler was configured with: the mux has already
	// applied its own gates by the time a hook sees them, so changing them
	// here affects only a CrossSite call the application makes itself.
	InsecureCookies bool
	OriginPatterns  []string

	// noStore points at the flag the mux reads back, shared by every Request
	// built from this request's context. nil outside the mux. A live
	// connection hands one Request to several goroutines, so it is atomic.
	noStore *atomic.Bool

	csrfKey       []byte
	csrfSessionFn func(*Request) string
	// csrfSession points at the arrival session shared by every Request
	// built from this request's context. nil outside the mux.
	csrfSession *arrivalSession
}

// arrivalSession caches Options.CSRFSession's first answer for a request.
// One Request can reach several goroutines on a live connection, so
// resolving is once-only rather than a plain flag.
type arrivalSession struct {
	once  sync.Once
	value string
}

// CSRFSession is the session this request arrived with: the first result of
// Options.CSRFSession, kept for the rest of the request, or "" when nothing
// binds tokens to a session. Verifying a submitted token uses it, so a hook
// that logs the user in partway through does not make a later route reject
// the very submission that performed the login.
func (r *Request) CSRFSession() string {
	if r.csrfSession == nil {
		return r.CSRFSessionNow()
	}
	r.csrfSession.once.Do(func() { r.csrfSession.value = r.CSRFSessionNow() })
	return r.csrfSession.value
}

// CSRFSessionNow asks Options.CSRFSession again. Minting a token uses it, so
// a form rendered after a login carries a token bound to the session the
// user now has rather than the one they arrived with.
func (r *Request) CSRFSessionNow() string {
	if r.csrfSessionFn == nil {
		return ""
	}
	return r.csrfSessionFn(r)
}

// NoStore marks the response as carrying a per-user secret (a CSRF token).
// The mux sets Cache-Control: no-store once every route in the stack has had
// a say, so a layout's own Cache-Control cannot re-enable caching of it. The
// mark travels in the request context, so it survives a hook rebuilding the
// Request; on a Request not built by the mux it does nothing.
func (r *Request) NoStore() {
	if r.noStore != nil {
		r.noStore.Store(true)
	}
}

// requestOptions is what the mux attaches to every request's context, so any
// Request built from it — by the mux or by a hook rebuilding one downstream —
// sees the same settings.
type requestOptions struct {
	insecureCookies bool
	originPatterns  []string
	noStore         *atomic.Bool
	csrfKey         []byte
	csrfSessionFn   func(*Request) string
	csrfSession     *arrivalSession
	csp             *cspPolicy
}

// withOptions returns r carrying o for NewRequest to read.
func withOptions(r *http.Request, o requestOptions) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestOptionsKey{}, o))
}

type requestOptionsKey struct{}

func NewRequest(r *http.Request) *Request {
	req := &Request{
		Request:   r,
		URLParams: map[string]string{},
	}
	if o, ok := r.Context().Value(requestOptionsKey{}).(requestOptions); ok {
		req.InsecureCookies = o.insecureCookies
		req.OriginPatterns = o.originPatterns
		req.noStore = o.noStore
		req.csrfKey = o.csrfKey
		req.csrfSessionFn = o.csrfSessionFn
		req.csrfSession = o.csrfSession
	}
	return req
}

func (r *Request) URLParam(key string) string {
	return r.URLParams[key]
}
