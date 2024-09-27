package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Expression{}

type Expression struct {
	BaseNode
	Value Node
}

func (n *Expression) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	buf.WriteString("if _, err := mux.WriteHtmlEscaped(w,")
	n.Value.WriteGoCode(buf)
	buf.WriteStringLn("); err != nil {")
	buf.WriteStringLn("return err")
	buf.WriteStringLn("}")
}
