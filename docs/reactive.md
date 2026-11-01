# Reactive bindings: live values without client-side rendering

A reactive variable is rendered on the server as usual, and then re-rendered and pushed to the browser as a
small HTML patch whenever it changes. The browser receives markup, not JSON, and there is no client-side
template.

## Declaring

```html
<ssr:var name="visitorsOnline" type="int" reactive="true"/>
<ssr:var name="displayName" type="string" reactive="true" client-writable="true"/>
```

| Attribute | Effect |
|---|---|
| `reactive="true"` | the value can be pushed from the server after the initial render |
| `client-writable="true"` | the browser may also write it, gated by a generated `Validate<Name>` hook |

`reactive="true"` works with any Go type — scalars, structs, slices, maps, pointers, `time.Time`, named types.
`client-writable="true"` additionally requires a `Validate<Name>` implementation.

Declaring a reactive variable makes the route reactive, which adds a `Subscribe` method to its
`RouteDataProvider`, generates a `ReactiveState` type and emits `__ssr_gen__.ts`; every page that renders the
route opens a live connection.

## Pushing values from the server

```go
// Subscribe is called once per WebSocket connection and runs for its lifetime.
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
    ticker := time.NewTicker(4 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            n := p.m.VisitorsOnline()
            state.SetVisitorsOnline(n)
        }
    }
}
```

`ReactiveState` has one `Set<Name>` method per reactive variable, typed to the declared Go type. Calling it
re-renders every binding site that reads the variable and enqueues the patches.

Things worth knowing about `Subscribe`:

- It runs on its own goroutine per connection, so blocking in it is fine and expected.
- Return when `ctx` is done. The context is cancelled when the connection closes.
- Its context derives from the handshake request's context, so a value your middleware put there — a
  session, a user — stays available for as long as the connection lives.
- Returning `nil` leaves the connection open; the route just pushes nothing more. Returning an `*HttpError`
  with 401 or 403, or a redirect, closes the connection with 1008 and the error's message as the reason; any
  other error closes it with 1011, logged.
- A panic is recovered: that connection closes with 1011 and the client reconnects, but the server stays up.
- `r.URLParam` reads the page's parameters, and `r.URL` is the page's address with its query, so a
  per-user subscription can read them.
- `Data` runs again on every connection, before `Subscribe`. Do not rely on `r.Method`, a POST body, or
  writing response headers inside `Data`: on a connection it gets a `mux.NoopResponseWriter`.

To wake `Subscribe` from elsewhere in the process — another handler, a worker — use the small pub/sub in
`pkg/reactive`; see `runtime-api`.

## Accepting values from the browser

Add `client-writable="true"` and implement the validation hook:

```go
func (p *DP) ValidateDisplayName(ctx context.Context, r *mux.Request, val string) (string, error) {
    val = strings.TrimSpace(val)
    if len(val) > 40 {
        return "", errors.New("display name is too long")
    }
    return val, nil
}
```

The returned value is what gets stored and re-rendered, so the hook can normalise as well as reject. Returning
an error rejects the write; the browser receives an error frame, which reaches `ssr.onError` in TypeScript. The
stub the generator writes rejects every write until you replace it.

A client-writable variable may not hold markup: the generator refuses a type that is or contains a `render`
type, such as `render.SafeHTML`, and any generic type. `render.SafeHTML` also refuses to decode from JSON or
text, so no value a page writes can become markup, whatever package declares the type.

There are two ways to write from the browser:

```html
<input ssr:bind="displayName" placeholder="Your name"/>
<p>Hello, {{ displayName }}</p>
```

`ssr:bind` wires a native `<input>`, `<select>` or `<textarea>` in both directions with no TypeScript at all.
Restrictions:

- the variable must be declared in the same route as the element,
- it must be `client-writable="true"`,
- it must have a scalar type (`string`, `bool`, a sized integer or float type), because a native element's
  value is a string,
- it cannot be used on `<ssr:input>`, `<ssr:select>` or `<ssr:textarea>` — those submit forms.

Each of those is a generation error; see `errors`. The generator marks the element with `data-ssr-route`, the
key of its route, so only that route's runtime wires it; a template cannot set `data-ssr-route` itself.

What the element sends: its text on each `input` event (text-like inputs, `<textarea>`) or on `change`
(`<select>`, radio buttons); a checkbox sends its checked state as a JSON boolean. The server decodes the
value into the variable's type, and for a number or `bool` variable it also reads text: `strconv.ParseBool`
forms (`true`, `false`, `1`, `0`, `t`, `f`, …) for `bool`, base-10 `strconv.ParseInt`/`ParseUint` for integers,
and `strconv.ParseFloat` for floats, never `NaN` or an infinity. Text that does not parse is refused with a
`validation_failed` error frame before `Validate<Name>` runs.

