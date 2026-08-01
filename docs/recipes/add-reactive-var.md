# Recipe: add a live value

Pushing a value from the server over the WebSocket, and accepting one back from the browser.

## Server-pushed, read-only

### 1. Declare it

```html
<!-- internal/web/pages/home/index.html -->
<ssr:var name="visitorsOnline" type="int" reactive="true"/>
<p>{{ visitorsOnline }} people online</p>
```

### 2. Regenerate

```bash
go-ssr
```

The route is now reactive: its interface gains `Subscribe`, a `ReactiveState` type is generated, and
`__ssr_gen__.ts` appears next to the template. Webpack bundles that file automatically, so the WebSocket
connects with no TypeScript from you.

### 3. Set the initial value in Data, push changes from Subscribe

```go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    data.VisitorsOnline = p.m.VisitorsOnline()
    return nil
}

func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
    ticker := time.NewTicker(4 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            state.SetVisitorsOnline(p.m.VisitorsOnline())
        }
    }
}
```

`Data` runs again on every reconnect, so keep it free of anything that depends on the original request being a
POST or on writing headers.

## Pushing from elsewhere in the process

Polling on a ticker is rarely what you want. `pkg/reactive` lets any goroutine wake the subscriber:

```go
// package-level, next to your model
var presence = reactive.NewBroadcast[int]()

// wherever the count changes
presence.Publish(newCount)
```

```go
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
    sub := presence.Subscribe()
    defer sub.Close()

    for {
        select {
        case <-ctx.Done():
            return nil
        case n := <-sub.Updates():
            state.SetVisitorsOnline(n)
        }
    }
}
```

Use `reactive.NewTopic[K, V]()` for per-key fan-out, keyed by user id for example, and read the key with
`r.URLParam(...)`. `Publish` never blocks. See `runtime-api`.

## Client-writable, with two-way binding

### 1. Declare and bind

```html
<ssr:var name="displayName" type="string" reactive="true" client-writable="true"/>
<input ssr:bind="displayName" placeholder="Your name"/>
<p>Hello, {{ displayName }}</p>
<span id="display-name-error"></span>
```

### 2. Regenerate and implement the validator

```go
func (p *DP) ValidateDisplayName(ctx context.Context, r *mux.Request, val string) (string, error) {
    val = strings.TrimSpace(val)
    if len(val) > 40 {
        return "", errors.New("display name is too long")
    }
    return val, nil
}
```

The returned value is what gets stored and rendered, so this is the place to normalise. Returning an error
rejects the write and sends an error frame to the browser.

### 3. Optional: surface rejections

```typescript
// internal/web/pages/home/index.ts
import { ssr } from './__ssr_gen__';

const errorEl = document.getElementById('display-name-error');

ssr.onError((varName, message) => {
    if (varName === 'displayName' && errorEl) {
        errorEl.textContent = message;
    }
});

ssr.on('displayName', () => {
    if (errorEl) errorEl.textContent = '';
});
```

`ssr:bind` only works on native `<input>`, `<select>` and `<textarea>` elements, with a scalar variable declared
in the same route. For anything else call `ssr.set('name', value)` from TypeScript.

## Non-scalar live values

Any Go type works with `reactive="true"`:

```html
<ssr:var name="rows" type="[]Row" reactive="true"/>
<table>
    <tr ssr:for="row in rows"><td>{{ row.Name }}</td><td>{{ row.Total }}</td></tr>
</table>
```

The whole loop re-renders on every change — there is no keyed diffing — so keep frequently-updating reactive
regions small. A struct or slice cannot be used with `ssr:bind` (see error E07 in `errors`); write it with
`ssr.set()`.

## Checklist

- [ ] the variable has `reactive="true"`, and `client-writable="true"` only if the browser writes it
- [ ] `Data` sets the initial value; `Subscribe` pushes changes and returns on `ctx.Done()`
- [ ] `Validate<Name>` exists for every client-writable variable
- [ ] no directory named `__ws` anywhere under `pages/` (error E01)
- [ ] `gossr-runtime` is in `package.json` and `moduleResolution` is `bundler` or `nodenext`
