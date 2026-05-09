package reactive_test

// bench_test.go — performance smoke tests for reactive bindings (RB-PERF-LOOP-1000).
//
// RB-PERF-LOOP-1000: Server-side render time for a loop with 1000 reactive items
// must be below 10 ms per renderBlock call.
//
// This benchmark simulates what the generated writeBlock_KEY / renderBlock_KEY
// functions do for a reactive ssr:for with 1000 items. The generated code writes
// each iteration into a strings.Builder using reactive.RenderValue for each item,
// then returns b.String(). We replicate that pattern here to measure the server-side
// render cost in isolation (no WS round-trip, no alloc for the Conn itself).

import (
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/pkg/reactive"
)

// writeBlock_loop1000 simulates the generated writeBlock_KEY for a reactive ssr:for
// with 1000 string items. Each iteration renders <li>ITEM</li> using RenderValue.
// This mirrors exactly what the generated code emits for:
//
//	<ssr:for var="item" in="items"><li>{{ item }}</li></ssr:for>
//
// where items is a []string of length 1000.
func writeBlock_loop1000(items []string, w *strings.Builder) {
	for _, item := range items {
		w.WriteString("<li>")
		w.WriteString(reactive.RenderValue(item))
		w.WriteString("</li>")
	}
}

// renderBlock_loop1000 is the string-returning wrapper (mirrors generated renderBlock_KEY).
func renderBlock_loop1000(items []string) string {
	var b strings.Builder
	writeBlock_loop1000(items, &b)
	return b.String()
}

// BenchmarkReactiveLoop1000 measures the server-side render cost for a reactive
// loop block with 1000 items.
//
// Budget (RB-PERF-LOOP-1000): single renderBlock call must complete in < 10 ms
// (10,000,000 ns). This benchmark uses -benchtime=5s for stable measurements.
func BenchmarkReactiveLoop1000(b *testing.B) {
	// Build a 1000-item slice of strings, each a short HTML-safe value.
	items := make([]string, 1000)
	for i := range items {
		items[i] = "item-" + reactive.RenderValue(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = renderBlock_loop1000(items)
	}
}
