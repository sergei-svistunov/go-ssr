package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Variable{}

type Variable struct {
	BaseNode
	Name string
}

func (n *Variable) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString(n.Name)
}
