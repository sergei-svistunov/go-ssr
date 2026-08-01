# Runtime API: the Go packages a data provider uses

Generated code imports these; your `dataprovider.go` files use them directly. Import path prefix:
`github.com/sergei-svistunov/go-ssr`.

## pkg/mux — request, response, errors

```go
import "github.com/sergei-svistunov/go-ssr/pkg/mux"
```

### Request

```go
type Request struct {
    *http.Request
    URLParams map[string]string
}

func (r *Request) URLParam(key string) string
```

`mux.Request` embeds `*http.Request`, so everything from the standard library is available: `r.Context()`,
`r.Header`, `r.URL.Query()`, `r.Cookie(...)`. `URLParam` reads a dynamic path segment captured by a
`_name_` directory. Inside a WebSocket `Subscribe`, the same parameters are populated from the socket's path.

`mux.URLParamsFromContext(ctx)` retrieves the parameter map from a context when a `*Request` is not at hand.

### ResponseWriter

```go
type ResponseWriter interface {
    Header() http.Header
    // …
}
```

Hooks receive this to set headers and cookies before rendering starts. Do not write a body through it — the
renderer owns the body. `mux.NoopResponseWriter` is a discard implementation, useful in tests and on the
WebSocket path where there is no HTTP response to write.

### Errors and redirects

```go
func NewHttpError(code int, message string) *HttpError
func NotFound() *HttpError
func Redirect(code int, location string) *HttpRedirect
```

Return one of these from any hook to take over the response. Anything else becomes a 500. Both types implement
`error`, so they compose with `fmt.Errorf("%w", …)` and `errors.Is`.

### Handler options

```go
type Options struct {
    ErrorHandler ErrorHandler
}

type ErrorHandler func(w http.ResponseWriter, r *Request, err error)
```

Pass `mux.Options{}` for the default behaviour: `*HttpError` and `*HttpRedirect` are honoured, anything else is
a 500. Supply an `ErrorHandler` to render your own error pages.

### Writers used by generated code

`mux.WriteHtmlEscaped` and `mux.WriteRaw` back `{{ }}` and `{{$ }}`, and `mux.TernaryIf` backs the conditional
operator. You would not normally call them yourself, but they explain the rendering semantics described in
`expressions`.

## pkg/form — form values

```go
import "github.com/sergei-svistunov/go-ssr/pkg/form"
```

| Type | Value accessor |
|---|---|
| `Input[T]` | `GetValue() T`, `GetFormValue() string` |
| `InputMultiple[T]` | `GetValue() map[T]struct{}` |
| `Select[T]` | `GetValue() T`, `SetOptions`, `GetOptions` |
| `SelectMultiple[T]` | `GetValue() map[T]struct{}`, `SetOptions` |
| `Textarea` | `GetValue() string` |
| `File` | `GetValue() *FileHeader` |
| `FileMultiple` | `GetValue() []*FileHeader` |

Every element type also has `SetValue`, `SetError(string)`, `HasError() bool`, `GetError() string` and
`IsNotNull() bool`. `T` may be `string`, `bool`, or any sized integer or float type.

Options are built from `SelectOption[T]{Value, Label, Disabled}` and `SelectOptionGroup[T]{Label, Options,
Disabled}`, both satisfying `SelectOptionElement[T]`.

The embedded `BaseFormValues` provides `IsValidated()`, `HasError()`, `SetError`, `GetError` and
`GetCSRFToken()`. See `forms` for the lifecycle these fit into.

## pkg/reactive — waking Subscribe from elsewhere

```go
import "github.com/sergei-svistunov/go-ssr/pkg/reactive"
```

A `Subscribe` goroutine usually needs to be woken by something outside its route: another HTTP handler, a
worker, a message consumer. Two small generic primitives cover that.

```go
// Per-key fan-out: every subscriber to a key receives published values.
var balances = reactive.NewTopic[uint32, float64]()

// Global fan-out: every subscriber receives every value.
var presence = reactive.NewBroadcast[int]()
```

Publish from anywhere:

```go
balances.Publish(userID, newBalance)
presence.Publish(onlineCount)
```

Consume in `Subscribe`:

```go
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
    sub := balances.Subscribe(userIDFrom(r))
    defer sub.Close()

    online := presence.Subscribe()
    defer online.Close()

    for {
        select {
        case <-ctx.Done():
            return nil
        case v := <-sub.Updates():
            state.SetBalance(v)
        case n := <-online.Updates():
            state.SetOnline(n)
        }
    }
}
```

Semantics:

- **`Publish` never blocks.** Each subscription holds a one-slot channel; a pending value is replaced by the
  newer one, so the subscriber always sees the latest. Safe to call inside a database transaction.
- **`Close` is idempotent**, and an empty key is removed from the map, so a churning key space does not leak.
- **Everything is safe for concurrent use.**

Publishing the new value avoids a read-your-own-write race against the publisher's transaction. When the value
is inconvenient to carry, use `struct{}` as a dirty bit and re-query in the subscriber:

```go
var dirty = reactive.NewTopic[uint32, struct{}]()
// publisher
dirty.Publish(userID, struct{}{})
// subscriber
case <-sub.Updates():
    state.SetUnread(p.m.UnreadCount(userID))
```

`Topic.Len()`, `Topic.TotalSubs()` and `Broadcast.TotalSubs()` exist for metrics and tests.

The rest of `pkg/reactive` — `Conn`, the frame types, `HandleWrite` — is used by generated code and is not part
of the API you write against.

## pkg/static — embedded asset serving

Used by the generated `ssrstaticfiles_gen.go`; it serves the embedded build output with ETag validation and
gzip negotiation. See `static-assets`.
