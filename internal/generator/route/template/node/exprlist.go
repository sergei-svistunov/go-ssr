package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &ExpressionsList{}

type ExpressionsList struct {
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
