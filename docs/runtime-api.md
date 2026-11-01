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
func (r *Request) CSRFToken(w ResponseWriter) (string, error)
func (r *Request) CSRFSession() string
func (r *Request) CSRFSessionNow() string

func CrossSite(r *http.Request, originPatterns []string) bool
func CrossSiteStrict(r *http.Request, originPatterns []string) bool
func SecureRequest(r *http.Request) bool
func SafeMethod(method string) bool
```

`mux.Request` embeds `*http.Request`, so everything from the standard library is available: `r.Context()`,
`r.Header`, `r.URL.Query()`, `r.Cookie(...)`. `URLParam` reads a dynamic path segment captured by a
`_name_` directory. On a live connection and in a page call, the request is the page's: its URL is the page's
path and query, its parameters are the page's, and its context derives from the handshake request's context.

`CrossSite` is the one origin rule the mux applies everywhere — it refuses cross-site POST/PUT/PATCH/DELETE
requests once the route is resolved and before its hooks run, and refuses cross-site live connection
handshakes before any hook: `Sec-Fetch-Site: same-origin` is trusted outright, `same-site`/`cross-site` are
refused unless the `Origin` matches a pattern, and without `Sec-Fetch-Site` the `Origin` host must equal the
request `Host` (the port the scheme implies is ignored) and must be `https` when the request arrived over TLS.
A request with neither header, or `Origin: null`, passes — the CSRF token is the defence behind it.
`CrossSiteStrict` is the same rule for a request with no token behind it, and refuses `Origin: null`; the mux
applies it to live connection handshakes. `SecureRequest` reports whether the browser reached the server over
TLS, directly or via `X-Forwarded-Proto: https`. `SafeMethod` reports whether a method is GET, HEAD, OPTIONS or
TRACE.

`NoStore` marks the response as carrying a per-user secret; the mux sets `Cache-Control: no-store` once the
routes have had a say, whether the outcome is a page, a redirect or an error — the mark travels in the request
context, so it survives a hook rebuilding the `Request`. `CSRFToken` returns the token a form rendered into
this response carries and sets its cookie; generated code calls it through `form.SetCSRFToken`. `CSRFSession`
is the session the request arrived with — kept for the whole request, so a login partway through cannot make
a later route reject the submission that performed it — and `CSRFSessionNow` asks again, which is what minting
uses so a form rendered after a login carries a token for the new session.

### ResponseWriter

```go
type ResponseWriter interface {
    Header() http.Header
}
```

Hooks receive this to set headers and cookies before rendering starts; the mux owns the body. The security
headers (`security`) are set again after the hooks, replacing any a hook set. `mux.NoopResponseWriter`
discards header writes; the mux passes it to `Data` on a live connection, where there is no HTTP response to
write, and it is useful in tests.

### Errors and redirects

```go
func NewHttpError(code int, message string) *HttpError
func NotFound() *HttpError
func Redirect(code int, location string) *HttpRedirect
```

Return one of these from any hook to take over the response. Anything else becomes a 500. Both types implement
`error`, so they compose with `fmt.Errorf("%w", …)` and `errors.As`. On a live connection and in a page call,
a redirect becomes 401 (`reactive`, `page-calls`).

### Routes and guards

```go
type Route interface {
    NewState() RouteState
    Layout() bool
}

type Guarded interface {
    Guard(ctx context.Context, r *Request) error
}

func CheckGuard(dp any, path string)
```

Generated code implements `Route` and `RouteState` for every route; `Layout` reports whether the route's
template has `<ssr:content/>` (`routing`). A data provider that implements `Guarded` has its `Guard` run before
any other hook of any route on the path. The generated `NewRoute` calls `CheckGuard`, which panics (E21) when
the data provider has a method named `Guard` that `Guarded` does not accept.

### Page calls

```go
type Caller interface {
    RouteKey() string
    Call(ctx context.Context, r *Request, name string, args json.RawMessage) (any, error)
}

func DecodeArgs(raw json.RawMessage, v any) error
```

The generated state of a route with `<ssr:call>` implements `Caller`, dispatching to the data provider's
`Call<Name>`. `DecodeArgs` reads a call's arguments into `v`: an unknown field, trailing data, a value that
does not fit, or `null` for a target that is not a pointer, slice, map, interface or `struct{}` is an
`*HttpError` 400 `The call has the wrong arguments.` See `page-calls`.

### Handler options

```go
type Options struct {
    ErrorHandler           ErrorHandler
    InsecureCookies        bool
    OriginPatterns         []string
    AllowCrossSite         func(r *Request) bool
    CSRFKey                []byte
    CSRFSession            func(r *Request) string
    MaxLiveConnsPerSession int
    CSPSources             map[string][]string
    MaxBodyBytes           int64
}

type ErrorHandler func(w http.ResponseWriter, r *Request, err error)

func New(routes map[string]Route, opts Options) (*Mux, error)

var MaxMultipartMemory int64 = 32 << 20

