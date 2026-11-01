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
    csrfKey, err := hex.DecodeString(os.Getenv("CSRF_KEY")) // from your secret store
    if err != nil || len(csrfKey) != 32 {
        log.Fatal("CSRF_KEY must be 64 hex characters")
    }
    handler, err := web.New(model.New(openDB()), csrfKey)
    if err != nil {
        log.Fatal(err)
    }

    srv := &http.Server{
        Addr:              ":8080",
        Handler:           http.MaxBytesHandler(handler, 16<<20),
        ReadHeaderTimeout: 5 * time.Second,
    }
    log.Fatal(srv.ListenAndServe())
}
```

- **The CSRF key** comes from your secret store, and every instance that serves the site gets the same one;
  `NewSsrHandler` refuses one shorter than 32 bytes. Tokens survive a restart only if the key does, and
  rotating it costs every open form one 400.
- **Request bodies** need a cap in front of the handler: a url-encoded form stops at the standard library's
  10 MB, but a multipart body keeps `mux.MaxMultipartMemory` in memory and spills the rest to temporary files
  with no total limit. `http.MaxBytesHandler`, as above, or the proxy's body size limit, set to what the
  largest form needs.
- **https**: the CSRF cookie is `__Host-`-prefixed and `Secure`, so browsers drop it on plain http
  (terminating TLS at the proxy is fine).
- **Behind a reverse proxy**, pass the public host through — `proxy_set_header Host $http_host;` in nginx
  (`$http_host` keeps a non-default port, `$host` drops it) — and `proxy_set_header X-Forwarded-Proto
  $scheme;`, because the cross-site check compares the browser's `Origin` against them for both form
  submissions and live connection handshakes, and the Content-Security-Policy names the WebSocket origin from
  them. Clients that do not send `Sec-Fetch-Site` (Safari before 16.4, older WebViews) get 403 on form
  submissions and failed live connections behind a proxy that rewrites `Host` to the upstream.
- **WebSocket upgrades** must be forwarded too — a page with live values or calls connects to
  `<page>/__ws` on the same origin. That means `proxy_set_header Upgrade $http_upgrade;` and
  `proxy_set_header Connection "upgrade";` plus a `proxy_read_timeout` long enough for an idle connection to
  survive; the server itself closes each connection after 10 minutes and the client reconnects.

A form on one host posting to another — `www.example.com` to `app.example.com`, say — is `same-site`, not
same-origin, and is refused; list the posting origin in `mux.Options.OriginPatterns`. A third-party callback
by design posts no CSRF token, so serve it from an `http.Handler` in front of the mux;
`mux.Options.AllowCrossSite` does not lift the token check. Assets or APIs on other origins need their origins
in `mux.Options.CSPSources`; see `security`.

## Caching

Asset URLs are content-hashed and served with `Cache-Control: public, max-age=31536000, immutable`, so a CDN
can cache them indefinitely. The handler sets no caching headers on HTML, and responses that render a form are always
sent with `Cache-Control: no-store` (set after every route has run), so an intermediary that honours it never
shares one user's CSRF token. Do not strip that header upstream.

## Operational notes

- Each connected client of a live page holds a goroutine per reactive route plus a WebSocket. Size the
  process accordingly, and cap connections per session with `mux.Options.MaxLiveConnsPerSession`.
- A `Subscribe` panic is recovered and closes that connection only; the client reconnects with backoff.
- After a redeploy, clients still holding a connection reconnect and receive a fresh init frame. Clients whose
  route key no longer exists get an `unknown_route` error frame and recover on reconnect.
- Set `env` in `gossr.yaml` only for the watch-mode process; production configuration belongs in the real
  environment.

## Checklist

- [ ] `go-ssr -prod` run, generated files committed, `git diff --exit-code` clean
- [ ] `static_embed/`, `static/` and `node_modules/` ignored
- [ ] served over https, `mux.Options.InsecureCookies` not set
- [ ] `mux.Options.CSRFKey` read from the secret store, the same on every instance
- [ ] request bodies capped in front of the handler
- [ ] proxy forwards the public `Host` and `X-Forwarded-Proto`, WebSocket upgrades, and tolerates idle
  connections
- [ ] the runtime image contains only the binary
