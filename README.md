# GoSSR: Go Generator for HTML Server-Side Rendering

**GoSSR** is a Go-based tool that simplifies the development of web applications by generating `http.Handler`
implementations. It leverages your project's directory structure to define routing and uses HTML templates for efficient
server-side rendering (SSR).

## Key features

- **Directory-based routing**: Define web routes based on your project's folder structure. Folders with leading and
  trailing underscores (e.g., `_userId_`) are interpreted as dynamic URL parameters, accessible via the `UrlParam`
  method in the request object.
- **HTML template rendering**: Transform HTML templates into Go code, enabling fast, type-safe server-side rendering.
- **Dynamic URL parameters**: Use folder names to define dynamic parts of URLs, which are passed as parameters to the
  corresponding handlers.
- **Data providers**: Each route has its own `RouteDataProvider` interface with short method names (`Data`,
  `DefaultRoute`, `Init*`, `Process*`). Routes are self-contained - each carries its own data provider.
- **Dependency injection**: Optionally configure a `depsPackage` in `gossr.yaml` to pass a shared dependencies struct
  to all route data providers via constructor injection. No composite interfaces or manual wiring needed.
- **Static asset management**: Seamlessly integrate with `gossr-assets-webpack-plugin` to manage static assets (CSS,
  JavaScript, images) and dynamically replace paths with hashed filenames.
- **Embedded static serving**: Webpack output is gzip-precompressed at generate time and embedded directly into the
  binary via `//go:embed`. The generated handler serves static assets with ETags, `Cache-Control: immutable`,
  conditional 304s, and gzip content-negotiation - no separate file server or filesystem dependency at runtime.
- **Automatic rebuild**: Watches for file changes, rebuilding assets and templates as needed, and automatically restarts
  the project.
- **Form Handling**: Automatically generate Go code to handle HTML forms, including validation and error management.
- **Reactive Bindings**: Opt-in, Vue-like live bindings. Mark a variable `reactive="true"` and the server pushes DOM
  patches to every connected client over WebSocket whenever the value changes. TypeScript can write back to
  server-validated state with `ssr.set()` or a declarative `ssr:bind` attribute on any native input element. No
  WebSocket boilerplate required.

## It's very fast

The example below shows how you can benchmark SSR handler performance:

```go
var (
    ssrHandler = ctxMiddleware{
        pages.NewSsrHandler(
            &model.Model{}, mux.Options{},
        ),
    }
    req1 = httptest.NewRequest(http.MethodGet, "/home", nil)
    req2 = httptest.NewRequest(http.MethodGet, "/users/johndoe123/info", nil)
    dw = DiscardWriter{}
)

func BenchmarkSsrHandlerSimple(b *testing.B) {
    for i := 0; i < b.N; i++ {
        ssrHandler.ServeHTTP(dw, req1)
    }
}

func BenchmarkSsrHandlerDeep(b *testing.B) {
    for i := 0; i < b.N; i++ {
        ssrHandler.ServeHTTP(dw, req2)
    }
}
```

Results:

```
goos: linux
goarch: amd64
pkg: github.com/sergei-svistunov/go-ssr/example/internal/web/pages
cpu: AMD Ryzen 7 5800H with Radeon Graphics
BenchmarkSsrHandlerSimple
BenchmarkSsrHandlerSimple-16    	  432955	      2343 ns/op
BenchmarkSsrHandlerDeep
BenchmarkSsrHandlerDeep-16      	  164113	      7131 ns/op
```

## Installation

To install GoSSR, run:

```bash
go install github.com/sergei-svistunov/go-ssr@latest
```

## Usage

### Initialize a new project

To initialize a new project, run the generator in your project directory:

```bash
go-ssr -init -pkg-name <appname>
```

This generates a boilerplate for your application.

### Generate GoSSR files

Run the generator to create the necessary SSR files:

```bash
go-ssr
```

### Automatically rebuild the project

Run the generator in watch mode to automatically rebuild your project when changes are detected:

```bash
go-ssr -watch
```

### Building static files in production mode

Just add the argument `-prod`:

```bash
go-ssr -prod
```

## GoSSR config

The config for the current project is in the `gossr.yaml` file with the following structure:

