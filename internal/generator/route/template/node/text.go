package node

import (
	"golang.org/x/net/html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Text{}

type Text struct {
	BaseNode
	Text string
}

func (n *Text) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WritePrintString(html.EscapeString(n.Text))
}

func (n *Text) CollectVarRefs(_ map[string]bool) []string { return []string{} }
