# Security: what the handler enforces

The handler `NewSsrHandler` returns applies these rules to every request on its own: route guards, CSRF
tokens, the cross-site refusal, context escaping, a Content-Security-Policy with security headers, and limits
on live connections. None of them needs wiring beyond a `CSRFKey`; this page says what each one does and
what is left to you.

## The order of a request

The mux runs each step for every route on the path before the next step starts, root first:

1. a cross-site POST, PUT, PATCH or DELETE is refused with 403, then a request that is not GET, HEAD or POST
   with 405, before any hook;
2. `Guard` on every route on the path, gates included;
3. on a POST, the CSRF token check;
4. on the routes that render the page — every layout on the path and the last route — `Init<Form>`, then on a
   POST the submitted form's `Process<Form>`, then `Data`;
5. the page is rendered whole, then sent.

The first hook that returns an error stops the request. A live connection and a page call run their own
subset of these steps; see `reactive` and `page-calls`. Layouts, gates and default children are in `routing`.

## Guards

`Guard` is an optional method of a data provider:

```go
func (p *DP) Guard(ctx context.Context, r *mux.Request) error {
    if sessionFrom(ctx) == nil {
        return mux.Redirect(http.StatusSeeOther, "/login")
    }
    return nil
}
```

It runs before any other hook of any route on the path: before a form's `Init` or `Process`, before any
`Data`, before a live connection opens and before every page call. A guard on a layout or a gate therefore
covers every page below it. Return an `*HttpError` or an `*HttpRedirect` to refuse; see `routing` for what
each becomes on a page, a live connection and a call.

`NewRoute` panics (E21) when a data provider has a method named `Guard` with another signature, or one
declared on `*DP` while the handler is given a `DP` value, so a misspelt guard never leaves a route open.

## CSRF tokens

`mux.Options.CSRFKey` is required: `mux.New`, and so `NewSsrHandler`, returns an error when it is empty or
shorter than 32 bytes. The key signs every token; every instance that serves the site needs the same one,
and tokens survive a restart only if the key does. Keep it in your secret store and read it at start-up, as
the scaffolded `main.go` does with `CSRF_KEY`.

The mux checks the token of every POST after the guards and before any `Init` or `Process` runs, so a forged
POST reaches no form hook. A POST without the cookie is refused with 400 `Missed CSRF token`, one with a bad
token with 400 `Invalid CSRF token`. Cookie names, lifetimes and rotation are in `forms`.

## Cross-site requests

The mux refuses a POST, PUT, PATCH or DELETE that the browser marks as coming from another origin with 403
`Cross-site request`, and a live connection handshake from another origin with 403 `Cross-site live
connection`, before any hook. The rule is `mux.CrossSite` (`mux.CrossSiteStrict` for handshakes, which also
refuses `Origin: null`); `runtime-api` describes it.

- `mux.Options.OriginPatterns` lists other origins allowed to post forms and open live connections, such as
  a development server on another port.
- `mux.Options.AllowCrossSite` exempts single requests from the refusal, and only from it: an exempt POST to
  a page still needs the CSRF cookie and a token the mux signed, and is refused with 400 without them.
- A third-party POST that carries no token, such as a sign-in or payment callback, belongs in an
  `http.Handler` in front of the mux, not in a page.

## Escaping

Every `{{ }}` is escaped for the place the browser reads it: element text, a quoted attribute, or a URL
attribute, where a value that is the whole URL must be relative or use `http`, `https` or `mailto`. Places
where no escaping is safe — a `<script>` or `<style>` body, an `on*` or `style` attribute, an unquoted
attribute, a tag name — are refused when the template is generated, each with a code and a fix (E08–E18 in
`errors`).

`{{$ }}` writes a value unescaped, and **the caller owns the safety of what it writes**. It is refused in an
attribute, `<title>` and `<textarea>` (E13), and in a raw-text element such as `<script>` (E08, E12). Live values are escaped on the server with the
same escapers before they are sent. See `expressions`.

## Content-Security-Policy and headers

Every response of the mux — a page, an error, a redirect, a refused live handshake — carries these headers,
replacing any a hook set:

```
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self' <ws-origin>; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'
X-Content-Type-Options: nosniff
Referrer-Policy: same-origin
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
```

`<ws-origin>` is `wss://<Host>` when the request arrived over TLS (`mux.SecureRequest`), else `ws://<Host>`.
It is left out, with the space before it, when `Host` is neither a host name with an optional port nor a
bracketed IPv6 address with an optional port.

The policy runs only scripts and styles loaded from the site, so the generator refuses an inline `<script>`
body, a `<style>` element, `on*` attributes and `style` attributes (E16, E09, E10): put code and styles in the
route's `index.ts` and `styles.scss`. The embedded static handler sets `X-Content-Type-Options: nosniff` and
`Cross-Origin-Resource-Policy: same-origin` on every asset.

`mux.Options.CSPSources` adds sources to a directive, after its defaults; nothing removes a default:

```go
handler, err := pages.NewSsrHandler(deps, mux.Options{
    CSRFKey: csrfKey,
    CSPSources: map[string][]string{
        "img-src":     {"https://images.example.com"},
        "connect-src": {"https://api.example.com"},
    },
})
```

The directives that take sources are `default-src`, `script-src`, `style-src`, `img-src`, `font-src`,
`connect-src`, `form-action`, `media-src`, `worker-src`, `frame-src` and `manifest-src`. `mux.New` returns an
error for any other directive, and for a source that is empty, contains whitespace, `;`, `,` or a control
character, is `*` or has `*` as its whole host (`https://*`, `*:443`), is a quoted keyword containing `unsafe-`
or is `'strict-dynamic'`; in `script-src` also for a scheme on its own, such as `https:` or `data:`.

## Live connections

A page with live values or page calls opens one WebSocket at its own path followed by `/__ws`. The handshake
passes the checks the page passes — the cross-site refusal, every `Guard`, then `Data` of the routes that
render the page — before it upgrades. Then:

- the connection lives at most 10 minutes; the server closes it with 4000 and the client reconnects at once,
  passing every check again;
- `mux.Options.MaxLiveConnsPerSession` caps the connections open at once for one `CSRFSession` value; a
  handshake beyond it is refused with 429 `Too many live connections.` A request without a session is not
  counted, and 0 means no limit;
- a client that takes more than 10 seconds to accept a frame is closed with 1008 `slow client`;
- a frame larger than 32 KiB, or one that is not JSON, ends the connection;
- a page runs at most 4 calls at once, each for at most 30 seconds (`page-calls`).

`reactive` covers the connection's lifecycle and `fe-be-contract` the frames and close codes.

## Request bodies

The mux caps the body of every POST to a page at `mux.Options.MaxBodyBytes`, 10 MiB by default, multipart
uploads included. A body declared longer is refused with 413 `Request body too large.` before any hook runs;
one without a declared length that turns out longer is refused with 413 when it is read, after the guards and
before any form hook. A multipart body keeps `mux.MaxMultipartMemory` (32 MB) of it in memory and writes the
rest to temporary files, which the cap bounds. Set the cap to what your largest form needs; a proxy body
limit in front is still advised.

## gossr-runtime

go-ssr 0.7.0 needs gossr-runtime 0.2.0, and gossr-runtime 0.2.0 needs go-ssr 0.7.0. Nothing checks the
pairing at run time: install them together. See `typescript-api`.
