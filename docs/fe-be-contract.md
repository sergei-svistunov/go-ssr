# How the frontend and backend talk

There are exactly three channels between a GoSSR page and the server, plus static assets. Everything the
browser receives is HTML produced on the server.

## 1. Page loads: HTTP GET

```
GET /users/bob/info
        │
        ▼
  router matches segments, records URL params (userId=bob)
        │
        ▼
  route stack: [ / , /users , /users/_userId_ , /users/_userId_/info ]
        │
        ▼
  GetDataContext called deepest → shallowest
    info.Data(ctx, r, w, data)      ─┐
    _userId_.Data(...)               │ each parent receives its rendered child
    users.Data(...)                  │
    root.Data(...)                  ─┘
        │
        ▼
  200 text/html; charset=utf-8
```

Each route's `Data` fills its own `RouteData`. Parents do not see child data and children do not see parent
data; composition happens through `<ssr:content/>`. A route with children never renders itself — it redirects
to a default child (302). See `routing`.

Hooks may write headers and cookies through the `mux.ResponseWriter` they receive, and may return
`mux.NewHttpError`, `mux.NotFound()` or `mux.Redirect(...)` to take over the response.

## 2. Forms: HTTP POST to the same URL

```
POST /contact                (multipart or url-encoded, hidden _csrf_token_)
        │
        ▼
  Init<Name>            populate options and defaults (also runs on GET)
        │
        ▼
  form.Parse            CSRF check, identify which form was submitted   → 400 on failure
        │
        ▼
  field parsing         string → declared Go type, required checks; errors recorded per field
        │
        ▼
  Process<Name>         your hook; runs even when fields have errors
        │
        ▼
  Data + render         the same page, now with values and messages     (or a redirect you returned)
```

There is no JSON API and no client-side validation contract: the form posts, the server re-renders. See
`forms`.

## 3. Reactive updates: one WebSocket per page

```
                    GET /users/bob/info/__ws   (Upgrade: websocket)
browser ────────────────────────────────────────────────────► server
                                                               │
                                        Data re-runs for the route stack
                                        Subscribe starts per reactive route
        ◄──── init      { bindings: { key: "<rendered html>" } }
        ◄──── patch     { key, html }        on every state.Set<Name>
        ───── write ──► { var, value }       from ssr.set() or ssr:bind
        ◄──── ack       { var }              write accepted
        ◄──── err       { var, msg, code }   Validate<Name> rejected it
```

The endpoint lives at the deepest route of the reactive stack with `__ws` appended, and URL parameters are
available inside `Subscribe`. All reactive routes on the page share this one connection; a parent and a child
are multiplexed by route key.

Server pushes carry **pre-rendered HTML**, and the runtime replaces the content of the matching
`data-ssr-bind` element. That is the whole DOM-update model: no vdom, no client templates, no serialisation of
your Go types to the browser. See `reactive` for what becomes a binding and `typescript-api` for the client
surface.

## 4. Static assets

`<ssr:assets/>` renders the `<link>` and `<script defer>` tags for the current route's webpack chunk, using
content-hashed filenames. In production the built files are embedded into the binary and served by a generated
handler with ETag and gzip support. See `static-assets`.

## What runs where

| Concern | Server | Browser |
|---|---|---|
| Rendering HTML | always | never |
| Routing | yes | full page loads only; there is no client router |
| Validation | authoritative (`required`, `Validate<Name>`, your `Process` hook) | optional, cosmetic |
| State of a reactive variable | in the WebSocket connection | last received HTML string only |
| Business logic | data providers | none required |

## Appendix: WebSocket frame shapes

**Internal detail, not a stable contract.** These frames exist to let `gossr-runtime` and the generated Go
talk to each other, and they change together. Documented here so a stuck binding or a reconnect loop can be
diagnosed in browser devtools — do not build another client on them.

Every frame is a WebSocket text frame containing one JSON object with a discriminator field `t`.

```jsonc
// server → client, once per connection
{ "t": "init",  "routeKey": "a1b2c3d4", "bindings": { "<key>": "<rendered html>" } }

// server → client, on each change
{ "t": "patch", "routeKey": "a1b2c3d4", "key": "<binding key>", "html": "<rendered html>" }

// client → server, from ssr.set() or ssr:bind
{ "t": "write", "routeKey": "a1b2c3d4", "var": "displayName", "value": <any JSON> }

// server → client, write accepted
{ "t": "ack",   "routeKey": "a1b2c3d4", "var": "displayName" }

// server → client, write refused
{ "t": "err",   "routeKey": "a1b2c3d4", "var": "displayName", "msg": "…", "code": "validation_failed" }
```

`routeKey` is a short hash of the route path, which is how frames for a parent and a child are told apart on
one connection. A binding key identifies a rendered site, not a variable: a composite expression such as
`{{ a + b }}` has its own key. `err` codes are `unknown_route`, `validation_failed` and `decode_error`; the
last of those means the JSON value did not fit the declared Go type and is logged rather than surfaced to
`ssr.onError`.