const DefaultMaxBodyBytes = 10 << 20
```

`NewSsrHandler` passes its options to `mux.New`, which returns an error for a `CSRFKey` shorter than 32 bytes,
a malformed `OriginPatterns` entry, a `CSPSources` entry that would weaken the policy or a negative
`MaxBodyBytes`. `config` has an
example of each option.

- `ErrorHandler` renders refusals and errors; without one, `*HttpError` is written as plain text with its
  status, `*HttpRedirect` is followed, and anything else is a logged 500. It also answers refused live
  connection handshakes.
- `InsecureCookies` drops the `Secure` flag and the `__Host-` prefix from the CSRF cookie for plain-http
  development on hosts other than localhost; never enable it in production. See `forms`.
- `OriginPatterns` lists extra origins allowed to submit forms and open live connections; by default the
  browser's `Origin` must match the request `Host`. A pattern is matched, case-insensitively, against the
  origin's host as the browser sends it, which omits default ports: `dev.example` matches
  `https://dev.example`, `dev.example:*` matches any explicit port such as `http://dev.example:3000`, and
  `https://dev.example` also pins the scheme. The brackets of an IPv6 literal must be escaped: `\[::1\]:*`.
  One passed to the exported `CrossSite` at request time that does not parse counts as no match. The mux
  applies its own checks before any hook runs, so setting `r.OriginPatterns` in a hook changes only a
  `CrossSite` call the application makes itself.
- `AllowCrossSite`, when it returns true, lets a cross-site state-changing request or live connection
  handshake past the cross-site refusal, and nothing more: an exempt POST still needs the CSRF cookie and the
  signed token and is refused with 400 without them. A third-party POST by design, such as a sign-in or
  payment callback, carries no token; handle it in an `http.Handler` in front of the mux.
- `CSRFKey` signs the CSRF tokens. It is required, at least 32 bytes, and the same on every instance; read it
  from your secret store.
- `CSRFSession` binds each token to a session, so one captured before a login is refused after it; return the
  session identifier or `""`, and keep the hook cheap — it is consulted once per request for the session the
  request arrived with, and again on every render that issues or reuses a token. Its value also keys
  `MaxLiveConnsPerSession`.
- `MaxLiveConnsPerSession` caps the live connections open at once per session; a handshake beyond it is
  refused with 429. 0 means no limit.
- `CSPSources` adds sources to directives of the Content-Security-Policy; see `security`.
- `MaxBodyBytes` caps the body of a POST to a page, multipart included; 0 means `DefaultMaxBodyBytes`
  (10 MiB). A body declared longer is refused with 413 `Request body too large.` before any hook runs, and one
  that turns out longer when read is refused with 413 before any form hook.
- `MaxMultipartMemory` is how much of a multipart form body is kept in memory; the rest goes to temporary
  files, within `MaxBodyBytes`.

### Writers used by generated code

`pkg/render` backs `{{ }}` with the escaper of the context the value lands in (`render.Text`, `render.Attr`,
`render.URL`, `render.URLPart`, `render.URLPathStart`, `render.URLQuery`) and `{{$ }}` with `render.HTML`, which
writes any value unescaped; `render.Format` turns a value into its text first. `render.UnsafeURL`
(`about:invalid#gossr-unsafe-url`) replaces a whole-URL value with another scheme. `mux.TernaryIf` backs the
conditional operator. You would not normally call them yourself, but they explain the rendering semantics
described in `expressions`.

`render.JSON` writes a value as JSON with `<`, `>` and `&` escaped, for app code such as a JSON response.

`render.SafeHTML` is markup that `{{$ }}` writes as it is and `{{ }}` escapes; build it with
`render.HTMLConst` (string constants only), `render.EscapeHTML` and `render.JoinHTML`. It refuses to decode
from JSON or text.

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
`GetCSRFToken()`. `form.SetCSRFToken` and `form.IsMultipart` serve generated code. See `forms` for the
lifecycle these fit into.

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

### Connection and frames

The rest of `pkg/reactive` is used by the mux and generated code, and is not part of the API you write against:

| Name | Purpose |
|---|---|
| `Accept(w, r) (*Conn, error)` | upgrades a request whose checks the caller has run; on its own it checks only that `Origin` matches `Host` |
| `Conn` | one connection: `SendJSON`, `Enqueue`, `StartSendLoop`, `CloseWithError`, `CloseNow`, `Ctx` |
| `(*Conn).ReadFrames(ctx, onWrite, onCall)` | the read loop: each write frame goes to `onWrite`, each call frame with an id of at least 1 to `onCall`; other frame types are ignored, and a frame that is not JSON ends it |
| `InitMsg`, `PatchMsg`, `WriteMsg`, `AckMsg`, `ErrMsg` | live value frames, with `NewInitMsg`, `NewPatchMsg`, `NewAckMsg`, `NewErrMsg` |
| `CallMsg`, `ResultMsg`, `FailMsg` | page call frames, with `NewResult(id, value)` and `NewFail(id, status, msg)` |
| `StatusReauth` | the close code 4000, sent when a connection has lived 10 minutes |
| `ParseValue[T](s)` | parses a scalar from its text as an `ssr:bind` input's text is read: `strconv.ParseBool` for `bool`, base-10 integers within the size of `T`, finite floats; generated code does not call it |

`fe-be-contract` shows the frames on the wire.

## pkg/static — embedded asset serving

Used by the generated `ssrstaticfiles_gen.go`; it serves the embedded build output with ETag validation and
gzip negotiation. See `static-assets`.
