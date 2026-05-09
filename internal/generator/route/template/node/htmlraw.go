package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &HtmlRaw{}

type HtmlRaw struct {
	BaseNode
	Data string
}

func (n *HtmlRaw) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WritePrintString(n.Data)
}

func (n *HtmlRaw) CollectVarRefs(_ map[string]bool) []string { return []string{} }
