# GoSSR: Go Generator for HTML Server-Side Rendering

**GoSSR** is a Go-based tool that simplifies the development of web applications by generating `http.Handler`
implementations. It leverages your project's directory structure to define routing and uses HTML templates for efficient
server-side rendering (SSR).

## Key features

- **Directory-based routing**: Define web routes based on your project’s folder structure. Folders with leading and
  trailing underscores (e.g., `_userId_`) are interpreted as dynamic URL parameters, accessible via the `UrlParam`
  method in the request object.
- **HTML template rendering**: Transform HTML templates into Go code, enabling fast, type-safe server-side rendering.
- **Dynamic URL parameters**: Use folder names to define dynamic parts of URLs, which are passed as parameters to the
  corresponding handlers.
- **Data providers**: Automatically generate interfaces that allow injecting custom application logic into handlers via
  `RouteDataProvider`.
- **Static asset management**: Seamlessly integrate with `gossr-assets-webpack-plugin` to manage static assets (CSS,
  JavaScript, images) and dynamically replace paths with hashed filenames.
- **Automatic rebuild**: Watches for file changes, rebuilding assets and templates as needed, and automatically restarts
  the project.
- **Form Handling**: Automatically generate Go code to handle HTML forms, including validation and error management.

## It's very fast

The example below shows how you can benchmark SSR handler performance:

```go
var (
    ssrHandler = ctxMiddleware{
        pages.NewSsrHandler(
            web.NewDataProvider(&model.Model{}), mux.Options{},
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
    WebDir           string            `yaml:"webDir"`     // Directory containing SSR handlers and templates
    WebPackage       string            `yaml:"webPackage"` // Full path to the web package
    GoRunArgs        string            `yaml:"goRunArgs"` // Arguments for `go run`
    Env              map[string]string `yaml:"env"`       // Environment variables
    GenDataProviders bool              `yaml:"genDataProviders"` // Enable basic DataProviders implementation generation (experimental)
}
```

## Project structure

Create a directory for all GoSSR files, such as `internal/web`. This directory must include:

- **`pages/`**: Contains routes, GoSSR templates, TypeScript, and SCSS files. Each subdirectory is a route. Key files
  include:
    - `index.html`: Required, the page template.
    - `index.ts`: Optional, the page script.
    - `styles.scss`: Optional, the page's CSS styles.
    - `ssrhandler_gen.go`: Auto-generated, only in `pages` directory, contains common `DataProvider` interface and SSR
      router constructor.
    - `ssrroute_gen.go`: Auto-generated, defines route `DataProvider` interface and code for rendering templates.
- `package.json`: Contains JS and CSS dependencies.
- `tsconfig.json`: TypeScript configuration.
- `webpack.config.js`: Webpack configuration for building static assets.
- `webpack-assets.json`: Auto-generated file with asset information.

## Static asset management

GoSSR integrates with Webpack for managing JavaScript, CSS, and images using the `gossr-assets-webpack-plugin`. Key
features include:

- **JavaScript & styles**: The plugin automatically includes `index.ts` and `styles.scss` as entry point dependencies if
  they exist in the directory.
- **Image management**: Images are copied to the `/static` folder, and their paths are updated to use hashed filenames.
  For example:

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
            <div ssr:if="form.Login.HasError()">{{ form.Login.Error }}</div>
        </div>

        <div>
            <label for="age">Age</label>
            <ssr:input name="age" type="number" gotype="uint8" required id="age" placeholder="Age"/>
            <div ssr:if="form.Age.HasError()">{{ form.Age.Error }}</div>
        </div>

        <div>
            <label for="gender">Gender</label>
            <ssr:select name="gender" gotype="string" required id="gender"/>
            <div ssr:if="form.Gender.HasError()">{{ form.Gender.Error }}</div>
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

func (p *DataProvider) InitRouteAddUserData_FormAddUser(ctx context.Context, r *http.Request, w http.ResponseWriter, form *FormAddUserValues) error {
    // Initialize default values, if any
    form.Gender.Options = []form.SelectOption[string]{
        {Value: "male", Label: "Male"},
        {Value: "female", Label: "Female"},
    }
    return nil
}

func (p *DataProvider) ProcessRouteAddUserData_FormAddUser(ctx context.Context, r *http.Request, w http.ResponseWriter, form *FormAddUserValues) error {
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

For a working example, refer to the [example folder](/example). It demonstrates directory-based routing, template
embedding, dynamic URL parameters, and asset management with Webpack.

## Contributing

Contributions are welcome! Feel free to submit pull requests for new features, bug fixes, or improvements to
documentation.

## License

GoSSR is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
