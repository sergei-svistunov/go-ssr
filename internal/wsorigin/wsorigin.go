// Package wsorigin marks a request whose Origin the mux has already checked,
// so the WebSocket upgrade does not check it again under narrower rules. It
// is internal so that only go-ssr can set the mark.
package wsorigin

import "context"

type key struct{}

// Mark returns ctx marked as belonging to a request whose Origin the caller
// has already checked against its own rules.
func Mark(ctx context.Context) context.Context {
	return context.WithValue(ctx, key{}, true)
}

// Checked reports whether ctx carries the mark.
func Checked(ctx context.Context) bool {
	checked, _ := ctx.Value(key{}).(bool)
	return checked
}
