package mux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type callFunc func(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error)

// callFake is a route with page calls whose RouteKey is its key; with live
// set it has live values too.
type callFake struct {
	*liveFake
	live bool
	call callFunc
	// keyPanics, when set and true, makes RouteKey panic.
	keyPanics *atomic.Bool
}

func (f *callFake) NewState() RouteState {
	s := &liveFakeState{route: f.liveFake}
	if f.live {
		return &callFakeLive{&liveFakeLive{s}, f}
	}
	return &callFakeState{s, f}
}

func (f *callFake) run(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
	f.ran.add(f.name + ".call:" + name)
	return f.call(ctx, r, name, args)
}

type callFakeState struct {
	*liveFakeState
	f *callFake
}

func (s *callFakeState) RouteKey() string {
	if s.f.keyPanics != nil && s.f.keyPanics.Load() {
		panic("boom in RouteKey")
	}
	return s.f.key
}

func (s *callFakeState) Call(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
	return s.f.run(ctx, r, name, args)
}

type callFakeLive struct {
	*liveFakeLive
	f *callFake
}

func (s *callFakeLive) Call(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
	return s.f.run(ctx, r, name, args)
}

// guardSwitch is what the guarded routes of the call site start returning
// partway through a test.
type guardSwitch struct {
	mu  sync.Mutex
	err error
}

func (d *guardSwitch) set(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

func (d *guardSwitch) get() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

type sessionKey struct{}

// callSite serves /admin (a layout whose Guard refuses with deny) with
// /admin/tool (calls rk000002), /admin/both (calls and live values rk000003)
// and /admin/plain (neither), and /gate (a gate with calls rk000004, guarded
// by deny) above /gate/live (live values rk000005). The host puts "s-1"
// under sessionKey in every request's context.
func callSite(t *testing.T, call callFunc) (srv *httptest.Server, checks, ran *hookLog, deny *guardSwitch) {
	t.Helper()
	captureLiveLog(t)
	checks, ran, deny = &hookLog{}, &hookLog{}, &guardSwitch{}
	fake := func(name string, layout bool, key string) *liveFake {
		return &liveFake{name: name, layout: layout, key: key, checks: checks, ran: ran}
	}
	admin := fake("admin", true, "")
	admin.guardFn = deny.get
	gate := fake("gate", false, "rk000004")
	gate.guardFn = deny.get
	h := newMux(map[string]Route{
		"/":            fake("root", true, ""),
		"/admin":       admin,
		"/admin/tool":  &callFake{liveFake: fake("tool", false, "rk000002"), call: call},
		"/admin/both":  &callFake{liveFake: fake("both", false, "rk000003"), live: true, call: call},
		"/admin/plain": fake("plain", false, ""),
		"/gate":        &callFake{liveFake: gate, call: call},
		"/gate/live":   fake("gatelive", false, "rk000005"),
	}, Options{})
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, "s-1")))
	}))
	t.Cleanup(srv.Close)
	return srv, checks, ran, deny
}

// echo answers "echo" with its arguments, the context's session and the
// request's path; any other name is not on the page.
func echo(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
	if name != "echo" {
		return nil, NewHttpError(http.StatusNotFound, "This call is not on this page.")
	}
	var in struct {
		On bool `json:"on"`
	}
	if err := DecodeArgs(args, &in); err != nil {
		return nil, err
	}
	return map[string]any{"on": in.On, "session": ctx.Value(sessionKey{}), "path": r.URL.Path}, nil
}

// openCalls opens the live connection of page and reads its init frame.
func openCalls(t *testing.T, srv *httptest.Server, page string) *websocket.Conn {
	t.Helper()
	c := mustDial(t, srv, page+"/__ws", nil)
	if f := readFrame(t, c); f.T != "init" {
		t.Fatalf("first frame %+v, want init", f)
	}
	return c
}

func sendCall(t *testing.T, c *websocket.Conn, id int, routeKey, name, args string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg := fmt.Sprintf(`{"t":"call","id":%d,"routeKey":%q,"name":%q,"args":%s}`, id, routeKey, name, args)
	if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("call frame: %v", err)
	}
}

