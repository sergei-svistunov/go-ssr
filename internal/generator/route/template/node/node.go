package node

import (
	"fmt"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

type Node interface {
	WriteGoCode(buf *gobuf.GoBuf)
	FilePos() string
}

type BaseNode struct {
	File string
	Line int
}

func (n *BaseNode) FilePos() string {
	return fmt.Sprintf("//line %s:%d", n.File, n.Line)
}
