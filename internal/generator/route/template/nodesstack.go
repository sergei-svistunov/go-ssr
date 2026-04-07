package template

import (
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/htmlutils"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

type NodesStack []node.WithChildren

func (s *NodesStack) Push(n node.WithChildren) {
	*s = append(*s, n)
}

func (s *NodesStack) Pop() {
	*s = (*s)[:len(*s)-1]
}

func (s *NodesStack) Top() node.WithChildren {
	if len(*s) == 0 {
		return nil
	}
	return (*s)[len(*s)-1]
}

func (s NodesStack) isWhitespacePreserved() bool {
	for _, n := range s {
		if el, ok := n.(*node.HtmlElement); ok {
			if htmlutils.LiteralElements[el.TagName] || htmlutils.PreserveWhitespaceElements[el.TagName] {
				return true
			}
		}
	}
	return false
}