// readAnswer reads the next frame as "result <id> <value>",
// "fail <id> <status> <msg>" or "<t> <routeKey>".
func readAnswer(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var f struct {
		T        string          `json:"t"`
		ID       uint32          `json:"id"`
		Status   int             `json:"status"`
		Msg      string          `json:"msg"`
		Value    json.RawMessage `json:"value"`
		RouteKey string          `json:"routeKey"`
	}
	if err := wsjson.Read(ctx, c, &f); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	switch f.T {
	case "result":
		return fmt.Sprintf("result %d %s", f.ID, f.Value)
	case "fail":
		return fmt.Sprintf("fail %d %d %s", f.ID, f.Status, f.Msg)
	}
	return f.T + " " + f.RouteKey
}

func TestCallReturnsResult(t *testing.T) {
	srv, checks, ran, _ := callSite(t, echo)
	c := openCalls(t, srv, "/admin/tool")
	before := checks.String()

	sendCall(t, c, 7, "rk000002", "echo", `{"on":true}`)
	if got, want := readAnswer(t, c), `result 7 {"on":true,"path":"/admin/tool","session":"s-1"}`; got != want {
		t.Errorf("answer %s, want %s: the call sees the page's path and the host's context", got, want)
	}
	if got := strings.TrimPrefix(checks.String(), before); got != " root.guard admin.guard tool.guard" {
		t.Errorf("hooks of the call: %q, want every Guard and no Data", got)
	}
	if got := ran.String(); got != "tool.call:echo" {
		t.Errorf("calls run: %q", got)
	}
}

func TestCallRechecksGuards(t *testing.T) {
	for _, tc := range []struct {
		name           string
		err            error
		answer, reason string
		log            string
	}{
		{"refused", NewHttpError(http.StatusForbidden, "Signed out."), "fail 2 403 Signed out.", "Signed out.", ""},
		{"server error", NewHttpError(http.StatusServiceUnavailable, "down"), "fail 2 500 Something went wrong.", "Something went wrong.", "503: down"},
		{"other error", errors.New("database down"), "fail 2 500 Something went wrong.", "Something went wrong.", "database down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, checks, ran, deny := callSite(t, echo)
			logged := captureLiveLog(t)
			c := openCalls(t, srv, "/admin/tool")
			sendCall(t, c, 1, "rk000002", "echo", `{}`)
			if got := readAnswer(t, c); !strings.HasPrefix(got, "result 1 ") {
				t.Fatalf("first call answered %s", got)
			}

			deny.set(tc.err)
			before := checks.String()
			sendCall(t, c, 2, "rk000002", "echo", `{}`)
			if got := readAnswer(t, c); got != tc.answer {
				t.Errorf("answer %s, want %s", got, tc.answer)
			}
			if ce := readClose(t, c); ce.Code != websocket.StatusPolicyViolation || ce.Reason != tc.reason {
				t.Errorf("closed with %d %q, want %d %q", ce.Code, ce.Reason, websocket.StatusPolicyViolation, tc.reason)
			}
			if got := strings.TrimPrefix(checks.String(), before); got != " root.guard admin.guard" {
				t.Errorf("hooks of the refused call: %q, want the Guards up to the refusal", got)
			}
			if got := ran.String(); got != "tool.call:echo" {
				t.Errorf("calls run: %q, want only the first", got)
			}
			if got := logged.String(); (tc.log == "") != (got == "") || !strings.Contains(got, tc.log) {
				t.Errorf("logged %q, want %q", got, tc.log)
			}
		})
	}
}

func TestCallGuardRedirectIs401(t *testing.T) {
	srv, _, ran, deny := callSite(t, echo)
	c := openCalls(t, srv, "/admin/tool")
	deny.set(Redirect(http.StatusFound, "/login"))
	sendCall(t, c, 1, "rk000002", "echo", `{}`)
	if got := readAnswer(t, c); got != "fail 1 401 Unauthorized" {
		t.Errorf("answer %s, want fail 1 401 Unauthorized", got)
	}
	if ce := readClose(t, c); ce.Code != websocket.StatusPolicyViolation {
		t.Errorf("closed with %d, want %d", ce.Code, websocket.StatusPolicyViolation)
	}
	if got := ran.String(); got != "" {
		t.Errorf("calls run: %q", got)
	}
}

type panicJSON struct{}

func (panicJSON) MarshalJSON() ([]byte, error) { panic("boom in encoding") }

