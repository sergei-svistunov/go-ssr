# Error catalog

Generator errors are reported as `file:line: message (CODE)`, the code only for coded errors, one per line. An
error with a fix line adds it below, indented: `\tfix: …`. A run reports every broken route rather than stopping
at the first. Nothing is written for a route that failed.

## Coded validation errors

These have stable codes because they express rules rather than syntax.

### E01 — `route folder "__ws" is reserved`

A directory under `pages/` is named `__ws`, which is the address suffix of every page's live connection.

Fix: rename the directory.

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
bindings. See `forms` and `reactive`.

Fix: use a native `<input>`, `<select>` or `<textarea>` for two-way binding, and keep the `ssr:` elements for
form fields.

### E07 — `ssr:bind` requires a scalar variable type

The bound variable is a struct, slice, map or other non-scalar. A native element's value is a string, so only
`string`, `bool` and the sized integer and float types can round-trip through it.

Fix: keep `reactive="true"` on the variable — that works with any type — and write it from TypeScript with
`ssr.set()` instead.

## Unsafe contexts

The generator gives every `{{ }}` the escaper of the place the browser reads it: element text and the text of
`<title>` and `<textarea>` (`render.Text`), a quoted attribute value (`render.Attr`), and URL attributes —
`href`, `src`, `action`, `formaction`, the other URL attributes of HTML, `xlink:href`, and any attribute whose
name, without a `data-` prefix, contains `src`, `uri` or `url`. In a URL attribute a value that is the whole
URL goes through `render.URL`: a scheme other than `http`, `https` or `mailto`, or a URL that does not parse,
becomes `about:invalid#gossr-unsafe-url`, and a stray `%` is written as `%25` (a valid `%XX` is kept). A value
after a fixed scheme or path goes through `render.URLPart`, right after a lone leading `/` through
`render.URLPathStart` (so the URL never starts with `//`), and after `?` or `#` through `render.URLQuery`.
`{{$ }}` writes any value as it is, without escaping, and is allowed only in element text.

The generator reads the template as the browser does: the text after `<title/>`, `<textarea/>`, `<script/>` and
the other raw-text elements belongs to the element, up to its end tag. A place where no escaper is safe is
refused with one of the codes below; each prints its fix line.

### E08 — a value in a `<script>` or `<style>` body

A `{{ }}` or `{{$ }}` sits in the body of `<script>` or `<style>`, where HTML escaping does not apply.

Fix: `pass the value through a data-* attribute and read it in the page's TypeScript`

### E09 — an `on*` attribute

An element has an event handler attribute such as `onclick`, which runs inline code.

Fix: `remove the attribute and add the listener in the page's TypeScript`

### E10 — a `style` attribute

An element has a `style` attribute. The Content-Security-Policy blocks inline styles, and a value in one could
change the page's look.

Fix: `move the style into a stylesheet and switch classes instead`

### E11 — an unquoted attribute with a value

An attribute holds a `{{ }}` without quotes, so a space in the value would start another attribute.

Fix: `quote the value: name="{{ … }}"`, where `name` is the attribute's name.

### E12 — a value in `iframe`, `noembed`, `noframes`, `noscript`, `plaintext` or `xmp`

The browser shows the text of these elements without decoding it, so an escaped value would show its escapes
and an unescaped one could end the element.

Fix: `move the value out of <tag>; its text is shown undecoded`, where `tag` is the element.

### E13 — `{{$ }}` in an attribute, in `<title>` or in `<textarea>`

Raw markup only means something in element text; an attribute value and the text of `<title>` and `<textarea>`
are never markup.

Fix: `use {{ }}, which escapes the value`

### E14 — a literal `javascript:` or `vbscript:` URL, including in animation values, or a literal scheme other than `http`, `https` or `mailto` in front of a value

A URL attribute starts with a scheme that runs code, as the browser reads it (spaces, tabs and case do not
matter, and entities are decoded), or an `animate`, `set`, `animateMotion` or `animateTransform` element sets
such a URL through one of its values. A value may also follow a literal scheme only when it is `http`, `https`
or `mailto`: `href="data:text/html,{{ x }}"` or `src="blob:{{ x }}"` lets the value carry a page or a script.
A relative URL, such as `href="/notes/{{ id }}"`, takes a value as before, and a literal `data:` or `blob:`
URL without a value is allowed.

Fix: `link to a page, or handle the click in the page's TypeScript`

### E15 — a URL attribute whose scheme depends on a value

A URL attribute starts with a value followed by more text or values, such as `href="{{ base }}/x"`, or its
literal start does not yet fix the scheme, such as `href="java{{ x }}"`, so the value could make it
`javascript:`.

Fix: `start the attribute with a fixed scheme or path, or put the whole URL in one value`

