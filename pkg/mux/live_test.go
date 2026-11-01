package mux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/sergei-svistunov/go-ssr/pkg/reactive"
)

// hookLog is a log of "<route>.<hook>" entries written by the server's
// goroutines and read by the test.
type hookLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *hookLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
}

func (l *hookLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.entries, " ")
}

// syncBuffer collects log output written by the server's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLiveLog collects the log for the rest of the test.
func captureLiveLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return buf
}

// liveFake is a route of the live test site. Guard and Data go to the site's
// checks log, Subscribe and HandleWrite to its live log. With a key the route
// has one live value, "<key>.v", whose value is the route's name.
type liveFake struct {
	name   string
	layout bool
	key    string
	fail   map[string]error // "guard" or "data" → the error that hook returns
	// guardFn, when set, decides what Guard returns.
	guardFn func() error
	// subscribe replaces the default Subscribe, which waits for the end of
	// the connection.
	subscribe func(ctx context.Context) error
	// see is called with every hook's Request.
	see         func(r *Request)
	checks, ran *hookLog
}

func (f *liveFake) NewState() RouteState {
	s := &liveFakeState{route: f}
	if f.key == "" {
		return s
	}
	return &liveFakeLive{s}
}

func (f *liveFake) Layout() bool { return f.layout }

type liveFakeState struct {
	Frame
	route *liveFake
}

func (s *liveFakeState) hook(name string, r *Request) error {
	if s.route.see != nil {
		s.route.see(r)
	}
	s.route.checks.add(s.route.name + "." + name)
	if name == "guard" && s.route.guardFn != nil {
		return s.route.guardFn()
	}
	return s.route.fail[name]
}

func (s *liveFakeState) Guard(_ context.Context, r *Request) error { return s.hook("guard", r) }

func (s *liveFakeState) InitForms(context.Context, *Request, ResponseWriter) error { return nil }

func (s *liveFakeState) SubmitForm(context.Context, *Request, ResponseWriter, string) (bool, error) {
	return false, nil
}

func (s *liveFakeState) Data(_ context.Context, r *Request, _ ResponseWriter) error {
	return s.hook("data", r)
}

func (s *liveFakeState) DefaultRoute(context.Context, *Request) (string, error) { return "", nil }

func (s *liveFakeState) Write(io.Writer) error { return nil }

type liveFakeLive struct{ *liveFakeState }

func (l *liveFakeLive) RouteKey() string { return l.route.key }

func (l *liveFakeLive) Snapshot() map[string]string {
	return map[string]string{l.route.key + ".v": l.route.name}
}

func (l *liveFakeLive) Subscribe(ctx context.Context, r *Request, _ *reactive.Conn) error {
	if l.route.see != nil {
		l.route.see(r)
	}
	l.route.ran.add(l.route.name + ".subscribe")
	if l.route.subscribe != nil {
		return l.route.subscribe(ctx)
	}
	<-ctx.Done()
	return nil
}

func (l *liveFakeLive) HandleWrite(_ context.Context, _ *Request, conn *reactive.Conn, msg reactive.WriteMsg) {
	l.route.ran.add(l.route.name + ".write")
	_ = conn.SendJSON(reactive.NewAckMsg(msg.RouteKey, msg.Var))
}

// liveSite serves the live test site through wrap (nil serves the mux
// itself); change adjusts its routes before the mux is made. It returns the
// server and the checks and live logs.
func liveSite(t *testing.T, opts Options, change func(map[string]*liveFake), wrap func(http.Handler) http.Handler) (*httptest.Server, *hookLog, *hookLog) {
	t.Helper()
	captureLiveLog(t)
	checks, ran := &hookLog{}, &hookLog{}
	routes := map[string]*liveFake{
		"/":                           {name: "root", layout: true},
		"/home":                       {name: "home"},
		"/admin":                      {name: "admin", layout: true},
		"/admin/live":                 {name: "live", key: "rk00000a"},
		"/users/_id_/live":            {name: "user", key: "rk00000c"},
		"/orgs/_org_/users/_id_/live": {name: "member", key: "rk00000d"},
	}
	if change != nil {
		change(routes)
	}
	m := map[string]Route{}
	for p, r := range routes {
		r.checks, r.ran = checks, ran
		m[p] = r
	}
	var h http.Handler = newMux(m, opts)
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, checks, ran
}

// dialLive opens the live connection at path with the given headers.
func dialLive(t *testing.T, srv *httptest.Server, path string, header http.Header) (*websocket.Conn, *refusal, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, &websocket.DialOptions{HTTPHeader: header})
	if c != nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, refusalOf(resp), err
}

