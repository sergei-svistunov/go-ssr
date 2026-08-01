# Quickstart: create and run an application

Everything needed to get from an empty directory to a running GoSSR app.

## Requirements

- Go 1.25 or newer, for both the `go-ssr` tool and the application it generates.
- Node.js and npm, used for the asset pipeline (webpack, TypeScript, Sass).

## Install the tool

```bash
go install github.com/sergei-svistunov/go-ssr@latest
```

## Scaffold a project

`-init` requires an empty directory and refuses to run in one that is not.

```bash
mkdir myapp && cd myapp
go-ssr -init -pkg-name example.com/myapp
```

That writes:

```
gossr.yaml                        project config
go.mod                            module example.com/myapp
main.go                           http.ListenAndServe(":8080", web.New())
internal/web/web.go               mounts pages.NewSsrHandler
internal/web/package.json         webpack, ts-loader, sass, gossr-runtime
internal/web/tsconfig.json
internal/web/webpack.config.js
internal/web/pages/index.html     the root template
internal/web/pages/index.ts
internal/web/pages/styles.scss
```

It then installs npm dependencies, builds the assets, generates the Go code and runs `go mod tidy`, so the
project compiles immediately.

The scaffolded `index.html` is a reactive hello-world:

```html
<!doctype html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>GoSSR</title>
    <ssr:assets/>
</head>
<body>
<ssr:var name="greeting" type="string" reactive="true" client-writable="true"/>
<h1>{{ greeting }}</h1>
<input ssr:bind="greeting" placeholder="Type something..."/>
</body>
</html>
```

## Run it

```bash
go run .
```

Open <http://localhost:8080/>. Typing in the input writes the value to the server, which validates it and
pushes the re-rendered `<h1>` back over a WebSocket.

## The development loop

```bash
go-ssr -watch
```

Watch mode rebuilds on change and restarts the app: an edited `.html` regenerates the Go code, an edited
`.ts` or `.scss` re-runs webpack, an edited `.go` restarts the process.

## Adding a page

Create a directory with an `index.html` under `pages/`, then regenerate:

```bash
mkdir -p internal/web/pages/about
printf '<h1>About</h1>\n' > internal/web/pages/about/index.html
go-ssr
```

The parent template needs an `<ssr:content/>` for a child route to render inside it. Regeneration writes
`internal/web/pages/about/dataprovider.go` with a `Data` stub; fill it in to supply values. See
`recipes/add-route` for the whole flow.

## Production build

```bash
go-ssr -prod   # webpack in production mode, assets embedded into the binary
go build -o myapp .
```

The resulting binary serves its own static files; see `static-assets`.
