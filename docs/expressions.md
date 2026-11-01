# Expressions: the `{{ }}` language

Expressions are compiled to Go, not interpreted. Whatever you write between the braces becomes a Go
expression in the generated renderer, so it is type-checked by the Go compiler.

## Where expressions appear

```html
<p>Total: {{ a + b }}</p>                        <!-- text -->
<span class="badge {{ tone }}">…</span>          <!-- attribute value -->
<div ssr:if="user.Age >= 18">…</div>             <!-- condition -->
<li ssr:for="user in users">…</li>               <!-- loop -->
```

An attribute value may mix literal text with any number of expressions. Text and attribute expressions use
the same grammar; `ssr:if` and `ssr:for` take a bare expression without braces.

## Escaped and raw output

| Form | Behaviour |
|---|---|
| `{{ expr }}` | escaped for its context: element text and quoted attributes are HTML-escaped (`&`, `<`, `>`, `"`, `'` and carriage return become entities, and a NUL byte becomes U+FFFD), URL attributes are also checked and percent-encoded |
| `{{$ expr }}` | written verbatim, no escaping; allowed only in element text |

The generator picks the escaper from where the value lands; `template-syntax` lists the contexts and the places
it refuses (E08–E18). A whole-URL value whose scheme is not `http`, `https` or `mailto`, such as
`javascript:alert(1)`, is written as `about:invalid#gossr-unsafe-url`.

`{{$ }}` writes any value as it is, and **the caller owns the safety of what it writes**: a user-supplied value
written with `{{$ }}` is an HTML injection. It is refused in an attribute, `<title>` and `<textarea>` (E13),
and, like `{{ }}`, in `<script>`, `<style>` and the other raw-text elements (E08, E12). Use it only for markup your code produced, such as the output of a Markdown renderer that
sanitises its input.

`pkg/render` has helpers for building markup in Go:

| Helper | Returns |
|---|---|
| `render.HTMLConst("<b>Hi</b>")` | markup from a string constant; a string variable does not compile |
| `render.EscapeHTML(text)` | markup that shows exactly `text` |
| `render.JoinHTML(a, b, …)` | the parts concatenated |

They return `render.SafeHTML`. `{{$ }}` writes it as markup; `{{ }}` escapes it like any other value, showing
the tags as text. A `render.SafeHTML` never decodes from JSON or text, and a client-writable variable may not
hold one (`reactive`).

```html
<ssr:var name="title" type="string"/>
<ssr:var name="body" type="render.SafeHTML"/>
<h1>{{ title }}</h1>   <!-- safe for any input -->
<div>{{$ body }}</div> <!-- markup the data provider built -->
```

```go
data.Title = post.Title
data.Body = render.JoinHTML(render.HTMLConst("<p>"), render.EscapeHTML(post.Summary), render.HTMLConst("</p>"))
```

## Literals

| Kind | Examples |
|---|---|
| String | `'text'`, `"text"`, `` `text` `` — all three quote styles are accepted |
| Number | `42`, `-7`, `3.14` |

A string literal is emitted as a Go double-quoted string regardless of the quotes used in the template, so
single quotes are the convenient choice inside HTML attributes.

## Operators

| Group | Operators |
|---|---|
| Arithmetic | `+` `-` `*` `/` `%` |
| Comparison | `==` `!=` `<` `<=` `>` `>=` |
| Logical | `&&` `||` `!` |
| Grouping | `( … )` |
| Member access | `.` for struct fields and methods |
| Indexing | `[ … ]` for slices, arrays and maps |
| Call | `f(a, b)` |
| Conditional | `cond ? whenTrue : whenFalse` |

Because the operators are emitted straight into Go, Go's typing rules apply: no implicit numeric conversion,
no truthiness, and `+` on a string means concatenation.

### Precedence

Binary operators are written into the generated Go source unchanged, so **Go's precedence governs them**:
`{{ 1 + 2 * 3 }}` compiles to `1+2*3` and yields 7. Parentheses in a template are preserved, so use them
whenever the intent is not obvious.

