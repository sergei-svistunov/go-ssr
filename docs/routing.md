# Routing: directories, parameters and nested layouts

The URL space is the directory tree under `<webDir>/pages/`. There is no route table to maintain.

## Directories are routes

A directory containing an `index.html` becomes a route at its path relative to `pages/`:

```
pages/index.html              ->  /
pages/home/index.html         ->  /home
pages/users/index.html        ->  /users
pages/users/add/index.html    ->  /users/add
```

Empty directories and directories without a template are skipped. A trailing slash in the request URL is
ignored, so `/users/` and `/users` match the same route.

## Dynamic parameters

A directory whose name is wrapped in single underscores captures one URL segment:

```
pages/users/_userId_/index.html          ->  /users/{userId}
pages/users/_userId_/contacts/index.html ->  /users/{userId}/contacts
```

Read the captured value in the data provider:

```go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    user, err := p.m.UserByLogin(r.URLParam("userId"))
    if err != nil {
        return err
    }
    data.User = user
    return nil
}
```

The method is `URLParam`. Parameter names must not contain underscores — `_user_id_` is not a parameter
directory. A literal segment always wins over a parameter: with both `users/add/` and `users/_userId_/`, the
URL `/users/add` matches `add`.

Inside a route the parameters of every ancestor are available, so `/users/{userId}/contacts` can read
`userId`. In a `Subscribe` hook on a reactive route, `r.URLParam` works the same way — the WebSocket endpoint
inherits the parameters of its path.

## Nested layouts

A parent template marks where its child renders:

```html
<!-- pages/index.html -->
<!doctype html>
<html>
<head><title>{{ title }}</title><ssr:assets/></head>
<body>
  <nav>…</nav>
  <div class="container"><ssr:content/></div>
</body>
</html>
```

On a request the router builds the stack of matched routes and calls them from the deepest upward, handing
each parent its already-rendered child. Parent and child never reference each other's variables: each has its
own `RouteData` and its own data provider.

Layouts nest arbitrarily. `/users/{userId}/contacts` renders `contacts` inside `_userId_` inside `users`
inside the root shell, provided each ancestor has an `<ssr:content/>`.

## Routes with children always redirect

A route that has child directories cannot render on its own. Requesting it redirects (HTTP 302) to a child.
Which child comes from one of two places:

```html
<ssr:content default="/home"/>
```

or, when `default` is absent, from the generated `DefaultRoute` hook:

```go
func (p *DP) DefaultRoute(ctx context.Context, r *mux.Request) (string, error) {
    return "/overview", nil
}
```

The returned path is joined onto the current URL, so returning `/info` from `/users/bob` redirects to
`/users/bob/info`. The generator only adds `DefaultRoute` to a route's `RouteDataProvider` interface when the
template has an `<ssr:content/>` without a `default` attribute — otherwise there is nothing to decide.

## Errors, redirects and status codes

Return an error from any hook to stop rendering. `pkg/mux` provides the shapes the default handler
understands:

```go
return mux.NotFound()                                  // 404
return mux.NewHttpError(http.StatusForbidden, "nope")  // arbitrary status
return mux.Redirect(http.StatusSeeOther, "/login")     // redirect
```

Anything else is a 500. Replace the default behaviour with your own:

```go
pages.NewSsrHandler(deps, mux.Options{
    ErrorHandler: func(w http.ResponseWriter, r *mux.Request, err error) {
        // render your own error page
    },
})
```

See `runtime-api` for the full `pkg/mux` surface.

## Mounting

The generated handler is an ordinary `http.Handler`, so it composes with anything:

```go
mux := http.NewServeMux()
mux.Handle("/api/", apiHandler())
mux.Handle("/", pages.NewSsrHandler(deps, ssrMux.Options{}))
http.ListenAndServe(":8080", mux)
```

When any route is reactive, the generated handler also registers the WebSocket endpoints; nothing extra is
needed at the mounting site.
