package node

// SsrCall is an <ssr:call name="…" in="…" out="…"/> declaration: a function of
// the route the page runs over its live connection. It renders nothing.
type SsrCall struct {
	BaseNode
	Name string
	// In and Out are the Go types of the argument and the result.
	In, Out string
}
