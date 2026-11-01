package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/sergei-svistunov/go-ssr/pkg/reactive"
)

// Caller is implemented by the state of a route with <ssr:call> functions.
// A call frame on the page's live connection runs every Guard on the path
// again, on fresh states, then the Call of the route that renders the page
// and whose RouteKey the frame names. Data does not run.
type Caller interface {
	RouteKey() string
	// Call decodes args and runs the route's call named name.
	Call(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error)
}

// callTimeout bounds one page call, its Guards included.
var callTimeout = 30 * time.Second

// maxCallsInFlight is how many calls one live connection runs at once.
const maxCallsInFlight = 4

// The messages of the fail frames the mux writes itself.
const (
	callBadArgs  = "The call has the wrong arguments."
	callNotFound = "This call is not on this page."
	callTooMany  = "Too many calls at once."
	callTooLong  = "The call took too long."
	callFailed   = "Something went wrong."
)

// DecodeArgs reads the arguments of a page call into v. Unknown fields,
// trailing data and anything else that does not decode into v are a 400, so
// a page cannot send more than the call declares. So is a null for a value
// that cannot be nil, which would leave it unset.
func DecodeArgs(raw json.RawMessage, v any) error {
	if string(bytes.TrimSpace(raw)) == "null" && !takesNull(v) {
		return NewHttpError(http.StatusBadRequest, callBadArgs)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return NewHttpError(http.StatusBadRequest, callBadArgs)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return NewHttpError(http.StatusBadRequest, callBadArgs)
	}
	return nil
}

// takesNull reports whether v points to a value a JSON null may stand for: a
// pointer, slice, map or interface, which null sets to nil, or an empty
// struct, which has no field null could leave unset. The page's client sends
// null for a call made with no arguments, the usual form of a call whose in
// type is struct{}.
func takesNull(v any) bool {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Pointer {
		return false
	}
	switch t = t.Elem(); t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		return true
	case reflect.Struct:
		return t.NumField() == 0
	}
	return false
}

// pageCalls runs the calls of one live connection. A call may outlive the
// connection; it then touches only its own connection, whose writes fail
// once it is closed.
type pageCalls struct {
	r      *Request
	conn   *reactive.Conn
	routes []Route
	slots  chan struct{}
}

func newPageCalls(r *Request, conn *reactive.Conn, routes []Route) *pageCalls {
	return &pageCalls{r: r, conn: conn, routes: routes, slots: make(chan struct{}, maxCallsInFlight)}
}

// start runs msg on its own goroutine, or fails it with 429 when the
// connection already runs maxCallsInFlight calls. A call keeps its place
// until it returns, even after its answer has gone at its deadline.
func (p *pageCalls) start(msg reactive.CallMsg) {
	select {
	case p.slots <- struct{}{}:
	default:
		_ = p.conn.SendJSON(reactive.NewFail(msg.ID, http.StatusTooManyRequests, callTooMany))
		return
	}
	go func() {
		defer func() { <-p.slots }()
		p.run(msg)
	}()
}

// run answers msg with one result or fail frame: at callTimeout at the
// latest, whether or not the call has returned. A Guard's refusal closes the
// connection with 1008 after its fail frame; any other panic fails the call
// with 500 and leaves the connection open.
func (p *pageCalls) run(msg reactive.CallMsg) {
	ctx, cancel := context.WithTimeout(p.conn.Ctx(), callTimeout)
	defer cancel()
	var once sync.Once
	send := func(frame any) { once.Do(func() { _ = p.conn.SendJSON(frame) }) }
	stop := context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("mux: page call %q took longer than %s", logName(msg.Name), callTimeout)
			send(reactive.NewFail(msg.ID, http.StatusGatewayTimeout, callTooLong))
		}
	})
	defer stop()
	defer func() {
		if v := recover(); v != nil {
			log.Printf("mux: panic in page call %q: %v", logName(msg.Name), v)
			send(reactive.NewFail(msg.ID, http.StatusInternalServerError, callFailed))
		}
	}()

	frame, refusal := p.answer(ctx, msg)
	send(frame)
	if refusal != "" {
		p.conn.CloseWithError(websocket.StatusPolicyViolation, closeReason(refusal))
	}
}

