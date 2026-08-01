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
attributes not understood by GoSSR (`class`, `id`, `placeholder`, `step`, `style`, …) pass through to the HTML.

See `template-syntax` for the full attribute list on `<ssr:form>`, `<ssr:input>`, `<ssr:select>` and
`<ssr:textarea>`.

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

Field names are exported forms of the `name` attribute, so `first_name` becomes `FirstName`. The Go type of
each field follows the element and its attributes:

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

Every request to the route, GET or POST:

1. The form struct is built and its elements registered.
2. `Init<Name>` runs. This is where select options and default values belong — it runs before parsing, on
   every request, so the form can always be rendered.

Additionally on POST:

3. `form.Parse` reads the body (multipart or url-encoded), verifies the CSRF token and identifies which form
   was submitted. A bad or missing token is a 400.
4. The enctype is checked against the declaration; a mismatch is a 400.
5. Each field is parsed into its Go type. A failed conversion or a missing `required` value sets that field's
   error.
6. `Process<Name>` runs — **whether or not step 5 found errors**. Check before acting.
7. The form is marked validated, which is what `form.IsValidated()` reports in the template.

Finally `Data` runs and the page renders.

Only the submitted form's fields are parsed, so several forms can live on one route without interfering.

## Writing Process

```go
func (p *DP) ProcessContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
    name := strings.TrimSpace(data.Name.GetValue())
    email := strings.TrimSpace(data.Email.GetValue())

    if !strings.Contains(email, "@") {
        data.Email.SetError("Please enter a valid email address")
    }
    if data.HasError() {
        return nil // fall through to rendering; the template shows the errors
    }

    if err := p.m.SaveContact(ctx, name, email); err != nil {
        data.SetError("Could not save your message, please try again")
        return nil
    }

    return mux.Redirect(http.StatusFound, "/contact?sent=1")
}
```

Two patterns worth copying from that:

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
rendering.

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

Multipart bodies are parsed with a memory limit; larger parts spill to temporary files, as with
`http.Request.ParseMultipartForm`.

## CSRF protection

Every form renders a hidden `_csrf_token_` field, and a matching `HttpOnly` cookie is set on each GET. On POST
the token in the body must match the cookie, otherwise the request is rejected with 400 before any hook runs.
Nothing needs to be wired up; just do not remove the hidden field or strip the cookie.

Because the cookie is issued while rendering, a form fetched over a cached response can post a stale token.
Serve pages containing forms with `Cache-Control: no-store` if you cache aggressively.

## Two-way binding is a different feature

`ssr:bind` on a native `<input>` is for live reactive variables, not form submission, and is rejected on
`<ssr:input>` / `<ssr:select>` / `<ssr:textarea>`. See `reactive`.
