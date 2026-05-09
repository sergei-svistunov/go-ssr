package node

import (
	"fmt"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

// UnionRefs merges multiple CollectVarRefs results, deduplicating by name.
// It is exported so that callers outside this package (e.g. template analysis
// passes in the generator) can reuse the same deduplication logic.
func UnionRefs(sets ...[]string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range sets {
		for _, name := range s {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	if result == nil {
		return []string{}
	}
	return result
}

type Node interface {
	WriteGoCode(buf *gobuf.GoBuf)
	FilePos() string
	// CollectVarRefs returns the subset of reactive variable names referenced
	// within this node's subtree. The reactive parameter is the set of all
	// variable names declared with reactive="true" in the template.
	// Implementations must never return nil; return an empty slice instead.
	CollectVarRefs(reactive map[string]bool) []string
}

type WithChildren interface {
	Node
	AddChildren(children ...Node)
	LastChild() Node
	PopChild()
}

type BaseNode struct {
	File string
	Line int
}

func (n *BaseNode) FilePos() string {
	return fmt.Sprintf("//line %s:%d", n.File, n.Line)
}