// answer runs the Guards and the call of msg and returns the frame that
// answers it, and the message of a Guard's refusal.
func (p *pageCalls) answer(ctx context.Context, msg reactive.CallMsg) (frame any, refusal string) {
	r := NewRequest(p.r.Request.WithContext(ctx))
	r.URLParams = maps.Clone(p.r.URLParams)

	states, err := guardCall(ctx, r, p.routes)
	if err != nil {
		if ctx.Err() != nil {
			return reactive.NewFail(msg.ID, http.StatusGatewayTimeout, callTooLong), ""
		}
		status, text := guardFailure(err)
		return reactive.NewFail(msg.ID, status, text), text
	}
	var caller Caller
	for _, s := range rendered(p.routes, states) {
		if c, ok := s.(Caller); ok && c.RouteKey() == msg.RouteKey {
			caller = c
			break
		}
	}
	if caller == nil {
		return reactive.NewFail(msg.ID, http.StatusNotFound, callNotFound), ""
	}
	value, err := invokeCall(ctx, r, caller, msg)
	if err != nil {
		status, text := callFailure(msg.Name, err)
		return reactive.NewFail(msg.ID, status, text), ""
	}
	return reactive.NewResult(msg.ID, value), ""
}

// guardCall runs guard, taking a panic for a refusal.
func guardCall(ctx context.Context, r *Request, routes []Route) (states []RouteState, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	return guard(ctx, r, routes)
}

// errCallPanicked stands for a panic in a call or in encoding its result,
// already logged.
var errCallPanicked = errors.New("page call panicked")

// invokeCall runs the call and encodes its result.
func invokeCall(ctx context.Context, r *Request, c Caller, msg reactive.CallMsg) (value json.RawMessage, err error) {
	defer func() {
		if v := recover(); v != nil {
			log.Printf("mux: panic in page call %q: %v", logName(msg.Name), v)
			value, err = nil, errCallPanicked
		}
	}()
	out, err := c.Call(ctx, r, msg.Name, msg.Args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// guardFailure maps a Guard's refusal of a call to its fail frame: a 4xx
// HttpError keeps its status and message, a redirect is 401, and anything
// else is 500, logged.
func guardFailure(err error) (int, string) {
	var httpErr *HttpError
	if errors.As(err, &httpErr) && httpErr.Code >= 400 && httpErr.Code < 500 {
		return httpErr.Code, errorMessage(httpErr)
	}
	var redirect *HttpRedirect
	if errors.As(err, &redirect) {
		return http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized)
	}
	log.Printf("mux: page call refused: %v", err)
	return http.StatusInternalServerError, callFailed
}

// callFailure maps a call's error to its fail frame: a 4xx or 5xx HttpError
// keeps its status and message, a deadline is 504, and anything else is 500,
// logged.
func callFailure(name string, err error) (int, string) {
	var httpErr *HttpError
	var redirect *HttpRedirect
	switch {
	case errors.As(err, &httpErr) && httpErr.Code >= 400 && httpErr.Code <= 599:
		return httpErr.Code, errorMessage(httpErr)
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, callTooLong
	case errors.Is(err, errCallPanicked):
		// Already logged in invokeCall.
	case errors.As(err, &redirect):
		log.Printf("mux: page call %q returned a redirect to %q; return data from the call and navigate with location.assign in the page's TypeScript", logName(name), redirect.Url)
	default:
		log.Printf("mux: page call %q failed: %v", logName(name), err)
	}
	return http.StatusInternalServerError, callFailed
}

// logName cuts a call name the page sent to the 32 bytes the logs show.
func logName(name string) string {
	if len(name) > 32 {
		return name[:32]
	}
	return name
}

func errorMessage(e *HttpError) string {
	if e.Message == "" {
		return http.StatusText(e.Code)
	}
	return e.Message
}
