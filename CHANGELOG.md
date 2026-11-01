# Changelog

## v0.7.0

go-ssr 0.7.0 needs gossr-runtime 0.2.0, and gossr-runtime 0.2.0 needs go-ssr 0.7.0. Nothing checks the pairing
at run time: upgrade both together.

### Security

Requests and forms:

- Route hooks run root to leaf. A data provider may have a `Guard(ctx, r) error` method; the guard of every route
  on the path runs before any other hook of any route, on page requests, form posts, live connection handshakes
  and page calls. `NewRoute` panics (E21) on a `Guard` method with the wrong signature.
- The mux checks the CSRF token of every POST itself, after the guards and before any `Init<Form>` or
  `Process<Form>`. `mux.Options.CSRFKey` is required and must be at least 32 bytes; no key is generated any more.
- `Process<Form>` runs only when the form has no validation errors, and the form is marked validated before it
  runs. Only the first route, root first, that renders the posted form processes it; a POST whose form no route
  of the page renders gets 400 `This form is not on this page.`
- A page route answers only GET, HEAD and POST; any other method gets 405 before any hook runs. A POST to a
  layout's own address gets 405.
- `mux.Options.AllowCrossSite` lifts only the cross-site refusal: an exempt POST to a page still needs the CSRF
  cookie and a signed token and is refused with 400 without them. Earlier docs suggested it for token-less
  third-party callbacks (OpenID Connect form_post, SAML, payment returns); serve those from an `http.Handler` in
  front of the mux instead.
- The body of every POST to a page is capped at `mux.Options.MaxBodyBytes`, 10 MiB by default, so a multipart
  upload no longer spills to temporary files without limit. A body declared longer gets 413
  `Request body too large.` before any hook runs; one that turns out longer when read gets it before any form
  hook. An app that takes larger uploads sets `MaxBodyBytes`.

Headers:

- Every mux response (page, error, redirect, refused live handshake) carries these headers, replacing any a hook
  set, with the request's WebSocket origin in `connect-src`:

  ```
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self' <ws-origin>; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'
  X-Content-Type-Options: nosniff
  Referrer-Policy: same-origin
  Cross-Origin-Opener-Policy: same-origin
  Cross-Origin-Resource-Policy: same-origin
  ```

- `mux.Options.CSPSources` adds sources to a directive. `mux.New` refuses a source that weakens the policy: `*`
  or a `*` host, any `'unsafe-…'` keyword, `'strict-dynamic'`, and a scheme on its own (`https:`, `data:`) in
  `script-src`.
- `pkg/static` sets `X-Content-Type-Options: nosniff` and `Cross-Origin-Resource-Policy: same-origin` on every
  asset. The default error handler writes plain text.

Escaping:

- Every `{{ }}` is escaped for its HTML context: element text, a quoted attribute or a URL attribute. A whole-URL
  value whose scheme is not `http`, `https` or `mailto` becomes `render.UnsafeURL`
  (`about:invalid#gossr-unsafe-url`).
- The generator refuses the places where no escaping is safe, each with a code and a fix line: a value in a
  `<script>` or `<style>` body (E08), `on*` attributes (E09), `style` attributes (E10), unquoted attributes with a
  value (E11), a value in `iframe`, `noembed`, `noframes`, `noscript`, `plaintext` or `xmp` (E12), `{{$ }}` in an
  attribute, `<title>` or `<textarea>` (E13), a literal `javascript:` or `vbscript:` URL, or a literal scheme other
  than `http`, `https` or `mailto` (such as `data:`, `tel:` or `ftp:`) before a value (E14), a URL whose scheme
  depends on a value (E15), an inline `<script>` body or a `<style>` element (E16), a value in a tag or attribute
  name (E17), and a value in an attribute that loads code or changes how the page is read (E18).