func TestCallFailures(t *testing.T) {
	srv, _, _, _ := callSite(t, func(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
		switch name {
		case "taken":
			return nil, NewHttpError(http.StatusConflict, "That name is taken.")
		case "unnamed":
			return nil, NewHttpError(http.StatusUnprocessableEntity, "")
		case "unavailable":
			return nil, NewHttpError(http.StatusServiceUnavailable, "Try again later.")
		case "status":
			return nil, NewHttpError(http.StatusOK, "Not an error.")
		case "leak":
			return nil, errors.New("db password is hunter2")
		case "deadline":
			return nil, fmt.Errorf("query: %w", context.DeadlineExceeded)
		case "canceled":
			return nil, fmt.Errorf("query: %w", context.Canceled)
		case "redirect":
			return nil, Redirect(http.StatusFound, "/done")
		case "panic":
			panic("boom in call")
		case "unencodable":
			return func() {}, nil
		case "encoder panic":
			return panicJSON{}, nil
		}
		return echo(ctx, r, name, args)
	})
	logged := captureLiveLog(t)
	c := openCalls(t, srv, "/admin/tool")

	for i, tc := range []struct {
		routeKey, name, args string
		answer, log          string
	}{
		{"rk000002", "taken", `null`, "409 That name is taken.", ""},
		{"rk000002", "unnamed", `null`, "422 Unprocessable Entity", ""},
		{"rk000002", "unavailable", `null`, "503 Try again later.", ""},
		{"rk000002", "panic", `null`, "500 Something went wrong.", "boom in call"},
		{"rk000002", "encoder panic", `null`, "500 Something went wrong.", "boom in encoding"},
		{"rk000002", "status", `null`, "500 Something went wrong.", "200: Not an error."},
		{"rk000002", "leak", `null`, "500 Something went wrong.", "hunter2"},
		{"rk000002", "deadline", `null`, "504 The call took too long.", ""},
		{"rk000002", "canceled", `null`, "500 Something went wrong.", "context canceled"},
		{"rk000002", "redirect", `null`, "500 Something went wrong.", "location.assign"},
		{"rk000002", "unencodable", `null`, "500 Something went wrong.", "unsupported type"},
		{"rk000002", "echo", `{"off":true}`, "400 The call has the wrong arguments.", ""},
		{"rk000002", "echo", `{"on":1}`, "400 The call has the wrong arguments.", ""},
		{"rk000002", "echo", `null`, "400 The call has the wrong arguments.", ""},
		{"rk000002", "nothing", `null`, "404 This call is not on this page.", ""},
		{"rk0000ff", "echo", `{}`, "404 This call is not on this page.", ""},
	} {
		before := logged.String()
		sendCall(t, c, i+1, tc.routeKey, tc.name, tc.args)
		if got, want := readAnswer(t, c), fmt.Sprintf("fail %d %s", i+1, tc.answer); got != want {
			t.Errorf("%s: answer %s, want %s", tc.name, got, want)
		}
		got := strings.TrimPrefix(logged.String(), before)
		if (tc.log == "") != (got == "") || !strings.Contains(got, tc.log) {
			t.Errorf("%s: logged %q, want %q", tc.name, got, tc.log)
		}
	}
	sendCall(t, c, 99, "rk000002", "echo", `{}`)
	if got := readAnswer(t, c); !strings.HasPrefix(got, "result 99 ") {
		t.Errorf("after the failures: %s, want a result on the same connection", got)
	}
}

func TestCallsInFlightAreLimited(t *testing.T) {
	var started atomic.Int32
	release := make(chan struct{})
	srv, _, _, _ := callSite(t, func(_ context.Context, _ *Request, name string, _ json.RawMessage) (any, error) {
		started.Add(1)
		<-release
		return name, nil
	})
	free := sync.OnceFunc(func() { close(release) })
	t.Cleanup(free)
	c := openCalls(t, srv, "/admin/both")

	for id := 41; id <= 44; id++ {
		sendCall(t, c, id, "rk000003", "wait", `null`)
	}
	eventually(t, "four calls running", func() bool { return started.Load() == 4 })
	sendCall(t, c, 45, "rk000003", "wait", `null`)
	if got := readAnswer(t, c); got != "fail 45 429 Too many calls at once." {
		t.Fatalf("fifth call answered %s, want 429", got)
	}
	writeTo(t, c, "rk000003")
	if got := readAnswer(t, c); got != "ack rk000003" {
		t.Errorf("write while calls run answered %s, want its ack", got)
	}

	free()
	var got []string
	for range 4 {
		got = append(got, readAnswer(t, c))
	}
	slices.Sort(got)
	if want := []string{`result 41 "wait"`, `result 42 "wait"`, `result 43 "wait"`, `result 44 "wait"`}; !slices.Equal(got, want) {
		t.Errorf("answers %q, want %q", got, want)
	}
	sendCall(t, c, 46, "rk000003", "wait", `null`)
	if got := readAnswer(t, c); got != `result 46 "wait"` {
		t.Errorf("call after the others returned: %s", got)
	}
}

