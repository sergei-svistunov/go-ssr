# Template syntax reference

A GoSSR template is HTML with two additions: `ssr:`-prefixed tags and attributes, and `{{ }}` expressions.
Everything else passes through unchanged. For the expression language itself see `expressions`.

## Tags

Every GoSSR tag is written `<ssr:name .../>`. An unknown `ssr:` tag is a generator error.

### `<ssr:var>` — declare a value the template uses

```html
<ssr:var name="users" type="[]User"/>
<ssr:var name="count" type="int" reactive="true" client-writable="true"/>
```

| Attribute | Required | Meaning |
|---|---|---|
| `name` | yes | the identifier used in expressions; becomes an exported field on `RouteData` |
| `type` | yes | any Go type visible in the route's package: `string`, `int`, `[]User`, `map[string]int`, `*Profile`, `time.Time`, `func() string` |
| `reactive` | no | `"true"` makes the value live: changes are re-rendered and pushed over the WebSocket |
| `client-writable` | no | `"true"` lets the browser write the value back; requires a `Validate<Name>` hook, and a type that holds no `render` type and no generic type |

Declarations may appear anywhere in the document; placement does not affect rendering. The declared type is
used verbatim in generated code, so a type must be resolvable in the route's package — define helper types in
a plain `.go` file next to `index.html`.

### `<ssr:content/>` — where the child route renders

```html
<ssr:content/>
<ssr:content default="/home"/>
```

| Attribute | Required | Meaning |
|---|---|---|
| `default` | no | child route to redirect to when this route is requested directly; without it the route's `DefaultRoute` hook decides |

One per template. A route whose template has `<ssr:content/>` is a layout; a route with routes below it and
no `<ssr:content/>` is a gate, which renders only its own page. See `routing`.

### `<ssr:assets/>` — the CSS and JS tags for this route

```html
<head>
    <ssr:assets/>
</head>
```

Expands to the `<link rel="stylesheet">` and `<script defer>` tags for the current route's webpack chunk,
with content-hashed filenames. Place it in `<head>`. Assets of parent routes are emitted once per page, never
duplicated. See `static-assets`.

### `<ssr:form>` — a server-processed form

```html
<ssr:form name="addUser" enctype="multipart/form-data">
  …
</ssr:form>
```

| Attribute | Required | Meaning |
|---|---|---|
| `name` | yes | identifies the form; drives the generated `Init<Name>` / `Process<Name>` hooks and value struct |
| `enctype` | no | `application/x-www-form-urlencoded` (default) or `multipart/form-data`; becomes multipart automatically when the form contains a file input |

Any other attribute is passed through to the rendered `<form>`. Forms cannot be nested. See `forms`.

### `<ssr:input>`, `<ssr:select>`, `<ssr:textarea>` — form fields

```html
<ssr:input name="login" type="text" required class="form-control"/>
<ssr:input name="age" type="number" gotype="uint8" required/>
<ssr:input name="image" type="file" multiple/>
<ssr:select name="role" gotype="uint8" multiple="multiple"/>
<ssr:textarea name="bio" required/>
```

| Attribute | Applies to | Meaning |
|---|---|---|
| `name` | all | field name; becomes an exported field on the form's values struct |
| `type` | input | the HTML input type: `text`, `number`, `checkbox`, `radio`, `file`, … |
| `gotype` | input, select | Go type of the parsed value; defaults to `string` |
| `required` | all | field must be present; a missing value produces a validation error |
| `multiple` | input (file), select | collects multiple values |

`gotype` accepts `string`, `bool`, and the sized numeric types (`int`, `int8`…`int64`, `uint`, `uint8`…`uint64`,
`float32`, `float64`). Any other value is an error. Elements of the same `name` are merged into one field:
`checkbox` inputs become a multi-value field, `radio` inputs a single one; other duplicate names are an error.
Unrecognised attributes are passed through to the rendered element. These tags must appear inside an
`<ssr:form>`.

### `<ssr:call/>` — a function the page calls over its live connection

```html
<ssr:call name="refreshLastSeen" in="struct{}" out="LastSeenOut"/>
```

| Attribute | Required | Meaning |
|---|---|---|
| `name` | yes | the call's name: a lower-case ASCII letter followed by letters and digits; drives the generated `Call<Name>` hook |
| `in` | yes | the Go type of the argument, which the route package can name; `struct{}` for none |
| `out` | yes | the Go type of the result |

Declare each call once, at the template's top level: not inside an element with `ssr:if` or `ssr:for`, nor in
an `<ssr:form>` (E19). Both types travel as JSON, so a func, chan or complex type, `unsafe.Pointer`, an
interface with methods such as `error`, or a `mux`, `render` or `reactive` type is refused (E20). The data
provider gains `Call<Name>(ctx context.Context, r *mux.Request, in In) (Out, error)`, which runs after every
`Guard` on the path; `Data` does not run. The generated stub refuses every call with 403 until it is written.
The arguments are decoded strictly: an unknown field is a 400, and so is `null` for an `in` type that cannot be
nil, other than `struct{}`. In TypeScript, `ssr.call('refreshLastSeen', {})` resolves with the result, typed
from the route package's declarations as `encoding/json` writes them.

