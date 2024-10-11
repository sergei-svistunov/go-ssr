
# GoSSR: Go Generator for HTML Server-Side Rendering

**GoSSR** is a Go-based tool that simplifies the development of web applications by generating `http.Handler` implementations. It leverages your project's directory structure to define routing and uses HTML templates for efficient server-side rendering (SSR).

## Key features

- **Directory-based routing**: Define web routes based on your project’s folder structure. Folders with leading and trailing underscores (e.g., `_userId_`) are interpreted as dynamic URL parameters, accessible via the `UrlParam` method in the request object.
- **HTML template rendering**: Transform HTML templates into Go code, enabling fast, type-safe server-side rendering.
- **Dynamic URL parameters**: Use folder names to define dynamic parts of URLs, which are passed as parameters to the corresponding handlers.
- **Data providers**: Automatically generate interfaces that allow injecting custom application logic into handlers via `RouteDataProvider`.
- **Static asset management**: Seamlessly integrate with `gossr-assets-webpack-plugin` to manage static assets (CSS, JavaScript, images) and dynamically replace paths with hashed filenames.
- **Automatic rebuild**: Watches for file changes, rebuilding assets and templates as needed, and automatically restarts the project.

## It's very fast

The example below shows how you can benchmark SSR handler performance:

```go
var (
    ssrHandler = ctxMiddleware{
        pages.NewSsrHandler(
            web.NewDataProvider(&model.Model{}), mux.Options{},
        ),
    }
    req1    = httptest.NewRequest(http.MethodGet, "/home", nil)
    req2    = httptest.NewRequest(http.MethodGet, "/users/johndoe123/info", nil)
    dw      = DiscardWriter{}
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
  WebDir           string            `yaml:"webDir"`           // Directory containing SSR handlers and templates
  WebPackage       string            `yaml:"webPackage"`       // Full path to the web package
  GoRunArgs        string            `yaml:"goRunArgs"`        // Arguments for `go run`
  Env              map[string]string `yaml:"env"`              // Environment variables
  GenDataProviders bool              `yaml:"genDataProviders"` // Enable basic DataProviders implementation generation (experimental)
}
```

## Project structure

Create a directory for all GoSSR files, such as `internal/web`. This directory must include:

- **`pages/`**: Contains routes, GoSSR templates, TypeScript, and SCSS files. Each subdirectory is a route. Key files include:
  - `index.html`: Required, the page template.
  - `index.ts`: Optional, the page script.
  - `styles.scss`: Optional, the page's CSS styles.
  - `ssrhandler_gen.go`: Auto-generated, only in `pages` directory, contains common `DataProvider` interface and SSR router constructor.
  - `ssrroute_gen.go`: Auto-generated, defines route `DataProvider` interface and code for rendering templates.
- `package.json`: Contains JS and CSS dependencies.
- `tsconfig.json`: TypeScript configuration.
- `webpack.config.js`: Webpack configuration for building static assets.
- `webpack-assets.json`: Auto-generated file with asset information.

## Static asset management

GoSSR integrates with Webpack for managing JavaScript, CSS, and images using the `gossr-assets-webpack-plugin`. Key features include:

- **JavaScript & styles**: The plugin automatically includes `index.ts` and `styles.scss` as entry point dependencies if they exist in the directory.
- **Image management**: Images are copied to the `/static` folder, and their paths are updated to use hashed filenames. For example:

```html
<img src="./logo.png">
<!-- becomes -->
<img src="/static/images/logo.<hash>.png">
```

## Template syntax

### Expressions

GoSSR templates support expressions for inserting dynamic data between HTML tags or within attributes. For example:

```html
<p>Some text: {{ textValue }}</p>
<span class="name {{ dynamicClass }}">Text</span>
```

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

### Embedding content

For routes with nested sub-routes, use the `<ssr:content/>` tag to embed child templates. You can also specify a default child route using the `default` attribute:

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

## Example

For a working example, refer to the [example folder](/example). It demonstrates directory-based routing, template embedding, dynamic URL parameters, and asset management with Webpack.

## Contributing

Contributions are welcome! Feel free to submit pull requests for new features, bug fixes, or improvements to documentation.

## License

GoSSR is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.