package template

import (
	"fmt"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

func TestTextParse(t *testing.T) {
	//YyLexDebug = true

	for i, tc := range []struct {
		input      string
		insideExpr bool
	}{
		{input: "<h1>Info</h1>\n    {{ info }}"},
		{input: "Hello {{ name }}!!!\nWelcome to the {{place}}{{'123'}} {{45}}"},
		{input: "key, value in array", insideExpr: true},
		{input: "value in array", insideExpr: true},
		{input: "{{ (a == 10) && (b > 'abc') || (d - e * 5) != (!b) && c }}"},
		{input: "{{ a.Field }}"},
		{input: "{{ a.func(123, '345', b) }}"},
		{input: "{{$ '<h1>Raw html</h1>' }}"},
		{input: "{{$ a }} {{$ '<h1>Raw html</h1>' }}"},
		{input: "{{ a[10] }}"},
		{input: "{{ a > 4 ? 'yes' : 'no' }}"},
	} {
		t.Run(fmt.Sprintf("Testcase #%d", i), func(t *testing.T) {
			nodes, err := parseText(tc.input, "", 0, tc.insideExpr)
			if err != nil {
				t.Fatal(err)
			}

			t.Logf("%#v", nodes)

			buf := gobuf.New()
			for _, node := range nodes {
				node.WriteGoCode(buf)
			}

			t.Log(buf.String())
		})
	}
}
