package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Number{}

type Number struct {
	Text string
}

func (n *Number) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString(n.Text)
}
