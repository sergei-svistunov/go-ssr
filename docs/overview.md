# Overview: how a GoSSR application works

GoSSR is a code generator. It reads a directory tree of HTML templates and writes Go source that renders
them, so rendering happens through compiled Go code with no template interpretation at runtime.

## The mental model

A GoSSR app is a tree of directories under `<webDir>/pages/`. Each directory that contains an `index.html`
is a route. Nesting directories nests templates: the parent renders the page shell and marks where the child
goes with `<ssr:content/>`.

```
internal/web/pages/
  index.html              ->  /            (page shell: <html>, nav, <ssr:content/>)
  home/index.html         ->  /home
  users/index.html        ->  /users       (list + its own <ssr:content/>)
  users/add/index.html    ->  /users/add
  users/_userId_/index.html -> /users/{userId}
```

For each route the generator writes:

| File | Written when | Contents |
|---|---|---|
| `ssrroute_gen.go` | every generation | the rendering code, the route's `RouteDataProvider` interface, `RouteData` struct, form value structs, reactive state |
| `dataprovider.go` | only if absent | a stub `DP` type implementing the interface — this is your file, edit it freely |
| `__ssr_gen__.ts` | route has reactive variables | the typed `ssr` client for the browser |
| `ssrhandler_gen.go` | once, in `pages/` | `NewSsrHandler()`, which wires every route and its data provider |
| `ssrstaticfiles_gen.go` | once, if assets were built | the embedded static file handler |

You write two kinds of code: templates (`index.html`, and optionally `index.ts` / `styles.scss`) and data
providers (`dataprovider.go`). Everything between them is generated.

## What happens on a request

1. `NewSsrHandler(deps, mux.Options{})` returns an `http.Handler` holding a route tree built from the
   generated route map.
2. The router matches the URL segment by segment. A directory named `_userId_` matches any single segment and
   records it as the URL parameter `userId`.
3. The matched route and all of its ancestors form a stack. The router calls `GetDataContext` from the
   deepest route upward, so each parent receives its already-rendered child. That is how a layout wraps a
   page without either knowing about the other.
4. Each route's `Data(ctx, r, w, data *RouteData)` fills in the variables its template declared.
5. The composed result is written to the response.

A request that arrives as a POST also runs the form lifecycle for that route (see `forms`), and a route with
reactive variables additionally accepts a WebSocket connection (see `reactive` and `fe-be-contract`).

## Type safety

Every value a template uses must be declared in the template with a Go type:

```html
<ssr:var name="users" type="[]User"/>
```

The generator turns that into a field on the route's `RouteData` struct, so `data.Users = ...` in your data
provider is checked by the Go compiler. A typo in a variable name or a type mismatch is a build error, not a
blank page at runtime. Expressions are compiled to Go too: `{{ user.Age >= 18 ? 'Adult' : 'Minor' }}` becomes
Go code operating on real types.

The generated `dataprovider.go` stub contains `var _ RouteDataProvider = &DP{}`, so if a template starts
requiring a new hook — a new form, a new client-writable variable — the compiler names the method you are
missing.

## Where to go next

| Topic | Read it for |
|---|---|
| `quickstart` | creating and running a new application |
| `project-structure` | the directory layout and which files are generated |
| `routing` | dynamic parameters, nested layouts, default child routes |
| `template-syntax` | every `ssr:` tag and attribute |
| `expressions` | the `{{ }}` language: operators, function calls, escaping |
| `forms` | form declaration, validation, and the Init/Process hooks |
| `reactive` | live server-pushed updates and two-way input binding |
| `fe-be-contract` | how the browser and the server actually talk |
| `runtime-api` | the Go packages your data providers use |
| `errors` | what a generator error means and how to fix it |
