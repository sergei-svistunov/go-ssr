package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Operator{}

type Operator struct {
	BaseNode
	Op    string
	Left  Node
	Right Node
}

func (n *Operator) WriteGoCode(buf *gobuf.GoBuf) {
	if n.Left != nil {
		n.Left.WriteGoCode(buf)
	}
	buf.WriteString(n.Op)
	n.Right.WriteGoCode(buf)
}