- The template parser reads markup as `golang.org/x/net/html` does: a raw-text element written self-closing, such
  as `<script/>`, stays open; an end tag closes the nearest open element of its name; literal text in
  `<ssr:form>` is escaped; whitespace is collapsed only in literal text. It refuses markup inside `xmp`,
  `noscript` and the other undecoded elements, an attribute written twice, a NUL in an attribute name, and
  attributes it cannot read (`cannot read the attributes of <tag>`).
- The literal `name`, `type` and `value` of `<ssr:input>`, `<ssr:select>` and `<ssr:textarea>` are escaped, and
  `type` must be an HTML input type.

Live values:

- The server escapes every live value with the escapers of the first render: a `{{ }}` value arrives escaped, a
  `{{$ }}` value as it is. A live value in the text of `<title>` or `<textarea>` is refused.
- A live connection handshake passes the page's checks before the upgrade: the cross-site refusal
  (`mux.CrossSiteStrict`), every `Guard`, then `Data` of every route that renders the page. Before, `Data` ran only
  on reactive routes, so a check in a non-reactive parent was skipped.
- A live connection lasts at most 10 minutes; the server then closes it with 4000 (`reactive.StatusReauth`), and
  the reconnect passes every check again.
- `mux.Options.MaxLiveConnsPerSession` caps the live connections open at once per `CSRFSession`; a handshake beyond
  it gets 429. A connection gives its place back when its read loop ends.
- A client-writable variable may not hold markup: the generator refuses a type that is or contains a `render`
  type, and any generic type. `render.SafeHTML` refuses to decode from JSON or text, `null` included.
- `ssr:bind` on a `bool`, integer or float variable parses the input's text strictly. `reactive.ParseValue` is
  strict too: `strconv.ParseBool` for booleans, no `NaN` or infinities, and integers that fit the type. Before,
  it accepted `on` and `yes`, `NaN` and infinities, and wrapped integers around.
- The generator writes `data-ssr-route` with the route key on every `ssr:bind` element, so only that route's
  runtime wires it; a template may not set `data-ssr-route` itself.

### Breaking

- go-ssr and the app it scaffolds need Go 1.26 or newer.
- `mux.New` returns `(*Mux, error)`, and the generated `NewSsrHandler` returns `(http.Handler, error)`. An empty
  or short `CSRFKey`, a malformed `OriginPatterns` entry (a panic before) and a weakening `CSPSources` entry are
  errors.
- `mux.Route` is `NewState() RouteState` and `Layout() bool`; `mux.DataContext` and `mux.RouteDataContext` are
  replaced by `mux.Frame` and `mux.Child`. Regenerate; hand-written routes need porting.
- Layouts and gates: only a route whose template has `<ssr:content/>` is a layout and redirects to its default
  child. A parent without it renders its own page, and the pages below it no longer render inside it; only its
  `Guard` runs for them. A layout whose default is empty or the layout itself answers 404 instead of
  redirecting to itself.
- A request segment shaped like a parameter folder's name, such as `/users/_userId_`, is always a value of the
  parameter. An empty segment (`/users//info`, `//`) answers 404.
- `{{$ }}` still writes any value unescaped, but only in element text (E13, E08, E12).
- Inline `<script>` bodies, `<style>` elements and `style` attributes are refused (E16, E10): the default
  Content-Security-Policy blocks them.
- Binding keys: only a site that renders exactly one variable keeps the variable's name as its key. Any other site
  gets a hash of its source, and a `{{$ }}` site never shares a key with a `{{ }}` site.
- The generated `__ssr_gen__.ts`: `ReadVars` entries are `string`, the markup the server rendered, so code that did
  arithmetic on `ssr.get()` must parse it. `WriteVars` and page call types are mapped from the route package's
  declarations as `encoding/json` writes them, replacing the opaque interfaces. The client is
  `createSsrClient<ReadVars, WriteVars, Calls>`, and its address carries the page's query string.
