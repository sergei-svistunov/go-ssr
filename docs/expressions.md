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
| `{{ expr }}` | HTML-escaped: `&`, `<`, `>`, `"`, `'` and carriage return are replaced with entities |
| `{{$ expr }}` | written verbatim, no escaping |

Use `{{$ }}` only for values you produced yourself, such as pre-rendered markup from a Markdown renderer.
A user-supplied value written with `{{$ }}` is an HTML injection.

```html
<div>{{ post.Title }}</div>       <!-- safe for any input -->
<div>{{$ post.BodyHTML }}</div>   <!-- trusted markup only -->
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
- functions declared in the route directory's package (a helper in a `.go` file next to `index.html`),
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

Strings are written as-is; all sized integer types, `float32`, `float64` and `bool` are formatted directly;
anything else falls back to `fmt.Sprint`. A type with a `String()` method therefore renders through it. Floats
use `strconv`'s shortest representation (`%g`), so format currency and other presentation-sensitive numbers
yourself:

```html
{{ balance / 100 }}.{{ balance % 100 < 10 ? '0' : '' }}{{ balance % 100 }}
```

## Inside forms

Within an `<ssr:form>` two extra names are in scope:

- `form` — the form's values struct: `form.IsValidated()`, `form.GetError()`, `form.Login.HasError()`,
  `form.Login.GetError()`.
- `input`, `select`, `textarea` — the element currently being rendered, which makes a per-field class
  expression possible on the element itself:

```html
<ssr:input name="login" class="form-control {{ form.IsValidated() ? input.HasError() ? 'is-invalid' : 'is-valid' : '' }}"/>
```

See `forms` for the generated types behind these names.

## Reactive expressions

If any variable in an expression is declared `reactive="true"`, that expression site becomes a live binding:
the server re-renders it and pushes a DOM patch when an input changes. A composite expression such as
`{{ a + b }}` tracks both variables. See `reactive`.

## Notes on script and style content

`{{ }}` is interpreted inside `<script>` and `<style>` too. That is handy for injecting a server value into an
inline script, and a hazard for JavaScript object literals — `{{` in a script that is not meant as an
expression will be compiled as one.
