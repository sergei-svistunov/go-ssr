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
    URLParams       map[string]string
    InsecureCookies bool     // mirrors Options.InsecureCookies
    OriginPatterns  []string // mirrors Options.OriginPatterns
}

func (r *Request) URLParam(key string) string
func (r *Request) CrossSite() bool
func (r *Request) NoStore()
func (r *Request) CSRFKey() []byte
func (r *Request) CSRFSession() string
func (r *Request) CSRFSessionNow() string

func CrossSite(r *http.Request, originPatterns []string) bool
func CrossSiteStrict(r *http.Request, originPatterns []string) bool
func SecureRequest(r *http.Request) bool
```

`mux.Request` embeds `*http.Request`, so everything from the standard library is available: `r.Context()`,
`r.Header`, `r.URL.Query()`, `r.Cookie(...)`. `URLParam` reads a dynamic path segment captured by a
`_name_` directory. Inside a WebSocket `Subscribe`, the same parameters are populated from the socket's path.

`CrossSite` is the one origin rule the framework applies everywhere — the mux refuses cross-site
POST/PUT/PATCH/DELETE requests once the route is resolved and before its hooks run, and refuses cross-site
WebSocket handshakes before dispatching them: `Sec-Fetch-Site: same-origin` is trusted outright,
`same-site`/`cross-site` are refused unless the `Origin` matches a pattern, and without `Sec-Fetch-Site` the
`Origin` host must equal the request `Host` (the port the scheme implies is ignored) and must be `https` when
the request arrived over TLS. A request with neither header, or `Origin: null`, passes — the CSRF token is
the defence behind it. `CrossSiteStrict` is the same rule for a request with no token behind it, and refuses
`Origin: null`; the mux applies it to WebSocket handshakes. `SecureRequest` reports whether the browser
reached the server over TLS, directly or via `X-Forwarded-Proto: https`. `NoStore` marks the response as
carrying a per-user secret; the mux sets `Cache-Control: no-store` once the routes have had a say, whether
the outcome is a page, a redirect or an error — the mark travels in the request context, so it survives a
hook rebuilding the `Request`. `CSRFKey` reports the signing key the handler was configured with.
`CSRFSession` is the session the request arrived with — kept for the whole request, so a login partway
through cannot make a later route reject the submission that performed it — and `CSRFSessionNow` asks again,
which is what minting uses so a form rendered after a login carries a token for the new session.

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
    ErrorHandler    ErrorHandler
    InsecureCookies bool
    OriginPatterns  []string
    AllowCrossSite  func(r *Request) bool
    CSRFKey         []byte
    CSRFSession     func(r *Request) string
}

type ErrorHandler func(w http.ResponseWriter, r *Request, err error)
```

Pass `mux.Options{}` for the default behaviour: `*HttpError` and `*HttpRedirect` are honoured, anything else is
a 500. Supply an `ErrorHandler` to render your own error pages. `InsecureCookies` drops the `Secure` flag and
the `__Host-` prefix from the CSRF cookie for plain-http development on hosts other than localhost; the mux
copies it onto every `Request` as `r.InsecureCookies`. See `forms`. `OriginPatterns` lists extra origins
allowed to submit forms and open reactive WebSockets; by default the browser's `Origin` must match the
request `Host`. A pattern is matched, case-insensitively, against the origin's host as the browser sends it,
which omits default ports: `dev.example` matches `https://dev.example`, `dev.example:*` matches any explicit
port such as `http://dev.example:3000`, and `https://dev.example` also pins the scheme. The brackets of an
IPv6 literal must be escaped: `\[::1\]:*`. A malformed pattern makes `New` panic; one passed to the exported
`CrossSite` at request time counts as no match instead, since a panic there would take the request down. The
mux applies its own gates before any hook runs, so setting `r.OriginPatterns` in a hook changes only a
`CrossSite` call the application makes itself — to let a specific route through, use `AllowCrossSite`. `AllowCrossSite`, when it returns true, lets a cross-site
state-changing request or WebSocket handshake through to its route — for an identity-provider callback or a
payment return that is a third-party POST by design; the route validates it itself.
`CSRFKey` signs the CSRF tokens — leave it empty and a key is generated when the handler is built, which
needs no setup but means tokens are not accepted across instances or restarts; set it from your secret store
when either matters. `CSRFSession` binds each token to a session, so one captured before a login is refused
after it; return the session identifier or `""`, and keep the hook cheap — it is consulted once per request
for the session the request arrived with, and again on every render that issues or reuses a token, so it
should read an already-loaded session rather than query.
See `forms` and `reactive`.

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