- `(*reactive.Conn).ReadFrames(ctx, onWrite, onCall)` replaces `HandleWrites` and `ReceiveWriteMsg`. Frames other
  than `write` and `call` are ignored; before, every frame was read as a write.
- Removed: `mux.WithWSHandlers`, `mux.OriginChecked`, `mux.URLParamsFromContext`, `mux.WriteHtmlEscaped`,
  `mux.WriteRaw`, `(*mux.Request).CSRFKey`, `form.Parse`, `form.MaxMultipartMemory`, `reactive.NewHandler`,
  `reactive.HandlerFunc` and `reactive.RenderValue`.
- Generator errors read `file:line: message (CODE)` with a `fix:` line below a coded error, and no column. MCP
  diagnostics drop `col` and gain `fix`.

### Added

- Page calls: `<ssr:call name="…" in="…" out="…"/>` declares a function of the route that the page's TypeScript
  calls with `ssr.call(name, args)`. The data provider implements `Call<Name>`; the generated stub refuses every
  call with 403 until it is written.
  - Each call runs every `Guard` on the page's path again, then `Call<Name>`. It has 30 seconds, and a connection
    runs at most 4 calls at once. Failures reach the page with their status: 400
    `The call has the wrong arguments.`, 404 `This call is not on this page.`, 429 `Too many calls at once.`, 504
    `The call took too long.`, 500 `Something went wrong.`, or an `*HttpError`'s status and message.
  - A call that ignores its context keeps its place until it returns; its 504 is sent on time.
  - `mux.DecodeArgs` reads the arguments strictly. It refuses unknown fields, trailing data, and a top-level `null`
    unless the target is a pointer, slice, map, interface or `struct{}`.
  - A call frame with id 0 gets no answer.
  - A route with calls gets a `__ssr_gen__.ts` with a `Calls` type, and a page with calls and no live values
    opens a live connection too.
  - E19 refuses a malformed `<ssr:call>`. E20 refuses `in` and `out` types that cannot travel as JSON, interfaces
    with methods (`error` included) among them.
  - The route inventory lists `calls`, and `dataProviderMethods` includes `Call<Name>`.
  - New API: `mux.Caller`, `mux.DecodeArgs`, `reactive.CallMsg`, `reactive.ResultMsg`, `reactive.FailMsg`,
    `reactive.NewResult` and `reactive.NewFail`.
- `pkg/render`: the context escapers `Text`, `Attr`, `URL`, `URLPart`, `URLPathStart` and `URLQuery`; `HTML`, which
  writes any value unescaped; `Format`, `JSON` and `UnsafeURL`; and `SafeHTML`, built with `HTMLConst`,
  `EscapeHTML` and `JoinHTML`.
- `mux`: `Guarded`, `CheckGuard`, `RouteState`, `Frame`, `Child`, `Live`, `CSRFFieldName`, `MaxMultipartMemory`,
  `(*Request).CSRFToken`, `DefaultMaxBodyBytes`, and the options `CSPSources`, `MaxLiveConnsPerSession` and
  `MaxBodyBytes`.
- `reactive`: `Accept`, `StatusReauth` and `(*Conn).CloseNow`. Called on its own, `Accept` checks only that
  `Origin` matches `Host`; the mux runs its own checks first and marks the request.
- The scaffolded app reads its CSRF key from `CSRF_KEY`, depends on gossr-runtime ^0.2.0, builds development
  bundles with source maps, since the policy blocks `eval`, and gives its server a 10-second `ReadHeaderTimeout`.

### Changed

- A static asset's ETag is a prefix of the SHA-256 of the stored file instead of its MD5, so browsers and caches
  fetch every asset once more after the upgrade.
- The mux's log lines quote the request method as they quote the path, for example
  `mux: "POST" "/users" refused as cross-site (…)`.

### Fixed

- The first build after adding a page call or a live value works: the generator writes `__ssr_gen__.ts` before it
  runs webpack and generates the Go code after. It rewrites `__ssr_gen__.ts` only when its content changes, and a
  changed one makes the assets stale.
