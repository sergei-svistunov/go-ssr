package node

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

var _ Node = &SsrCondition{}

type SsrConditionData struct {
	Condition Node
	Body      Node
}

type SsrCondition struct {
	Conditions []SsrConditionData
	ElseBody   Node
}

func (n *SsrCondition) WriteGoCode(buf *gobuf.GoBuf) {
	for i, c := range n.Conditions {
		if i > 0 {
			buf.WriteString(" else ")
		}
		buf.WriteString("if ")
		c.Condition.WriteGoCode(buf)
		buf.WriteStringLn(" {")
		c.Body.WriteGoCode(buf)
		buf.WriteString("}")
	}

	if n.ElseBody != nil {
		buf.WriteStringLn(" else {")
		n.ElseBody.WriteGoCode(buf)
		buf.WriteString("}")
	}

	buf.WriteString("\n")
}