What the element receives: the server's value for a plain `{{ displayName }}` site of the variable, as text.
The runtime decodes the server's escaped markup in an inert document and sets it as the element's value; a
checkbox is checked by `true`, a radio button by its own value, which is never rewritten. A text control that
has focus is not overwritten while the user types; a checkbox or radio button is set even when it has focus.
A refused write resets the element to the last value the server sent for that site. Without a plain
`{{ var }}` site of the variable on the page, the element gets no echo, and a refused write leaves it as the
user left it.

For anything else — non-scalar values, custom widgets, programmatic updates — call the generated client:

```typescript
import { ssr } from './__ssr_gen__';

ssr.set('displayName', 'Ada');
```

`set` sends any JSON value; the server decodes it with `encoding/json` into the declared type, so `null` for a
slice, map or pointer variable sets it to nil. See `typescript-api`.

## What becomes a live binding

The generator scans each template for sites that read a reactive variable:

| Site | Patch behaviour |
|---|---|
| `{{ count }}` | the expression is wrapped in a `<span data-ssr-bind>` and its text is replaced |
| `{{ a + b }}` | tracked against both variables; either change re-renders the site |
| attribute value, e.g. `class="badge {{ tone }}"` | the whole element is wrapped in an invisible block and re-rendered with new attributes |
| `ssr:if` / `ssr:else-if` chain | the chain is wrapped in an `<ssr-block>`; a change re-renders the active branch, or collapses to empty when none matches |
| `ssr:for`, or any reactive variable inside the loop body | the entire loop is wrapped and all iterations re-render |

The server renders a live value with the same escapers as the first render and sends one markup string per
binding: a `{{ }}` value arrives escaped, a `{{$ }}` value as it is. The runtime puts that string into the
`data-ssr-bind` element with `innerHTML`, or, for a form control, its decoded text as the control's value. A
live value cannot sit in the text of `<title>` or `<textarea>`, whose text a live update cannot change; the
generator refuses it and names the first live variable. Nor can an `<ssr:form>` sit in a live block, whose
re-render has neither the form's values nor its token; the generator refuses it at the form's line. For the
same reason a live block cannot hold `<ssr:content/>` or `<ssr:assets/>`.

Inside an `<ssr:form>` a live site is live as anywhere else, as long as it does not read the form: a site that
reads `form`, `input` or `textarea`, or a block that holds a form field, is refused at its line, and so is a
form or field attribute that reads a live variable. Keep the live value and the form's values in separate
sites:

```html
<ssr:form name="order">
    <p>{{ itemsLeft }} left</p>                         <!-- live -->
    <p ssr:if="form.Qty.HasError()">{{ form.Qty.GetError() }}</p>  <!-- reads the form, not live -->
</ssr:form>
```

Keep native `<input>`, `<select>` and `<textarea>` elements out of live blocks too, inside a form or not: a live
update replaces the block's markup, so it recreates them empty and drops what the user typed.

Each site has a binding key. A site that renders exactly one variable, `{{ count }}`, keeps the variable's name
as its key, which is what `ssr.get('count')`, `ssr.on('count', …)` and the `ssr:bind` echo use. Any other site
gets a hash of its source, and a `{{$ count }}` site never shares a key with a `{{ count }}` site.

Nested sites are not patched twice: when an outer block re-renders, the expressions inside it are covered by
that single patch.

The wrapper elements are real DOM nodes. `<ssr-block>` is an unknown element, so it is `display: contents` by
way of a stylesheet the runtime ships; for a loop whose body is a `<tr>` the wrapper is a `<tbody>` instead,
because a table would otherwise foster-parent the wrapper out and duplicate rows.

Loops re-render in full on every change. There is no keyed list diffing, so a large table that updates
frequently pays for the whole body each time.

A block whose markup changes is replaced with the elements inside it. The runtime wires the `ssr:bind` elements
of the new markup after each render, so they keep sending and receiving values, but what the user had typed
into the replaced element is gone: keep a control the user types into outside a block that changes while they
type. Focus inside the block moves, without scrolling, to the element at the same place in the new markup (the
same child-element index at every level down from the block); when nothing that can take focus is there, focus
is not moved back into the block.

A value equal to the markup a block already holds leaves the block alone: the markup the runtime last applied
to it or, before any, the markup the page was served with. The init frame after every connect and reconnect,
and a patch that repeats a value, therefore keep the block's elements, their focus and their DOM state, such
as an open `<details>`.

## One connection per page

A page whose rendered routes — every layout on its path and the page itself — have live values or page calls
opens one WebSocket at the page's path with `/__ws` appended: for `/users/{userId}/info`, that is
`/users/{userId}/info/__ws`, and for `/`, `/__ws`. The generated client adds the page's query string, so the
connection's `Data` sees the same query as the page. `__ws` is a reserved directory name because of this.