The conditional operator is the exception, because it has no Go equivalent and compiles to a helper call. Its
boundaries come from the template's own grammar, where every operator sits at one precedence level and
associates to the left. Two consequences:

```html
{{ a ? b : c + 1 }}
```

compiles to `TernaryIf(a, b, c) + 1` — the false branch is just `c`, and `+ 1` applies to the result of the
whole conditional, not to the branch.

```html
{{ x ? 'a' : y ? 'b' : 'c' }}
```

nests to the *left*: `TernaryIf(TernaryIf(x, 'a', y), 'b', 'c')`, which is almost certainly not what a chained
conditional is meant to do. Parenthesise chains explicitly, or use `ssr:if` / `ssr:else-if` instead:

```html
{{ x ? 'a' : (y ? 'b' : 'c') }}
```

## Field access, indexing and calls

```html
{{ user.Name }}                  <!-- struct field -->
{{ user.FullName() }}            <!-- method call -->
{{ users[0].Name }}              <!-- index then field -->
{{ scores['alice'] }}            <!-- map lookup -->
{{ formatMoney(balance, 2) }}    <!-- package-level function -->
```

A call compiles to a plain Go call, so the callee only has to resolve in the route's package. That means:

- methods on any value reachable from a variable,
- functions declared in the route directory's package (a helper in a `.go` file next to `index.html`) whose
  names do not start with `_`,
- a variable of func type declared with `<ssr:var name="title" type="func() string"/>` and called as
  `{{ title() }}`.

There is no built-in function library and no imports inside templates. To use something from another package,
wrap it in a helper function or pass the result in as a variable.

## The conditional operator

```html
{{ user.Age >= 18 ? 'Adult' : 'Minor' }}
<a class="nav-link {{ routePath == '/users' ? 'active' : '' }}">Users</a>
```

It compiles to `mux.TernaryIf(cond, whenTrue, whenFalse)`, a generic function rather than an `if` statement.
Two things follow: both branches must have the same type, and **both are evaluated**. A branch that would
panic — indexing a slice that may be empty, dereferencing a pointer that may be nil — panics even when the
condition would have avoided it. Use `ssr:if` for those cases.

## How values are rendered

Strings are written as they are, before escaping; all sized integer types, `float32`, `float64` and `bool` are
formatted directly; `nil` is empty; anything else falls back to `fmt.Sprint`. A type with a `String()` method
therefore renders through it, and is escaped like a string. Floats
use `strconv`'s shortest representation (`%g`), so format currency and other presentation-sensitive numbers
yourself:

```html
{{ balance / 100 }}.{{ balance % 100 < 10 ? '0' : '' }}{{ balance % 100 }}
```

## Inside forms

Within an `<ssr:form>` two extra names are in scope:

- `form` — the form's values struct: `form.IsValidated()`, `form.GetError()`, `form.Login.HasError()`,
  `form.Login.GetError()`.
- `input`, `textarea` — in the attributes of a field, the field being rendered: `input` on `<ssr:input>` and
  `<ssr:select>`, `textarea` on `<ssr:textarea>`. That makes a per-field class expression possible on the
  element itself:

```html
<ssr:input name="login" class="form-control {{ form.IsValidated() ? input.HasError() ? 'is-invalid' : 'is-valid' : '' }}"/>
```

See `forms` for the generated types behind these names. A template cannot declare `form`, `input` or
`textarea` itself, nor a name Go has, nor one that starts with `_`; see `template-syntax`.

## Reactive expressions

If any variable in an expression is declared `reactive="true"`, that expression site becomes a live binding:
the server re-renders it and pushes a DOM patch when an input changes. A composite expression such as
`{{ a + b }}` tracks both variables. See `reactive`.

## Script and style content

`{{ }}` and `{{$ }}` are refused inside `<script>` and `<style>` (E08), and an inline `<script>` body or a
`<style>` element is refused on its own (E16). To hand a server value to TypeScript, put it in a `data-*`
attribute and read it from the element:

```html
<div id="chart" data-points="{{ pointsJSON }}"></div>
```

```typescript
const points = JSON.parse(document.getElementById('chart')!.dataset.points ?? '[]');
```
