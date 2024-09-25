package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Parentheses{}

type Parentheses struct {
	Value Node
}

func (n *Parentheses) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString("(")
	n.Value.WriteGoCode(buf)
	buf.WriteString(")")
}
