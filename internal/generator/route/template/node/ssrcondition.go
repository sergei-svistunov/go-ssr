package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrCondition{}

type SsrConditionData struct {
	BaseNode
	Condition Node
	Body      Node
}

type SsrCondition struct {
	BaseNode
	Conditions []SsrConditionData
	ElseBody   Node
	// BlockKey is set by the generator's AnnotateBindings pass when any
	// reactive variable is referenced in this conditional block's condition
	// expressions or body branches. When non-empty, WriteGoCode emits an
	// <ssr-block data-ssr-bind="KEY"> wrapper around the rendered branch
	// output. The key is SHA256[:16] of "file:line:SsrCondition".
	BlockKey string
}

func (n *SsrCondition) CollectVarRefs(reactive map[string]bool) []string {
	var sets [][]string
	for _, c := range n.Conditions {
		sets = append(sets, c.Condition.CollectVarRefs(reactive))
		sets = append(sets, c.Body.CollectVarRefs(reactive))
	}
	if n.ElseBody != nil {
		sets = append(sets, n.ElseBody.CollectVarRefs(reactive))
	}
	return UnionRefs(sets...)
}

func (n *SsrCondition) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	if n.BlockKey != "" {
		buf.WritePrintString(`<ssr-block data-ssr-bind="` + n.BlockKey + `">`)
	}
	n.writeInner(buf)
	if n.BlockKey != "" {
		buf.WritePrintString("</ssr-block>")
	}
}

// WriteInnerGoCode emits the conditional logic without the <ssr-block> wrapper.
// Used by the generator when emitting renderBlock_KEY helper functions.
func (n *SsrCondition) WriteInnerGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	n.writeInner(buf)
}

func (n *SsrCondition) writeInner(buf *gobuf.GoBuf) {
	for i, c := range n.Conditions {
		if i > 0 {
			buf.WriteStringLn(c.FilePos())
			buf.WriteString("} else ")
		}
		buf.WriteString("if ")
		c.Condition.WriteGoCode(buf)
		buf.WriteStringLn(" {")
		c.Body.WriteGoCode(buf)
	}

	if n.ElseBody != nil {
		buf.WriteStringLn(n.ElseBody.FilePos())
		buf.WriteStringLn("} else {")
		n.ElseBody.WriteGoCode(buf)
	}

	buf.WriteString("}\n")
}