```go
type Config struct {
    WebDir      string            `yaml:"webDir"`      // Directory containing SSR handlers and templates
    WebPackage  string            `yaml:"webPackage"`   // Full path to the web package
    DepsPackage string            `yaml:"depsPackage"`  // Full path to the deps package (optional)
    DepsType    string            `yaml:"depsType"`     // Type name in the deps package (default: "Deps")
    GoRunArgs   string            `yaml:"goRunArgs"`    // Arguments for `go run`
    Env         map[string]string `yaml:"env"`          // Environment variables
}
```

### Dependency injection with `depsPackage`

When `depsPackage` is set, the generator creates `NewDP(d *<pkg>.<Type>)` constructors and wires them automatically in
`NewSsrHandler(d *<pkg>.<Type>, opts)`. The deps package should be independent of the web/pages packages to avoid
circular imports. Use `depsType` to specify the type name (defaults to `"Deps"`).

Example `gossr.yaml` with a dedicated deps package:

```yaml
webDir: ./internal/web
webPackage: github.com/example/internal/web
depsPackage: github.com/example/internal/deps
```

Or pass an existing type directly (e.g. `*model.Model`):

```yaml
webDir: ./internal/web
webPackage: github.com/example/internal/web
depsPackage: github.com/example/internal/model
depsType: Model
```

When `depsPackage` is omitted, the generator creates zero-arg `NewDP()` constructors instead.

## Project structure

Create a directory for all GoSSR files, such as `internal/web`. This directory must include:

- **`pages/`**: Contains routes, GoSSR templates, TypeScript, and SCSS files. Each subdirectory is a route. Key files
  include:
    - `index.html`: Required, the page template.
    - `index.ts`: Optional, the page script.
    - `styles.scss`: Optional, the page's CSS styles.
    - `ssrhandler_gen.go`: Auto-generated, only in `pages` directory, contains `NewSsrHandler` constructor that wires
      all routes.
    - `ssrroute_gen.go`: Auto-generated, defines route `RouteDataProvider` interface and code for rendering templates.
    - `dataprovider.go`: Generated once as a stub, then user-maintained. Implements `RouteDataProvider` with methods
      like `Data`, `DefaultRoute`, `Init*`, `Process*`.
- `package.json`: Contains JS and CSS dependencies.
- `tsconfig.json`: TypeScript configuration.
- `webpack.config.js`: Webpack configuration for building static assets.
- `webpack-assets.json`: Auto-generated file with asset information.

## Static asset management

GoSSR integrates with Webpack for managing JavaScript, CSS, and images using the `gossr-assets-webpack-plugin`. Key
features include:

- **JavaScript & styles**: The plugin automatically includes `index.ts`, `styles.scss`, and the
  generator-emitted `__ssr_gen__.ts` (for reactive routes) as entry point dependencies when they exist in the
  directory. The reactive client therefore loads automatically on every reactive route — no manual import is
  required in `index.ts`.
- **Image management**: Images are copied to the `/static` folder, and their paths are updated to use hashed filenames.
  For example:

```html
<img src="./logo.png">
<!-- becomes -->
<img src="/static/images/logo.<hash>.png">
```

### Embedded static handler

The generator inspects the webpack `outputPath` (e.g. `internal/web/dist/`) and, for each emitted file, stages a copy
under `pages/static_embed/` - gzip-precompressed for compressible types (CSS, JS, JSON, SVG, …) and stored verbatim for
already-compressed formats (PNG, JPEG, WOFF, WOFF2, MP4, …). It then writes `pages/ssrstaticfiles_gen.go` containing an
`embed.FS` (`//go:embed all:static_embed`) and a URL→file map. `NewSsrHandler` serves these assets first, falling
through to the SSR mux on miss - so no `http.FileServer` wiring is needed in user code.

Runtime behavior provided by `pkg/static`:

- Strong ETag header derived from the stored bytes; conditional `If-None-Match` returns `304 Not Modified`.
- `Cache-Control: public, max-age=31536000, immutable` (safe given hashed filenames).
- `Vary: Accept-Encoding` and gzip pass-through when the client accepts gzip; on-the-fly decompression otherwise.
- `405 Method Not Allowed` for non-GET/HEAD requests, with an `Allow` header.

Caching at generate time uses an `.etags.json` manifest keyed by source-file MD5, so unchanged sources skip
re-compression on subsequent builds. The `static_embed/` directory is reserved (it cannot be a route name) and is
ignored by the watcher to avoid feedback loops. If a static URL key would collide with a registered route path -
including dynamic `_param_` segments - generation fails with an explicit error instead of silently shadowing the route.

