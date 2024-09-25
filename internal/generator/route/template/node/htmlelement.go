package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/htmlutils"
)

var _ Node = &HtmlElement{}

type HtmlAttribute struct {
	Key    string
	Values []Node
}

type HtmlElement struct {
	TagName    string
	Attributes []HtmlAttribute
	SelfClosed bool
	Children   []Node
}

func (n *HtmlElement) WriteGoCode(buf *gobuf.GoBuf) {
	if n.TagName == "qssr:content" {
		buf.WriteStringLn("if c.child != nil {")
		buf.WriteStringLn("	err := c.child.Write(w)")
		buf.WriteStringLn("	if err != nil {")
		buf.WriteStringLn("		return err")
		buf.WriteStringLn("	}")
		buf.WriteStringLn("}")
		return
	}

	buf.WritePrintString("<" + n.TagName)
	for _, a := range n.Attributes {
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
	if n.SelfClosed || htmlutils.VoidElements[n.TagName] {
		buf.WritePrintString(">")
		return
	}
	buf.WritePrintString(">")

	if htmlutils.LiteralElements[n.TagName] {
		for _, c := range n.Children {
			if tNode, ok := c.(*Text); ok {
				buf.WritePrintString(tNode.Text)
			} else {
				c.WriteGoCode(buf)
			}
		}
	} else {
		for _, c := range n.Children {
			c.WriteGoCode(buf)
		}
	}

	buf.WritePrintString("</" + n.TagName + ">")
}
