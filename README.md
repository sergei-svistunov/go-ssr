
# GoSSR: Go Generator for HTML Server-Side Rendering

**GoSSR** is a Go-based tool designed to streamline the development of web applications by generating `http.Handler` implementations. It leverages the directory structure of your project to define routing and uses HTML templates for server-side rendering.

## Features

- **Directory-Based Routing**: Define web routes based on your project's folder structure. Each folder translates into a route, while folders with a leading and trailing underscore (e.g., `_userId_`) become dynamic URL parameters accessible via the `UrlParam` method in the request object.
- **HTML Template Conversion**: Convert HTML templates into Go code, enabling fast and type-safe server-side rendering.
- **Dynamic Parameters in Routes**: Define dynamic parts of URLs using folder names. These are passed as parameters to the corresponding handler.
- **Data Providers**: Automatically generate interfaces that enable injection of custom application logic into handlers via `RouteDataProvider`.
- **SSR Asset Management**: Works seamlessly with `gossr-assets-webpack-plugin` to manage static assets (CSS, JavaScript, images) and replace paths dynamically with hashed filenames.

## Installation

To install GoSSR, run the following command:

```bash
go install github.com/sergei-svistunov/go-ssr@latest
```

## Template Syntax

### Expressions

Expressions are used to insert dynamic data into HTML templates. GoSSR supports a wide variety of operations, including arithmetic, logical comparisons, and function calls.

Expressions can be inserted into text between HTML tags or inside attributes. For example:

```html
<p>Some text: {{ textValue }}</p>
<span class="name {{ dynamicClass }}">Text</span>
```

### Expression Operators

The following operators are supported in GoSSR templates:

- **Arithmetic**: `+`, `-`, `*`, `/`, `%`
- **Comparisons**: `==` (equal), `!=` (not equal), `<` (less than), `<=` (less than or equal), `>` (greater than), `>=` (greater than or equal)
- **Logical**: `&&` (and), `||` (or), `!` (not)
- **Accessors**: `.` for accessing struct fields, `[]` for indexing arrays
- **Function Calls**: `funcName(arg1, arg2, ...)`
- **Parentheses**: `()` for grouping expressions

Example of using operators:

```html
<p>Sum: {{ a + b }}</p>
<p>Age: {{ user.Age >= 18 ? 'Adult' : 'Minor' }}</p>
```

### Variables

GoSSR requires that each variable has an explicitly defined type. You can declare variables using the tag: 
```html
<ssr:var name="varName" type="varType"/>
``` 
Variables can be declared anywhere within the template.

If a template contains variables, a corresponding `RouteDataProvider` interface is generated, which must be implemented in your Go code.

### Embedding Content

For a route such as `/user/login/info`, which contains nested sub-routes, you can embed the content of each child template within the parent using the `<ssr:content/>` tag. You can also specify a default child route with the `default` attribute:

```html
<ssr:content default="/info"/>
```

If no default is specified, a `GetDefaultSubRoute` method will be added to the `RouteDataProvider` interface.

### Conditions

You can conditionally render HTML elements using the `ssr:if`, `ssr:else-if`, and `ssr:else` attributes:

```html
<span ssr:if="user.Age <= 18">0-18</span>
<span ssr:else-if="user.Age <= 30">19-30</span>
<span ssr:else-if="user.Age <= 60">31-60</span>
<span ssr:else>60+</span>
```

### Loops

You can use loops to iterate over arrays and render multiple elements:

```html
<ul>
    <li ssr:for="phone in phones">{{ phone }}</li>
</ul>
```

Or with an index variable:

```html
<ul>
    <p ssr:for="i, phone in phones">{{ i }}: {{ phone }}</p>
</ul>
```

## Static Asset Management

GoSSR integrates with Webpack for managing static assets like JavaScript, CSS, and images. By using the `gossr-assets-webpack-plugin`, you can automatically generate an entry point for each directory.

- **JavaScript & Styles**: The plugin uses `index.ts` and `styles.scss` as dependencies for the entry point if they exist in the directory.
- **Image Management**: Images in the project are collected and copied to the `/static` folder, and their paths in `<img>` tags are replaced with hashed filenames. For example, an image `logo.png` is transformed as follows:

```html
<img src="./logo.png">
<!-- becomes -->
<img src="/static/images/logo.<hash>.png">
```

## Example

For a working example of GoSSR in action, check out the [example folder](/example). It demonstrates directory-based routing, template embedding, dynamic URL parameters, and asset management with Webpack.

## Contributing

We welcome contributions! Whether you want to add new features, fix bugs, or improve documentation, feel free to fork the repository and submit a pull request.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for more details.