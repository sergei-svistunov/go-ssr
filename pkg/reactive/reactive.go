// Package reactive provides the runtime support for GoSSR reactive bindings.
//
// The generated code for each reactive route creates a route-specific
// ReactiveState struct (with typed Set<VarName> methods) that embeds
// *reactive.Conn. Conn manages the WebSocket send goroutine, coalesces
// pending patches, and encodes wire-protocol frames.
//
// Wire protocol: all messages are WebSocket text frames containing a single
// UTF-8 JSON object. Values are pre-rendered HTML strings.
package reactive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// slowClientTimeout is the maximum time the send goroutine will wait for
// the WS write buffer to drain before closing the connection.
const slowClientTimeout = 10 * time.Second

// ----------------------------------------------------------------------------
// Wire-protocol frame types
// ----------------------------------------------------------------------------

// InitMsg is the server→client frame sent once after the WS connection is
// accepted. It carries the current rendered value of every reactive binding.
type InitMsg struct {
	T        string            `json:"t"`
	RouteKey string            `json:"routeKey"`
	Bindings map[string]string `json:"bindings"`
}

// PatchMsg is the server→client frame sent on each state change.
type PatchMsg struct {
	T        string `json:"t"`
	RouteKey string `json:"routeKey"`
	Key      string `json:"key"`
	HTML     string `json:"html"`
}

// WriteMsg is the client→server frame sent when ssr.set() or ssr:bind fires.
// Value is a json.RawMessage carrying a JSON-encoded value of any shape
// (number, string, boolean, object, array, or null). The server unmarshals
// this into the declared Go type T for the named variable.
type WriteMsg struct {
	T        string          `json:"t"`
	RouteKey string          `json:"routeKey"`
	Var      string          `json:"var"`
	Value    json.RawMessage `json:"value"`
}

// AckMsg is the server→client frame sent after a successful validated write.
type AckMsg struct {
	T        string `json:"t"`
	RouteKey string `json:"routeKey"`
	Var      string `json:"var"`
}

// ErrMsg is the server→client frame sent when a write fails validation.
// Code is a machine-readable error kind; Msg is a human-readable description.
// Defined codes:
//   - "unknown_route": write frame routeKey not recognised.
//   - "validation_failed": Validate* returned error or variable name unknown.
//   - "decode_error": json.Unmarshal of the write frame Value field into the
//     target Go type failed (malformed JSON or type mismatch). The variable is
//     not modified. The TS runtime logs this and does NOT invoke ssr.onError.
type ErrMsg struct {
	T        string `json:"t"`
	RouteKey string `json:"routeKey"`
	Var      string `json:"var"`
	Msg      string `json:"msg"`
	Code     string `json:"code"`
}

// NewInitMsg constructs an InitMsg with type discriminator set.
// routeKey is the 8-hex-char route path hash of the sending route
// (the leaf route key by convention for multi-route init frames).
func NewInitMsg(routeKey string, bindings map[string]string) InitMsg {
	return InitMsg{T: "init", RouteKey: routeKey, Bindings: bindings}
}

// NewPatchMsg constructs a PatchMsg with type discriminator set.
// routeKey identifies the reactive route whose variable changed.
func NewPatchMsg(routeKey, key, html string) PatchMsg {
	return PatchMsg{T: "patch", RouteKey: routeKey, Key: key, HTML: html}
}

// NewAckMsg constructs an AckMsg with type discriminator set.
// routeKey echoes the routeKey of the originating write frame.
func NewAckMsg(routeKey, varName string) AckMsg {
	return AckMsg{T: "ack", RouteKey: routeKey, Var: varName}
}

// NewErrMsg constructs an ErrMsg with type discriminator set.
// routeKey echoes the routeKey of the originating write frame, or "" if the
// routeKey in the write frame was unrecognised.
// code is the machine-readable error kind ("unknown_route" or "validation_failed").
// msg is the human-readable description for logging and debugging.
func NewErrMsg(routeKey, varName, msg, code string) ErrMsg {
	return ErrMsg{T: "err", RouteKey: routeKey, Var: varName, Msg: msg, Code: code}
}

// ----------------------------------------------------------------------------
// Conn — per-connection WebSocket state manager
// ----------------------------------------------------------------------------

// pendingPatch holds the latest pending patch value for a single binding key.
type pendingPatch struct {
	mu       sync.Mutex
	routeKey string
	key      string
	html     string
	dirty    bool
}

// Conn manages the send goroutine for one WebSocket connection.
// Generated ReactiveState structs embed *Conn.
//
// The send goroutine is started by StartSendLoop and exits when ctx is cancelled
// or when a slow-client timeout occurs.
type Conn struct {
	ctx    context.Context
	cancel context.CancelFunc
	ws     *websocket.Conn

	// patches maps binding key → pending patch slot.
	// Written by generated Set* methods; read by the send goroutine.
	patchesMu sync.Mutex
	patches   map[string]*pendingPatch

	// notify is a non-blocking signal channel: Set* sends a token here after
	// updating a patch slot. The send goroutine drains all dirty slots when it
	// receives a token.
	notify chan struct{}
}

