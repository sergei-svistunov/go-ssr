package mux

import (
	"context"
	"errors"
	"log"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/sergei-svistunov/go-ssr/internal/wsorigin"
	"github.com/sergei-svistunov/go-ssr/pkg/reactive"
)

// Live is implemented by the state of a route with live values. A page with
// live values or calls has a live connection, at the page's path followed by
// /__ws. It runs the hooks the page itself runs — every Guard on the path,
// then Data on the routes that render the page — before it takes their
// snapshots, so the live values of a gate never reach a page below it. The
// connection also carries the page's calls (see Caller).
type Live interface {
	RouteKey() string
	// Snapshot returns the rendered value of every live binding of the route,
	// by binding key.
	Snapshot() map[string]string
	// Subscribe runs for as long as the connection lives and pushes the
	// route's changes through conn.
	Subscribe(ctx context.Context, r *Request, conn *reactive.Conn) error
	// HandleWrite applies a value the page wrote to one of the route's
	// variables.
	HandleWrite(ctx context.Context, r *Request, conn *reactive.Conn, msg reactive.WriteMsg)
}

const liveSuffix = "/__ws"

// liveMaxAge is how long a live connection lives before the client must
// reconnect and pass every check again. New reads it.
var liveMaxAge = 10 * time.Minute

// liveAddress reports whether p is the live connection address of a page and
// returns that page's path.
func liveAddress(p string) (string, bool) {
	page, ok := strings.CutSuffix(p, liveSuffix)
	if !ok {
		return "", false
	}
	if page == "" {
		page = "/"
	}
	return page, true
}

// serveLive opens the live connection of the page at path page. It refuses a
// cross-site handshake before any hook runs, then passes the checks the page
// itself passes: every Guard on the path, then the Data of the routes that
// render the page, once it knows they have live values or calls and the
// session has a place left. Only then does it take their live values and
// upgrade.
func (m *Mux) serveLive(w http.ResponseWriter, hr *http.Request, page string) {
	r := NewRequest(pageRequest(hr, page))
	if hr.Method != http.MethodGet || !headerHasToken(hr.Header, "Upgrade", "websocket") || !headerHasToken(hr.Header, "Connection", "upgrade") {
		m.errorHandler(w, r, NewHttpError(http.StatusBadRequest, "This address only takes live page connections."))
		return
	}
	routes, end, matchErr := m.match(r)

	// The handshake carries the browser's cookies and no token follows it,
	// so the stricter rule applies. The exemption sees the handshake's own
	// address, so a page exempt for its form posts is not exempt here.
	if CrossSiteStrict(hr, m.options.originPatterns) {
		handshake := NewRequest(hr)
		handshake.URLParams = r.URLParams
		if !m.allowsCrossSite(handshake) {
			handshakeRefusals.logf("mux: live connection to %s refused as cross-site (Origin %s, Host %s)",
				strconv.Quote(hr.URL.Path), strconv.Quote(hr.Header.Get("Origin")), strconv.Quote(hr.Host))
			m.errorHandler(w, r, NewHttpError(http.StatusForbidden, "Cross-site live connection"))
			return
		}
	}
	if matchErr != nil {
		m.errorHandler(w, r, matchErr)
		return
	}

	refuse := func(err error) { m.errorHandler(w, r, liveRefusal(err)) }
	session := r.CSRFSession()
	ctx := r.Context()
	states, err := guard(ctx, r, routes)
	if err != nil {
		refuse(err)
		return
	}
	// A layout opened itself redirects to its default child, so it has no
	// page of its own to connect to.
	if end.route.Layout() && len(end.children) > 0 {
		refuse(NotFound())
		return
	}
	states = rendered(routes, states)
	var lives []Live
	var calls bool
	var routeKey string
	for _, s := range states {
		if l, ok := s.(Live); ok {
			lives = append(lives, l)
			routeKey = l.RouteKey()
		}
		if c, ok := s.(Caller); ok {
			calls = true
			routeKey = c.RouteKey()
		}
	}
	if len(lives) == 0 && !calls {
		refuse(NotFound())
		return
	}
	release, ok := m.live.acquire(session)
	if !ok {
		refuse(NewHttpError(http.StatusTooManyRequests, "Too many live connections."))
		return
	}
	defer release()

	for _, s := range states {
		if err := s.Data(ctx, r, NoopResponseWriter{}); err != nil {
			refuse(err)
			return
		}
	}
	bindings := map[string]string{}
	for _, l := range lives {
		maps.Copy(bindings, l.Snapshot())
	}
	// The cross-site refusal above has run, so the library's own same-host
	// check, which knows nothing of OriginPatterns, is skipped.
	conn, err := reactive.Accept(w, hr.WithContext(wsorigin.Mark(hr.Context())))
	if err != nil {
		log.Printf("mux: live connection to %s not opened: %v", strconv.Quote(hr.URL.Path), err)
		return
	}
	runLive(r, conn, routes, lives, routeKey, bindings, m.liveMaxAge, release)
}

