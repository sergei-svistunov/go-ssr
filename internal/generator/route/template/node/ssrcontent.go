package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrContent{}

type SsrContent struct {
	BaseNode
	Default string
}

func (n *SsrContent) CollectVarRefs(_ map[string]bool) []string { return []string{} }

func (n *SsrContent) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("if err := c.WriteChild(w); err != nil {")
	buf.WriteStringLn("	return err")
	buf.WriteStringLn("}")
}