A fixed start is not always a fixed host: a value right after a literal `//`, `/\` or a bare `http:` or
`https:`, as in `href="//{{ host }}/x"` or `href="https:{{ rest }}"`, chooses the host the link goes to, as in
`html/template`. Write the host literally when it matters, or check the value in the data provider.

### E16 — an inline `<script>` body or a `<style>` element

The Content-Security-Policy runs only scripts and styles loaded from the site, so inline code never runs. A
`<script type="application/json">` or `type="application/ld+json"` body may hold literal data, but no value.
A `<script …/>` does not end the element: the text after it, up to `</script>`, is its body.

Fix: `move it into an asset next to the template; the Content-Security-Policy blocks inline code`

### E17 — a value in a tag name or attribute name

A `{{ }}` is part of a tag name or an attribute name, which could make any element or attribute.

Fix: `write the name literally`

### E18 — a value in an attribute that loads code or changes how the page is read

Any attribute of `script`, `base`, `meta`, `link`, `object`, `embed`, `applet`, `animate`, `set`,
`animateMotion` or `animateTransform`, and `srcdoc`, `srcset`, `imagesrcset` and `http-equiv` on any element,
holds a value; or an element has a `srcdoc` attribute at all, which holds a whole page inline.

Fix: `write the attribute literally, or remove it`

### E19 — a malformed `<ssr:call>`

The tag has an attribute other than `name`, `in` and `out`; no name, or one that is not a lower-case ASCII letter
followed by letters and digits; no `in` or `out`, or one that is not a Go type; it sits inside an element with
`ssr:if`, `ssr:else-if`, `ssr:else` or `ssr:for`, or in an `<ssr:form>`; or the template declares its name twice.

Fix: `declare <ssr:call name="lowerCamel" in="Type" out="Type"/> once, at the template's top level`

### E20 — a call type that cannot travel as JSON

The `in` or `out` type of an `<ssr:call>`, or a field `encoding/json` writes of a type the route package
declares, is a func, a chan, a complex number, `unsafe.Pointer`, an interface with methods (`error` among
them, since `encoding/json` cannot decode into one), a `mux`, `render` or `reactive` type, or a map whose keys
`encoding/json` cannot write. `any` and `interface{}` are accepted; an interface `out` sends whatever its
dynamic value encodes to. Types declared in other packages are not checked: one that cannot travel as JSON
fails the call at run time instead.

Fix: `use a struct with json tags, or struct{}`

## Runtime panics

### E21 — a data provider `Guard` of the wrong signature

`NewRoute` panics when the data provider has a method named `Guard` that `mux.Guarded` does not accept, such as
one with other parameters or one declared on the pointer type of a provider passed by value. Without the panic
the route would be left open.

Fix: `give Guard the signature Guard(ctx context.Context, r *mux.Request) error`

## Template syntax errors

| Message | Cause and fix |
|---|---|
| `cannot parse HTML: …` | malformed markup: an unclosed quote or a stray `<`. The reported line is where the tokenizer gave up |
| `cannot read the attributes of <tag>` | the tag's attributes are written so that the generator cannot tell which values are quoted. Write each attribute as `name="value"`, separated by spaces |
| `<tag> has the attribute x twice` | browsers keep the first of two attributes with one name (in any case) and other parsers the last, so the generator refuses both. Keep one |
| `<tag> has an attribute name with a NUL character` | remove the NUL byte from the attribute name |
| `markup inside <tag>, which some browsers parse and others show as text` | the text of `xmp`, `noscript`, `iframe`, `noembed`, `noframes`, `plaintext` or a `<script>` data block holds `<` followed by a letter, `/` or `!`. Inside SVG or MathML the browser parses it as markup the generator never checked. Write the text without tags, or `&lt;` where a `<` is shown |
| `unknown type "…" on <ssr:input name="…">` | `type` is one of the HTML input types: `text`, `email`, `password`, `number`, `checkbox`, `radio`, `file`, `hidden` and the rest; it may be left out |
| `value "…" of <ssr:input name="…"> is not a …` | the `value` of a checkbox or radio input must be a value of its `gotype`, such as `true` for `bool` or `1` for `uint8` |
| `<ssr:var> is missing the required name attribute` | add `name` |
| `<ssr:var name="x"> is missing the required type attribute` | every variable needs an explicit Go type: `type="string"` |
| `<ssr:var name="x"> is client-writable, but its type … holds a render.…` | a page may never write markup: the variable's type, a type of the route package it names or a field of one holds a `render` type. Declare a `string` and write it with `{{ }}`, which escapes it. The generator does not look inside types of other packages; a `render.SafeHTML` there is caught at run time instead, because `SafeHTML` refuses to decode from JSON or text |
| `<ssr:var name="x"> is client-writable, but its type … uses the generic type …` | the variable's type, a type of the route package it names or a field of one is a generic type such as `Box[string]`; client-writable variables cannot use generic types. Declare a concrete struct |
| `<ssr:call name="x">: the in type "…" names the package p, which no Go file of the route imports …` | a misspelt qualifier, or a package no Go file next to the template imports. Fix the name, or import the package in a Go file of the route |
| `… names the package p, which the generator cannot look up: GOROOT could not be found …` | the generator was built without a GOROOT and `go env GOROOT` failed. Set the `GOROOT` environment variable to the Go installation, or put the `go` command on `PATH` |
| `the live variable x inside <title>, whose text a live update cannot change; …` | a reactive variable's `{{ }}` or `{{$ }}` sits in the text of `<title>` or `<textarea>`, where a live site's wrapper would show as text. The message names the first live variable. Show it outside the element, or drop `reactive="true"` |
| `<tag> sets data-ssr-route, which the generator writes on elements with ssr:bind; remove it` | the attribute is reserved: the generator writes the route key into it on every `ssr:bind` element. Remove it from the template |
| `unknown GoSSR tag <ssr:…>` | a misspelled tag. The set is `var`, `content`, `assets`, `form`, `input`, `select`, `textarea`, `call` |
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
| `Call<Name>` | an `<ssr:call name="name">` |

