package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Loop{}

type Loop struct {
	BaseNode
	Index    string
	Variable string
	Array    Node
	Children []Node
	// BlockKey is set by the generator's AnnotateBindings pass when any
	// reactive variable is referenced in this loop's array expression or body.
	// When non-empty, WriteGoCode emits an <ssr-block data-ssr-bind="KEY">
	// wrapper around the entire loop output. The key is
	// SHA256[:16] of "file:line:Loop".
	BlockKey string
}

func (n *Loop) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	openTag, closeTag := n.wrapperTags()
	if openTag != "" {
		buf.WritePrintString(openTag)
	}
	n.writeInner(buf)
	if closeTag != "" {
		buf.WritePrintString(closeTag)
	}
}

// wrapperTags chooses the HTML element used to wrap the loop output for a
// reactive loop, returning (openTag, closeTag). For loops whose body is a
// <tr>, we use <tbody> instead of <ssr-block>, because <ssr-block> inside
// a <table>/<tbody> is foster-parented out by the HTML parser, which
// duplicates the rendered rows in the live DOM. <tbody> is a valid table
// child, accepts <tr> children, and the runtime locates the wrapper by
// data-ssr-bind regardless of element name.
func (n *Loop) wrapperTags() (string, string) {
	if n.BlockKey == "" {
		return "", ""
	}
	if n.firstChildIsTR() {
		return `<tbody data-ssr-bind="` + n.BlockKey + `">`, "</tbody>"
	}
	return `<ssr-block data-ssr-bind="` + n.BlockKey + `">`, "</ssr-block>"
}

func (n *Loop) firstChildIsTR() bool {
	for _, c := range n.Children {
		el, ok := c.(*HtmlElement)
		if !ok {
			continue
		}
		return el.TagName == "tr"
	}
	return false
}

// WriteInnerGoCode emits the loop logic without the <ssr-block> wrapper.
// Used by the generator when emitting renderBlock_KEY helper functions.
func (n *Loop) WriteInnerGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	n.writeInner(buf)
}

func (n *Loop) writeInner(buf *gobuf.GoBuf) {
	buf.WriteString("for ")
	if n.Index != "" {
		buf.WriteString(n.Index)
	} else {
		buf.WriteString("_")
	}
	buf.WriteString(",")
	buf.WriteString(n.Variable)
	buf.WriteString(":=range ")
	n.Array.WriteGoCode(buf)
	buf.WriteStringLn("{")
	for _, child := range n.Children {
		child.WriteGoCode(buf)
	}
	buf.WriteStringLn("}")
}

// CollectVarRefs returns the union of reactive variable refs from the array
// expression AND all body children. Both the loop's collection expression and
// the loop body are reactive. A reactive variable in the body causes the entire
// loop block to re-render when that variable changes.
func (n *Loop) CollectVarRefs(reactive map[string]bool) []string {
	sets := [][]string{n.Array.CollectVarRefs(reactive)}
	for _, child := range n.Children {
		sets = append(sets, child.CollectVarRefs(reactive))
	}
	return UnionRefs(sets...)
}
