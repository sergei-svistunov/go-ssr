package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrSelect{}

type SsrSelect struct {
	BaseNode
	Name          string
	GoType        string
	FormFieldName string
	Attributes    []HtmlAttribute
	Multiple      bool
}

var selectReservedAttrs = map[string]bool{
	"name":   true,
	"value":  true,
	"gotype": true,
}

func (n *SsrSelect) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("{")
	buf.WriteStringLn(n.FilePos())
	buf.WriteStringLn("input := form." + n.FormFieldName)
	buf.WriteStringLn("_ = input")
	buf.WritePrintString("<select")
	buf.WritePrintString(` name="` + n.Name + `"`)
	for _, a := range n.Attributes {
		if selectReservedAttrs[a.Key] {
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

	if n.Multiple {
		buf.WriteStringLn("isSelected := func(v " + n.GoType + ") bool { _, exists := input.GetValue()[v]; return exists }")
	} else {
		buf.WriteStringLn("isSelected := func(v " + n.GoType + ") bool { return v == input.GetValue() }")
	}
	buf.WriteStringLn("for _, o := range input.Options {")
	buf.WriteStringLn("	if err := o.WriteHtml(w, isSelected); err != nil {")
	buf.WriteStringLn("		return err")
	buf.WriteStringLn("	}")
	buf.WriteStringLn("}")

	buf.WritePrintString("</select>")
	buf.WriteStringLn("}")
}
