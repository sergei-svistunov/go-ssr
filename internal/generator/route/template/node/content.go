package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Content{}

type Content struct {
	BaseNode
	Children []Node
}

func (n *Content) WriteGoCode(buf *gobuf.GoBuf) {
	for _, child := range n.Children {
		child.WriteGoCode(buf)
	}
}

func (n *Content) CollectVarRefs(reactive map[string]bool) []string {
	sets := make([][]string, len(n.Children))
	for i, c := range n.Children {
		sets[i] = c.CollectVarRefs(reactive)
	}
	return UnionRefs(sets...)
}