// refusal is the status and header of the response to a dial.
type refusal struct {
	StatusCode int
	Header     http.Header
}

// refusalOf reads resp, which is nil when the dial got no response, and
// closes its body.
func refusalOf(resp *http.Response) *refusal {
	if resp == nil {
		return nil
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return &refusal{StatusCode: resp.StatusCode, Header: resp.Header}
}

// mustDial opens the live connection at path and fails the test if it is
// refused.
func mustDial(t *testing.T, srv *httptest.Server, path string, header http.Header) *websocket.Conn {
	t.Helper()
	c, resp, err := dialLive(t, srv, path, header)
	if err != nil {
		t.Fatalf("dial %s: %v (response %v)", path, err, resp)
	}
	return c
}

// wantRefused checks that dialling path is refused with status before the
// upgrade.
func wantRefused(t *testing.T, srv *httptest.Server, path string, header http.Header, status int) *refusal {
	t.Helper()
	c, resp, err := dialLive(t, srv, path, header)
	if err == nil || c != nil {
		t.Fatalf("dial %s upgraded, want %d", path, status)
	}
	if resp == nil || resp.StatusCode != status {
		t.Fatalf("dial %s: response %v, want %d", path, resp, status)
	}
	return resp
}

type frame struct {
	T        string            `json:"t"`
	RouteKey string            `json:"routeKey"`
	Code     string            `json:"code"`
	Bindings map[string]string `json:"bindings"`
}

func readFrame(t *testing.T, c *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var f frame
	if err := wsjson.Read(ctx, c, &f); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return f
}

func writeTo(t *testing.T, c *websocket.Conn, routeKey string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, reactive.WriteMsg{T: "write", RouteKey: routeKey, Var: "v", Value: []byte("1")}); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// readClose reads until the server closes the connection and returns its
// close frame.
func readClose(t *testing.T, c *websocket.Conn) websocket.CloseError {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		if !errors.As(err, &ce) {
			t.Fatalf("connection ended with %v, want a close frame", err)
		}
		return ce
	}
}

// eventually waits up to a second for cond.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestLiveSendsInitAfterChecks(t *testing.T) {
	srv, checks, ran := liveSite(t, Options{}, func(r map[string]*liveFake) { r["/"].key = "rk000001" }, nil)
	c := mustDial(t, srv, "/admin/live/__ws", nil)

	f := readFrame(t, c)
	want := map[string]string{"rk000001.v": "root", "rk00000a.v": "live"}
	if f.T != "init" || f.RouteKey != "rk00000a" || !maps.Equal(f.Bindings, want) {
		t.Fatalf("first frame = %+v, want init for rk00000a with %v", f, want)
	}
	if got := checks.String(); got != "root.guard admin.guard live.guard root.data admin.data live.data" {
		t.Errorf("hooks before init: %q", got)
	}

	// Both live routes share the connection: each gets its own writes.
	for _, key := range []string{"rk000001", "rk00000a"} {
		writeTo(t, c, key)
		if f := readFrame(t, c); f.T != "ack" || f.RouteKey != key {
			t.Errorf("write to %s answered %+v, want its ack", key, f)
		}
	}
	eventually(t, "both Subscribes", func() bool {
		s := ran.String()
		return strings.Contains(s, "root.subscribe") && strings.Contains(s, "live.subscribe")
	})
}

func TestLiveHooksSeePagePath(t *testing.T) {
	for _, tc := range []struct {
		route, name, address, want string
	}{
		{"/users/_id_/live", "user", "/users/7/live/__ws?tab=a", "/users/7/live?tab=a map[id:7]"},
		{"/orgs/_org_/users/_id_/live", "member", "/orgs/o7/users/u123/live/__ws", "/orgs/o7/users/u123/live? map[id:u123 org:o7]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var seen []string
			srv, _, ran := liveSite(t, Options{InsecureCookies: true, OriginPatterns: []string{"dev.example"}}, func(r map[string]*liveFake) {
				r[tc.route].see = func(r *Request) {
					mu.Lock()
					defer mu.Unlock()
					seen = append(seen, fmt.Sprintf("%s?%s %v cookies=%t patterns=%v",
						r.URL.Path, r.URL.RawQuery, r.URLParams, r.InsecureCookies, r.OriginPatterns))
				}
			}, nil)
			c := mustDial(t, srv, tc.address, nil)
			readFrame(t, c)
			eventually(t, "Subscribe", func() bool { return strings.Contains(ran.String(), tc.name+".subscribe") })

			mu.Lock()
			defer mu.Unlock()
			if len(seen) != 3 {
				t.Fatalf("hooks seen: %q, want Guard, Data and Subscribe", seen)
			}
			for _, s := range seen {
				if want := tc.want + " cookies=true patterns=[dev.example]"; s != want {
					t.Errorf("a hook saw %q, want %q: the page's path, query, parameters and options", s, want)
				}
			}
		})
	}
}

