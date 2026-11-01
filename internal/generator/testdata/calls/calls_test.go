package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"example.com/app/model"
	"example.com/app/web/pages"
	"example.com/app/web/pages/ext"
	"example.com/app/web/pages/page"
	"example.com/app/web/pages/plain"
	"example.com/app/web/pages/stub"
	"github.com/sergei-svistunov/go-ssr/pkg/mux"
)

const (
	badArgs  = "The call has the wrong arguments."
	notFound = "This call is not on this page."
)

func routeKey(path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(path)))[:8]
}

type answer struct {
	T      string          `json:"t"`
	ID     uint32          `json:"id"`
	Value  json.RawMessage `json:"value"`
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
}

// dial opens the live connection of the page at path and reads its init frame.
func dial(t *testing.T, path string) *websocket.Conn {
	t.Helper()
	h, err := pages.NewSsrHandler(mux.Options{CSRFKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path+"/__ws", nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	var init struct{ T string }
	if err := wsjson.Read(ctx, conn, &init); err != nil || init.T != "init" {
		t.Fatalf("init frame: %+v %v", init, err)
	}
	return conn
}

var lastID uint32

// call sends one call frame and returns the frame that answers it.
func call(t *testing.T, conn *websocket.Conn, path, name, args string) answer {
	t.Helper()
	lastID++
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frame := fmt.Sprintf(`{"t":"call","id":%d,"routeKey":%q,"name":%q,"args":%s}`, lastID, routeKey(path), name, args)
	if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatal(err)
	}
	var a answer
	if err := wsjson.Read(ctx, conn, &a); err != nil {
		t.Fatalf("answer to %s: %v", name, err)
	}
	if a.ID != lastID {
		t.Fatalf("answer %+v to call %d", a, lastID)
	}
	return a
}

func wantResult(t *testing.T, a answer, want string) {
	t.Helper()
	if a.T != "result" || string(a.Value) != want {
		t.Errorf("got %+v (value %s), want result %s", a, a.Value, want)
	}
}

func wantFail(t *testing.T, a answer, status int, msg string) {
	t.Helper()
	if a.T != "fail" || a.Status != status || a.Msg != msg {
		t.Errorf("got %+v, want fail %d %q", a, status, msg)
	}
}

func TestCallsThroughTheMux(t *testing.T) {
	conn := dial(t, "/page")
	wantResult(t, call(t, conn, "/", "ping", `{"text":"hi"}`), `{"echo":"hi","path":"/page"}`)
	wantResult(t, call(t, conn, "/page", "add", `{"a":1,"b":2}`), `3`)
	wantFail(t, call(t, conn, "/page", "add", `null`), http.StatusBadRequest, badArgs)
	wantFail(t, call(t, conn, "/page", "add", `{"a":1,"c":2}`), http.StatusBadRequest, badArgs)
	wantFail(t, call(t, conn, "/page", "add", `{"a":"1"}`), http.StatusBadRequest, badArgs)
	wantFail(t, call(t, conn, "/page", "nope", `null`), http.StatusNotFound, notFound)
	wantFail(t, call(t, conn, "/page", "secret", `null`), http.StatusNotFound, notFound)
	wantFail(t, call(t, conn, "/page", "ping", `{"text":"hi"}`), http.StatusNotFound, notFound)
	wantFail(t, call(t, conn, "/", "add", `{"a":1,"b":2}`), http.StatusNotFound, notFound)

	// A page without calls of its own connects for the calls of its layout.
	conn = dial(t, "/plain")
	wantResult(t, call(t, conn, "/", "ping", `{"text":"x"}`), `{"echo":"x","path":"/plain"}`)

	conn = dial(t, "/stub")
	wantFail(t, call(t, conn, "/stub", "peek", `{}`), http.StatusForbidden, "Forbidden")
	wantFail(t, call(t, conn, "/stub", "peek", `null`), http.StatusForbidden, "Forbidden")
}

func callState(t *testing.T, st mux.RouteState, name, args string) (any, error) {
	t.Helper()
	c, ok := st.(mux.Caller)
	if !ok {
		t.Fatalf("%T is not a mux.Caller", st)
	}
	return c.Call(context.Background(), mux.NewRequest(httptest.NewRequest(http.MethodGet, "/page", nil)), name, json.RawMessage(args))
}

func wantHTTPError(t *testing.T, err error, code int, msg string) {
	t.Helper()
	var he *mux.HttpError
	if !errors.As(err, &he) || he.Code != code || he.Message != msg {
		t.Errorf("error %v, want %d %q", err, code, msg)
	}
}

func TestCallDispatch(t *testing.T) {
	st := page.NewRoute(page.NewDP()).NewState()
	if v, err := callState(t, st, "add", `{"a":2,"b":3}`); err != nil || v != 5 {
		t.Errorf("add = %v, %v; want 5", v, err)
	}
	for _, args := range []string{`{"a":2,"x":3}`, `{"a":2} {}`, `[1]`} {
		_, err := callState(t, st, "add", args)
		wantHTTPError(t, err, http.StatusBadRequest, badArgs)
	}
	for _, name := range []string{"secret", "Secret", "ping", "Add", ""} {
		_, err := callState(t, st, name, `{}`)
		wantHTTPError(t, err, http.StatusNotFound, notFound)
	}

	root := pages.NewRoute(pages.NewDP()).NewState()
	if v, err := callState(t, root, "ping", `{"text":"t"}`); err != nil || v != (pages.PingOut{Echo: "t", Path: "/page"}) {
		t.Errorf("ping = %+v, %v", v, err)
	}

	if _, ok := plain.NewRoute(plain.NewDP()).NewState().(mux.Caller); ok {
		t.Error("a route without calls is a mux.Caller")
	}
}

func TestStubRefuses(t *testing.T) {
	v, err := callState(t, stub.NewRoute(stub.NewDP()).NewState(), "peek", `{}`)
	wantHTTPError(t, err, http.StatusForbidden, http.StatusText(http.StatusForbidden))
	if v != (stub.StubOut{}) {
		t.Errorf("value %+v next to the refusal", v)
	}

	// The stubs of calls whose types come from other packages compile with
	// their imports.
	st := ext.NewRoute(ext.NewDP()).NewState()
	v, err = callState(t, st, "at", `"2026-10-31T00:00:00Z"`)
	wantHTTPError(t, err, http.StatusForbidden, http.StatusText(http.StatusForbidden))
	if v != (model.Item{}) {
		t.Errorf("value %+v next to the refusal", v)
	}
	_, err = callState(t, st, "raw", `{"any":1}`)
	wantHTTPError(t, err, http.StatusForbidden, http.StatusText(http.StatusForbidden))
}
