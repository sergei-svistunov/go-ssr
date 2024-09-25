package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &StructField{}

type StructField struct {
	Expr      Node
	FieldName string
}

func (n *StructField) WriteGoCode(buf *gobuf.GoBuf) {
	n.Expr.WriteGoCode(buf)
	buf.WriteString(".")
	buf.WriteString(n.FieldName)
}