func TestLiveUnderDeniedParentIsRefused(t *testing.T) {
	denied := NewHttpError(http.StatusForbidden, "no")
	for _, tc := range []struct {
		name, path, hook string
		status           int
		log              string
	}{
		{"guard", "/admin/live/__ws", "guard", http.StatusForbidden, "root.guard admin.guard"},
		{"data", "/admin/live/__ws", "data", http.StatusForbidden, "root.guard admin.guard live.guard root.data admin.data"},
		{"layout", "/admin/__ws", "", http.StatusNotFound, "root.guard admin.guard"},
		{"denied layout", "/admin/__ws", "guard", http.StatusForbidden, "root.guard admin.guard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, checks, ran := liveSite(t, Options{}, func(r map[string]*liveFake) {
				r["/admin"].fail = map[string]error{tc.hook: denied}
			}, nil)
			wantRefused(t, srv, tc.path, nil, tc.status)
			if got := checks.String(); got != tc.log {
				t.Errorf("hooks: %q, want %q", got, tc.log)
			}
			if got := ran.String(); got != "" {
				t.Errorf("live hooks ran: %q", got)
			}
		})
	}
}

func TestLiveBelowGateRunsItsGuard(t *testing.T) {
	srv, checks, _ := liveSite(t, Options{}, func(r map[string]*liveFake) {
		r["/admin"].layout = false
		r["/admin"].fail = map[string]error{"guard": NotFound()}
	}, nil)
	wantRefused(t, srv, "/admin/live/__ws", nil, http.StatusNotFound)
	if got := checks.String(); got != "root.guard admin.guard" {
		t.Errorf("hooks: %q, want root.guard admin.guard", got)
	}
}

func TestLiveBelowGate(t *testing.T) {
	gate := func(r map[string]*liveFake) {
		r["/admin"].layout = false
		r["/admin"].key = "rk00000b"
	}

	t.Run("page below", func(t *testing.T) {
		srv, checks, ran := liveSite(t, Options{}, gate, nil)
		c := mustDial(t, srv, "/admin/live/__ws", nil)
		if f := readFrame(t, c); f.T != "init" || !maps.Equal(f.Bindings, map[string]string{"rk00000a.v": "live"}) {
			t.Fatalf("init = %+v, want only rk00000a's bindings", f)
		}
		if got := checks.String(); got != "root.guard admin.guard live.guard root.data live.data" {
			t.Errorf("hooks: %q", got)
		}
		writeTo(t, c, "rk00000b")
		if f := readFrame(t, c); f.T != "err" || f.Code != "unknown_route" {
			t.Errorf("write to the gate answered %+v, want an unknown_route err", f)
		}
		writeTo(t, c, "rk00000a")
		if f := readFrame(t, c); f.T != "ack" {
			t.Errorf("write to the page answered %+v, want an ack", f)
		}
		eventually(t, "the page's Subscribe", func() bool { return strings.Contains(ran.String(), "live.subscribe") })
		if got := ran.String(); strings.Contains(got, "admin.") {
			t.Errorf("the gate's live hooks ran below it: %q", got)
		}
	})

	t.Run("gate itself", func(t *testing.T) {
		srv, _, _ := liveSite(t, Options{}, gate, nil)
		c := mustDial(t, srv, "/admin/__ws", nil)
		if f := readFrame(t, c); f.T != "init" || !maps.Equal(f.Bindings, map[string]string{"rk00000b.v": "admin"}) {
			t.Fatalf("init = %+v, want the gate's bindings", f)
		}
	})
}

func TestLiveContextCarriesRequestValues(t *testing.T) {
	type key struct{}
	got := make(chan any, 1)
	done := make(chan struct{})
	srv, _, _ := liveSite(t, Options{}, func(r map[string]*liveFake) {
		r["/admin/live"].subscribe = func(ctx context.Context) error {
			got <- ctx.Value(key{})
			<-ctx.Done()
			close(done)
			return nil
		}
	}, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, "session-1")))
		})
	})
	c := mustDial(t, srv, "/admin/live/__ws", nil)
	readFrame(t, c)
	if v := <-got; v != "session-1" {
		t.Fatalf("Subscribe's context holds %v, want the value the host put in the request", v)
	}
	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Subscribe's context was not done within 1s of the client closing")
	}
}

