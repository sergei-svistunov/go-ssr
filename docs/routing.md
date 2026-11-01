# Routing: directories, parameters, layouts and guards

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
`userId`. On a live connection and in a page call, `r.URLParam` reads the parameters of the page.

A request segment shaped like a parameter directory's name, such as `/users/_userId_`, is always a value of
the parameter, never that directory. An empty segment (`/users//info`) names no route and answers 404.

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

Parent and child never reference each other's variables: each has its own `RouteData` and its own data
provider. The mux runs each route's hooks root first and renders the root template, which writes its child
where `<ssr:content/>` stands, and so on down to the page.

Layouts nest arbitrarily. `/users/{userId}/contacts` renders `contacts` inside `_userId_` inside `users`
inside the root shell, provided each ancestor has an `<ssr:content/>`.

## Layouts, gates and pages

A route with routes below it is one of two kinds, decided by its template:

| Route | Opened itself | For a page below it |
|---|---|---|
| **layout**: its template has `<ssr:content/>` | redirects (302) to its default child | its hooks run and it renders the page in place of `<ssr:content/>` |
| **gate**: its template has no `<ssr:content/>` | renders its own page: its forms, `Data`, live values and calls | only its `Guard` runs; the page is not shown inside it |

A layout with no routes below it renders itself, with nothing in place of `<ssr:content/>`. A directory with
no template is never a route: a request that ends there answers 404. Generated code reports the kind as
`Route.Layout()`.

A gate suits an address that is both a page and the parent of other pages, such as `/projects/{id}` with
`/projects/{id}/settings` below it: the gate's `Guard` protects both, and each renders a page of its own.

### Default child

A layout opened itself redirects to a child. Which child comes from one of two places:

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
`/users/bob/info`. A default that is empty or resolves to the layout itself answers 404, never a redirect. The
generator only adds `DefaultRoute` to a route's `RouteDataProvider` interface when the template has an
`<ssr:content/>` without a `default` attribute — otherwise there is nothing to decide. A POST to a layout's
own address answers 405.

## Guards

`Guard` is an optional method of a route's data provider:

```go
func (p *DP) Guard(ctx context.Context, r *mux.Request) error {
    if !p.m.CanSeeUser(ctx, r.URLParam("userId")) {
        return mux.NotFound()
    }
    return nil
}
```

The guard of every route on the path runs before any other hook of any route: before a form's `Init` and
`Process`, before any `Data`, before a live connection opens and before every page call. A guard on a layout
or a gate covers every page below it. Write each check in the guard of the highest route it applies to, and
keep guards cheap: they run on every request, handshake and call.

How a refusal reaches the browser:

| Guard returns | Page request | Live connection handshake | Page call |
|---|---|---|---|
| `*HttpError` with a 4xx status | goes to `ErrorHandler` | refused with that status | fail frame with that status and message, then close 1008 |
| `*HttpRedirect` | goes to `ErrorHandler`, which redirects | refused with 401 | fail 401, then close 1008 |
| any other error | goes to `ErrorHandler` (500) | refused with 500, logged | fail 500, logged, then close 1008 |

`NewRoute` panics (E21) when the data provider has a method named `Guard` with any other signature, so a
misspelt guard never leaves a route open.

## The order of a request

The mux runs each step for every route on the path before the next step starts, root first:

1. match the path; a POST, PUT, PATCH or DELETE from another origin is refused with 403, and a method other
   than GET, HEAD or POST with 405;
2. `Guard`, on every route on the path;
3. on a layout opened itself: `DefaultRoute` and the redirect;
4. on a POST: the CSRF token check (`forms`);
5. on the routes that render the page — every layout on the path and the last route: `Init<Form>`, then on a
   POST `Process<Form>` of the route that renders the submitted form (400 `This form is not on this page.`
   when none does), then `Data`;
6. render the page whole, then send it.

The first error stops the request and goes to `ErrorHandler`. See `security` for the headers every response
carries.

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
handler, err := pages.NewSsrHandler(deps, mux.Options{
    CSRFKey: csrfKey,
    ErrorHandler: func(w http.ResponseWriter, r *mux.Request, err error) {
        // render your own error page
    },
})
```

The error handler also answers a live connection handshake that a hook refused, before the upgrade; a page
call's failures go to the page instead (`page-calls`). See `runtime-api` for the full `pkg/mux` surface.

## Mounting

The generated handler is an ordinary `http.Handler`, so it composes with anything:

```go
ssrHandler, err := pages.NewSsrHandler(deps, ssrMux.Options{CSRFKey: csrfKey})
if err != nil {
    log.Fatal(err)
}
mux := http.NewServeMux()
mux.Handle("/api/", apiHandler())
mux.Handle("/", ssrHandler)
log.Fatal(http.ListenAndServe(":8080", mux))
```

The live connections of pages with live values or calls are served by the same handler, at each page's path
followed by `/__ws`; nothing extra is needed at the mounting site. A WebSocket of your own belongs on the
outer `http.ServeMux`, at an address the pages do not use, and must check the origin and the session itself.
