package template

import (
	"fmt"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
)

func TestTextParse(t *testing.T) {
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

			if len(nodes) == 0 {
				t.Fatal("expected at least one node")
			}

			buf := gobuf.New()
			for _, node := range nodes {
				node.WriteGoCode(buf)
			}

			code := buf.String()
			if code == "" {
				t.Fatal("expected non-empty generated code")
			}
		})
	}
}

func TestTextParse_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"unclosed expression", "{{ a"},
		{"invalid operator", "{{ a @@ b }}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseText(tc.input, "test.html", 1, false)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestTextParse_PlainText(t *testing.T) {
	nodes, err := parseText("just plain text", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node for plain text, got %d", len(nodes))
	}
}

func TestTextParse_MultipleExpressions(t *testing.T) {
	nodes, err := parseText("{{a}} and {{b}}", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	// Should have: expr(a), text(" and "), expr(b) = 3 nodes minimum
	if len(nodes) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(nodes))
	}
}

func TestTextParse_RawExpression(t *testing.T) {
	nodes, err := parseText("{{$ raw }}", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for raw expression")
	}

	buf := gobuf.New()
	for _, node := range nodes {
		node.WriteGoCode(buf)
	}
	code := buf.String()
	if code == "" {
		t.Fatal("expected generated code for raw expression")
	}
}

func TestTextParse_TernaryOperator(t *testing.T) {
	nodes, err := parseText("{{ x ? 'a' : 'b' }}", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for ternary")
	}
}

func TestTextParse_StructFieldAccess(t *testing.T) {
	nodes, err := parseText("{{ obj.Field }}", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for field access")
	}
}

func TestTextParse_FunctionCall(t *testing.T) {
	nodes, err := parseText("{{ obj.Method(1, 'arg') }}", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for method call")
	}
}

func TestTextParse_IndexAccess(t *testing.T) {
	nodes, err := parseText("{{ arr[0] }}", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for index access")
	}
}

func TestTextParse_ForLoop(t *testing.T) {
	nodes, err := parseText("key, value in items", "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for for loop")
	}
}

func TestTextParse_SyntaxError_HasFileInfo(t *testing.T) {
	_, err := parseText("{{ a @@ b }}", "myfile.html", 5, false)
	if err == nil {
		t.Fatal("expected error")
	}
	syntaxErr, ok := err.(*SyntaxError)
	if !ok {
		t.Fatalf("expected *SyntaxError, got %T", err)
	}
	if syntaxErr.Filename != "myfile.html" {
		t.Fatalf("expected filename 'myfile.html', got %q", syntaxErr.Filename)
	}
}
