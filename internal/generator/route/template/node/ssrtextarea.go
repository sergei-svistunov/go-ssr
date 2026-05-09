package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrTextarea{}

type SsrTextarea struct {
	BaseNode
	Name          string
	FormFieldName string
	Attributes    []HtmlAttribute
}

var textareaReservedAttrs = map[string]bool{
	"name":  true,
	"value": true,
}

// CollectVarRefs returns [] — form fields are not reactive.
func (n *SsrTextarea) CollectVarRefs(_ map[string]bool) []string { return []string{} }

func (n *SsrTextarea) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("{")
	buf.WriteStringLn(n.FilePos())
	buf.WriteStringLn("textarea := form." + n.FormFieldName)
	buf.WriteStringLn("_ = textarea")
	buf.WritePrintString("<textarea")
	buf.WritePrintString(` name="` + n.Name + `"`)

	for _, a := range n.Attributes {
		if textareaReservedAttrs[a.Key] {
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
	buf.WriteString("if _, err := mux.WriteHtmlEscaped(w, textarea.GetValue()); err != nil {")
	buf.WriteStringLn("return err")
	buf.WriteStringLn("}")
	buf.WritePrintString("</textarea>")

	buf.WriteStringLn("}")
}
