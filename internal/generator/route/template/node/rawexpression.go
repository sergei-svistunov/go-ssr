package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &RawExpression{}

type RawExpression struct {
	BaseNode
	Value Node
	// Source holds the raw expression text between {{$ and }} (trimmed), populated
	// at parse time. Used by the reactive analysis pass to generate the SHA-256
	// binding key for composite (multi-variable) expressions so that two distinct
	// raw expressions produce different keys.
	Source string
	// BindingKey is set by the generator's reactive analysis pass when this
	// expression references at least one reactive variable. When non-empty,
	// WriteGoCode emits a <span data-ssr-bind="KEY"> wrapper around the
	// rendered value.
	BindingKey string
}

func (n *RawExpression) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	if n.BindingKey != "" {
		buf.WritePrintString(`<span data-ssr-bind="` + n.BindingKey + `">`)
	}
	n.writeValue(buf)
	if n.BindingKey != "" {
		buf.WritePrintString("</span>")
	}
}

// WriteInnerGoCode emits the expression without the <span data-ssr-bind> wrapper.
// Used by the generator when emitting renderBlock_KEY helper functions.
func (n *RawExpression) WriteInnerGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	n.writeValue(buf)
}

func (n *RawExpression) writeValue(buf *gobuf.GoBuf) {
	buf.WriteString("if _, err := mux.WriteRaw(w,")
	n.Value.WriteGoCode(buf)
	buf.WriteStringLn("); err != nil {")
	buf.WriteStringLn("return err")
	buf.WriteStringLn("}")
}

func (n *RawExpression) CollectVarRefs(reactive map[string]bool) []string {
	return n.Value.CollectVarRefs(reactive)
}
