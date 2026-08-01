# Recipe: create a new application

From nothing to a running app with a layout, two pages and injected dependencies.

## 1. Scaffold

```bash
mkdir myapp && cd myapp
go-ssr -init -pkg-name example.com/myapp
go run .          # http://localhost:8080/
```

The directory must be empty. This writes the project, installs npm dependencies, builds assets, generates code
and runs `go mod tidy`. See `quickstart` for the file list.

## 2. Decide on a dependency container

Almost every app needs a database handle or a service in its data providers. Create a package that does **not**
import anything from the `pages` tree:

```go
// internal/model/model.go
package model

type Model struct{ db *sql.DB }

func New(db *sql.DB) *Model { return &Model{db: db} }

func (m *Model) Users() ([]User, error) { … }
```

Point the config at it:

```yaml
# gossr.yaml
webDir: ./internal/web
webPackage: example.com/myapp/internal/web
depsPackage: example.com/myapp/internal/model
depsType: Model
goRunArgs: .
```

Now every generated `NewDP` takes a `*model.Model`. Do this before writing data providers — changing it later
means editing every `dataprovider.go` by hand. See `config`.

Pass it in when mounting:

```go
// internal/web/web.go
func New(m *model.Model) http.Handler {
    mux := http.NewServeMux()
    mux.Handle("/", pages.NewSsrHandler(m, ssrMux.Options{}))
    return mux
}
```

```go
// main.go
func main() {
    m := model.New(openDB())
    log.Fatal(http.ListenAndServe(":8080", web.New(m)))
}
```

## 3. Turn the root template into a layout

```html
<!-- internal/web/pages/index.html -->
<!doctype html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <ssr:var name="title" type="string"/>
    <title>{{ title }}</title>
    <ssr:assets/>
</head>
<body>
<nav>
    <ssr:var name="routePath" type="string"/>
    <a href="/home" class="{{ routePath == '/home' ? 'active' : '' }}">Home</a>
    <a href="/users" class="{{ routePath == '/users' ? 'active' : '' }}">Users</a>
</nav>
<main><ssr:content default="/home"/></main>
</body>
</html>
```

The `default` attribute means a request to `/` redirects to `/home`. Without it the route needs a
`DefaultRoute` hook.

## 4. Add the pages

```bash
mkdir -p internal/web/pages/home internal/web/pages/users
printf '<h1>Home</h1>\n' > internal/web/pages/home/index.html
cat > internal/web/pages/users/index.html <<'HTML'
<ssr:var name="users" type="[]User"/>
<h1>Users</h1>
<ul>
    <li ssr:for="u in users">{{ u.Name }}</li>
</ul>
HTML
```

`[]User` must resolve in the route's package, so add the type next to the template:

```go
// internal/web/pages/users/user.go
package users

type User struct {
    Login string
    Name  string
}
```

## 5. Generate and fill in the data providers

```bash
go-ssr
```

That writes a `dataprovider.go` stub per new route. Fill them in:

```go
// internal/web/pages/index.html's provider: internal/web/pages/dataprovider.go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    data.Title = "My app"
    data.RoutePath = r.URL.Path
    return nil
}
```

```go
// internal/web/pages/users/dataprovider.go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    rows, err := p.m.Users()
    if err != nil {
        return err
    }
    for _, u := range rows {
        data.Users = append(data.Users, User{Login: u.Login, Name: u.Name})
    }
    return nil
}
```

The stub's `DP` struct starts empty; hold the injected deps on it:

```go
type DP struct{ m *model.Model }

func NewDP(d *model.Model) *DP { return &DP{m: d} }
```

## 6. Run the development loop

```bash
go-ssr -watch
```

Then keep going: `recipes/add-route`, `recipes/add-form`, `recipes/add-reactive-var`. For a release build see
`recipes/deploy-prod`.

## Checklist

- [ ] `gossr.yaml` has `webPackage`, and `depsPackage`/`depsType` if dependencies are needed
- [ ] the deps package does not import the `pages` tree
- [ ] the root template has `<ssr:assets/>` in `<head>` and one `<ssr:content/>`
- [ ] every route with children has a default child, via `default` or `DefaultRoute`
- [ ] `go-ssr` runs clean and `go build ./...` compiles
