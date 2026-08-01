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
| `client-writable` | no | `"true"` lets the browser write the value back; requires a `Validate<Name>` hook |

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

One per template. See `routing`.

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
fields, not live bindings. See `reactive` and `errors`.

## Expressions in text and attributes

```html
<p>Total: {{ a + b }}</p>
<span class="badge {{ tone }}">{{ label }}</span>
<img src="{{ user.Image }}" alt="{{ user.Name }}">
```

`{{ }}` output is HTML-escaped. `{{$ }}` writes the value unescaped — see `expressions`. Attribute values may
mix literal text and expressions freely.

## Whitespace and literal elements

Runs of whitespace in text are collapsed to a single space, and whitespace-only text between elements is
dropped, which keeps generated pages compact. Two groups of elements keep their content byte for byte:

- `pre` and `textarea` preserve whitespace.
- `script`, `style`, `iframe`, `noscript`, `noembed`, `noframes`, `plaintext` and `xmp` are literal elements
  and also preserve whitespace.

Whitespace is the only thing these exemptions cover. **`{{ }}` is still an expression inside `<script>` and
`<style>`**, so a brace pair in JavaScript or CSS that is not meant as an expression must be avoided or
broken up — `{{ x }}` in a script is compiled and rendered exactly as it would be in the body. This is useful
for injecting server values into inline scripts, and a trap otherwise.

## Images

A `src` attribute on an `<img>` is resolved against the webpack asset map, so `src="logo.png"` next to the
template becomes the hashed output filename. Other elements and attributes are left alone.

## Comments and doctype

HTML comments are dropped. The doctype is emitted verbatim, which is what keeps the browser out of quirks
mode — keep `<!doctype html>` as the first line of the root template.
