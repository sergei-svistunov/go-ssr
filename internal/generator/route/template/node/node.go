package node

import "github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"

type Node interface {
	WriteGoCode(buf *gobuf.GoBuf)
}
