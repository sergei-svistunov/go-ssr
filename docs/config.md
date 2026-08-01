# Configuration: gossr.yaml

`gossr.yaml` marks the project root. The tool searches for it in the working directory and every parent.

## Fields

```yaml
webDir: ./internal/web
webPackage: example.com/myapp/internal/web
depsPackage: example.com/myapp/internal/model
depsType: Model
goRunArgs: .
env:
  PORT: "8080"
```

| Field | Default | Meaning |
|---|---|---|
| `webDir` | `./internal/web` | directory holding `pages/`, `package.json` and the webpack config. Relative paths resolve against this file, not the working directory |
| `webPackage` | — | full import path of the web package; the generator needs it to write imports |
| `depsPackage` | empty | import path of a package whose type is injected into every data provider |
| `depsType` | `Deps` | the type name inside `depsPackage`; only used when `depsPackage` is set |
| `goRunArgs` | `.` | arguments passed to `go run` in watch mode |
| `env` | empty | environment variables for the application process in watch mode |

## Dependency injection

Without `depsPackage`, each route generates a zero-argument constructor:

```go
func NewDP() *DP { return &DP{} }
```

With `depsPackage` and `depsType`, the generated handler threads one value through to every route:

```yaml
depsPackage: example.com/myapp/internal/model
depsType: Model
```

```go
// pages/users/dataprovider.go
import "example.com/myapp/internal/model"

type DP struct{ m *model.Model }

func NewDP(d *model.Model) *DP { return &DP{m: d} }
```

and the handler takes it as its first argument:

```go
// main.go
m := model.New(db)
http.ListenAndServe(":8080", pages.NewSsrHandler(m, mux.Options{}))
```

Because `depsType` names the type directly, any type works — a struct of dependencies, a service container, or
an application model. No wrapper struct is required.

**The deps package must not import the `pages` tree.** Route packages import the deps package, so an import
in the other direction is a cycle and the project will not build. Keep it as a sibling package such as
`internal/model`.

Changing `depsPackage` or `depsType` changes every generated `NewDP` signature, so existing
`dataprovider.go` files — which the generator does not rewrite — need updating by hand.

## Production mode

`prod` is not a config field; pass `-prod` on the command line. It switches webpack to production mode and
embeds the built assets into the binary.
