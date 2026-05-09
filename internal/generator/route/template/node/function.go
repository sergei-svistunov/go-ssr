package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Function{}

type Function struct {
	BaseNode
	Expr      Node
	Arguments *ExpressionsList
}

func (n *Function) WriteGoCode(buf *gobuf.GoBuf) {
	n.Expr.WriteGoCode(buf)
	buf.WriteString("(")
	n.Arguments.WriteGoCode(buf)
	buf.WriteString(")")
}

func (n *Function) CollectVarRefs(reactive map[string]bool) []string {
	return UnionRefs(n.Expr.CollectVarRefs(reactive), n.Arguments.CollectVarRefs(reactive))
}