- A form's `Process` no longer runs before a parent layout's checks.
- Scaffolding refuses a `depsPackage` that climbs out of the module, such as `example.com/app/../x`, which wrote
  outside the project directory. The scaffold, the generator and the MCP route scaffold write through `os.Root`
  and never follow a symbolic link out of the project; generated Go files get mode 0644 instead of 0755.
- A `Subscribe` that returns nil leaves the connection open; before, the connection was closed.
- On a live connection and in a page call, `r.URLParam` and `r.URL` are the page's, query included.

### Migration from v0.6.0

| v0.6.0 | v0.7.0 |
|---|---|
| `mux.New(routes, opts) *Mux` | `mux.New(routes, opts) (*Mux, error)`, with `opts.CSRFKey` required (at least 32 bytes) |
| `NewSsrHandler(…) http.Handler` | `NewSsrHandler(…) (http.Handler, error)`; handle the error at start-up |
| an empty `CSRFKey` generates a key per process | read the key from your secret store; the scaffold uses `CSRF_KEY`, 64 hex characters |
| a malformed `OriginPatterns` entry panics | `mux.New` returns the error |
| `(*mux.Request).CSRFKey()` | removed; `(*mux.Request).CSRFToken(w)` mints a token |
| `mux.Route.GetDataContext`, `GetDefaultRoute` | `Route.NewState() RouteState` and `Route.Layout() bool`; regenerate |
| `mux.DataContext`, `mux.RouteDataContext` | `mux.Frame` and `mux.Child` |
| hooks run leaf to root | root to leaf; put access checks in the optional `Guard(ctx, r) error` of the highest route they cover |
| a parent with routes below redirects to its default child | only a layout (`<ssr:content/>`) does; a parent without it renders its own page, and the pages below no longer render inside it |
| a layout whose default is empty redirects to itself | 404 |
| a segment shaped `_name_` can name the parameter folder | always a value of the parameter |
| `Process<Form>` runs with validation errors | runs only without them; the page re-renders with the errors |
| `form.Parse` | nothing to call: the mux reads the body and checks the token before any form hook |
| `form.MaxMultipartMemory` | `mux.MaxMultipartMemory` |
| uploads of any size | at most `mux.Options.MaxBodyBytes`, 10 MiB by default; set it to what the largest form needs |
| `mux.WriteHtmlEscaped`, `mux.WriteRaw` | `render.Text`, `render.HTML`: `func(w io.Writer, v any) error` |
| `{{$ }}` anywhere | still unescaped, in element text only |
| templates with values in unsafe places | fix each E08–E18 error as its `fix:` line says; see `docs/errors.md` |
| inline `<script>` bodies, `<style>` elements, `style` attributes | move them into the route's `index.ts` and `styles.scss`; the default CSP blocks inline code |
| `AllowCrossSite` for token-less callbacks | an `http.Handler` in front of the mux |
| `mux.WithWSHandlers` | removed: the mux serves each page's live connection at `<page path>/__ws`; mount a WebSocket of your own on an outer `http.ServeMux` and check the origin and the session there |
| `reactive.NewHandler`, `reactive.HandlerFunc` | removed; `reactive.Accept` upgrades a request whose checks you ran |
| `mux.OriginChecked` | removed |
| `mux.URLParamsFromContext` | `r.URLParam` on the request `Data`, `Subscribe` and calls receive |
| `reactive.RenderValue` | removed: the server renders every live value with the first render's escapers |
| `(*reactive.Conn).HandleWrites`, `ReceiveWriteMsg` | `(*reactive.Conn).ReadFrames(ctx, onWrite, onCall)` |
| a client-writable variable of a `render` or generic type | a `string` or a concrete struct |
| `ReadVars` typed like the Go variable | `string`: parse what `ssr.get()` and `ssr.on()` give |
| gossr-runtime 0.1.x | gossr-runtime 0.2.0 |
