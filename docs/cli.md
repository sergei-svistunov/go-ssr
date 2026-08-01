# Command line interface

`go-ssr` is a single binary. Without flags it builds assets and regenerates the project.

## Synopsis

```bash
go-ssr [-init [-pkg-name <module>]] [-watch] [-prod] [-mcp]
```

The tool looks for `gossr.yaml` in the working directory and then in each parent directory, and treats the
directory containing it as the project root.

## Flags

| Flag | Effect |
|---|---|
| *(none)* | run webpack, analyze the templates, write the generated Go and TypeScript files |
| `-init` | scaffold a new project in the working directory, which must be empty, then generate and run `go mod tidy` |
| `-pkg-name <module>` | module path for `-init`; defaults to `gossr/app` |
| `-watch` | stay running, rebuild on change, restart the application process |
| `-prod` | build assets in production mode and embed them into the binary |
| `-mcp` | run as an MCP server over stdio instead of generating (see below) |

## Typical use

```bash
# scaffold
mkdir myapp && cd myapp
go-ssr -init -pkg-name example.com/myapp

# regenerate after editing templates
go-ssr

# development loop: regenerate, rebuild assets, restart on every change
go-ssr -watch

# release build
go-ssr -prod && go build -o myapp .
```

## Watch mode

`-watch` debounces changes and picks work by file type: `.html` regenerates the SSR code, `.ts` and `.scss`
re-run webpack, `.go` rebuilds and restarts the application. The application is started with the arguments in
`goRunArgs` and the environment in `env` from `gossr.yaml`.

## Exit status and errors

Template and validation problems are printed one per line as `file:line: message`, so an editor or a tool
can jump straight to them. A project with mistakes in several routes reports all of them in one run. Any
failure exits non-zero without writing partial output for the failing routes.

## MCP server mode

```bash
go-ssr -mcp
```

Speaks the Model Context Protocol over stdin/stdout, offering this documentation plus tools to scaffold,
generate, validate, inspect and run a project. Register it with an MCP host, for example:

```bash
claude mcp add gossr -- go-ssr -mcp
```

In this mode stdout belongs to the protocol: the output of npm, webpack and the Go toolchain is captured and
returned in tool results instead of being printed. Documentation tools work without a project; the project
tools resolve `gossr.yaml` from the working directory, or from an explicit `projectDir` argument.

There is no watch tool. Watching is for a person with a browser open: it regenerates on a timer, which for a
caller editing several files in a row means generating from a half-finished tree. A caller that has just
edited a template gets a better answer from `gossr_generate`, which reports on exactly that edit. Run
`go-ssr -watch` in a terminal when a human wants live reload.

Instead, `gossr_run` starts the application with `go run`, waits until it answers on the address it printed,
and leaves it running; `gossr_logs` returns what it has written since the last call, and `gossr_stop` ends
it, together with the binary `go run` built. This is the only way to find out whether generated code
compiles — generation formats Go but never builds it. The application is stopped when the MCP server exits,
so a host that restarts the server does not leave one holding the port.
