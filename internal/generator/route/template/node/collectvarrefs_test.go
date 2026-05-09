package node_test

import (
	"sort"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// reactive is the test set of reactive variable names.
var testReactive = map[string]bool{
	"counter": true,
	"name":    true,
}

func sortedRefs(refs []string) []string {
	out := make([]string, len(refs))
	copy(out, refs)
	sort.Strings(out)
	return out
}

// ---- Variable ----

func TestCollectVarRefs_Variable_Reactive(t *testing.T) {
	n := &node.Variable{Name: "counter"}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

func TestCollectVarRefs_Variable_NonReactive(t *testing.T) {
	n := &node.Variable{Name: "other"}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 0 {
		t.Errorf("want [], got %v", refs)
	}
}

// ---- String, Number, Text, HtmlRaw ----

func TestCollectVarRefs_Literals(t *testing.T) {
	cases := []node.Node{
		&node.String{Text: `"hello"`},
		&node.Number{Text: "42"},
		&node.Text{Text: "hello world"},
		&node.HtmlRaw{Data: "<!DOCTYPE html>"},
	}
	for _, n := range cases {
		if refs := n.CollectVarRefs(testReactive); len(refs) != 0 {
			t.Errorf("%T: want [], got %v", n, refs)
		}
	}
}

// ---- Operator ----

func TestCollectVarRefs_Operator_BothChildren(t *testing.T) {
	n := &node.Operator{
		Op:    "+",
		Left:  &node.Variable{Name: "counter"},
		Right: &node.Variable{Name: "name"},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 || refs[0] != "counter" || refs[1] != "name" {
		t.Errorf("want [counter name], got %v", refs)
	}
}

func TestCollectVarRefs_Operator_NilLeft(t *testing.T) {
	n := &node.Operator{
		Op:    "!",
		Right: &node.Variable{Name: "counter"},
	}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

// ---- Parentheses ----

func TestCollectVarRefs_Parentheses(t *testing.T) {
	n := &node.Parentheses{Value: &node.Variable{Name: "counter"}}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

// ---- StructField ----

func TestCollectVarRefs_StructField(t *testing.T) {
	// struct field access passes through to base expression
	n := &node.StructField{
		Expr:      &node.Variable{Name: "counter"},
		FieldName: "Value",
	}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

func TestCollectVarRefs_StructField_NonReactive(t *testing.T) {
	n := &node.StructField{
		Expr:      &node.Variable{Name: "user"},
		FieldName: "Name",
	}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 0 {
		t.Errorf("want [], got %v", refs)
	}
}

// ---- Indexed ----

func TestCollectVarRefs_Indexed(t *testing.T) {
	n := &node.Indexed{
		Expr:  &node.Variable{Name: "counter"},
		Index: &node.Variable{Name: "name"},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- Function ----

func TestCollectVarRefs_Function(t *testing.T) {
	n := &node.Function{
		Expr: &node.Variable{Name: "counter"},
		Arguments: &node.ExpressionsList{
			Values: []node.Node{&node.Variable{Name: "name"}},
		},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- TernaryIf ----

func TestCollectVarRefs_TernaryIf(t *testing.T) {
	n := &node.TernaryIf{
		Cond: &node.Variable{Name: "counter"},
		T:    &node.String{Text: `"yes"`},
		F:    &node.Variable{Name: "name"},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- Loop ----

func TestCollectVarRefs_Loop_ArrayAndBody(t *testing.T) {
	// Loop now collects from both the array expression AND the body children
	// Loop body reactivity is supported; both array and body refs are collected.
	n := &node.Loop{
		Array:    &node.Variable{Name: "counter"},
		Variable: "item",
		Children: []node.Node{&node.Variable{Name: "name"}},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	// Should contain both "counter" (from array) AND "name" (from body).
	if len(refs) != 2 || refs[0] != "counter" || refs[1] != "name" {
		t.Errorf("want [counter name], got %v", refs)
	}
}

func TestCollectVarRefs_Loop_ArrayOnly(t *testing.T) {
	// When the body has no reactive refs, only the array ref is returned.
	n := &node.Loop{
		Array:    &node.Variable{Name: "counter"},
		Variable: "item",
		Children: []node.Node{&node.Text{Text: "static"}},
	}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

func TestCollectVarRefs_Loop_BodyOnly(t *testing.T) {
	// When the array has no reactive refs but the body does.
	n := &node.Loop{
		Array:    &node.Variable{Name: "nonReactive"},
		Variable: "item",
		Children: []node.Node{&node.Variable{Name: "counter"}},
	}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

// ---- ExpressionsList ----

func TestCollectVarRefs_ExpressionsList(t *testing.T) {
	n := &node.ExpressionsList{
		Values: []node.Node{
			&node.Variable{Name: "counter"},
			&node.Number{Text: "42"},
			&node.Variable{Name: "name"},
		},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- Expression and RawExpression ----

func TestCollectVarRefs_Expression(t *testing.T) {
	n := &node.Expression{Value: &node.Variable{Name: "counter"}}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter], got %v", refs)
	}
}

func TestCollectVarRefs_RawExpression(t *testing.T) {
	n := &node.RawExpression{Value: &node.Variable{Name: "name"}}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "name" {
		t.Errorf("want [name], got %v", refs)
	}
}

// ---- Content ----

func TestCollectVarRefs_Content(t *testing.T) {
	n := &node.Content{
		Children: []node.Node{
			&node.Variable{Name: "counter"},
			&node.Text{Text: "static"},
			&node.Variable{Name: "name"},
		},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- HtmlElement ----

func TestCollectVarRefs_HtmlElement(t *testing.T) {
	n := &node.HtmlElement{
		TagName: "div",
		Attributes: []node.HtmlAttribute{
			{
				Key:    "class",
				Values: []node.Node{&node.Variable{Name: "counter"}},
			},
		},
		Children: []node.Node{&node.Variable{Name: "name"}},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- SsrCondition ----

func TestCollectVarRefs_SsrCondition(t *testing.T) {
	n := &node.SsrCondition{
		Conditions: []node.SsrConditionData{
			{
				Condition: &node.Variable{Name: "counter"},
				Body:      &node.Text{Text: "visible"},
			},
		},
		ElseBody: &node.Variable{Name: "name"},
	}
	refs := sortedRefs(n.CollectVarRefs(testReactive))
	if len(refs) != 2 {
		t.Errorf("want [counter name], got %v", refs)
	}
}

// ---- No-op node types ----

func TestCollectVarRefs_SsrNoops(t *testing.T) {
	noops := []node.Node{
		&node.SsrContent{},
		&node.SsrAssets{},
		&node.SsrForm{},
		&node.SsrInput{},
		&node.SsrSelect{},
		&node.SsrTextarea{},
	}
	for _, n := range noops {
		if refs := n.CollectVarRefs(testReactive); len(refs) != 0 {
			t.Errorf("%T: want [], got %v", n, refs)
		}
	}
}

// ---- Deduplication ----

func TestCollectVarRefs_Deduplication(t *testing.T) {
	// Two separate branches both reference "counter" — should appear once.
	n := &node.Operator{
		Op:    "+",
		Left:  &node.Variable{Name: "counter"},
		Right: &node.Variable{Name: "counter"},
	}
	refs := n.CollectVarRefs(testReactive)
	if len(refs) != 1 || refs[0] != "counter" {
		t.Errorf("want [counter] (deduplicated), got %v", refs)
	}
}
