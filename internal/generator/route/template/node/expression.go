package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &Expression{}

type Expression struct {
	Value Node
}

func (n *Expression) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString("if _, err := mux.WriteHtmlEscaped(w,")
	n.Value.WriteGoCode(buf)
	buf.WriteStringLn("); err != nil {")
	buf.WriteStringLn("return err")
	buf.WriteStringLn("}")
}