// NewConn creates a Conn for the given WebSocket connection.
// ctx should be the request context; cancel will be called when the connection
// closes for any reason (clean close, timeout, or error).
func NewConn(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn) *Conn {
	return &Conn{
		ctx:     ctx,
		cancel:  cancel,
		ws:      ws,
		patches: make(map[string]*pendingPatch),
		notify:  make(chan struct{}, 1),
	}
}

// SendJSON sends a single JSON-encoded value as a WebSocket text frame.
// It is safe to call from any goroutine but is not concurrent-safe with itself;
// callers must serialise writes (the send goroutine does this by design).
func (c *Conn) SendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.ws.Write(c.ctx, websocket.MessageText, data)
}

// Enqueue updates the pending patch for the given binding key and signals the
// send goroutine. It is called by generated Set* methods.
//
// routeKey is the owning route's 8-hex-char path hash; it is included in the
// PatchMsg frame. key is the fully-namespaced binding key
// (routeKey + "." + localKey).
//
// Concurrency contract:
//   - Non-blocking: returns immediately after updating the patch slot.
//   - Coalesces: if a patch for this key is already pending, the old value is
//     replaced (last-write-wins).
//   - Post-cancellation no-op: if ctx is already done, returns immediately.
func (c *Conn) Enqueue(routeKey, key, html string) {
	if c.ctx.Err() != nil {
		return
	}

	c.patchesMu.Lock()
	slot, ok := c.patches[key]
	if !ok {
		slot = &pendingPatch{routeKey: routeKey, key: key}
		c.patches[key] = slot
	}
	c.patchesMu.Unlock()

	slot.mu.Lock()
	slot.html = html
	slot.dirty = true
	slot.mu.Unlock()

	// Non-blocking signal: if the channel already has a token the goroutine
	// will wake up and process all dirty slots including this one.
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// StartSendLoop starts the background send goroutine. It must be called once
// per connection after SendJSON(InitMsg) has returned successfully.
// The goroutine exits when ctx is cancelled or a fatal send error occurs.
func (c *Conn) StartSendLoop() {
	go func() {
		defer c.cancel()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-c.notify:
				if err := c.flushDirty(); err != nil {
					log.Printf("reactive: send error: %v", err)
					return
				}
			}
		}
	}()
}

// flushDirty sends all dirty patch slots. Returns any write error.
func (c *Conn) flushDirty() error {
	c.patchesMu.Lock()
	keys := make([]string, 0, len(c.patches))
	for k := range c.patches {
		keys = append(keys, k)
	}
	c.patchesMu.Unlock()

	for _, k := range keys {
		c.patchesMu.Lock()
		slot := c.patches[k]
		c.patchesMu.Unlock()

		slot.mu.Lock()
		if !slot.dirty {
			slot.mu.Unlock()
			continue
		}
		rk := slot.routeKey
		html := slot.html
		slot.dirty = false
		slot.mu.Unlock()

		patch := NewPatchMsg(rk, k, html)
		if err := c.sendWithTimeout(patch); err != nil {
			return err
		}
	}
	return nil
}

// sendWithTimeout encodes v as JSON and sends it as a text frame, applying the
// slow-client timeout. If the write blocks for more than slowClientTimeout,
// the connection is closed with WS close code 1008 (policy violation),
// c.cancel() is called to terminate the send loop, and an error is returned
// so the send loop exits cleanly.
func (c *Conn) sendWithTimeout(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	writeCtx, writeCancel := context.WithTimeout(c.ctx, slowClientTimeout)
	defer writeCancel()

	err = c.ws.Write(writeCtx, websocket.MessageText, data)
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		c.ws.Close(websocket.StatusPolicyViolation, "slow client")
		c.cancel()
		return fmt.Errorf("reactive: slow client: send timed out after %s", slowClientTimeout)
	}

	return err
}

// Ctx returns the connection's context. Generated code uses this to check
// whether the connection is still alive before acquiring per-variable mutexes.
func (c *Conn) Ctx() context.Context {
	return c.ctx
}

// CloseWithError sends a WS close frame with the given status code and reason,
// then cancels the connection context. Used by the generated WS handler to
// close with code 1011 (internal error) when a Subscribe goroutine returns an
// error.
func (c *Conn) CloseWithError(code websocket.StatusCode, reason string) {
	c.ws.Close(code, reason)
	c.cancel()
}

