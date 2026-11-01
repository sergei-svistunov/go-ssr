# Forms

A GoSSR form is a plain HTML form that posts to its own URL. The generator writes the parsing, type
conversion, required-field checking and CSRF handling; you write two hooks.

## Declaring a form

```html
<ssr:form name="contact">
    <ssr:input name="name" type="text" required class="form-control"/>
    <div ssr:if="form.Name.HasError()">{{ form.Name.GetError() }}</div>

    <ssr:input name="email" type="email" required/>
    <div ssr:if="form.Email.HasError()">{{ form.Email.GetError() }}</div>

    <ssr:select name="topic" required/>
    <ssr:textarea name="message" required/>

    <div ssr:if="form.GetError() != ''">{{ form.GetError() }}</div>
    <button type="submit">Send</button>
</ssr:form>
```

The rendered `<form>` posts to the current URL with `method="post"` and carries a hidden CSRF field. Field
attributes not understood by GoSSR (`class`, `id`, `placeholder`, `step`, …) pass through to the HTML, with
the same escaping and refusals as on any element (`template-syntax`).

See `template-syntax` for the full attribute list on `<ssr:form>`, `<ssr:input>`, `<ssr:select>` and
`<ssr:textarea>`.

## Showing a form conditionally

`ssr:if`, `ssr:else-if` and `ssr:else` work on `<ssr:form>` as on any element:

```html
<ssr:form name="reply" ssr:if="!thread.Closed">
    <ssr:textarea name="text" required/>
    <button type="submit">Reply</button>
</ssr:form>
<p ssr:else>This thread is closed.</p>
```

The form and its hidden CSRF field render only when the branch is taken. The condition decides what the page
shows and nothing else: the form's hooks are generated and run by its name, as for any form, so `Init<Name>`
runs on every request and a POST of the form is parsed and handed to `Process<Name>` whether the condition
holds or not. `ssr:if` hides the form; it does not protect it. A check that must stop the form belongs in a
`Guard` or in `Process<Name>`.

A form cannot sit in a block that a live variable renders again — an `ssr:if` chain or an `ssr:for` that reads
one, or an element with an attribute that does — since a live update renders the block from the route's data
alone; the generator refuses it. Inside a form, a live value is live as long as its site does not read the
form; see `reactive`. Any `ssr:` attribute on `<ssr:form>` other than one of `ssr:if`, `ssr:else-if` and
`ssr:else` is refused too.

A form's name and its fields' names become Go names — `FormContactValues`, `InitContact`, the field `Name` — so
each is a letter followed by letters, digits and `_`, and no two forms of a template, nor two fields of a form,
may differ only in the case of their first letter. A field may not be named `baseFormValues`, nor after a
method of `form.BaseFormValues`, such as `getError` or `hasError`, which the field would hide from the template.

## What gets generated

For `name="contact"` with the fields above, `ssrroute_gen.go` contains:

```go
type FormContactValues struct {
    form.BaseFormValues
    Name    form.Input[string]
    Email   form.Input[string]
    Topic   form.Select[string]
    Message form.Textarea
}
```

and the route's `RouteDataProvider` grows two methods:

```go
InitContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error
ProcessContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error
```

The `dataprovider.go` the generator writes for a new route has both; its `Process<Name>` refuses every
submission with 403 until you write it.

Field names are the `name` attribute with its first letter upper-cased, so `firstName` becomes `FirstName`
and `first_name` becomes `First_name`. The Go type of each field follows the element and its attributes:

| Declaration | Field type |
|---|---|
| `<ssr:input name="a"/>` | `form.Input[string]` |
| `<ssr:input name="a" gotype="uint8"/>` | `form.Input[uint8]` |
| `<ssr:input name="a" type="checkbox" gotype="bool"/>` (repeated name) | `form.InputMultiple[bool]` |
| `<ssr:input name="a" type="file"/>` | `form.File` |
| `<ssr:input name="a" type="file" multiple/>` | `form.FileMultiple` |
| `<ssr:select name="a" gotype="int"/>` | `form.Select[int]` |
| `<ssr:select name="a" multiple/>` | `form.SelectMultiple[string]` |
| `<ssr:textarea name="a"/>` | `form.Textarea` |

