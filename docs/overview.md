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
| `__ssr_gen__.ts` | route has reactive variables or page calls | the typed `ssr` client for the browser |
| `ssrhandler_gen.go` | once, in `pages/` | `NewSsrHandler()`, which wires every route and its data provider |
| `ssrstaticfiles_gen.go` | once, if assets were built | the embedded static file handler |

You write two kinds of code: templates (`index.html`, and optionally `index.ts` / `styles.scss`) and data
providers (`dataprovider.go`). Everything between them is generated.

## What happens on a request

1. `NewSsrHandler(deps, mux.Options{CSRFKey: key})` returns an `http.Handler` holding a route tree built from
   the generated route map, or an error for an option it refuses.
2. The router matches the URL segment by segment. A directory named `_userId_` matches any single segment and
   records it as the URL parameter `userId`.
3. The matched route and all of its ancestors form a stack. Every route's `Guard` runs first, root to leaf, so
   a layout can refuse a request before anything below it runs.
4. On the routes that render the page — each layout on the path and the last route — each hook runs root to
   leaf: on a POST, the form's `Init` and `Process` after the mux has checked the CSRF token; then each
   route's `Data(ctx, r, w, data *RouteData)` fills in the variables its template declared.
5. The root template renders, writing each child where `<ssr:content/>` stands, and the page is sent with a
   Content-Security-Policy and other security headers.

A request that arrives as a POST also runs the form lifecycle (see `forms`), and a page whose routes have
reactive variables or page calls opens one WebSocket to the server (see `reactive`, `page-calls` and
`fe-be-contract`). `security` lists what the handler enforces on every request.

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
requiring a new hook — a new form, a new client-writable variable, a new page call — the compiler names the
method you are missing.

## Where to go next

| Topic | Read it for |
|---|---|
| `quickstart` | creating and running a new application |
| `project-structure` | the directory layout and which files are generated |
| `routing` | dynamic parameters, layouts and gates, guards, default child routes |
| `template-syntax` | every `ssr:` tag and attribute |
| `expressions` | the `{{ }}` language: operators, function calls, escaping |
| `forms` | form declaration, validation, and the Init/Process hooks |
| `reactive` | live server-pushed updates and two-way input binding |
| `page-calls` | Go functions the page's TypeScript calls |
| `security` | guards, CSRF, escaping, the Content-Security-Policy and connection limits |
| `fe-be-contract` | how the browser and the server actually talk |
| `runtime-api` | the Go packages your data providers use |
| `errors` | what a generator error means and how to fix it |
