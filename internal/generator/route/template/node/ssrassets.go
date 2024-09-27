package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrAssets{}

type SsrAssets struct {
	BaseNode
}

func (n *SsrAssets) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("if err := c.WriteAssets(w, map[string]struct{}{}); err != nil {")
	buf.WriteStringLn("	return err")
	buf.WriteStringLn("}")
}