## Lifecycle

On every request to the route, GET or POST, after every `Guard` on the path has passed:

1. On POST, the mux checks the CSRF token and finds which form was submitted, before any form hook runs. The
   body is read as multipart or url-encoded. A missing cookie or a bad token is a 400.
2. The form struct is built and its elements registered.
3. `Init<Name>` runs. This is where select options and default values belong — it runs on every request, so
   the form can always be rendered.

Additionally on POST, for the submitted form only:

4. The enctype is checked against the declaration; a mismatch is a 400.
5. Each field is parsed into its Go type. A failed conversion or a missing `required` value sets that field's
   error. The form is marked validated, which is what `form.IsValidated()` reports in the template.
6. `Process<Name>` runs **only when step 5 found no errors**. With errors, the page re-renders with the
   submitted values and messages, and `Process` is not called.

A POST whose form no route of the page declares is a 400 `This form is not on this page.`; a form hidden by
`ssr:if` is still on the page; see *Showing a form conditionally*. Finally `Data` runs and the page renders.

Only the submitted form's fields are parsed, so several forms can live on one route without interfering.

## Writing Process

```go
func (p *DP) ProcessContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
    name := strings.TrimSpace(data.Name.GetValue())
    email := strings.TrimSpace(data.Email.GetValue())

    if !strings.Contains(email, "@") {
        data.Email.SetError("Please enter a valid email address")
        return nil // fall through to rendering; the template shows the error
    }

    if err := p.m.SaveContact(ctx, name, email); err != nil {
        data.SetError("Could not save your message, please try again")
        return nil
    }

    return mux.Redirect(http.StatusFound, "/contact?sent=1")
}
```

`Process` sees only values that parsed and passed `required`; the checks it adds are its own. Two patterns
worth copying from that:

- Returning `nil` after setting errors re-renders the page with the submitted values and messages in place.
- Returning `mux.Redirect` on success avoids the duplicate-submission problem of rendering directly.

Returning any other error aborts rendering and goes to the error handler, so use field errors and
`data.SetError` for anything the user should see.

## Populating select options

```go
func (p *DP) InitContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
    data.Topic.SetOptions([]form.SelectOptionElement[string]{
        form.SelectOption[string]{Value: "general", Label: "General Enquiry"},
        form.SelectOption[string]{Value: "support", Label: "Technical Support"},
        form.SelectOptionGroup[string]{Label: "Other", Options: []form.SelectOptionElement[string]{
            form.SelectOption[string]{Value: "billing", Label: "Billing", Disabled: true},
        }},
    })
    return nil
}
```

Options can come from a database — `Init<Name>` receives the request and the deps value, and runs before
rendering. On a POST it runs after the CSRF check, so a forged POST never reaches it.

## Reading values in the template

Inside `<ssr:form>` the name `form` refers to the values struct and `input` / `select` / `textarea` refer to
the element being rendered:

| Expression | Meaning |
|---|---|
| `form.IsValidated()` | the form has been submitted and parsed |
| `form.HasError()` | any field has an error |
| `form.GetError()` | the form-level error set with `SetError` |
| `form.Email.HasError()` | that field has an error |
| `form.Email.GetError()` | that field's message |
| `input.HasError()` | the element currently being rendered has an error |

The idiom for validation styling:

```html
<ssr:input name="email" required
           class="form-control {{ form.IsValidated() ? input.HasError() ? 'is-invalid' : 'is-valid' : '' }}"/>
```

## Files

```html
<ssr:form name="upload">
    <ssr:input name="avatar" type="file" required/>
    <ssr:input name="gallery" type="file" multiple/>
</ssr:form>
```

A file input forces `multipart/form-data`. `form.File` yields one `*form.FileHeader` and `form.FileMultiple`
yields a slice; `FileHeader` embeds `*multipart.FileHeader`, so the standard library API is available:

```go
if h := data.Avatar.GetValue(); h != nil {
    f, err := h.Open()
    if err != nil {
        return err
    }
    defer f.Close()
    // h.Filename, h.Size, h.Header are the multipart values
}

for _, h := range data.Gallery.GetValue() {
    …
}
```

