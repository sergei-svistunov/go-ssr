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

Behind a reverse proxy, make sure WebSocket upgrades are forwarded — a reactive page connects to
`<route>/__ws` on the same origin. In nginx that means `proxy_set_header Upgrade $http_upgrade;` and
`proxy_set_header Connection "upgrade";` plus a `proxy_read_timeout` long enough for an idle connection to
survive.

## Caching

Asset URLs are content-hashed and served with `Cache-Control: public, max-age=31536000, immutable`, so a CDN
can cache them indefinitely. HTML is not cached by the framework. Pages containing forms should not be cached
by an intermediary: the CSRF cookie is issued while rendering, so a shared cached page produces stale-token
400s. Set `Cache-Control: no-store` on those responses.

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
- [ ] proxy forwards WebSocket upgrades and tolerates idle connections
- [ ] pages with forms are served `no-store`
- [ ] the runtime image contains only the binary
