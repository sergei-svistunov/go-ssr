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
`RouteDataProvider`, generates a `ReactiveState` type, emits `__ssr_gen__.ts`, and mounts a WebSocket endpoint.

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
- Return when `ctx` is done. The context is cancelled when the client disconnects.
- A panic is recovered: that connection closes and the client reconnects, but the server stays up.
- `r.URLParam` works, so a per-user subscription can read the route's parameters.
- `Data` runs again on every reconnect. Do not rely on `r.Method`, a POST body, or writing response headers
  inside `Data`, because a reconnect replays it outside the original request.

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
an error rejects the write; the browser receives an error frame, which reaches `ssr.onError` in TypeScript.

There are two ways to write from the browser:

```html
<input ssr:bind="displayName" placeholder="Your name"/>
```

`ssr:bind` wires a native `<input>`, `<select>` or `<textarea>` in both directions with no TypeScript at all:
user input sends the value, a server push updates `element.value`. Restrictions:

- the variable must be declared in the same route as the element,
- it must be `client-writable="true"`,
- it must have a scalar type, because a native element's value is a string,
- it cannot be used on `<ssr:input>`, `<ssr:select>` or `<ssr:textarea>` — those submit forms.

Each of those is a generation error; see `errors`.

For anything else — non-scalar values, custom widgets, programmatic updates — call the generated client:

```typescript
import { ssr } from './__ssr_gen__';

ssr.set('displayName', 'Ada');
```

See `typescript-api`.

## What becomes a live binding

The generator scans each template for sites that read a reactive variable:

| Site | Patch behaviour |
|---|---|
| `{{ count }}` | the expression is wrapped in a `<span data-ssr-bind>` and its text is replaced |
| `{{ a + b }}` | tracked against both variables; either change re-renders the site |
| attribute value, e.g. `style="color: {{ tone }}"` | the whole element is wrapped in an invisible block and re-rendered with new attributes |
| `ssr:if` / `ssr:else-if` chain | the chain is wrapped in an `<ssr-block>`; a change re-renders the active branch, or collapses to empty when none matches |
| `ssr:for`, or any reactive variable inside the loop body | the entire loop is wrapped and all iterations re-render |

Nested sites are not patched twice: when an outer block re-renders, the expressions inside it are covered by
that single patch.

The wrapper elements are real DOM nodes. `<ssr-block>` is an unknown element, so it is `display: contents` by
way of a stylesheet the runtime ships; for a loop whose body is a `<tr>` the wrapper is a `<tbody>` instead,
because a table would otherwise foster-parent the wrapper out and duplicate rows.

Loops re-render in full on every change. There is no keyed list diffing yet, so a large table that updates
frequently pays for the whole body each time.

## One connection per page

The WebSocket endpoint is mounted at the deepest route of a reactive stack, with `__ws` appended — for a page
at `/users/{userId}/info` whose root layout is reactive, that is `/users/{userId}/info/__ws`. `__ws` is a
reserved directory name because of this.

A parent and a child may both declare reactive variables. The generator emits a single endpoint at the leaf,
runs each route's `Subscribe` concurrently, and multiplexes the patches over one connection. Each route sees
only its own `*ReactiveState`; binding keys are namespaced per route, so a parent and a child can both have a
variable named `count`.

In the browser, all reactive routes on a page share one WebSocket through a multiplexing singleton in
`gossr-runtime`, so a nested page opens one connection, not several.

## Reconnection

The client reconnects with backoff after an unexpected close. On reconnect the server re-runs `Data` and
`Subscribe` for the route stack and sends a fresh init frame containing the current rendered value of every
binding, so the page converges on the current state without a reload. Anything cached in a `Subscribe` local
variable is rebuilt from scratch — treat `Subscribe` as restartable.

## Cost and caveats

- Every connected client holds a goroutine per reactive route plus a WebSocket. Patches are coalesced per
  binding, and a slow client is disconnected rather than allowed to back up the sender.
- Patch payloads are rendered HTML strings, so a binding wrapping a large subtree sends that subtree on every
  change. Keep reactive sites small.
- A reactive variable's value lives in the connection's state, not in a session. Two tabs are two
  independent connections with independent state.
