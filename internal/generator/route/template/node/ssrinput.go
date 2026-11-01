package node

import (
	"html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrInput{}

type SsrInput struct {
	BaseNode
	Name          string
	FormFieldName string
	Type          string
	Value         string
	// GoValue is Value as a Go literal of the field's type, for a checkbox or a radio.
	GoValue    string
	Multiple   bool
	Attributes []HtmlAttribute
}

var inputReservedAttrs = map[string]bool{
	"name":   true,
	"type":   true,
	"value":  true,
	"gotype": true,
}

// CollectVarRefs returns [] — form fields are not reactive.
func (n *SsrInput) CollectVarRefs(_ map[string]bool) []string { return []string{} }

func (n *SsrInput) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("{")
	buf.WriteStringLn(n.FilePos())
	buf.WriteStringLn("input := form." + n.FormFieldName)
	buf.WriteStringLn("_ = input")
	buf.WritePrintString("<input")

	buf.WritePrintString(` name="` + html.EscapeString(n.Name) + `"`)
	if n.Type != "" {
		buf.WritePrintString(` type="` + n.Type + `"`)
	}
	if n.Type != "file" && n.Type != "checkbox" && n.Type != "radio" {
		buf.WriteStringLn("if input.IsNotNull() {")
		buf.WritePrintString(` value="`)
		buf.WriteStringLn("v := any(input.GetValue())")

		// For invalid values show original string value
		buf.WriteStringLn("if input.HasError() {")
		buf.WriteStringLn("	v = input.GetFormValue()")
		buf.WriteStringLn("}")
		buf.WriteStringLn("if err := render.Attr(_w, v); err != nil {")
		buf.WriteStringLn("return err")
		buf.WriteStringLn("}")
		buf.WritePrintString(`"`)
		buf.WriteStringLn("}")
	}
	if n.Type == "radio" || n.Type == "checkbox" {
		buf.WritePrintString(` value="` + html.EscapeString(n.Value) + `"`)

		if n.Multiple {
			buf.WriteStringLn("if _, exists := input.GetValue()[" + n.GoValue + "]; exists {")
		} else {
			buf.WriteStringLn("if input.GetValue() == " + n.GoValue + " {")
		}
		buf.WritePrintString(" checked")
		buf.WriteStringLn("}")
	}

	for _, a := range n.Attributes {
		if inputReservedAttrs[a.Key] {
			continue
		}
		buf.WritePrintString(" ")
		buf.WritePrintString(a.Key)
		if len(a.Values) > 0 {
			buf.WritePrintString(`="`)
			for _, value := range a.Values {
				value.WriteGoCode(buf)
			}
			buf.WritePrintString(`"`)
		}
	}
	buf.WritePrintString(">")
	buf.WriteStringLn("}")
}
