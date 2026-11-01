# How the frontend and backend talk

There are exactly three channels between a GoSSR page and the server, plus static assets: page loads, form
posts, and one live connection per page that carries live values and page calls. Everything the browser
renders is HTML produced on the server.

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
  Guard on every route, root first           a refusal goes to ErrorHandler
        │
        ▼
  Data on the routes that render the page, root first
    root.Data(ctx, r, w, data)
    users.Data(...)
    _userId_.Data(...)
    info.Data(...)
        │
        ▼
  render the root template, each layout writing its child at <ssr:content/>
        │
        ▼
  200 text/html; charset=utf-8, with the security headers
```

Each route's `Data` fills its own `RouteData`. Parents do not see child data and children do not see parent
data; composition happens through `<ssr:content/>`. A layout opened itself redirects to its default child
(302), and a gate renders only its own page. See `routing`.

Hooks may write headers and cookies through the `mux.ResponseWriter` they receive, and may return
`mux.NewHttpError`, `mux.NotFound()` or `mux.Redirect(...)` to take over the response. Every response carries
the Content-Security-Policy and the other headers in `security`.

## 2. Forms: HTTP POST to the same URL

```
POST /contact                (multipart or url-encoded, hidden _csrf_token_)
        │
        ▼
  mux                   cross-site request → 403 before any hook
        │
        ▼
  Guard                 on every route on the path
        │
        ▼
  mux                   CSRF cookie and token check → 400; identify which form was submitted
        │
        ▼
  Init<Name>            populate options and defaults (also runs on GET)
        │
        ▼
  field parsing         string → declared Go type, required checks; errors recorded per field
        │
        ▼
  Process<Name>         your hook; runs only when the fields have no errors
        │
        ▼
  Data + render         the same page, now with values and messages     (or a redirect you returned)
```

There is no JSON API and no client-side validation contract: the form posts, the server re-renders. See
`forms`.

## 3. The live connection: one WebSocket per page

```
                    GET /users/bob/info/__ws   (Upgrade: websocket)
browser ────────────────────────────────────────────────────► server
                                                               │
                                        cross-site check, Guard, session limit
                                        Data on the routes that render the page
                                        Subscribe starts per reactive route
        ◄──── init      { bindings: { key: "<rendered html>" } }
        ◄──── patch     { key, html }        on every state.Set<Name>
        ───── write ──► { var, value }       from ssr.set() or ssr:bind
        ◄──── ack       { var }              write accepted
        ◄──── err       { var, msg, code }   Validate<Name> rejected it
        ───── call ───► { id, name, args }   from ssr.call()
        ◄──── result    { id, value }        the call returned
        ◄──── fail      { id, status, msg }  the call failed
```

A page opens the connection when a route that renders it — a layout on its path or the page itself — has live
values or page calls. Its address is the page's path with `/__ws` appended (`/__ws` for `/`), plus the page's
query string. All routes on the page share this one connection, told apart by route key; the page's webpack
entries share one runtime for that (`static-assets`). `ssr.stop()` on any route's client closes it for good,
for every route.

Server pushes carry **markup the server rendered and escaped**, one string per binding, and the runtime puts it
into the matching `data-ssr-bind` element unless the element already holds that markup; a form control gets
the decoded text as its value instead. That is the whole DOM-update model: no vdom, no client templates. Page
calls carry JSON arguments and results. See `reactive` for what becomes a binding, `page-calls` for calls and
`typescript-api` for the client surface.

## 4. Static assets

`<ssr:assets/>` renders the `<link>` and `<script defer>` tags for the current route's webpack chunk, using
content-hashed filenames. In production the built files are embedded into the binary and served by a generated
handler with ETag and gzip support. See `static-assets`.

## What runs where

| Concern | Server | Browser |
|---|---|---|
| Rendering HTML | always | never |
| Routing and access | yes, `Guard` on every request, handshake and call | full page loads only; there is no client router |
| Validation | authoritative (`required`, `Validate<Name>`, your `Process` hook, call arguments) | optional, cosmetic |
| State of a reactive variable | in the WebSocket connection | last received markup only |
| Business logic | data providers | none required |

## Appendix: WebSocket frames and close codes

**Internal detail, not a stable contract.** These frames exist to let `gossr-runtime` and the generated Go
talk to each other, and they change together: go-ssr 0.7.0 pairs with gossr-runtime 0.2.0, and nothing checks
the pairing at run time. Documented here so a stuck binding, a failed call or a reconnect loop can be diagnosed
in browser devtools — do not build another client on them.

Every frame is a WebSocket text frame containing one JSON object with a discriminator field `t`.

```jsonc
// server → client, once per connection
{ "t": "init",   "routeKey": "a1b2c3d4", "bindings": { "<key>": "<rendered html>" } }

