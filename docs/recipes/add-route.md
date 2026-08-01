# Recipe: add a route

Adding a page, a route with a URL parameter, or turning an existing route into a layout.

## Static route

```bash
mkdir -p internal/web/pages/about
cat > internal/web/pages/about/index.html <<'HTML'
<ssr:var name="version" type="string"/>
<h1>About</h1>
<p>Version {{ version }}</p>
HTML
go-ssr
```

Generation writes `internal/web/pages/about/dataprovider.go`:

```go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    data.Version = build.Version
    return nil
}
```

The parent template needs an `<ssr:content/>` for the page to appear inside it. `/about` is now live.

## Route with a URL parameter

Wrap the directory name in single underscores:

```bash
mkdir -p internal/web/pages/users/_userId_
cat > internal/web/pages/users/_userId_/index.html <<'HTML'
<ssr:var name="user" type="User"/>
<h1>{{ user.Name }}</h1>
HTML
go-ssr
```

```go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    u, err := p.m.UserByLogin(r.URLParam("userId"))
    if err != nil {
        return mux.NotFound()
    }
    data.User = User{Name: u.Name}
    return nil
}
```

The parameter name must not contain underscores. A literal sibling directory wins over the parameter, so
`users/add/` and `users/_userId_/` coexist.

## Route that becomes a layout

Adding a child directory to an existing route changes its behaviour: a route with children no longer renders
itself, it redirects to a default child. Two things to do:

1. add `<ssr:content/>` where the child should render,
2. give it a default child — `<ssr:content default="/info"/>`, or implement `DefaultRoute`.

```html
<!-- internal/web/pages/users/_userId_/index.html -->
<ssr:var name="user" type="User"/>
<h1>{{ user.Name }}</h1>
<nav>
    <a href="/users/{{ user.Login }}/info">Info</a>
    <a href="/users/{{ user.Login }}/contacts">Contacts</a>
</nav>
<ssr:content default="/info"/>
```

`DefaultRoute` appears in the route's interface only when `<ssr:content/>` has no `default`, so switching
between the two changes what the compiler asks of you.

## Per-route TypeScript and styles

```bash
touch internal/web/pages/about/index.ts internal/web/pages/about/styles.scss
go-ssr   # runs webpack because a source file is newer than the last build
```

Both are picked up by convention and bundled into that route's chunk, loaded by the `<ssr:assets/>` in the
layout. Nothing to register.

## Removing a route

Delete the directory and regenerate. The stale `ssrroute_gen.go` disappears with it; if you keep the directory
but delete `index.html`, it stops being a route.

## Checklist

- [ ] directory contains `index.html`
- [ ] every `<ssr:var>` has a `type` that resolves in this directory's package
- [ ] the parent has `<ssr:content/>`
- [ ] a route that gained children has a default child
- [ ] `go-ssr` run, `dataprovider.go` filled in, `go build ./...` clean
