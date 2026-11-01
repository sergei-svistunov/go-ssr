# Page calls: functions the page's TypeScript calls

A page call is a Go function of a route that the page's TypeScript runs and awaits, over the page's live
connection. The template declares it, the data provider implements it, and the generated client calls it with
typed arguments and result. Every call passes the route guards again.

## Declare, implement, call

The template declares the call with its argument and result types:

```html
<!-- pages/users/_userId_/info/index.html -->
<ssr:call name="refreshLastSeen" in="struct{}" out="LastSeenOut"/>
<button type="button" id="refresh-last-seen">Refresh last seen</button>
<span id="refresh-last-seen-result"></span>
```

Both types must be ones the route package can name; declare them in a Go file next to the template. After
`go-ssr`, the route's `RouteDataProvider` requires `Call<Name>`:

```go
// LastSeenOut is the result of the refreshLastSeen call.
type LastSeenOut struct {
    LastSeen string    `json:"lastSeen"`
    At       time.Time `json:"at"`
}

func (p *DP) CallRefreshLastSeen(ctx context.Context, r *mux.Request, in struct{}) (LastSeenOut, error) {
    login := r.URLParam("userId")
    if p.model.GetUserByLogin(login) == nil {
        return LastSeenOut{}, mux.NewHttpError(http.StatusNotFound, "user wasn't found")
    }
    now := time.Now()
    p.model.TouchUserLastSeen(login, now.Unix())
    return LastSeenOut{LastSeen: formatLastSeen(now.Unix()), At: now.UTC()}, nil
}
```

The stub the generator writes into a new `dataprovider.go` refuses every call with 403 until you write it.
The page calls it through the generated client:

```typescript
// pages/users/_userId_/info/index.ts
import { CallError } from 'gossr-runtime';
import { ssr } from './__ssr_gen__';

const result = document.getElementById('refresh-last-seen-result');

document.getElementById('refresh-last-seen')?.addEventListener('click', () => {
    ssr.call('refreshLastSeen', {})
        .then((out) => {
            if (result) result.textContent = 'Refreshed: ' + out.lastSeen;
        })
        .catch((err: unknown) => {
            if (result) result.textContent = err instanceof CallError ? err.message : 'The call failed.';
        });
});
```

`<ssr:call>` takes `name` (a lower-case ASCII letter followed by letters and digits), `in` and `out`, and sits
once at the template's top level (E19). See `template-syntax`.

## What runs on a call

A call frame names the route and the call. The server then:

1. runs every `Guard` on the page's path again, on fresh route states;
2. finds the route that renders the page and has that route key — every layout on the path and the page
   itself, but not a gate above the page;
3. decodes the arguments and runs `Call<Name>`.

`Data` does not run, and the route's `RouteData` is empty: load what the call needs in the call. The
`*mux.Request` is the page's: its URL is the page's path and query, `r.URLParam` reads the page's parameters,
and its context keeps the values of the handshake request. The context is cancelled when the call times out
or the connection closes.

A layout's calls can therefore be called from every page below it, through the layout's own `__ssr_gen__.ts`.
A gate renders only its own page, so its calls are reachable from that page alone.

A page that declares calls, and no live values, still opens a live connection, which counts toward
`mux.Options.MaxLiveConnsPerSession`.

## Arguments

`mux.DecodeArgs` reads the arguments strictly; each of these is a 400 `The call has the wrong arguments.`,
and the call does not run:

- a field the `in` type does not have;
- data after the JSON value;
- a value that does not decode into the `in` type;
- `null` for an `in` type that is not a pointer, slice, map, interface or a struct with no fields, such as
  `struct{}`.

An `in` of `struct{}` is `Record<string, never>` in TypeScript: pass `{}`. An argument left `undefined` travels
as `null`, which a `struct{}` also accepts.

## Results and failures

The call's result is encoded with `encoding/json` and resolves the promise. Its TypeScript type is mapped from
the route package's declarations as `encoding/json` writes them: `time.Time` becomes `string`, a pointer
`T | null`, a `[]byte` a base64 `string`. A nil slice or map arrives as `null`, though its type does not say so;
return an empty one to send `[]` or `{}`. A type with a value-receiver `MarshalText` maps to `string`. A type
with its own `MarshalJSON`, a generic type, or a type from another package other than `time.Time` maps to
`unknown`, and the generator prints a warning where it is written. An `out` of interface type sends whatever
its dynamic value encodes to.

A call that fails rejects with a `CallError` whose `status` and `message` come from the fail frame:

| What happened | `status` | `message` |
|---|---|---|
| the call returned an `*HttpError` with a 4xx or 5xx status | its status | its message, or the status text when empty |
| a `Guard` returned an `*HttpError` with a 4xx status | its status | its message, or the status text when empty |
| a `Guard` returned a redirect | 401 | `Unauthorized` |
| the arguments did not decode | 400 | `The call has the wrong arguments.` |
| the route key or the name is not a call of this page | 404 | `This call is not on this page.` |
| the connection already runs 4 calls | 429 | `Too many calls at once.` |
| the call, guards included, ran past 30 seconds | 504 | `The call took too long.` |
| the call returned a redirect, any other error or panicked; a `Guard` returned any other error | 500 | `Something went wrong.` |
| the connection was lost before the answer came | 0 | `The connection was lost.` |

The message of an `*HttpError` reaches the page, so write it for people and keep internals out of it; every
500 is logged on the server and shows only `Something went wrong.`, except a call that returns its context's
cancellation after the connection ended, which no one waits for. A call cannot redirect the browser: return
data from the call and navigate with `location.assign` in the page's TypeScript. A returned
`mux.Redirect` is a 500, logged with that advice.

A `Guard` that refuses a call answers its fail frame and then closes the connection with 1008, as it would
refuse a new one. A panic in the call fails only that call; the connection stays open.

## Limits

- A call has 30 seconds, its guards included. At the deadline the page gets 504, whether or not the call has
  returned, and its context is cancelled.
- A connection runs at most 4 calls at once; a fifth is answered with 429 at once. A call keeps its place until
  it returns, even after its 504 has gone.
- A frame larger than 32 KiB, or one that is not JSON, ends the connection, failing every call in flight with
  status 0. Keep arguments small; send files with a form.

## Connection loss and retries

A call made before the connection opens, or while it reconnects, waits and is sent when it opens. Every call
that waited goes out at once, so some may get 429. While the tab is hidden the connection is closed: calls in
flight reject with status 0, and new calls wait until the tab is shown again (`typescript-api`).

Status 0 means the answer did not arrive, not that the call did not run: the server may have finished it. The
runtime never retries a call. Retry yourself only when running the call twice is harmless, or make the call
idempotent.
