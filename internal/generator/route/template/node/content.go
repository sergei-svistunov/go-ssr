package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Content{}

type Content struct {
	Children []Node
}

func (n *Content) WriteGoCode(buf *gobuf.GoBuf) {
	for _, child := range n.Children {
		child.WriteGoCode(buf)
	}
}