The `static_embed/` staging directory is build output and should be added to `.gitignore`:

```gitignore
**/pages/static_embed/
```

## Template syntax

### Expressions

GoSSR templates support expressions for inserting dynamic data between HTML tags or within attributes. For example:

```html
<p>Some text: {{ textValue }}</p>
<span class="name {{ dynamicClass }}">Text</span>
```

If any variable in an expression is declared `reactive="true"`, the expression site becomes a live binding: the
server re-renders the expression and pushes a DOM patch over WebSocket whenever any of its inputs changes.
Composite sites like `{{ a + b }}` work the same way - both `a` and `b` are tracked, and changing either patches
the rendered HTML.

The same applies to expressions inside attribute values. When at least one attribute value references a reactive
variable (for example `style="color: {{ userColor }}"` or `class="badge {{ tone }}"`), the whole element is
wrapped in an invisible block and re-rendered with its updated attributes on every change. Any other reactive
expressions inside that element are covered by the same patch and are not double-rendered.

### Operators

Supported operators include:

- **Arithmetic**: `+`, `-`, `*`, `/`, `%`
- **Comparisons**: `==`, `!=`, `<`, `<=`, `>`, `>=`
- **Logical**: `&&`, `||`, `!`
- **Accessors**: `.` for struct fields, `[]` for arrays
- **Function calls**: `funcName(arg1, arg2, ...)`
- **Ternary if**: `<boolean expression> ? <true value> : <false value>`

Example:

```html
<p>Sum: {{ a + b }}</p>
<p>Age: {{ user.Age >= 18 ? 'Adult' : 'Minor' }}</p>
```

### Declaring variables

Variables in GoSSR templates must have explicitly defined types. Declare them using the following syntax:

```html
<ssr:var name="varName" type="varType"/>
```

A variable can opt into live, server-pushed updates by adding `reactive="true"`. Adding `client-writable="true"`
also allows TypeScript to write the value back to the server (validated by a generated `Validate<Name>` hook):

```html
<ssr:var name="count" type="int" reactive="true" client-writable="true"/>
```

| Attribute | Values | Notes |
|-----------|--------|-------|
| `reactive` | `"true"` | Opts the variable into live binding. Any Go type is supported (structs, slices, maps, pointers, primitives). |
| `client-writable` | `"true"` | Allows TypeScript to write via `ssr.set()` or `ssr:bind`. Requires a `Validate<Name>` hook. |

For each reactive route the generator adds a `Subscribe(ctx, r, state)` stub plus a `Validate<Name>` stub for
each `client-writable` variable. Fill them in `dataprovider.go`:

```go
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
    // Called once when a WebSocket connects; runs for the connection's lifetime.
    // Data() is also re-invoked on every reconnect, so do not rely on r.Method or response headers here.
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            state.SetCount(state.Count + 1)
        }
    }
}

func (p *DP) ValidateCount(_ context.Context, _ *mux.Request, val int) (int, error) {
    if val < 0 {
        return 0, errors.New("count cannot be negative")
    }
    return val, nil
}
```

The generated WebSocket handler is mounted at the deepest reactive route's path, e.g. `/dashboard/users/__ws`.
The `__ws` segment is reserved: using it as a folder name in `pages/` is a generator error.

Nested reactive routes are supported: a parent and a child can both declare `reactive="true"` variables. The
generator emits a single WebSocket handler at the leaf path that runs each route's `Subscribe` concurrently
and multiplexes patches over one connection. From the developer's perspective each route works with its own
`*ReactiveState` and never sees the other route's variables. Binding keys are namespaced internally so
parent and child can both declare a variable named `count` without collision.

`ssr:bind` references must be local to the route declaring the variable; binding to a parent's variable from
a child template is a generator error. Subscribe goroutines are isolated by `recover()`: a panic in one
route's Subscribe closes the connection (clients reconnect via standard backoff) but does not crash the
server.

Reactive declarations are governed by these rules:

| Code | Trigger | Aborts generation |
|------|---------|-------------------|
| E01 | A folder in `pages/` is named `__ws` | yes |
| E05 | `ssr:bind` on a variable without `client-writable="true"`, undeclared, or declared in a different route | yes |
| E06 | `ssr:bind` on a GoSSR form primitive (`<ssr:input>`, `<ssr:select>`, `<ssr:textarea>`) | yes |
| E07 | `ssr:bind` on a non-scalar variable (struct, slice, map, etc.) | yes |

