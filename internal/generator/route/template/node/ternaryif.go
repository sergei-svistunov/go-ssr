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

func (n *TernaryIf) CollectVarRefs(reactive map[string]bool) []string {
	return UnionRefs(
		n.Cond.CollectVarRefs(reactive),
		n.T.CollectVarRefs(reactive),
		n.F.CollectVarRefs(reactive),
	)
}