func TestCallTimeout(t *testing.T) {
	defer func(d time.Duration) { callTimeout = d }(callTimeout)
	callTimeout = 100 * time.Millisecond
	block := make(chan struct{})
	srv, _, _, _ := callSite(t, func(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
		switch name {
		case "watch":
			<-ctx.Done()
			return nil, ctx.Err()
		case "deaf":
			<-block
			return "late", nil
		}
		return echo(ctx, r, name, args)
	})
	t.Cleanup(func() { close(block) })
	logged := captureLiveLog(t)
	c := openCalls(t, srv, "/admin/tool")

	for i, name := range []string{"watch", "deaf"} {
		sendCall(t, c, i+1, "rk000002", name, `null`)
		if got, want := readAnswer(t, c), fmt.Sprintf("fail %d 504 The call took too long.", i+1); got != want {
			t.Errorf("%s: answer %s, want %s", name, got, want)
		}
	}
	if got := logged.String(); !strings.Contains(got, `"deaf" took longer than 100ms`) {
		t.Errorf("logged %q, want the timeout", got)
	}
	sendCall(t, c, 3, "rk000002", "echo", `{}`)
	if got := readAnswer(t, c); !strings.HasPrefix(got, "result 3 ") {
		t.Errorf("after the timeouts: %s, want a result on the same connection", got)
	}
}

func TestCallPanicOutsideTheCall(t *testing.T) {
	logged := captureLiveLog(t)
	checks, ran := &hookLog{}, &hookLog{}
	keyPanics := &atomic.Bool{}
	srv := httptest.NewServer(newMux(map[string]Route{
		"/": &callFake{liveFake: &liveFake{name: "tool", key: "rk000002", checks: checks, ran: ran}, call: echo, keyPanics: keyPanics},
	}, Options{}))
	t.Cleanup(srv.Close)
	c := mustDial(t, srv, "/__ws", nil)
	readFrame(t, c)

	keyPanics.Store(true)
	for id := 1; id <= maxCallsInFlight+1; id++ {
		sendCall(t, c, id, "rk000002", "echo", `{}`)
		if got, want := readAnswer(t, c), fmt.Sprintf("fail %d 500 Something went wrong.", id); got != want {
			t.Fatalf("answer %s, want %s, each call with its place back", got, want)
		}
	}
	if got := logged.String(); strings.Count(got, "boom in RouteKey") != maxCallsInFlight+1 {
		t.Errorf("logged %q, want each panic", got)
	}
	keyPanics.Store(false)
	sendCall(t, c, 9, "rk000002", "echo", `{}`)
	if got := readAnswer(t, c); !strings.HasPrefix(got, "result 9 ") {
		t.Errorf("after the panics: %s, want a result on the same connection", got)
	}
	if got := ran.String(); got != "tool.call:echo" {
		t.Errorf("calls run: %q", got)
	}
}

func TestCallIgnoresOtherFrames(t *testing.T) {
	srv, _, ran, _ := callSite(t, echo)
	logged := captureLiveLog(t)
	c := openCalls(t, srv, "/admin/tool")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, f := range []string{
		`{"t":"call","id":0,"routeKey":"rk000002","name":"echo","args":{}}`,
		`{"t":"call","routeKey":"rk000002","name":"echo","args":{}}`,
		`{"t":"hello","v":2}`,
		`{"t":"bye","secret":"s3cr3t"}`,
	} {
		if err := c.Write(ctx, websocket.MessageText, []byte(f)); err != nil {
			t.Fatal(err)
		}
	}
	for id := 1; id <= 2; id++ {
		sendCall(t, c, id, "rk000002", "echo", `{}`)
		if got := readAnswer(t, c); !strings.HasPrefix(got, fmt.Sprintf("result %d ", id)) {
			t.Fatalf("answer %s, want result %d and nothing for the other frames", got, id)
		}
	}
	if got := ran.String(); got != "tool.call:echo tool.call:echo" {
		t.Errorf("calls run: %q, want only the two with an id", got)
	}
	if got, want := logged.String(), `reactive: ignoring frames of unknown type "hello"`; strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, want+"\n") {
		t.Errorf("logged %q, want one line %q and nothing of the frame", got, want)
	}
}