// runLive sends the page's live values in an init frame for routeKey, runs
// every Subscribe, applies the page's writes and runs its calls on the
// routes until the client leaves, a hook fails or the connection has lived
// for age. Once the connection is closed it calls release and waits for the
// Subscribe hooks, not for calls still running: their answers go nowhere.
func runLive(r *Request, conn *reactive.Conn, routes []Route, lives []Live, routeKey string, bindings map[string]string, age time.Duration, release func()) {
	defer conn.CloseNow()
	ctx := conn.Ctx()
	expire := time.AfterFunc(age, func() { conn.CloseWithError(reactive.StatusReauth, "reconnect") })
	defer expire.Stop()

	if err := conn.SendJSON(reactive.NewInitMsg(routeKey, bindings)); err != nil {
		return
	}
	conn.StartSendLoop()

	byKey := make(map[string]Live, len(lives))
	var subscribes sync.WaitGroup
	defer subscribes.Wait()
	for _, l := range lives {
		byKey[l.RouteKey()] = l
		subscribes.Go(func() {
			defer recoverLive(conn)
			if err := l.Subscribe(ctx, r, conn); err != nil && ctx.Err() == nil {
				conn.CloseWithError(subscribeClose(err))
			}
		})
	}
	calls := newPageCalls(r, conn, routes)
	_ = conn.ReadFrames(ctx, func(msg reactive.WriteMsg) {
		defer recoverLive(conn)
		l, ok := byKey[msg.RouteKey]
		if !ok {
			_ = conn.SendJSON(reactive.NewErrMsg("", msg.Var, "unknown route", "unknown_route"))
			return
		}
		l.HandleWrite(ctx, r, conn, msg)
	}, calls.start)
	conn.CloseNow()
	release()
}

func recoverLive(conn *reactive.Conn) {
	if v := recover(); v != nil {
		log.Printf("mux: panic on a live connection: %v", v)
		conn.CloseWithError(websocket.StatusInternalError, "server error")
	}
}

// liveRefusal maps a hook's error on a live handshake to the refusal sent
// before the upgrade: a 4xx HttpError keeps its status, a redirect is 401,
// and anything else is 500.
func liveRefusal(err error) *HttpError {
	var httpErr *HttpError
	if errors.As(err, &httpErr) && httpErr.Code >= 400 && httpErr.Code < 500 {
		return httpErr
	}
	var redirect *HttpRedirect
	if errors.As(err, &redirect) {
		return NewHttpError(http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))
	}
	log.Printf("mux: live connection refused: %v", err)
	return NewHttpError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
}

// subscribeClose maps a Subscribe error to the close of its connection: a
// 401 or 403 HttpError, or a redirect, refuses with 1008 and the error's
// message; anything else is a server error.
func subscribeClose(err error) (websocket.StatusCode, string) {
	var httpErr *HttpError
	if errors.As(err, &httpErr) && (httpErr.Code == http.StatusUnauthorized || httpErr.Code == http.StatusForbidden) {
		reason := httpErr.Message
		if reason == "" {
			reason = http.StatusText(httpErr.Code)
		}
		return websocket.StatusPolicyViolation, closeReason(reason)
	}
	var redirect *HttpRedirect
	if errors.As(err, &redirect) {
		return websocket.StatusPolicyViolation, http.StatusText(http.StatusUnauthorized)
	}
	log.Printf("mux: live Subscribe failed: %v", err)
	return websocket.StatusInternalError, "server error"
}

// closeReason cuts s to the 123 bytes a close frame has room for, on a rune
// boundary.
func closeReason(s string) string {
	const max = 123
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// pageRequest returns a copy of the handshake hr whose URL names the page at
// path page, query kept, so the page's hooks see the address they see when
// the page loads.
func pageRequest(hr *http.Request, page string) *http.Request {
	u := *hr.URL
	u.Path = page
	if raw, ok := strings.CutSuffix(u.RawPath, liveSuffix); ok && raw != "" {
		u.RawPath = raw
	} else {
		u.RawPath = ""
	}
	pr := hr.WithContext(hr.Context())
	pr.URL = &u
	pr.RequestURI = u.RequestURI()
	return pr
}

// headerHasToken reports whether the comma-separated header name lists
// token, ignoring case.
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// liveConns counts the open live connections of each session.
type liveConns struct {
	max int
	mu  sync.Mutex
	n   map[string]int
}

// acquire takes a place for session, or reports that it has none left.
// Calling release more than once gives the place back once.
func (l *liveConns) acquire(session string) (release func(), ok bool) {
	if l.max <= 0 || session == "" {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n[session] >= l.max {
		return nil, false
	}
	if l.n == nil {
		l.n = map[string]int{}
	}
	l.n[session]++
	return sync.OnceFunc(func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.n[session]--; l.n[session] == 0 {
			delete(l.n, session)
		}
	}), true
}