// server → client, on each change
{ "t": "patch",  "routeKey": "a1b2c3d4", "key": "<binding key>", "html": "<rendered html>" }

// client → server, from ssr.set() or ssr:bind
{ "t": "write",  "routeKey": "a1b2c3d4", "var": "displayName", "value": <any JSON> }

// server → client, write accepted
{ "t": "ack",    "routeKey": "a1b2c3d4", "var": "displayName" }

// server → client, write refused
{ "t": "err",    "routeKey": "a1b2c3d4", "var": "displayName", "msg": "…", "code": "validation_failed" }

// client → server, from ssr.call()
{ "t": "call",   "id": 7, "routeKey": "a1b2c3d4", "name": "refreshLastSeen", "args": <any JSON> }

// server → client, the call returned
{ "t": "result", "id": 7, "value": <any JSON> }

// server → client, the call failed
{ "t": "fail",   "id": 7, "status": 404, "msg": "This call is not on this page." }
```

`routeKey` is a short hash of the route path, which is how frames for a parent and a child are told apart on
one connection. A binding key is the route key and a local key joined by `.`; the local key is the variable's
name for a site that renders exactly that variable, and a hash of the source for any other site. `err` codes
are `unknown_route`, `validation_failed` and `decode_error`; the last of those means the JSON value did not fit
the declared Go type and is logged rather than surfaced to `ssr.onError`.

A call's `id` counts up from 1 for each page, across reconnects, and comes back in the one `result` or `fail`
frame that answers it. The server ignores a call with id 0. The client ignores an answer to a call it did not
send on that socket.

The server ignores frames of an unknown type, and logs the first by its type. A frame that is not JSON ends
the connection without a close frame, and one larger than 32 KiB closes it with 1009. A client that takes more
than 10 seconds to accept a frame is disconnected: the frame it stalled is cut off, so no close frame follows,
and the client sees 1006.

| Close code | Sent when | Client |
|---|---|---|
| 1000 | normal close; also the client's own close when the tab is hidden | final; reopens only when a hidden tab is shown |
| 1001 | the other side is going away | final; fires `onDisconnect` |
| 1008 | a `Subscribe` returned a 401 or 403 `*HttpError` or a redirect, or a `Guard` refused a call (after its `fail` frame) | logs the reason, reconnects with back-off |
| 1011 | a `Subscribe` failed or a hook panicked | reconnects with back-off |
| 4000 | the connection has lived 10 minutes | reconnects at once if it was open at least 30 s, else with back-off |

A connection that drops without a close frame (1006), such as after an invalid frame, or closes with any other
code, reconnects with back-off. A refused upgrade carries an HTTP status instead: 400 for a request that is not a WebSocket GET, 403
for a cross-site handshake, 404 for a page without live values or calls, 429 beyond
`mux.Options.MaxLiveConnsPerSession`, and a guard's or `Data`'s refusal (a redirect becomes 401). The client
treats the first refused upgrade, and one after a drop, as final; after the tab is shown again it retries with
back-off, and the third refused upgrade in a row is final.
