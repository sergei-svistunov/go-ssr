package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Function{}

type Function struct {
	Expr      Node
	Arguments *ExpressionsList
}

func (n *Function) WriteGoCode(buf *gobuf.GoBuf) {
	n.Expr.WriteGoCode(buf)
	buf.WriteString("(")
	n.Arguments.WriteGoCode(buf)
	buf.WriteString(")")
}
