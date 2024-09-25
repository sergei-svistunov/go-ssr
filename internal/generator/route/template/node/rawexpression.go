package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &RawExpression{}

type RawExpression struct {
	Value Node
}

func (n *RawExpression) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteString("if _, err := mux.WriteRaw(w,")
	n.Value.WriteGoCode(buf)
	buf.WriteStringLn("); err != nil {")
	buf.WriteStringLn("return err")
	buf.WriteStringLn("}")
}
