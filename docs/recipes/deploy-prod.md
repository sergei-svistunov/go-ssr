# Recipe: build for production

The goal is one binary that serves both pages and assets, with no runtime dependency on Node or on files next
to the executable.

## Build

```bash
go-ssr -prod          # webpack in production mode, assets staged and embedded
go build -o myapp .
```

`-prod` also regenerates the Go code, so run it before `go build`, not after. The result embeds the built
assets, gzip-precompressed where it helps, and serves them from the generated static handler ahead of the SSR
router. Nothing else needs deploying.

## What to commit

Commit the generated files. `ssrroute_gen.go`, `ssrhandler_gen.go` and `ssrstaticfiles_gen.go` are ordinary Go
source, and committing them means a plain `go build` works in CI without Node installed.

Do not commit the staging directory:

```gitignore
**/pages/static_embed/
internal/web/static/
internal/web/node_modules/
```

## CI shape

```bash
# build stage: needs Node + Go
npm --prefix internal/web ci
go-ssr -prod
go vet ./...
go test ./...
go build -trimpath -o myapp .

# runtime stage: needs neither
COPY --from=build /src/myapp /usr/local/bin/myapp
```

If the generated files are committed and unchanged, `go build` alone is enough for a runtime image; the
`go-ssr -prod` step exists to prove the committed output matches the sources. A useful CI check is to run it and
fail on a dirty tree:

```bash
go-ssr -prod && git diff --exit-code
```

## Serving

```go
func main() {
    m := model.New(openDB())

    srv := &http.Server{
        Addr:              ":8080",
        Handler:           web.New(m),
        ReadHeaderTimeout: 5 * time.Second,
    }
    log.Fatal(srv.ListenAndServe())
}
```

Serve production over https: the CSRF cookie is `__Host-`-prefixed and `Secure`, so browsers drop it on
plain http (terminating TLS at the proxy is fine). Set `mux.Options.CSRFKey` from your secret store when
more than one instance serves the site, or when tokens should survive a restart: left empty, each process
signs with a key of its own and rejects the others' tokens with a one-off 400 per affected form. Behind a
reverse proxy, pass the public host through —
`proxy_set_header Host $http_host;` in nginx (`$http_host` keeps a non-default port, `$host` drops it) — and
`proxy_set_header X-Forwarded-Proto $scheme;`, because the cross-site check compares the browser's `Origin`
against them for both form submissions and WebSocket handshakes. Make sure WebSocket upgrades are forwarded
too — a reactive page
connects to `<route>/__ws` on the same origin. That means `proxy_set_header Upgrade $http_upgrade;` and
`proxy_set_header Connection "upgrade";` plus a `proxy_read_timeout` long enough for an idle connection to
survive.

## Upgrading an existing deployment

Cross-site state-changing requests and WebSocket handshakes are now refused, which changes two working
setups with no application change.

A form on one host posting to another — `www.example.com` to `app.example.com`, say — is `same-site`, not
same-origin, and is refused on every modern browser from the moment you upgrade. List the posting origin in
`mux.Options.OriginPatterns`, or exempt the route with `mux.Options.AllowCrossSite` if it is a third-party
callback by design.

Behind a proxy that rewrites `Host` to the upstream — nginx's `proxy_pass` default — clients that do not
send `Sec-Fetch-Site` (Safari before 16.4, older WebViews) get 403 on form submissions and failed reactive
sockets. Forward `Host` and `X-Forwarded-Proto` before upgrading, and set `mux.Options.CSRFKey` if more than
one instance serves the site.

## Caching

Asset URLs are content-hashed and served with `Cache-Control: public, max-age=31536000, immutable`, so a CDN
can cache them indefinitely. HTML is not cached by the framework, and responses that render a form are always
sent with `Cache-Control: no-store` (set after every route has run), so an intermediary that honours it never
shares one user's CSRF token. Do not strip that header upstream.

## Operational notes

- Each connected client of a reactive page holds a goroutine per reactive route plus a WebSocket. Size the
  process and any per-connection limits accordingly.
- A `Subscribe` panic is recovered and closes that connection only; the client reconnects with backoff.
- After a redeploy, clients still holding a connection reconnect and receive a fresh init frame. Clients whose
  route key no longer exists get an `unknown_route` error frame and recover on reconnect.
- Set `env` in `gossr.yaml` only for the watch-mode process; production configuration belongs in the real
  environment.

## Checklist

- [ ] `go-ssr -prod` run, generated files committed, `git diff --exit-code` clean
- [ ] `static_embed/`, `static/` and `node_modules/` ignored
- [ ] served over https, `mux.Options.InsecureCookies` not set
- [ ] `mux.Options.CSRFKey` set from the secret store if more than one instance serves the site
- [ ] proxy forwards the public `Host`, WebSocket upgrades, and tolerates idle connections
- [ ] the runtime image contains only the binary