A layout's calls can be called from every page below it; a gate's only from the gate's own page, since a gate
does not render the pages below it. See `page-calls`.

## Attributes on ordinary HTML elements

These attach GoSSR behaviour to any HTML element. An unknown `ssr:` attribute is a generator error.

### `ssr:if`, `ssr:else-if`, `ssr:else` — conditional rendering

```html
<span ssr:if="user.Age <= 18">0-18</span>
<span ssr:else-if="user.Age <= 30">19-30</span>
<span ssr:else>31+</span>
```

`ssr:else-if` and `ssr:else` must directly follow an element carrying `ssr:if` or `ssr:else-if`; only
whitespace may sit between them. When no branch matches and there is no `ssr:else`, nothing is rendered. If a
condition reads a reactive variable, the whole chain becomes a live block — see `reactive`.

### `ssr:for` — loops

```html
<li ssr:for="user in users">{{ user.Name }}</li>
<p ssr:for="i, phone in phones">{{ i }}: {{ phone }}</p>
```

The expression is `item in collection` or `index, item in collection`. The element carrying the attribute is
what repeats. The loop variable exists only inside the element and is not itself reactive.

The generated code is a two-variable `for … := range` over the collection, so slices, arrays, maps and
strings all work. For a map the first variable is the key; for a string it is the byte index and the second is
a rune. A plain integer count or a channel cannot be iterated, because a two-variable range over them is not
valid Go.

### `ssr:bind` — two-way binding on a native input

```html
<input ssr:bind="count" type="number"/>
<select ssr:bind="role">…</select>
<textarea ssr:bind="note"></textarea>
```

Keeps a native `<input>`, `<select>` or `<textarea>` in sync with a reactive variable in both directions with
no TypeScript. The variable must be declared in the same route, be `client-writable="true"`, and have a scalar
type. `ssr:bind` is not valid on `<ssr:input>`, `<ssr:select>` or `<ssr:textarea>` — those are form-submission
fields, not live bindings. The element receives the server's value only when the page also has a plain
`{{ count }}` site of the variable. The generator writes `data-ssr-route` on every element with `ssr:bind`,
so a template may not set that attribute itself. See `reactive` and `errors`.

## Expressions in text and attributes

```html
<p>Total: {{ a + b }}</p>
<span class="badge {{ tone }}">{{ label }}</span>
<img src="{{ user.Image }}" alt="{{ user.Name }}">
<a href="/users/{{ user.Login }}/info">Info</a>
```

Attribute values may mix literal text and expressions freely, and must be quoted. `{{ }}` output is escaped
for the place the browser reads it:

| Context | Escaping |
|---|---|
| element text, and the text of `<title>` and `<textarea>` | HTML-escaped |
| a quoted attribute value | HTML-escaped |
| a URL attribute (`href`, `src`, `action`, …) whose value is the whole URL | must be relative or `http`, `https` or `mailto`, else it becomes `about:invalid#gossr-unsafe-url`; then percent- and HTML-escaped |
| a URL attribute after a fixed scheme or path | percent- and HTML-escaped; right after a lone leading `/` it can never start the URL with `//` |
| a URL attribute after `?` or `#` | encoded as one query or fragment component |

Some places have no safe escaping, and the generator refuses a value there with a code and a fix line: a
`<script>` or `<style>` body (E08), an `on*` attribute (E09), a `style` attribute (E10), an unquoted attribute
(E11), the text of `iframe`, `noembed`, `noframes`, `noscript`, `plaintext` and `xmp` (E12), a URL attribute
whose scheme a value could choose (E14, E15), a tag or attribute name (E17), and the attributes that load code
or change how the page is read (E18). An inline `<script>` body and any `<style>` element are refused even
without a value (E16), because the Content-Security-Policy blocks them; put code in the route's `index.ts` and
styles in `styles.scss`.

`{{$ }}` writes the value unescaped — see `expressions`. It is allowed only in element text, not in an
attribute, `<title>` or `<textarea>` (E13).

## Whitespace and literal elements

Runs of whitespace in text are collapsed to a single space, and whitespace-only text between elements is
dropped, which keeps generated pages compact. Two groups of elements keep their content byte for byte:

- `pre` and `textarea` preserve whitespace.
- `script`, `style`, `iframe`, `noscript`, `noembed`, `noframes`, `plaintext` and `xmp` are literal elements
  and also preserve whitespace.

The text of a raw-text element belongs to it up to its end tag, as the browser reads it; a `<script …/>` does
not end the element. A `<script type="application/json">` or `type="application/ld+json"` body may hold
literal data, but no `{{ }}`.

## Images

A `src` attribute on an `<img>` is resolved against the webpack asset map, so `src="logo.png"` next to the
template becomes the hashed output filename. Other elements and attributes are left alone. An image from another
origin also needs its origin in `img-src` through `mux.Options.CSPSources`; see `security`.

## Comments and doctype

HTML comments are dropped. The doctype is emitted verbatim, which is what keeps the browser out of quirks
mode — keep `<!doctype html>` as the first line of the root template.