func TestLiveGuardRedirectIs401(t *testing.T) {
	srv, checks, _ := liveSite(t, Options{}, func(r map[string]*liveFake) {
		r["/"].fail = map[string]error{"guard": Redirect(http.StatusFound, "/login")}
	}, nil)
	resp := wantRefused(t, srv, "/admin/live/__ws", nil, http.StatusUnauthorized)
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Errorf("Location = %q, want none", loc)
	}
	if got := checks.String(); got != "root.guard" {
		t.Errorf("hooks: %q, want root.guard", got)
	}
}

func TestLiveRefusesOtherOrigins(t *testing.T) {
	allow := func(r *Request) bool { return r.URL.Path == "/users/42/live/__ws" && r.URLParam("id") == "42" }
	for _, tc := range []struct {
		name    string
		opts    Options
		path    string
		header  http.Header
		refused bool
	}{
		{"other origin", Options{}, "/admin/live/__ws", http.Header{"Origin": {"https://evil.example"}}, true},
		{"opaque origin", Options{}, "/admin/live/__ws", http.Header{"Origin": {"null"}}, true},
		{"browser says cross-site", Options{}, "/admin/live/__ws", http.Header{"Origin": {"https://evil.example"}, "Sec-Fetch-Site": {"cross-site"}}, true},
		{"unlisted origin", Options{OriginPatterns: []string{"dev.example:*"}}, "/admin/live/__ws", http.Header{"Origin": {"http://evil.example"}}, true},
		{"listed origin", Options{OriginPatterns: []string{"dev.example:*"}}, "/admin/live/__ws", http.Header{"Origin": {"http://dev.example:3000"}}, false},
		{"browser says same-origin", Options{}, "/admin/live/__ws", http.Header{"Origin": {"https://app.example"}, "Sec-Fetch-Site": {"same-origin"}}, false},
		{"exempt address", Options{AllowCrossSite: allow}, "/users/42/live/__ws", http.Header{"Origin": {"https://partner.example"}}, false},
		{"not exempt", Options{AllowCrossSite: allow}, "/admin/live/__ws", http.Header{"Origin": {"https://partner.example"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, checks, _ := liveSite(t, tc.opts, nil, nil)
			if tc.refused {
				wantRefused(t, srv, tc.path, tc.header, http.StatusForbidden)
				if got := checks.String(); got != "" {
					t.Errorf("hooks ran before the refusal: %q", got)
				}
				return
			}
			if f := readFrame(t, mustDial(t, srv, tc.path, tc.header)); f.T != "init" {
				t.Errorf("first frame %+v, want init", f)
			}
		})
	}
}

func TestLiveAcceptsTheHostWithItsDefaultPort(t *testing.T) {
	srv, _, _ := liveSite(t, Options{}, nil, nil)
	c, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/admin/live/__ws", &websocket.DialOptions{
		Host:       "app.example:443",
		HTTPHeader: http.Header{"Origin": {"https://app.example"}},
	})
	r := refusalOf(resp)
	if err != nil {
		t.Fatalf("dial: %v (response %v)", err, r)
	}
	_ = c.CloseNow()
}

func TestLiveAddressNeedsUpgrade(t *testing.T) {
	srv, checks, _ := liveSite(t, Options{}, nil, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+"/admin/live/__ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s without Upgrade: %d, want 400", method, resp.StatusCode)
		}
	}
	if got := checks.String(); got != "" {
		t.Errorf("hooks ran: %q", got)
	}
}

func TestLiveUpgradeTokenIsCaseInsensitive(t *testing.T) {
	captureLiveLog(t)
	checks := &hookLog{}
	m := newMux(map[string]Route{"/": &liveFake{name: "root", key: "rk000001", checks: checks, ran: &hookLog{}}}, Options{})
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/__ws", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "WebSocket")
	m.ServeHTTP(httptest.NewRecorder(), r)
	if got := checks.String(); got != "root.guard root.data" {
		t.Errorf("hooks: %q; Upgrade is a case-insensitive token", got)
	}
}

func TestLiveNotFound(t *testing.T) {
	srv, checks, _ := liveSite(t, Options{}, nil, nil)
	wantRefused(t, srv, "/home/__ws", nil, http.StatusNotFound)
	if got := checks.String(); got != "root.guard home.guard" {
		t.Errorf("hooks: %q, want the Guards and no Data before the 404", got)
	}
	wantRefused(t, srv, "/nowhere/__ws", nil, http.StatusNotFound)
}

