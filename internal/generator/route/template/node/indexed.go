package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Indexed{}

type Indexed struct {
	Expr  Node
	Index Node
}

func (n *Indexed) WriteGoCode(buf *gobuf.GoBuf) {
	n.Expr.WriteGoCode(buf)
	buf.WriteString("[")
	n.Index.WriteGoCode(buf)
	buf.WriteString("]")
}