Any Go type works with `reactive="true"` (scalars, structs, slices, maps, pointers, `time.Time`, custom types). The
`ssr:bind` attribute itself is restricted to scalars because native HTML element values are strings; use `ssr.set()`
in TypeScript for non-scalar writes.

A note on loop reactivity: `<ssr:for>` over a reactive collection re-renders the whole loop body on every
change. Keyed list diffing is planned but not yet implemented.

### Embedding content

For routes with nested sub-routes, use the `<ssr:content/>` tag to embed child templates. You can also specify a default
child route using the `default` attribute:

```html
<ssr:content default="/info"/>
```

### Conditional rendering

Render elements conditionally using `ssr:if`, `ssr:else-if`, and `ssr:else` attributes:

```html
<span ssr:if="user.Age <= 18">0-18</span>
<span ssr:else-if="user.Age <= 30">19-30</span>
<span ssr:else>60+</span>
```

When a condition references a reactive variable, the entire conditional chain becomes reactive: the generator
wraps the chain in an `<ssr-block>` element, and any change to an input variable re-renders the active branch
server-side and pushes one HTML patch. If no branch matches (no `ssr:else`), the block collapses to empty
content. Reactive `{{ expr }}` sites inside such a block are not double-patched: the outer block's re-render
covers them.

### Loops

Use loops to iterate over arrays:

```html
<ul>
    <li ssr:for="phone in phones">{{ phone }}</li>
</ul>
```

With an index variable:

```html
<p ssr:for="i, phone in phones">{{ i }}: {{ phone }}</p>
```

When the iterated collection is reactive, or when any reactive variable is referenced inside the loop body,
the whole loop becomes a reactive block: any change re-renders all iterations and patches the DOM. The loop
variable (`phone` above) is per-iteration and is not itself reactive. The full loop body is re-rendered on
every change; keyed list diffing is planned but not yet implemented.

### Two-way input binding

Native `<input>`, `<select>`, and `<textarea>` elements can be bound to a `client-writable` reactive variable
using the `ssr:bind` attribute:

```html
<input ssr:bind="count" type="number"/>
```

The element's value stays in sync with the server variable in both directions: user input fires `ssr.set`
(validated by `Validate<Name>` server-side); a server push sets `element.value`. No TypeScript event handler
is required. `ssr:bind` is invalid on `<ssr:input>`/`<ssr:select>`/`<ssr:textarea>` (those are form-submission
primitives, not reactive bindings - see error E06).

### TypeScript API

For each reactive route the generator emits `__ssr_gen__.ts` containing the route's typed variable map and a
fully-wired `ssr` client. The route key, WebSocket URL, and `createSsrClient` call are all generator-managed.
The webpack plugin (`gossr-assets-webpack-plugin`) bundles `__ssr_gen__.ts` into the route's chunk
automatically, so the WebSocket connects on every reactive route regardless of whether you author an `index.ts`
or what it contains.

You only need to import `ssr` if you want to drive the runtime from your own code:

```typescript
import { ssr } from './__ssr_gen__';

// Read the last-received rendered HTML string for a binding.
const rendered = ssr.get('count'); // string | undefined

// Write a value to the server (fire-and-forget; validated server-side).
ssr.set('count', 42);

// Subscribe to server-pushed updates. The callback receives a pre-rendered HTML string.
// The generated runtime patches the DOM automatically; ssr.on() is for custom side-effects.
const unsub = ssr.on('count', (renderedHtml) => {
    console.log('count changed to', renderedHtml);
});

// React to validation errors from the server.
ssr.onError((varName, message) => {
    const el = document.getElementById(`${varName}-error`);
    if (el) el.textContent = message;
});
```

`ssr.set` is only callable on variables declared `client-writable="true"`; calling it on a read-only variable
is a TypeScript compile error. `ssr.get` and `ssr.on` accept any reactive variable name.

The runtime is published as `gossr-runtime`. Add it to your `package.json` alongside `gossr-assets-webpack-plugin`.
TypeScript's `moduleResolution` must be `"bundler"` or `"nodenext"` to honour the runtime package's `exports` map.