func TestLiveNeedsValuesOrCalls(t *testing.T) {
	srv, checks, _, _ := callSite(t, echo)
	c := mustDial(t, srv, "/admin/tool/__ws", nil)
	if f := readFrame(t, c); f.T != "init" || f.RouteKey != "rk000002" || f.Bindings == nil || len(f.Bindings) != 0 {
		t.Errorf("first frame of a page with calls only: %+v, want init for rk000002 with no bindings", f)
	}

	before := checks.String()
	wantRefused(t, srv, "/admin/plain/__ws", nil, http.StatusNotFound)
	if got := strings.TrimPrefix(checks.String(), before); got != " root.guard admin.guard plain.guard" {
		t.Errorf("hooks before the 404: %q, want the Guards and no Data", got)
	}
}

func TestCallsBelowGate(t *testing.T) {
	srv, checks, ran, deny := callSite(t, echo)
	c := openCalls(t, srv, "/gate/live")
	sendCall(t, c, 1, "rk000004", "echo", `{}`)
	if got := readAnswer(t, c); got != "fail 1 404 This call is not on this page." {
		t.Errorf("the gate's call from the page below: %s, want 404", got)
	}

	deny.set(NotFound())
	before := checks.String()
	sendCall(t, c, 2, "rk000004", "echo", `{}`)
	if got := readAnswer(t, c); got != "fail 2 404 Not Found" {
		t.Errorf("answer %s, want the gate's refusal", got)
	}
	if ce := readClose(t, c); ce.Code != websocket.StatusPolicyViolation {
		t.Errorf("closed with %d, want %d", ce.Code, websocket.StatusPolicyViolation)
	}
	if got := strings.TrimPrefix(checks.String(), before); got != " root.guard gate.guard" {
		t.Errorf("hooks: %q, want the gate's Guard to run again", got)
	}
	if got := ran.String(); strings.Contains(got, "call") {
		t.Errorf("calls run: %q", got)
	}
}

func TestDecodeArgs(t *testing.T) {
	type in struct {
		On   bool     `json:"on"`
		Tags []string `json:"tags"`
	}
	for raw, want := range map[string]in{
		`{"on":true,"tags":["a"]}`: {On: true, Tags: []string{"a"}},
		` {"on":true} `:            {On: true},
		`{}`:                       {},
	} {
		var v in
		if err := DecodeArgs(json.RawMessage(raw), &v); err != nil || v.On != want.On || !slices.Equal(v.Tags, want.Tags) {
			t.Errorf("DecodeArgs(%s) = %+v, %v; want %+v", raw, v, err, want)
		}
	}
	for _, raw := range []string{``, `{"off":true}`, `{"on":1}`, `{"on":true} {}`, `{"on":true}]`, `{"on":true} x`, `[`, `"on"`, `null`, ` null `} {
		wantBadArgs(t, raw, DecodeArgs(json.RawMessage(raw), new(in)))
	}
	for _, v := range []any{new(int), new(string), new(bool), new([2]int)} {
		wantBadArgs(t, fmt.Sprintf("null into %T", v), DecodeArgs(json.RawMessage(`null`), v))
	}

	// null is no value for a pointer, slice, map or interface, and an empty
	// struct has no fields it could leave unset.
	p, sl, m, a, e := new(in), []int{1}, map[string]int{"a": 1}, any(1), struct{}{}
	for _, v := range []any{&p, &sl, &m, &a, &e} {
		if err := DecodeArgs(json.RawMessage(` null`), v); err != nil {
			t.Errorf("DecodeArgs(null, %T) = %v, want nil", v, err)
		}
	}
	if p != nil || sl != nil || m != nil || a != nil {
		t.Errorf("after null: %v %v %v %v, want each nil", p, sl, m, a)
	}
}

func wantBadArgs(t *testing.T, what string, err error) {
	t.Helper()
	var httpErr *HttpError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest || httpErr.Message != "The call has the wrong arguments." {
		t.Errorf("DecodeArgs(%s) = %v, want 400 The call has the wrong arguments.", what, err)
	}
}

