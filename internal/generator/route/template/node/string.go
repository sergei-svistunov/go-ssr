package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &String{}

type String struct {
	BaseNode
	Text string
}

func (n *String) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteQuotedString(n.Text[1 : len(n.Text)-1])
}
