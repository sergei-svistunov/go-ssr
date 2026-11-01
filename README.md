# GoSSR: Go Generator for HTML Server-Side Rendering

**GoSSR** is a Go-based tool that simplifies the development of web applications by generating `http.Handler`
implementations. It leverages your project's directory structure to define routing and uses HTML templates for efficient
server-side rendering (SSR).

Full documentation lives in [docs/](docs/) and is also served by the built-in
[MCP server](#mcp-server-for-ai-agents), so the version an AI agent reads always matches the binary you installed.

## Key features

- **Directory-based routing**: Define web routes based on your project's folder structure. Folders with leading and
  trailing underscores (e.g., `_userId_`) are interpreted as dynamic URL parameters, accessible via the `URLParam`
  method in the request object. A parent route is a layout that wraps its pages, or a gate that guards them.
- **Route guards**: An optional `Guard` on any data provider runs before every other hook on the path — forms,
  `Data`, live connections and page calls — so a check on a layout covers every page below it.
- **HTML template rendering**: Transform HTML templates into Go code, enabling fast, type-safe server-side rendering.
  Every value is escaped for the place it lands — text, attribute or URL — and places with no safe escaping are
  refused at generate time with a code and a fix.
- **Data providers**: Each route has its own `RouteDataProvider` interface with short method names (`Data`,
  `DefaultRoute`, `Init*`, `Process*`, `Call*`). Routes are self-contained - each carries its own data provider.
- **Dependency injection**: Optionally configure a `depsPackage` in `gossr.yaml` to pass a shared dependencies type
  to all route data providers via constructor injection. No composite interfaces or manual wiring needed.
- **Static asset management**: Seamlessly integrate with `gossr-assets-webpack-plugin` to manage static assets (CSS,
  JavaScript, images) and dynamically replace paths with hashed filenames.
- **Embedded static serving**: Webpack output is gzip-precompressed at generate time and embedded directly into the
  binary via `//go:embed`. The generated handler serves static assets with ETags, `Cache-Control: immutable`,
  conditional 304s, and gzip content-negotiation - no separate file server or filesystem dependency at runtime.
- **Automatic rebuild**: Watches for file changes, rebuilding assets and templates as needed, and automatically restarts
  the project.
- **Form handling**: Automatically generate Go code to handle HTML forms, including validation, file uploads, CSRF
  protection and error management. The handler checks every POST's signed CSRF token before any form hook runs.
- **Security headers**: Every response carries a strict Content-Security-Policy, `nosniff`, `Referrer-Policy`
  and cross-origin isolation headers; cross-site form posts and WebSocket handshakes are refused.
- **Reactive bindings**: Opt-in, Vue-like live bindings. Mark a variable `reactive="true"` and the server pushes DOM
  patches to every connected client over WebSocket whenever the value changes. TypeScript can write back to
  server-validated state with `ssr.set()` or a declarative `ssr:bind` attribute on any native input element. No
  WebSocket boilerplate required.
- **Page calls**: Declare `<ssr:call name="save" in="SaveIn" out="SaveOut"/>`, implement `CallSave` in Go, and
  `await ssr.call('save', args)` from typed TypeScript, over the page's live connection, with the route guards
  checked on every call.
- **MCP server**: `go-ssr -mcp` exposes the documentation and the generator itself to AI agents over stdio.

## It's very fast

The example below, from `example/internal/web/pages/handler_test.go`, shows how you can benchmark SSR handler
performance:

```go
var (
    ssrHandler = ctxMiddleware{newSsrHandler()}
    req1 = httptest.NewRequest(http.MethodGet, "/home", nil)
    req2 = httptest.NewRequest(http.MethodGet, "/users/johndoe123/info", nil)
    dw = DiscardWriter{}
)

func newSsrHandler() http.Handler {
    h, err := pages.NewSsrHandler(&model.Model{}, mux.Options{CSRFKey: []byte("bench-key-bench-key-bench-key-32")})
    if err != nil {
        panic(err)
    }
    return h
}

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

Requires Go 1.25 or newer, plus Node.js and npm for the asset pipeline. go-ssr 0.7.0 works with gossr-runtime
0.2.0, and gossr-runtime 0.2.0 with go-ssr 0.7.0; nothing checks the pairing at run time, so install them
together.

```bash
go install github.com/sergei-svistunov/go-ssr@latest
```

## Quickstart

```bash
mkdir myapp && cd myapp
go-ssr -init -pkg-name example.com/myapp   # scaffold, install, build, generate
export CSRF_KEY=$(openssl rand -hex 32)    # signs CSRF tokens; a development key
go run .                                    # http://localhost:8080/
```

The scaffolded page is a reactive hello-world: typing in the input writes to the server, which validates the
value and pushes the re-rendered heading back over a WebSocket.

Day-to-day commands:

```bash
go-ssr           # regenerate after editing templates
go-ssr -watch    # rebuild and restart on every change
go-ssr -prod     # production assets, embedded into the binary
```

See [docs/quickstart.md](docs/quickstart.md) for the walkthrough and [docs/cli.md](docs/cli.md) for every flag.

## Documentation

| Topic | |
|---|---|
| [overview](docs/overview.md) | how a GoSSR application works, and what gets generated |
| [quickstart](docs/quickstart.md) | from an empty directory to a running app |
| [project-structure](docs/project-structure.md) | directories, generated files, reserved names |
| [cli](docs/cli.md) | command line flags and modes |
| [config](docs/config.md) | `gossr.yaml`, dependency injection and handler options |
| [routing](docs/routing.md) | dynamic parameters, layouts and gates, guards, default child routes |
| [template-syntax](docs/template-syntax.md) | every `ssr:` tag and attribute |
| [expressions](docs/expressions.md) | the `{{ }}` language: operators, calls, escaping, `SafeHTML` |
| [forms](docs/forms.md) | form declaration, validation, files, CSRF, the Init/Process hooks |
| [reactive](docs/reactive.md) | live server-pushed values and two-way input binding |
| [page-calls](docs/page-calls.md) | Go functions the page's TypeScript calls |
| [security](docs/security.md) | guards, CSRF, escaping, the Content-Security-Policy, connection limits |
| [fe-be-contract](docs/fe-be-contract.md) | how the browser and the server talk |
| [typescript-api](docs/typescript-api.md) | the generated `ssr` client and `gossr-runtime` |
| [runtime-api](docs/runtime-api.md) | `pkg/mux`, `pkg/form`, `pkg/render`, `pkg/reactive`, `pkg/static` |
| [static-assets](docs/static-assets.md) | the webpack pipeline and embedded serving |
| [errors](docs/errors.md) | what each generator error means and how to fix it |
| **Recipes** | [new app](docs/recipes/new-app.md) · [add a route](docs/recipes/add-route.md) · [add a form](docs/recipes/add-form.md) · [add a live value](docs/recipes/add-reactive-var.md) · [build for production](docs/recipes/deploy-prod.md) |

## MCP server for AI agents

GoSSR has its own template language and conventions, which an agent cannot guess. Running the binary as a
[Model Context Protocol](https://modelcontextprotocol.io) server hands it the documentation above plus tools to
drive the generator:

```bash
claude mcp add gossr -- go-ssr -mcp
```

Or configure it directly in any MCP host:

```json
{
  "mcpServers": {
    "gossr": {
      "command": "go-ssr",
      "args": ["-mcp"]
    }
  }
}
```

| Tool | |
|---|---|
| `gossr_docs` | read a documentation topic, or one section of it |
| `gossr_search_docs` | find the topic covering a tag, attribute, method or error |
| `gossr_init` | scaffold a new project, install, build, generate |
| `gossr_scaffold_route` | create a route and its data provider stub |
| `gossr_generate` | regenerate after template edits, building assets when they are stale |
| `gossr_validate` | check templates and bindings without writing files |
| `gossr_routes` | describe routes: parameters, variables, forms, calls, live connections, required methods |
| `gossr_webpack` | install npm dependencies and run webpack |
| `gossr_run` | start the application and wait until it answers, or report why it did not |
| `gossr_logs` | return what the running application has printed since the last call |
| `gossr_stop` | stop the application |

Every documentation topic is also published as a resource (`gossr://docs/<topic>`). Project tools resolve
`gossr.yaml` from the working directory, or from an explicit `projectDir` argument.

Generation writes and formats Go code without compiling it, so `gossr_run` is how an agent finds out whether
what it wrote actually builds and serves. The application runs in the background and is stopped when the
server exits.

## Example

The [example folder](/example) demonstrates every feature of GoSSR woven into a small running app:

- Directory-based routing, dynamic URL params, embedded layouts, dependency injection, webpack-managed assets
- A live navbar balance pushed from the root layout's `Subscribe`
- A live visitor counter and a two-way bound `displayName` input on `/home`, with a server-side `Validate*`
  hook that logs and rejects oversized values
- A live user count on `/users` and a relative-time "last seen" indicator on `/users/<id>/info`, all updating
  simultaneously over a single multiplexed WebSocket connection
- A page call on `/users/<id>/info` that refreshes "last seen" from TypeScript
- A traditional form at `/contact` covering `<ssr:form>`, validation, and `Process*`

Run the app on `:18080`:

```bash
cd example
CSRF_KEY=$(openssl rand -hex 32) go run .
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
