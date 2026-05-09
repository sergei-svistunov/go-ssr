package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Number{}

type Number struct {
	BaseNode
	Text string
}

func (n *Number) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString(n.Text)
}

func (n *Number) CollectVarRefs(_ map[string]bool) []string { return []string{} }
