package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrCondition{}

type SsrConditionData struct {
	BaseNode
	Condition Node
	Body      Node
}

type SsrCondition struct {
	BaseNode
	Conditions []SsrConditionData
	ElseBody   Node
}

func (n *SsrCondition) WriteGoCode(buf *gobuf.GoBuf) {
	buf.WriteStringLn(n.FilePos())
	for i, c := range n.Conditions {
		if i > 0 {
			buf.WriteStringLn(c.FilePos())
			buf.WriteString("} else ")
		}
		buf.WriteString("if ")
		c.Condition.WriteGoCode(buf)
		buf.WriteStringLn(" {")
		c.Body.WriteGoCode(buf)
	}

	if n.ElseBody != nil {
		buf.WriteStringLn(n.ElseBody.FilePos())
		buf.WriteStringLn("} else {")
		n.ElseBody.WriteGoCode(buf)
	}

	buf.WriteString("}\n")
}