A parent and a child may both declare reactive variables. The connection runs each route's `Subscribe`
concurrently and multiplexes the patches over it. Each route sees only its own `*ReactiveState`; binding keys
are namespaced per route, so a parent and a child can both have a variable named `count`. A gate's live
values never reach a page below it: the gate does not render that page.

In the browser, all routes on a page share one WebSocket through a multiplexing singleton in `gossr-runtime`,
so a nested page opens one connection, not several. A page with calls and no live values opens one too. The
singleton is one per copy of `gossr-runtime`, so the page's webpack entries must share one runtime: the
scaffolded `webpack.config.js` sets `optimization.runtimeChunk: 'single'`. Without it each entry runs its own
copy, opens its own connection, and the runtime logs a warning.

## What the handshake checks

The handshake passes the checks the page itself passes, before the upgrade:

1. the cross-site refusal, `mux.CrossSiteStrict`: `Sec-Fetch-Site: same-origin` or `none` is accepted; with
   `same-site` or `cross-site` the `Origin` must be listed in `mux.Options.OriginPatterns`; when the browser
   sends no `Sec-Fetch-Site`, the `Origin` must be listed there or match the request `Host` (and be `https` when
   the request arrived over TLS). It is the rule forms use with one difference: an opaque `Origin: null`, which
   a sandboxed iframe sends, is always refused, because no CSRF token follows a handshake. A handshake with no
   `Origin` at all is accepted — that is a non-browser client. `mux.Options.AllowCrossSite` exempts single
   handshakes; it sees the handshake's own address, ending in `/__ws`;
2. every `Guard` on the path;
3. the page has live values or calls, and is not a layout opened itself; otherwise 404;
4. `mux.Options.MaxLiveConnsPerSession`: a session at its limit is refused with 429;
5. `Data` of the routes that render the page, whose values become the init frame.

A refusal goes to `ErrorHandler` before the upgrade: a 4xx `*HttpError` keeps its status, a redirect becomes
401, anything else 500. The connection carries the session cookies, which is why a third-party page must not
open one; a refused cross-site handshake is logged with the `Origin` and `Host` seen.

If the page is served from a different origin than the Go process — a dev server that proxies only some
paths — list that origin in `mux.Options{OriginPatterns: []string{"dev.example:*"}}`; `dev.example:*` matches
any explicit port and a bare `dev.example` the default port.

`reactive.Accept`, which the mux calls after these checks, knows nothing of `OriginPatterns` or guards. Called
on its own it applies only the WebSocket library's check that `Origin` matches `Host`; a handler of your own
that accepts WebSockets must check the origin and the session itself.

## Reconnection and hidden tabs

- **Every 10 minutes** the server closes the connection with 4000. The client reconnects at once, and the
  handshake checks the guards and the session again; a connection closed with 4000 sooner than 30 seconds
  after it opened reconnects with back-off instead.
- **After an unexpected close** (1006, 1008, 1011) the client reconnects with back-off. 1000 and 1001 are
  final.
- **While the tab is hidden** the client closes the connection, and reopens it when the tab is shown again;
  a page opened in a hidden tab connects when first shown. If that reopening handshake is refused, the client
  retries with back-off and rejects the page calls waiting at that moment, until a socket opens or the third
  handshake in a row is refused.
- **A refused handshake** — the first one, one after a drop, or the third in a row after the tab is shown
  again — is final: the client fires `onConnectError` and does not retry. The session may have ended; reload the page there.
- **`ssr.stop()`**, called on any route's client, closes the page's connection for good: no route reconnects,
  and page calls reject with status 0. The event that ended a connection reaches every route's
  `onDisconnect` or `onConnectError` once, even when an earlier handler calls `stop()`.

On every new connection the server runs `Guard` and `Data` again and sends a fresh init frame with the current
rendered value of every binding, so the page converges on the current state without a reload. Anything cached
in a `Subscribe` local variable is rebuilt from scratch — treat `Subscribe` as restartable.

## Cost and caveats

- Every connected client holds a goroutine per reactive route plus a WebSocket. Patches are coalesced per
  binding, and a client that takes more than 10 seconds to accept a frame is disconnected rather than allowed
  to back up the sender; it sees the connection drop (1006), since the frame it stalled is cut off. Cap connections per session with `mux.Options.MaxLiveConnsPerSession`.
- A frame from the browser larger than 32 KiB, or one that is not JSON, ends the connection.
- Patch payloads are rendered HTML strings, so a binding wrapping a large subtree sends that subtree on every
  change. Keep reactive sites small.
- A reactive variable's value lives in the connection's state, not in a session. Two tabs are two
  independent connections with independent state.