Multipart bodies are parsed with `http.Request.ParseMultipartForm`: up to `mux.MaxMultipartMemory` (32 MB) is
kept in memory and the rest spills to temporary files. The whole body is capped at `mux.Options.MaxBodyBytes`,
10 MiB by default; raise it to what your largest upload needs. See `security`.

## CSRF protection

Every form renders a hidden `_csrf_token_` field, and a matching `__Host-_csrf_token_` cookie (`Secure`,
`HttpOnly`, `SameSite=Lax`, `Path=/`) is set while rendering. The token is a random nonce and an issue time
signed with `mux.Options.CSRFKey`, so a value the server never issued is refused rather than trusted for
matching a cookie — which is what planting a cookie would otherwise buy an attacker.

The mux checks every POST itself, after the route guards and before any form hook: the token in the body must
be one it signed and must match the cookie, otherwise the request is rejected with 400 and no `Init` or
`Process` runs. Before any of that, the mux refuses any POST/PUT/PATCH/DELETE the browser marks as coming from
elsewhere — `Sec-Fetch-Site: cross-site` or `same-site`, or an `Origin` that is not the request host — with 403
before any of the route's hooks run, unless the origin is listed in `mux.Options.OriginPatterns` (the same
list live connections use; the rule is `mux.CrossSite`) or `mux.Options.AllowCrossSite` exempts the request.
The exemption lifts only that refusal: an exempt POST still needs the cookie and the signed token, so a
third-party POST by design, such as a sign-in or payment callback, belongs in an `http.Handler` in front of
the mux, not in a page. When the browser sends no `Sec-Fetch-Site` at all, an absent `Origin` — and
`Origin: null`, as `Referrer-Policy: no-referrer` produces — is no signal either way and falls through to the
token check; when the browser does say `same-site` or `cross-site`, the request is refused whatever the
`Origin` says, unless a pattern lists it. Nothing needs to be wired up; just do not remove the hidden field or
strip the cookie.

Responses that issue a token are always sent with `Cache-Control: no-store`: the mux sets it once the routes
in the stack have run — whether the outcome is a page, a redirect or an error — so a layout's own
`Cache-Control` cannot re-enable caching of a response carrying a token.

The same token is reused while it is valid rather than minted per render, so a form open in one tab is not
invalidated by opening another, and the cookie is re-issued on each render so an active user does not fall
off its edge. That cannot extend a token: it expires a day after the issue time it carries, whatever the
cookie says. Past half that life a render rotates it, so a form is never handed a token about to expire
under it; the token it replaces moves to a `__Host-_csrf_token_prev` cookie and keeps being accepted until
its own expiry, so pages already open still submit. Set `mux.Options.CSRFSession` to bind tokens to a
session, and one captured before a login stops working after it. A login that re-renders instead of
redirecting gets a token for the new session on every form rendered after the rotation, but a form an
earlier route in the same response already rendered keeps one bound to the old session and will be refused —
redirect after a login and the question does not arise.

`mux.Options.CSRFKey` is required and must be at least 32 bytes; `NewSsrHandler` returns an error otherwise.
Every instance that serves the site needs the same key, and tokens survive a restart only if the key does,
so read it from your secret store. For development, any random 32-byte value works; see `quickstart`.

The `__Host-` prefix means browsers only store the cookie over https (Chrome and Firefox also accept it on
`http://localhost`; Safari does not). To develop over plain http on another host, or in Safari, pass
`mux.Options{InsecureCookies: true}` to `NewSsrHandler`; it drops the prefix and the `Secure` flag and must
never be enabled in production. A POST that arrives over plain http without the cookie is logged with that
likely cause (a TLS-terminating proxy should forward `X-Forwarded-Proto: https`, which silences the log
line). Behind a reverse proxy the `Origin` check compares against the `Host` the app sees, so forward the
public host (`proxy_set_header Host $http_host` in nginx).

## Two-way binding is a different feature

`ssr:bind` on a native `<input>` is for live reactive variables, not form submission, and is rejected on
`<ssr:input>` / `<ssr:select>` / `<ssr:textarea>`. See `reactive`.
