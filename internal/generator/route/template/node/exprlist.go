package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &ExpressionsList{}

type ExpressionsList struct {
	BaseNode
	Values []Node
}

func (n *ExpressionsList) WriteGoCode(buf *gobuf.GoBuf) {
	for i, value := range n.Values {
		if i > 0 {
			buf.WriteString(",")
		}
		value.WriteGoCode(buf)
	}
}

func (n *ExpressionsList) CollectVarRefs(reactive map[string]bool) []string {
	sets := make([][]string, len(n.Values))
	for i, v := range n.Values {
		sets[i] = v.CollectVarRefs(reactive)
	}
	return UnionRefs(sets...)
}