func TestClosedConnectionFreesItsPlace(t *testing.T) {
	logged := captureLiveLog(t)
	ran := &hookLog{}
	block, returned := make(chan struct{}), make(chan struct{})
	free := sync.OnceFunc(func() { close(block) })
	t.Cleanup(free)
	call := func(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error) {
		if name == "deaf" {
			defer close(returned)
			<-block
			return "late", nil
		}
		return echo(ctx, r, name, args)
	}
	srv := httptest.NewServer(newMux(map[string]Route{
		"/": &callFake{liveFake: &liveFake{name: "tool", key: "rk000002", checks: &hookLog{}, ran: ran}, call: call},
	}, Options{MaxLiveConnsPerSession: 1, CSRFSession: func(*Request) string { return "s1" }}))
	t.Cleanup(srv.Close)
	first := mustDial(t, srv, "/__ws", nil)
	readFrame(t, first)
	sendCall(t, first, 1, "rk000002", "deaf", `null`)
	eventually(t, "the call running", func() bool { return ran.String() == "tool.call:deaf" })

	if err := first.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	var second *websocket.Conn
	eventually(t, "the place of the closed connection", func() bool {
		second, _, _ = dialLive(t, srv, "/__ws", nil)
		return second != nil
	})
	readFrame(t, second)

	free()
	<-returned
	sendCall(t, second, 2, "rk000002", "echo", `{}`)
	if got := readAnswer(t, second); !strings.HasPrefix(got, "result 2 ") {
		t.Errorf("answer %s, want a result on the new connection", got)
	}
	wantRefused(t, srv, "/__ws", nil, http.StatusTooManyRequests)
	if got := logged.String(); got != "" {
		t.Errorf("logged %q, want nothing from the late call", got)
	}
}

// TestCallOfAClosedConnectionLogsNothing closes a connection while its call
// waits on its context: the call's cancellation is not logged.
func TestCallOfAClosedConnectionLogsNothing(t *testing.T) {
	returned := make(chan struct{})
	srv, _, ran, _ := callSite(t, func(ctx context.Context, _ *Request, _ string, _ json.RawMessage) (any, error) {
		defer close(returned)
		<-ctx.Done()
		return nil, fmt.Errorf("query: %w", ctx.Err())
	})
	logged := captureLiveLog(t)
	c := openCalls(t, srv, "/admin/tool")
	sendCall(t, c, 1, "rk000002", "watch", `null`)
	eventually(t, "the call running", func() bool { return strings.Contains(ran.String(), "tool.call:watch") })
	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("the call did not see its connection end")
	}
	time.Sleep(100 * time.Millisecond)
	if got := logged.String(); got != "" {
		t.Errorf("logged %q, want nothing", got)
	}
}

func TestCallNamesAreCutInLogs(t *testing.T) {
	defer func(d time.Duration) { callTimeout = d }(callTimeout)
	callTimeout = 100 * time.Millisecond
	logged := captureLiveLog(t)
	var mode atomic.Value
	keyPanics := &atomic.Bool{}
	call := func(ctx context.Context, _ *Request, _ string, _ json.RawMessage) (any, error) {
		switch mode.Load() {
		case "error":
			return nil, errors.New("db down")
		case "redirect":
			return nil, Redirect(http.StatusFound, "/done")
		case "panic":
			panic("boom")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	srv := httptest.NewServer(newMux(map[string]Route{
		"/": &callFake{liveFake: &liveFake{name: "tool", key: "rk000002", checks: &hookLog{}, ran: &hookLog{}}, call: call, keyPanics: keyPanics},
	}, Options{}))
	t.Cleanup(srv.Close)
	c := openCalls(t, srv, "")
	name := strings.Repeat("a", 30) + "\nforged: mux: page call ok"

	for i, m := range []string{"error", "redirect", "panic", "timeout", "key panic"} {
		mode.Store(m)
		keyPanics.Store(m == "key panic")
		before := logged.String()
		sendCall(t, c, i+1, "rk000002", name, `null`)
		readAnswer(t, c)
		got := strings.TrimPrefix(logged.String(), before)
		if strings.Count(got, "\n") != 1 || strings.Contains(got, "forged") || !strings.Contains(got, `"`+strings.Repeat("a", 30)+`\nf"`) {
			t.Errorf("%s: logged %q, want one line with the name quoted and cut to 32 bytes", m, got)
		}
	}
}