func TestLiveConnectionsExpire(t *testing.T) {
	defer func(age time.Duration) { liveMaxAge = age }(liveMaxAge)
	liveMaxAge = 200 * time.Millisecond
	srv, _, _ := liveSite(t, Options{}, nil, nil)
	c := mustDial(t, srv, "/admin/live/__ws", nil)
	readFrame(t, c)
	if ce := readClose(t, c); ce.Code != reactive.StatusReauth {
		t.Errorf("closed with %d, want %d", ce.Code, reactive.StatusReauth)
	}
}

func TestLiveConnectionsPerSession(t *testing.T) {
	srv, checks, _ := liveSite(t, Options{
		MaxLiveConnsPerSession: 2,
		CSRFSession:            func(r *Request) string { return r.Header.Get("Session") },
	}, nil, nil)
	s1 := http.Header{"Session": {"s1"}}
	first := mustDial(t, srv, "/admin/live/__ws", s1)
	readFrame(t, first)
	readFrame(t, mustDial(t, srv, "/admin/live/__ws", s1))
	before := checks.String()
	wantRefused(t, srv, "/admin/live/__ws", s1, http.StatusTooManyRequests)
	if got := strings.TrimPrefix(checks.String(), before); got != " root.guard admin.guard live.guard" {
		t.Errorf("hooks of the refused handshake: %q, want the Guards and no Data", got)
	}
	readFrame(t, mustDial(t, srv, "/admin/live/__ws", http.Header{"Session": {"s2"}}))
	readFrame(t, mustDial(t, srv, "/admin/live/__ws", nil))

	if err := first.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a free place", func() bool {
		c, _, err := dialLive(t, srv, "/admin/live/__ws", s1)
		return err == nil && c != nil
	})
}

func TestDeafSubscribeFreesItsPlace(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(sync.OnceFunc(func() { close(block) }))
	srv, _, _ := liveSite(t, Options{
		MaxLiveConnsPerSession: 1,
		CSRFSession:            func(*Request) string { return "s1" },
	}, func(r map[string]*liveFake) {
		r["/admin/live"].subscribe = func(context.Context) error {
			<-block
			return nil
		}
	}, nil)
	first := mustDial(t, srv, "/admin/live/__ws", nil)
	readFrame(t, first)
	if err := first.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the place of the closed connection", func() bool {
		c, _, _ := dialLive(t, srv, "/admin/live/__ws", nil)
		return c != nil
	})
}

func TestSubscribeRefusalCloses1008(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   websocket.StatusCode
		reason string
		log    string // logged by a server error
	}{
		{"refused", NewHttpError(http.StatusUnauthorized, "sign in again"), websocket.StatusPolicyViolation, "sign in again", ""},
		{"forbidden", NewHttpError(http.StatusForbidden, ""), websocket.StatusPolicyViolation, "Forbidden", ""},
		{"redirect", Redirect(http.StatusFound, "/login"), websocket.StatusPolicyViolation, "Unauthorized", ""},
		{"other error", errors.New("database down"), websocket.StatusInternalError, "", "mux: live Subscribe failed: database down"},
		{"other status", NewHttpError(http.StatusNotFound, "gone"), websocket.StatusInternalError, "", "mux: live Subscribe failed: 404: gone"},
		{"panic", nil, websocket.StatusInternalError, "", "mux: panic on a live connection: boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := liveSite(t, Options{}, func(r map[string]*liveFake) {
				r["/admin/live"].subscribe = func(context.Context) error {
					if tc.err == nil {
						panic("boom")
					}
					return tc.err
				}
			}, nil)
			logged := captureLiveLog(t)
			c := mustDial(t, srv, "/admin/live/__ws", nil)
			readFrame(t, c)
			ce := readClose(t, c)
			if got := logged.String(); (tc.log == "") != (got == "") || !strings.Contains(got, tc.log) {
				t.Errorf("logged %q, want %q", got, tc.log)
			}
			if ce.Code != tc.code || (tc.reason != "" && ce.Reason != tc.reason) {
				t.Errorf("closed with %d %q, want %d %q", ce.Code, ce.Reason, tc.code, tc.reason)
			}
			if tc.code == websocket.StatusInternalError && strings.Contains(ce.Reason, "database") {
				t.Errorf("the close reason leaks the error: %q", ce.Reason)
			}
		})
	}
}