### Driving Subscribe goroutines from elsewhere in the process

A reactive route's `Subscribe(ctx, r, state)` is a long-lived goroutine. The hard part is usually waking it up
when something changes outside the route — a different HTTP handler, a background worker, another service inside
the same process. `pkg/reactive` exposes a small generic pub/sub for that:

```go
import "github.com/sergei-svistunov/go-ssr/pkg/reactive"

// Per-user fan-out. Publish carries the new value so the subscriber doesn't
// need to re-query (avoids read-your-own-write races against the publisher's
// transaction).
var balances = reactive.NewTopic[uint32, float64]()

// Global fan-out, e.g. for a "users online" counter shown in every navbar.
var presence = reactive.NewBroadcast[int]()
```

Publisher side (any goroutine — typically inside an HTTP handler or worker):

```go
balances.Publish(userId, newBalance)
presence.Publish(currentOnline)
```

Subscriber side (the route's `Subscribe` loop):

```go
func (p *DP) Subscribe(ctx context.Context, r *mux.Request, state *ReactiveState) error {
    userId := currentUserID(r)
    sub := balances.Subscribe(userId)
    defer sub.Close()

    presenceSub := presence.Subscribe()
    defer presenceSub.Close()

    for {
        select {
        case <-ctx.Done():
            return nil
        case bal := <-sub.Updates():
            state.SetBalance(bal)
        case n := <-presenceSub.Updates():
            state.SetOnline(n)
        }
    }
}
```

Semantics:

- **`Publish` is non-blocking.** Each subscription has a cap-1 channel. If a buffered value is already pending,
  the old value is replaced with the new one (freshest-wins). Publishers are safe to call from inside database
  transactions; a slow subscriber can never block them, and a missed intermediate value doesn't matter because
  the subscriber always reads the latest.
- **`Close` is idempotent.** Once a key's subscription set is empty, the key is removed from the underlying map
  so a churning user-id space doesn't grow forever.
- **All methods are safe for concurrent use.**

The "dirty bit" pattern (signal that *something* changed and let the subscriber re-query the source of truth)
falls out naturally: parameterise `V` as `struct{}` and ignore the value:

```go
var notifyDirty = reactive.NewTopic[uint32, struct{}]()

// Publisher:
notifyDirty.Publish(userId, struct{}{})

// Subscriber:
case <-notifySub.Updates():
    count := db.UnreadCount(userId)
    state.SetUnread(count)
```

## Form Handling with Server-Side Rendering

GoSSR supports generating and processing HTML forms, streamlining the process of capturing user input and handling it
server-side with Go. The generator parses your HTML form templates, automatically generating Go code that processes the
forms, validates input, and manages errors. This functionality helps developers focus on designing user interfaces,
while GoSSR handles the complex Go code generation behind the scenes.

### Supported HTML Form Elements

The GoSSR generator supports a variety of HTML form elements that can be used in your templates to collect user input.
These elements are defined using the `ssr:` prefix, which signals the GoSSR generator to treat them as special form
elements and generate the appropriate Go code.

#### Supported Elements

1. **`<ssr:form>`** - Represents an HTML form. It is required to wrap all input fields. The generator will treat all
   enclosed fields as part of the form and generate the necessary backend code to process it.

- Attributes:
    - `name` (required): Unique identifier for the form.
    - `enctype` (optional): Can be `application/x-www-form-urlencoded` or `multipart/form-data`. The default is
      `application/x-www-form-urlencoded`. If `enctype` is not specified and the form contains at least one input with
      type `file`, the `enctype` will be set to `multipart/form-data`.

Forms also use CSRF tokens for security reasons, ensuring that submissions are protected from cross-site request forgery
attacks.

2. **`<ssr:input>`** - Represents an input element.

- Attributes:
    - `name` (required): Unique identifier for the input field.
    - `type` (required): The type of the input (`text`, `number`, `checkbox`, `radio`, `file`, etc.).
    - `gotype` (optional): Go data type for the value (`string`, `int`, `bool`, etc.). Defaults to `string`.
    - `required` (optional): If set, marks the input as mandatory.
    - `multiple` (optional): Can be used for `file` or `radio` inputs to handle multiple values.

3. **`<ssr:textarea>`** - Represents a multi-line input field for text.

- Attributes:
    - `name` (required): Unique identifier for the textarea.
    - `required` (optional): If set, marks the textarea as mandatory.

4. **`<ssr:select>`** - Represents a dropdown list.

- Attributes:
    - `name` (required): Unique identifier for the select field.
    - `gotype` (optional): Go data type for the value (`string`, `int`, etc.). Defaults to `string`.
    - `multiple` (optional): Allows multiple options to be selected.
    - `required` (optional): Marks the field as mandatory.

### Example Usage

Below is an example of how to use the supported elements within a form in your `index.html` file.

```html
<div>
    <ssr:form name="addUser" enctype="multipart/form-data">
        <div>
            <label for="login">Login</label>
            <ssr:input name="login" type="text" required id="login" placeholder="Login"/>
            <div ssr:if="form.Login.HasError()">{{ form.Login.GetError() }}</div>
        </div>

        <div>
            <label for="age">Age</label>
            <ssr:input name="age" type="number" gotype="uint8" required id="age" placeholder="Age"/>
            <div ssr:if="form.Age.HasError()">{{ form.Age.GetError() }}</div>
        </div>

        <div>
            <label for="gender">Gender</label>
            <ssr:select name="gender" gotype="string" required id="gender"/>
            <div ssr:if="form.Gender.HasError()">{{ form.Gender.GetError() }}</div>
        </div>

        <button type="submit">Submit</button>
    </ssr:form>
</div>
```

### Generated Go Code

GoSSR processes the HTML forms and automatically generates Go code to handle the following aspects:

1. **Form Parsing and Validation**: The generated Go code will parse form values, check for required fields, and apply
   validation rules defined in the template.
2. **Form Data Structs**: For each form, a corresponding `FormValues` struct is generated that encapsulates all the
   input fields. The Go code structure will contain parsed values with types declared in the `gotype` attribute,
   ensuring type safety.
3. **Error Handling**: The generator produces logic for managing and displaying errors for each form field.
4. **Multipart Form Handling**: If the form includes file inputs, the generated code will handle multipart form data
   appropriately.

### Generated Structs and Methods

For the above form example, GoSSR will generate the following Go struct and methods:

```go
type FormAddUserValues struct {
    form.BaseFormValues
    Login   form.Input[string]
    Age     form.Input[uint8]
    Gender  form.Select[string]
}

func (p *DP) InitAddUser(ctx context.Context, r *mux.Request, w mux.ResponseWriter, form *FormAddUserValues) error {
    // Initialize default values, if any
    form.Gender.SetOptions([]form.SelectOptionElement[string]{
        form.SelectOption[string]{Value: "male", Label: "Male"},
        form.SelectOption[string]{Value: "female", Label: "Female"},
    })
    return nil
}

func (p *DP) ProcessAddUser(ctx context.Context, r *mux.Request, w mux.ResponseWriter, form *FormAddUserValues) error {
    // Handle form submission logic, e.g., saving data to the database
    return nil
}
```

### Form Lifecycle

1. **Initialization**: During initialization, the generator produces functions to populate default values and options
   for `select` elements. This allows the form to be rendered with dynamic data, such as dropdown options fetched from a
   database.

2. **Validation**: On form submission, the generated Go code will validate the input fields as per the rules defined in
   the HTML template (e.g., checking if `required` fields are filled).

3. **Error Display**: Validation errors are passed back to the form and displayed to the user by dynamically setting the
   `is-valid` or `is-invalid` classes.

## Example

The [example folder](/example) demonstrates every feature of the framework woven into a small running app:

- Directory-based routing, dynamic URL params, embedded layouts, dependency injection, webpack-managed assets
- A live navbar balance pushed from the root layout's `Subscribe`
- A live visitor counter and a two-way bound `displayName` input on `/home`, with a server-side `Validate*`
  hook that logs and rejects oversized values
- A live user count on `/users` and a relative-time "last seen" indicator on `/users/<id>/info`, all updating
  simultaneously over a single multiplexed WebSocket connection
- A traditional form at `/contact` covering `<ssr:form>`, validation, and `Process*`

Run the app on `:18080`:

```bash
cd example
go run .
```

End-to-end browser tests live in `example/tests/` (Playwright, Chromium):

```bash
cd example/tests
npm install
npx playwright install chromium
npx playwright test
```

## Contributing

Contributions are welcome! Feel free to submit pull requests for new features, bug fixes, or improvements to
documentation.

## License

GoSSR is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
