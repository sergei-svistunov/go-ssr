// Package reactive provides the runtime support for GoSSR reactive bindings.
//
// The generated code for each reactive route creates a route-specific
// ReactiveState struct (with typed Set<VarName> methods) that embeds
// *reactive.Conn. Conn manages the WebSocket send goroutine, coalesces
// pending patches, and encodes wire-protocol frames.
//
// Wire protocol: all messages are WebSocket text frames containing a single
// UTF-8 JSON object. Values are markup the server rendered with the context
// escapers. Besides the frames of live values (init, patch, write, ack,
// err), a page runs its calls with call frames, each answered by a result or
// fail frame with its id.
package reactive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/sergei-svistunov/go-ssr/internal/wsorigin"
)

// slowClientTimeout is the longest a frame may wait to be written before the
// connection is closed as a slow client.
var slowClientTimeout = 10 * time.Second

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

// CallMsg is the client→server frame that runs one of a page's calls. ID
// counts up per page, across reconnects, and comes back in the frame that
// answers the call. Args is the call's argument as JSON.
type CallMsg struct {
	T        string          `json:"t"`
	ID       uint32          `json:"id"`
	RouteKey string          `json:"routeKey"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
}

// ResultMsg is the server→client frame that answers a call that returned.
// Value is the call's result as JSON.
type ResultMsg struct {
	T     string          `json:"t"`
	ID    uint32          `json:"id"`
	Value json.RawMessage `json:"value"`
}

// FailMsg is the server→client frame that answers a call that failed, with
// an HTTP status and a message the page may show.
type FailMsg struct {
	T      string `json:"t"`
	ID     uint32 `json:"id"`
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// NewResult constructs a ResultMsg answering call id; value is already JSON.
func NewResult(id uint32, value json.RawMessage) ResultMsg {
	return ResultMsg{T: "result", ID: id, Value: value}
}

// NewFail constructs a FailMsg answering call id.
func NewFail(id uint32, status int, msg string) FailMsg {
	return FailMsg{T: "fail", ID: id, Status: status, Msg: msg}
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

// SendJSON sends a single JSON-encoded value as a WebSocket text frame. A
// frame the client does not accept within 10 seconds closes the connection
// with 1008 "slow client". It is safe for concurrent use.
func (c *Conn) SendJSON(v any) error {
	return c.sendWithTimeout(v)
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
// slow-client timeout. If the write, including its wait for other writes,
// takes more than slowClientTimeout, the connection is closed with WS close
// code 1008 (policy violation), c.cancel() is called to end the connection's
// hooks and the send loop, and an error is returned.
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
		_ = c.ws.Close(websocket.StatusPolicyViolation, "slow client")
		c.cancel()
		return fmt.Errorf("reactive: slow client: send timed out after %s", slowClientTimeout)
	}

	return err
}

// Ctx returns the connection's context. The mux passes it to the live hooks
// of the connection; generated Set methods use it to skip work once the
// connection is gone.
func (c *Conn) Ctx() context.Context {
	return c.ctx
}

// CloseWithError sends a WS close frame with the given status code and reason,
// then cancels the connection context.
func (c *Conn) CloseWithError(code websocket.StatusCode, reason string) {
	_ = c.ws.Close(code, reason)
	c.cancel()
}

// CloseNow closes the connection without a close frame and cancels the
// connection context.
func (c *Conn) CloseNow() {
	_ = c.ws.CloseNow()
	c.cancel()
}

// clientFrame holds the fields of every frame a page sends.
type clientFrame struct {
	T        string          `json:"t"`
	ID       uint32          `json:"id"`
	RouteKey string          `json:"routeKey"`
	Var      string          `json:"var"`
	Value    json.RawMessage `json:"value"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
}

// ReadFrames runs the read loop for client→server frames until ctx is
// cancelled or the connection is closed. Each write frame goes to onWrite and
// each call frame with an id of at least 1 to onCall, on the reading
// goroutine, so neither may block for long. A nil handler ignores its
// frames; frames of any other type are ignored, and the first of them is
// logged by its type alone. A frame that is not valid JSON ends the loop
// with the decoding error.
//
// Unknown routeKey handling (sending err{code:"unknown_route"}) must be
// performed inside onWrite.
//
// The function returns nil on clean shutdown and the read error otherwise.
func (c *Conn) ReadFrames(ctx context.Context, onWrite func(WriteMsg), onCall func(CallMsg)) error {
	unknownLogged := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		_, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return err
		}
		var f clientFrame
		if err := json.Unmarshal(data, &f); err != nil {
			return err
		}
		switch f.T {
		case "write":
			if onWrite != nil {
				onWrite(WriteMsg{T: f.T, RouteKey: f.RouteKey, Var: f.Var, Value: f.Value})
			}
		case "call":
			if onCall != nil && f.ID >= 1 {
				onCall(CallMsg{T: f.T, ID: f.ID, RouteKey: f.RouteKey, Name: f.Name, Args: f.Args})
			}
		default:
			if !unknownLogged {
				unknownLogged = true
				log.Printf("reactive: ignoring frames of unknown type %q", cut(f.T, 32))
			}
		}
	}
}

