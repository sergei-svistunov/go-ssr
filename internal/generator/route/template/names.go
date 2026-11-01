package template

import (
	"go/token"
	"regexp"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// goPredeclared are the identifiers Go declares in its universe scope.
var goPredeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "rune": true, "string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true,
	"true": true, "false": true, "iota": true, "nil": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true, "delete": true,
	"imag": true, "len": true, "make": true, "max": true, "min": true, "new": true, "panic": true,
	"print": true, "println": true, "real": true, "recover": true,
}

// generatedNames are the names the generated code reads where a template's names are in
// scope: form, input and textarea, which a template reads inside <ssr:form>, and the
// packages mux and render. The generated code's other names there start with "_".
var generatedNames = map[string]bool{"form": true, "input": true, "textarea": true, "mux": true, "render": true}

// reName matches the names the template's expressions can write.
var reName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// checkIdent refuses a name of a form or a form field that is not one the generated code can
// turn into a Go identifier. what describes where the name stands, as in "<ssr:form name>".
func checkIdent(file string, line int, what, name string) error {
	if !reName.MatchString(name) {
		return syntaxErrf(file, line, "%s %q is not a name; use letters, digits and _, starting with a letter", what, name)
	}
	return nil
}

// checkDeclared refuses a name a template declares, with <ssr:var> or ssr:for, that would
// collide with a name of Go or of the generated code where the template's names are in scope.
func checkDeclared(file string, line int, what, name string) error {
	switch {
	case strings.HasPrefix(name, "_"):
		return syntaxErrf(file, line, "%s %q starts with _, which the generated code keeps for its own names; pick another name", what, name)
	case !reName.MatchString(name):
		return syntaxErrf(file, line, "%s %q is not a name; use letters, digits and _, starting with a letter", what, name)
	case token.IsKeyword(name):
		return syntaxErrf(file, line, "%s %q is a Go keyword; pick another name", what, name)
	case strings.EqualFold(name, "in"):
		return syntaxErrf(file, line, "%s %q is the word ssr:for puts before its collection; pick another name", what, name)
	case goPredeclared[name]:
		return syntaxErrf(file, line, "%s %q is predeclared in Go; pick another name", what, name)
	case generatedNames[name]:
		return syntaxErrf(file, line, "%s %q is a name the generated code uses; pick another name", what, name)
	}
	return nil
}

// checkExprNames refuses the names in nodes, parsed from one attribute value or text, that
// cannot compile: a value or field named by a Go keyword, and a value whose name starts with
// "_", which the generated code keeps for its own names.
func checkExprNames(nodes []node.Node) error {
	for _, n := range nodes {
		var err error
		switch v := n.(type) {
		case *node.Variable:
			switch {
			case strings.HasPrefix(v.Name, "_"):
				err = syntaxErrf(v.File, v.Line, "%q starts with _, which the generated code keeps for its own names; the template cannot read it", v.Name)
			case token.IsKeyword(v.Name):
				err = syntaxErrf(v.File, v.Line, "%q is a Go keyword; it cannot name a value", v.Name)
			}
		case *node.StructField:
			if token.IsKeyword(v.FieldName) {
				err = syntaxErrf(v.File, v.Line, "%q is a Go keyword; it cannot name a field or method", v.FieldName)
			}
		}
		if err != nil {
			return err
		}
		if err := checkExprNames(subNodes(n)); err != nil {
			return err
		}
	}
	return nil
}

// reLoopNames matches the names that every ssr:for expression the grammar reads declares.
var reLoopNames = regexp.MustCompile(`^\s*(\w+)\s*(?:,\s*(\w+)\s*)?(?i:in)\s`)

// checkLoopNames refuses the names an ssr:for expression declares that checkDeclared refuses,
// and one name for both the index and the item. It reads them before the grammar does, which
// would refuse "in" without naming it.
func checkLoopNames(file string, line int, expr string) error {
	m := reLoopNames.FindStringSubmatch(expr)
	if m == nil {
		return nil
	}
	item, index := m[1], ""
	if m[2] != "" {
		item, index = m[2], m[1]
	}
	if err := checkDeclared(file, line, "the ssr:for item", item); err != nil {
		return err
	}
	if index == "" || index == "_" {
		return nil
	}
	if err := checkDeclared(file, line, "the ssr:for index", index); err != nil {
		return err
	}
	if index == item {
		return syntaxErrf(file, line, "ssr:for names both its index and its item %q; pick another name for one of them", item)
	}
	return nil
}

// subNodes returns the nodes directly inside n: its children, the values of its attributes,
// and the operands of an expression.
func subNodes(n node.Node) []node.Node {
	attrs := func(as []node.HtmlAttribute) []node.Node {
		var out []node.Node
		for _, a := range as {
			out = append(out, a.Values...)
		}
		return out
	}
	switch v := n.(type) {
	case *node.Content:
		return v.Children
	case *node.HtmlElement:
		return append(attrs(v.Attributes), v.Children...)
	case *node.SsrForm:
		return append(attrs(v.Attributes), v.Children...)
	case *node.SsrInput:
		return attrs(v.Attributes)
	case *node.SsrSelect:
		return attrs(v.Attributes)
	case *node.SsrTextarea:
		return attrs(v.Attributes)
	case *node.SsrCondition:
		var out []node.Node
		for _, c := range v.Conditions {
			out = append(out, c.Condition, c.Body)
		}
		if v.ElseBody != nil {
			out = append(out, v.ElseBody)
		}
		return out
	case *node.Loop:
		return append([]node.Node{v.Array}, v.Children...)
	case *node.Expression:
		return []node.Node{v.Value}
	case *node.RawExpression:
		return []node.Node{v.Value}
	case *node.Operator:
		if v.Left == nil {
			return []node.Node{v.Right}
		}
		return []node.Node{v.Left, v.Right}
	case *node.Parentheses:
		return []node.Node{v.Value}
	case *node.StructField:
		return []node.Node{v.Expr}
	case *node.Indexed:
		return []node.Node{v.Expr, v.Index}
	case *node.Function:
		return []node.Node{v.Expr, v.Arguments}
	case *node.ExpressionsList:
		return v.Values
	case *node.TernaryIf:
		return []node.Node{v.Cond, v.T, v.F}
	}
	return nil
}

// ExportedName returns the name of the Go field, type or method the generated code derives
// from a template name.
func ExportedName(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
