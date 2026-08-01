# Recipe: add a form

Declaring a form, wiring its two hooks, and handling validation, files and multiple forms per page.

## 1. Declare it in the template

```html
<!-- internal/web/pages/contact/index.html -->
<ssr:var name="success" type="bool"/>

<div ssr:if="success">Thanks, we will be in touch.</div>

<ssr:form name="contact">
    <label for="name">Name</label>
    <ssr:input name="name" type="text" required id="name"
               class="form-control {{ form.IsValidated() ? input.HasError() ? 'is-invalid' : 'is-valid' : '' }}"/>
    <div class="invalid-feedback" ssr:if="form.Name.HasError()">{{ form.Name.GetError() }}</div>

    <label for="email">Email</label>
    <ssr:input name="email" type="email" required id="email"/>
    <div ssr:if="form.Email.HasError()">{{ form.Email.GetError() }}</div>

    <label for="topic">Topic</label>
    <ssr:select name="topic" required id="topic"/>
    <div ssr:if="form.Topic.HasError()">{{ form.Topic.GetError() }}</div>

    <label for="message">Message</label>
    <ssr:textarea name="message" required id="message"></ssr:textarea>
    <div ssr:if="form.Message.HasError()">{{ form.Message.GetError() }}</div>

    <div ssr:if="form.GetError() != ''">{{ form.GetError() }}</div>
    <button type="submit">Send</button>
</ssr:form>
```

## 2. Regenerate

```bash
go-ssr
```

The route's interface now requires two methods, so the build fails until they exist — that is the reminder:

```
InitContact(ctx, r, w, data *FormContactValues) error
ProcessContact(ctx, r, w, data *FormContactValues) error
```

`FormContactValues` has one field per element, named after the `name` attribute:

```go
type FormContactValues struct {
    form.BaseFormValues
    Name    form.Input[string]
    Email   form.Input[string]
    Topic   form.Select[string]
    Message form.Textarea
}
```

## 3. Implement Init — options and defaults

Runs on every request, before parsing, so the form can always render:

```go
func (p *DP) InitContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
    data.Topic.SetOptions([]form.SelectOptionElement[string]{
        form.SelectOption[string]{Value: "general", Label: "General Enquiry"},
        form.SelectOption[string]{Value: "support", Label: "Technical Support"},
    })
    return nil
}
```

## 4. Implement Process — validate and act

Runs on POST after the fields have been parsed, **including when parsing already found errors**:

```go
func (p *DP) ProcessContact(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *FormContactValues) error {
    email := strings.TrimSpace(data.Email.GetValue())
    if !strings.Contains(email, "@") {
        data.Email.SetError("Please enter a valid email address")
    }

    if data.HasError() {
        return nil // re-render with messages
    }

    if err := p.m.SaveEnquiry(ctx, data.Name.GetValue(), email, data.Message.GetValue()); err != nil {
        data.SetError("Could not send your message, please try again")
        return nil
    }

    return mux.Redirect(http.StatusFound, "/contact?sent=1")
}
```

Redirecting on success prevents a resubmission on refresh. The `success` variable in the template then comes
from `Data`:

```go
func (p *DP) Data(ctx context.Context, r *mux.Request, w mux.ResponseWriter, data *RouteData) error {
    data.Success = r.URL.Query().Get("sent") == "1"
    return nil
}
```

## Typed and grouped fields

```html
<ssr:input name="age" type="number" gotype="uint8" required/>   <!-- form.Input[uint8] -->
<ssr:input name="agree" type="checkbox" gotype="bool" value="true"/>
<ssr:input name="plan" type="radio" gotype="uint8" value="1"/>
<ssr:input name="plan" type="radio" gotype="uint8" value="2"/>  <!-- same name: one field -->
<ssr:select name="tags" gotype="string" multiple/>              <!-- form.SelectMultiple[string] -->
```

Repeated `checkbox` names produce a multi-value field; repeated names on anything else are an error. All
elements sharing a name must agree on `gotype`.

## File uploads

```html
<ssr:form name="upload">
    <ssr:input name="avatar" type="file" required/>
</ssr:form>
```

The enctype becomes `multipart/form-data` automatically:

```go
if h := data.Avatar.GetValue(); h != nil {
    f, err := h.Open()
    if err != nil {
        return err
    }
    defer f.Close()
    // h.Filename, h.Size
}
```

## Several forms on one page

Give each an `<ssr:form name="…">`; only the submitted one is parsed, and each gets its own `Init`/`Process`
pair and values struct.

## Checklist

- [ ] every field has a unique `name`, or is a deliberate checkbox/radio group
- [ ] `gotype` set for anything that is not a string
- [ ] error display for each field plus the form-level error
- [ ] `Process` checks `data.HasError()` before acting
- [ ] success path redirects instead of rendering
- [ ] the hidden CSRF field is left alone, and pages with forms are not cached
