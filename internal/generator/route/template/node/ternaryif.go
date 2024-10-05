package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &TernaryIf{}

type TernaryIf struct {
	BaseNode
	Cond Node
	T    Node
	F    Node
}

func (n *TernaryIf) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString("mux.TernaryIf(")
	n.Cond.WriteGoCode(buf)
	buf.WriteString(", ")
	n.T.WriteGoCode(buf)
	buf.WriteString(", ")
	n.F.WriteGoCode(buf)
	buf.WriteString(")")
}
