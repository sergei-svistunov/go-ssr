package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrContent{}

type SsrContent struct {
	Default string
}

func (n *SsrContent) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn("if c.Child != nil {")
	buf.WriteStringLn("	err := c.Child.Write(w)")
	buf.WriteStringLn("	if err != nil {")
	buf.WriteStringLn("		return err")
	buf.WriteStringLn("	}")
	buf.WriteStringLn("}")
}
