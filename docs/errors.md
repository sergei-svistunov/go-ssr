# Error catalog

Generator errors are reported as `file:line: message` — with a column when the generator knows one — one per
line, and a run reports every broken route rather than stopping at the first. Nothing is written for a route
that failed.

## Coded validation errors

These have stable codes because they express rules rather than syntax.

### E01 — `route folder "__ws" is reserved`

A directory under `pages/` is named `__ws`, which is where the generated WebSocket endpoint of a reactive route
is mounted. Rename the directory.

### E05 — `ssr:bind` target is not writable, or not local

Raised in three situations, all about `ssr:bind="name"`:

| Situation | Fix |
|---|---|
| the variable exists in this route but is not `client-writable` | add `client-writable="true"` to its `<ssr:var>`, or drop `ssr:bind` |
| the variable is declared in a *different* route | declare it in this route; a binding may not reach across routes |
| the variable is not declared anywhere | add `<ssr:var name="…" type="…" reactive="true" client-writable="true"/>` to this template |

A `client-writable` variable also needs a `Validate<Name>` method; that one is reported by the Go compiler, not
by the generator.

### E06 — `ssr:bind` on a GoSSR form primitive

`ssr:bind` was used on `<ssr:input>`, `<ssr:select>` or `<ssr:textarea>`. Those submit forms; they are not live
bindings. Use a native `<input>`, `<select>` or `<textarea>` for two-way binding, and keep the `ssr:` elements
for form fields. See `forms` and `reactive`.

### E07 — `ssr:bind` requires a scalar variable type

The bound variable is a struct, slice, map or other non-scalar. A native element's value is a string, so only
scalars can round-trip through it. Keep `reactive="true"` on the variable — that works with any type — and
write it from TypeScript with `ssr.set()` instead.

## Template syntax errors

| Message | Cause and fix |
|---|---|
| `cannot parse HTML: …` | malformed markup: an unclosed quote or a stray `<`. The reported line is where the tokenizer gave up |
| `<ssr:var> is missing the required name attribute` | add `name` |
| `<ssr:var name="x"> is missing the required type attribute` | every variable needs an explicit Go type: `type="string"` |
| `unknown GoSSR tag <ssr:…>` | a misspelled tag. The set is `var`, `content`, `assets`, `form`, `input`, `select`, `textarea` |
| `unknown GoSSR attribute "ssr:…"` | a misspelled attribute. The set is `if`, `else-if`, `else`, `for`, `bind` |
| `ssr:else must directly follow an element with ssr:if or ssr:else-if` | the previous sibling is not part of a conditional chain. Only whitespace may sit between the branches; a comment or another element breaks the chain |
| `invalid ssr:for expression "…"` | the syntax is `item in collection` or `index, item in collection` |
| `<ssr:input> must be inside an <ssr:form>` | form fields only exist inside a form |
| `<ssr:form> cannot be nested inside another <ssr:form>` | close the outer form first; HTML forms do not nest either |
| `<ssr:form> has an invalid enctype` | use `application/x-www-form-urlencoded` or `multipart/form-data` |
| `invalid enctype '…' for an input with type file` | a file input needs `multipart/form-data`; either set it or remove the explicit enctype and let it be inferred |
| `unknown gotype '…'` | `gotype` accepts `string`, `bool`, `int`, `int8`…`int64`, `uint`, `uint8`…`uint64`, `float32`, `float64` |
| `form elements with name 'x' have different gotypes` | grouped inputs sharing a name must agree on `gotype` |
| `form contains at least 2 elements with name 'x'` | duplicate names are only allowed for `checkbox` and `radio` groups |
| `syntax error` inside `{{ }}` | an expression the grammar cannot parse: an unclosed `{{`, an unsupported operator, a missing operand. See `expressions` |

## Generation errors

| Message | Cause and fix |
|---|---|
| `static asset URL(s) collide with route path(s)` | a built asset would be served at a URL that also matches a route, including through a `_param_` segment, and the static handler wins. Rename the route or change the webpack output path |
| `gossr.yaml not found in … or any parent directory` | run the tool inside the project, or point it at the project explicitly |
| `directory is not empty` | `-init` only scaffolds into an empty directory |
| `could not generate route …` / `could not write …` | a filesystem problem: permissions, or a generated file left read-only |

## Compile errors that mean a template changed

The generated `dataprovider.go` stub asserts `var _ RouteDataProvider = &DP{}`, so adding a feature to a
template surfaces as a missing-method build error rather than a silent runtime hole.

| Missing method | Triggered by |
|---|---|
| `Data` | every route |
| `DefaultRoute` | `<ssr:content/>` without a `default` attribute |
| `Init<Form>` / `Process<Form>` | an `<ssr:form name="Form">` |
| `Subscribe` | any `reactive="true"` variable in the route |
| `Validate<Name>` | a `client-writable="true"` variable |

A changed `depsPackage` or `depsType` in `gossr.yaml` changes every generated `NewDP` signature; existing
`dataprovider.go` files are yours and are not rewritten, so they need updating by hand.

## Runtime errors

| Response | Cause |
|---|---|
| `400 Missed CSRF token` / `400 Invalid CSRF token` | the form posted without the cookie the render set, or with a stale one. Common behind an aggressive cache — serve pages with forms as `no-store` |
| `400 Content-Type is not multipart/form-data` | the declared enctype and the submitted one disagree |
| `404` | no route matched, or every hook in the stack returned no data context |
| `302` from a route you expected to render | the route has child directories, so it redirects to its default child. See `routing` |
| `500` | a hook returned an error that is not an `*HttpError` or `*HttpRedirect`. Supply `mux.Options.ErrorHandler` to render your own page |

## WebSocket error frames

Visible in browser devtools; described fully in `fe-be-contract`.

| `code` | Meaning |
|---|---|
| `validation_failed` | `Validate<Name>` rejected the write, or the variable name is unknown. Reaches `ssr.onError` |
| `decode_error` | the JSON value did not fit the declared Go type. Logged by the runtime, not surfaced to `ssr.onError`; the variable is unchanged |
| `unknown_route` | the frame's route key is not one this connection serves — normally a stale client after a redeploy; reconnecting fixes it |
