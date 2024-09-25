package template

import "github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"

type NodesStack []*node.HtmlElement

func (s *NodesStack) Push(n *node.HtmlElement) {
	*s = append(*s, n)
}

func (s *NodesStack) Pop() {
	*s = (*s)[:len(*s)-1]
}

func (s *NodesStack) Top() *node.HtmlElement {
	if len(*s) == 0 {
		return nil
	}
	return (*s)[len(*s)-1]
}
