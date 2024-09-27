package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Loop{}

type Loop struct {
	BaseNode
	Index    string
	Variable string
	Array    Node
	Children []Node
}

func (n *Loop) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	buf.WriteString("for ")
	if n.Index != "" {
		buf.WriteString(n.Index)
	} else {
		buf.WriteString("_")
	}
	buf.WriteString(",")
	buf.WriteString(n.Variable)
	buf.WriteString(":=range ")
	n.Array.WriteGoCode(buf)
	buf.WriteStringLn("{")
	for _, child := range n.Children {
		child.WriteGoCode(buf)
	}
	buf.WriteStringLn("}")
}