`Guard` is optional and never required by the interface; one with the wrong signature is E21.

A changed `depsPackage` or `depsType` in `gossr.yaml` changes every generated `NewDP` signature; existing
`dataprovider.go` files are yours and are not rewritten, so they need updating by hand.

## Start-up errors

`NewSsrHandler` returns the error `mux.New` reports:

| Error | Fix |
|---|---|
| `mux: CSRFKey is empty` / `mux: CSRFKey must be at least 32 bytes` | pass a key of at least 32 bytes from your secret store; for development, see `config` |
| `mux: CSPSources: directive "…" takes no added sources` | use one of the directives `security` lists |
| `mux: CSPSources: … source "…" …` | the source would weaken the policy or break out of its directive; list the exact origin instead |
| `mux: malformed OriginPatterns entry "…"` | a pattern `path.Match` cannot parse; fix its syntax |
| `mux: OriginPatterns entry "…" reads as a character class, not an IPv6 literal: …` | escape the brackets of an IPv6 literal: `\[::1\]:*` |

## Runtime errors

| Response | Cause |
|---|---|
| `400 Missed CSRF token` / `400 Invalid CSRF token` | the form posted without the cookie the render set, or with a token the handler will not accept — it expired (a day after it was issued), the session it was bound to changed, another instance with a different `mux.Options.CSRFKey` served the request or the key changed, or the browser refused the cookie because the site is served over plain http on a host other than localhost, or in Safari (the server logs that case once; see `forms`) |
| `400 This form is not on this page.` | the posted form name is not a form any route of the page renders |
| `403 Cross-site request` | the browser reported a POST/PUT/PATCH/DELETE as coming from another origin or a sibling subdomain, or `Origin` does not match the `Host` the app sees (logged with both) — behind a proxy, forward the public host; for a page served from elsewhere, list its origin in `mux.Options.OriginPatterns`; a third-party POST by design, such as a sign-in or payment callback, carries no CSRF token, so handle it in an `http.Handler` in front of the mux, not in a page |
| `400 Content-Type is not multipart/form-data` | the declared enctype and the submitted one disagree |
| `404` | no route matched, the path ends at a directory with no template, or a layout's default child is empty or the layout itself |
| `405` | a method other than GET, HEAD or POST, or a POST to a layout's own address |
| `302` from a route you expected to render | the route is a layout opened itself, so it redirects to its default child. Remove its `<ssr:content/>` to make it a gate, which renders its own page. See `routing` |
| `500` | a hook returned an error that is not an `*HttpError` or `*HttpRedirect`. Supply `mux.Options.ErrorHandler` to render your own page |

A refused live connection handshake answers before the upgrade, and the browser shows only a failed
connection: 400 `This address only takes live page connections.`, 403 `Cross-site live connection`, 404 for a
page without live values or calls, 429 `Too many live connections.`, or a guard's or `Data`'s refusal, a
redirect becoming 401. The fail statuses of page calls are in `page-calls`, and the close codes of live
connections in `fe-be-contract`.

## WebSocket error frames

Visible in browser devtools; described fully in `fe-be-contract`.

| `code` | Meaning |
|---|---|
| `validation_failed` | `Validate<Name>` rejected the write, or the variable name is unknown. Reaches `ssr.onError` |
| `decode_error` | the JSON value did not fit the declared Go type. Logged by the runtime, not surfaced to `ssr.onError`; the variable is unchanged |
| `unknown_route` | the frame's route key is not one this connection serves — normally a stale client after a redeploy; reconnecting fixes it |
