package node

import (
	"slices"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/htmlutils"
)

var _ WithChildren = &HtmlElement{}

type HtmlAttribute struct {
	Key    string
	Values []Node
}

type HtmlElement struct {
	BaseNode
	TagName    string
	Attributes []HtmlAttribute
	SelfClosed bool
	Children   []Node
	// BlockKey is set by AnnotateBindings when at least one attribute value
	// references a reactive variable. When non-empty, WriteGoCode wraps the
	// element in <ssr-block data-ssr-bind="KEY"> so the whole element (with
	// its rendered attributes) can be replaced on any input-variable change.
	BlockKey string
}

func (n *HtmlElement) LastChild() Node {
	if len(n.Children) == 0 {
		return nil
	}
	return n.Children[len(n.Children)-1]
}

func (n *HtmlElement) PopChild() {
	if len(n.Children) > 0 {
		n.Children = n.Children[:len(n.Children)-1]
	}
}

func (n *HtmlElement) AddChildren(children ...Node) {
	n.Children = append(n.Children, children...)
}

func (n *HtmlElement) CollectVarRefs(reactive map[string]bool) []string {
	var sets [][]string
	for _, a := range n.Attributes {
		for _, v := range a.Values {
			sets = append(sets, v.CollectVarRefs(reactive))
		}
	}
	for _, c := range n.Children {
		sets = append(sets, c.CollectVarRefs(reactive))
	}
	return UnionRefs(sets...)
}

// CollectAttributeVarRefs returns reactive variable names referenced only in
// this element's attribute values (excluding child nodes). Used by
// AnnotateBindings to decide whether the element itself becomes a reactive
// block: attribute substrings cannot be wrapped individually, so the whole
// element is the patch target.
func (n *HtmlElement) CollectAttributeVarRefs(reactive map[string]bool) []string {
	var sets [][]string
	for _, a := range n.Attributes {
		for _, v := range a.Values {
			sets = append(sets, v.CollectVarRefs(reactive))
		}
	}
	return UnionRefs(sets...)
}

func (n *HtmlElement) WriteGoCode(buf *gobuf.GoBuf) {
	if n.BlockKey != "" {
		buf.WritePrintString(`<ssr-block data-ssr-bind="` + n.BlockKey + `">`)
	}
	n.writeInner(buf)
	if n.BlockKey != "" {
		buf.WritePrintString("</ssr-block>")
	}
}

// WriteInnerGoCode emits the element without the <ssr-block> wrapper. Used by
// the generator when emitting renderBlock_KEY helper functions for reactive
// attribute sites.
func (n *HtmlElement) WriteInnerGoCode(buf *gobuf.GoBuf) {
	n.writeInner(buf)
}

func (n *HtmlElement) writeInner(buf *gobuf.GoBuf) {
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

	children := n.Children
	if n.TagName == "textarea" {
		children = textareaChildren(buf, children)
	}

	if htmlutils.LiteralElements[n.TagName] {
		for _, c := range children {
			if tNode, ok := c.(*Text); ok {
				buf.WritePrintString(tNode.Text)
			} else {
				c.WriteGoCode(buf)
			}
		}
	} else {
		for _, c := range children {
			c.WriteGoCode(buf)
		}
	}

	buf.WritePrintString("</" + n.TagName + ">")
}

// textareaChildren writes the newline that keeps a value's own leading newline, since HTML
// parsing drops one newline right after <textarea>, and returns the children to write after
// it, without the newline the template has there, which the browser would have dropped.
func textareaChildren(buf *gobuf.GoBuf, children []Node) []Node {
	buf.WritePrintString("\n")
	if len(children) == 0 {
		return children
	}
	t, ok := children[0].(*Text)
	if !ok || !strings.HasPrefix(t.Text, "\n") {
		return children
	}
	rest := slices.Clone(children)
	rest[0] = &Text{BaseNode: t.BaseNode, Text: t.Text[1:]}
	return rest
}
