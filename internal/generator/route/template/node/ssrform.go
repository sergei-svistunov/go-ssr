package node

import (
	"html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ WithChildren = &SsrForm{}

type SsrForm struct {
	BaseNode
	Name           string
	DPCtxFieldName string
	EncType        string
	Attributes     []HtmlAttribute
	Children       []Node
}

var formReservedAttrs = map[string]bool{
	"name":    true,
	"enctype": true,
}

const (
	FormEncTypeMultipart = "multipart/form-data"
	FormEncUrlEncoded    = "application/x-www-form-urlencoded"
)

func (n *SsrForm) AddChildren(children ...Node) {
	n.Children = append(n.Children, children...)
}

func (n *SsrForm) LastChild() Node {
	if len(n.Children) == 0 {
		return nil
	}
	return n.Children[len(n.Children)-1]
}

func (n *SsrForm) PopChild() {
	if len(n.Children) > 0 {
		n.Children = n.Children[:len(n.Children)-1]
	}
}

func (n *SsrForm) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("{")
	buf.WriteStringLn("form := c." + n.DPCtxFieldName)
	buf.WriteStringLn("_ = form")
	buf.WritePrintString(`<form method="post"`)
	if n.EncType == "" {
		n.EncType = FormEncUrlEncoded
	}
	buf.WritePrintString(` enctype="` + n.EncType + `"`)
	for _, a := range n.Attributes {
		if formReservedAttrs[a.Key] {
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

	buf.WritePrintString(`<input type="hidden" name="_csrf_token_" value="` + html.EscapeString(n.Name) + `:`)
	buf.WriteStringLn("if _, err := w.Write([]byte(form.BaseFormValues.GetCSRFToken())); err != nil {")
	buf.WriteStringLn("return err")
	buf.WriteStringLn("}")
	buf.WritePrintString(`">`)

	for _, c := range n.Children {
		if tNode, ok := c.(*Text); ok {
			buf.WritePrintString(tNode.Text)
		} else {
			c.WriteGoCode(buf)
		}
	}

	buf.WritePrintString("</form>")
	buf.WriteStringLn("}")
}