// cut returns s cut to at most n bytes.
func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ----------------------------------------------------------------------------
// Value helpers (ParseValue)
// ----------------------------------------------------------------------------

// ParseValue parses a raw string value received from the client into the
// target Go type T, as an ssr:bind input's text is read: bool by
// strconv.ParseBool, integers in base 10 within the size of T, and floats
// within the size of T, refusing NaN and infinities. Supported types match the
// scalarTypes set in the generator.
func ParseValue[T any](s string) (T, error) {
	var zero T
	var result any
	var err error

	switch any(zero).(type) {
	case string:
		result = s
	case bool:
		var b bool
		if b, err = strconv.ParseBool(s); err != nil {
			err = fmt.Errorf("cannot parse %q as bool: %w", s, err)
		}
		result = b
	case int:
		n, e := strconv.ParseInt(s, 10, 0)
		result, err = int(n), intErr(s, e)
	case int8:
		n, e := strconv.ParseInt(s, 10, 8)
		result, err = int8(n), intErr(s, e)
	case int16:
		n, e := strconv.ParseInt(s, 10, 16)
		result, err = int16(n), intErr(s, e)
	case int32:
		n, e := strconv.ParseInt(s, 10, 32)
		result, err = int32(n), intErr(s, e)
	case int64:
		n, e := strconv.ParseInt(s, 10, 64)
		result, err = n, intErr(s, e)
	case uint:
		n, e := strconv.ParseUint(s, 10, 0)
		result, err = uint(n), uintErr(s, e)
	case uint8:
		n, e := strconv.ParseUint(s, 10, 8)
		result, err = uint8(n), uintErr(s, e)
	case uint16:
		n, e := strconv.ParseUint(s, 10, 16)
		result, err = uint16(n), uintErr(s, e)
	case uint32:
		n, e := strconv.ParseUint(s, 10, 32)
		result, err = uint32(n), uintErr(s, e)
	case uint64:
		n, e := strconv.ParseUint(s, 10, 64)
		result, err = n, uintErr(s, e)
	case float32:
		var f float64
		f, err = parseFloat(s, 32)
		result = float32(f)
	case float64:
		result, err = parseFloat(s, 64)
	default:
		return zero, fmt.Errorf("ParseValue: unsupported type %T", zero)
	}
	if err != nil {
		return zero, err
	}

	typed, ok := result.(T)
	if !ok {
		return zero, fmt.Errorf("ParseValue: type assertion failed (internal error)")
	}
	return typed, nil
}

// ----------------------------------------------------------------------------
// Accept
// ----------------------------------------------------------------------------

// StatusReauth closes a connection that has lived its allotted time; the
// client reconnects at once and the server checks the request again.
const StatusReauth websocket.StatusCode = 4000

// Accept upgrades r to a WebSocket connection whose context is derived from
// r's context and cancelled when the connection is closed through the Conn.
//
// Unless the mux has already applied its own cross-site refusal to r, Accept
// refuses a handshake whose Origin host differs from r.Host.
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	// The server's read and write deadlines would cut a long-lived
	// connection short once it is hijacked.
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, err
	}
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, err
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: wsorigin.Checked(r.Context())})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(r.Context())
	return NewConn(ctx, cancel, ws), nil
}

// ----------------------------------------------------------------------------
// Internal parse helpers
// ----------------------------------------------------------------------------

func intErr(s string, err error) error {
	if err != nil {
		return fmt.Errorf("cannot parse %q as integer: %w", s, err)
	}
	return nil
}

func uintErr(s string, err error) error {
	if err != nil {
		return fmt.Errorf("cannot parse %q as unsigned integer: %w", s, err)
	}
	return nil
}

func parseFloat(s string, bits int) (float64, error) {
	f, err := strconv.ParseFloat(s, bits)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as float: %w", s, err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("cannot parse %q as float: not a finite number", s)
	}
	return f, nil
}