// ReceiveWriteMsg reads the next WriteMsg from the WebSocket. It blocks until
// a message is received or the connection is closed.
func (c *Conn) ReceiveWriteMsg(msg *WriteMsg) error {
	_, data, err := c.ws.Read(c.ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, msg)
}

// HandleWrites runs the read loop for client→server write frames. For each
// received WriteMsg the dispatch function is called. HandleWrites returns when
// ctx is cancelled or the connection is closed.
//
// dispatch receives the parsed WriteMsg. Unknown routeKey handling
// (sending err{code:"unknown_route"}) must be performed inside dispatch.
//
// The function returns nil on clean shutdown and the read error otherwise.
func (c *Conn) HandleWrites(ctx context.Context, dispatch func(WriteMsg)) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		var msg WriteMsg
		if err := c.ReceiveWriteMsg(&msg); err != nil {
			return err
		}
		dispatch(msg)
	}
}

// ----------------------------------------------------------------------------
// Value helpers (RenderValue, ParseValue)
// ----------------------------------------------------------------------------

// RenderValue converts a Go scalar value to its pre-rendered HTML string
// representation. This is the string the client receives in patch messages.
// Numeric and bool values are formatted directly; strings are used as-is
// (the template layer handles HTML escaping when rendering in the SSR path).
func RenderValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// ParseValue parses a raw string value received from the client into the
// target Go type T. The string is the "value" field of a WriteMsg.
// Supported types match the scalarTypes set in the generator.
func ParseValue[T any](s string) (T, error) {
	var zero T
	var result any

	switch any(zero).(type) {
	case string:
		result = s
	case bool:
		b, err := parseBool(s)
		if err != nil {
			return zero, fmt.Errorf("cannot parse %q as bool: %w", s, err)
		}
		result = b
	case int:
		n, err := parseInt(s)
		if err != nil {
			return zero, err
		}
		result = int(n)
	case int8:
		n, err := parseInt(s)
		if err != nil {
			return zero, err
		}
		result = int8(n)
	case int16:
		n, err := parseInt(s)
		if err != nil {
			return zero, err
		}
		result = int16(n)
	case int32:
		n, err := parseInt(s)
		if err != nil {
			return zero, err
		}
		result = int32(n)
	case int64:
		n, err := parseInt(s)
		if err != nil {
			return zero, err
		}
		result = n
	case uint:
		n, err := parseUint(s)
		if err != nil {
			return zero, err
		}
		result = uint(n)
	case uint8:
		n, err := parseUint(s)
		if err != nil {
			return zero, err
		}
		result = uint8(n)
	case uint16:
		n, err := parseUint(s)
		if err != nil {
			return zero, err
		}
		result = uint16(n)
	case uint32:
		n, err := parseUint(s)
		if err != nil {
			return zero, err
		}
		result = uint32(n)
	case uint64:
		n, err := parseUint(s)
		if err != nil {
			return zero, err
		}
		result = n
	case float32:
		f, err := parseFloat(s)
		if err != nil {
			return zero, err
		}
		result = float32(f)
	case float64:
		f, err := parseFloat(s)
		if err != nil {
			return zero, err
		}
		result = f
	default:
		return zero, fmt.Errorf("ParseValue: unsupported type %T", zero)
	}

	typed, ok := result.(T)
	if !ok {
		return zero, fmt.Errorf("ParseValue: type assertion failed (internal error)")
	}
	return typed, nil
}

// ----------------------------------------------------------------------------
// HTTP handler factory
// ----------------------------------------------------------------------------

// HandlerFunc is the signature for the server-side WS logic provided by
// generated code. It is called once per connection after the WS handshake.
// The *Conn is fully initialised; the function must send the init frame,
// call StartSendLoop, then call the route's Subscribe method.
type HandlerFunc func(ctx context.Context, r *http.Request, conn *Conn)

// NewHandler returns an http.Handler that upgrades HTTP requests to WebSocket
// and calls fn for each connection. Origin checks are bypassed (the generated
// code is responsible for session validation inside Subscribe).
func NewHandler(fn HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Accept connections from any origin; auth is handled in Subscribe.
			InsecureSkipVerify: true,
		})
		if err != nil {
			// Accept writes its own HTTP error response on failure.
			return
		}
		defer c.CloseNow()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		conn := NewConn(ctx, cancel, c)
		fn(ctx, r, conn)
	})
}

// ----------------------------------------------------------------------------
// Internal parse helpers
// ----------------------------------------------------------------------------

func parseBool(s string) (bool, error) {
	switch s {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no", "":
		return false, nil
	default:
		return strconv.ParseBool(s)
	}
}

func parseInt(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as integer: %w", s, err)
	}
	return n, nil
}

func parseUint(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as unsigned integer: %w", s, err)
	}
	return n, nil
}

func parseFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as float: %w", s, err)
	}
	return f, nil
}
