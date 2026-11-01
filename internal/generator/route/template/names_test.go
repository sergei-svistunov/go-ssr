package template

import (
	"strconv"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// A name a template declares as a value, a loop item or a loop index is refused at its line,
// with the name and what to do, when Go or the generated code has it where the template's
// names are in scope.
func TestDeclaredNamesRefused(t *testing.T) {
	for _, name := range []string{"_", "_w", "_c", "__x", "type", "func", "range", "in", "In", "nil", "true", "len", "string", "any", "error", "form", "input", "textarea", "mux", "render"} {
		for _, src := range []string{
			`<ssr:var name="` + name + `" type="int"/>`,
			`<p ssr:for="` + name + ` in xs">x</p>`,
			`<p ssr:for="` + name + `, x in xs">x</p>`,
		} {
			if name == "_" && strings.Contains(src, ", x") {
				continue
			}
			_, err := parseSource(t, "<ssr:var name=\"xs\" type=\"[]int\"/>\n\n"+src)
			if syntaxLine(err) != 3 || !strings.Contains(err.Error(), strconv.Quote(name)) || !strings.Contains(err.Error(), "pick another name") {
				t.Errorf("%s: %v, want a syntax error on line 3 that names %q and asks for another name", src, err, name)
			}
		}
	}

	for _, src := range []string{
		`<ssr:var name="a b" type="int"/>`,
		`<ssr:var name="1a" type="int"/>`,
		`<ssr:var name="a-b" type="int"/>`,
		`<ssr:var name="x" type="int"/><ssr:var name="x" type="int"/>`,
		`<ssr:var name="x" type="int"/><ssr:var name="X" type="int"/>`,
		`<p ssr:for="i, i in xs">x</p>`,
	} {
		if _, err := parseSource(t, "<ssr:var name=\"xs\" type=\"[]int\"/>\n\n"+src); syntaxLine(err) != 3 {
			t.Errorf("%s: %v, want a syntax error on line 3", src, err)
		}
	}
}

// The names the generated code no longer keeps for itself, and a blank loop index, are a
// template's to declare.
func TestDeclaredNamesAccepted(t *testing.T) {
	for _, src := range []string{
		`<ssr:var name="w" type="int"/><ssr:var name="c" type="int"/><ssr:var name="data" type="int"/><ssr:var name="err" type="int"/>`,
		`<ssr:var name="Form" type="int"/><ssr:var name="inputs" type="int"/><ssr:var name="index" type="int"/><ssr:var name="x_" type="int"/>`,
		`<p ssr:for="w in xs">{{ w }}</p><p ssr:for="c, data in xs">{{ c }}{{ data }}</p><p ssr:for="_, x in xs">{{ x }}</p>`,
		`<p ssr:for="x IN xs">{{ x }}</p>`,
	} {
		if _, err := parseSource(t, `<ssr:var name="xs" type="[]int"/>`+src); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// An expression cannot read a name that starts with "_", which the generated code keeps for
// its own, nor name a value or a field with a Go keyword.
func TestExpressionNamesRefused(t *testing.T) {
	for _, src := range []string{
		`<p>{{ _w }}</p>`,
		`<p>{{ _ }}</p>`,
		`<p class="{{ _c.Frame }}">x</p>`,
		`<p ssr:if="_data != nil">x</p>`,
		`<p ssr:for="x in _xs">x</p>`,
		`<p>{{ type }}</p>`,
		`<p>{{ a.func() }}</p>`,
		`<p>{{ f(a, range) }}</p>`,
	} {
		_, err := parseSource(t, "<div>\n\n"+src+"\n</div>")
		if syntaxLine(err) != 3 {
			t.Errorf("%s: %v, want a syntax error on line 3", src, err)
		}
	}
}

// The names of forms and their fields become Go names of the generated code, so each must be
// a name, and two that become the same Go name are refused.
func TestFormNamesRefused(t *testing.T) {
	for _, src := range []string{
		`<ssr:form name="a-b"></ssr:form>`,
		`<ssr:form></ssr:form>`,
		`<ssr:form name="_f"></ssr:form>`,
		"<ssr:form name=\"f\"></ssr:form>\n<ssr:form name=\"F\"></ssr:form>",
		"<ssr:form name=\"f\"></ssr:form>\n<ssr:form name=\"f\"></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"a&quot;b\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:textarea name=\"a b\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:select name=\"a.b\"/></ssr:form>",
		"<ssr:form name=\"f\"><ssr:input name=\"a_b\"/>\n<ssr:input name=\"A_b\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"baseFormValues\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"setElements\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"markValidated\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"isValidated\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"getCSRFToken\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"setError\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"getError\"/></ssr:form>",
		"<ssr:form name=\"f\">\n<ssr:input name=\"hasError\"/></ssr:form>",
	} {
		_, err := parseSource(t, "<div>\n\n"+src+"\n</div>")
		if line := strings.Count(src, "\n") + 3; syntaxLine(err) != line {
			t.Errorf("%s: %v, want a syntax error on line %d", src, err, line)
		}
	}

	for _, src := range []string{
		`<ssr:form name="f"><ssr:input name="a" type="radio" value="1"/><ssr:input name="a" type="radio" value="2"/></ssr:form><ssr:form name="g"></ssr:form>`,
		`<ssr:form name="type"><ssr:input name="func"/><ssr:input name="nil"/></ssr:form>`,
	} {
		if _, err := parseSource(t, src); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// A live update renders a block again from the route's data alone, so a block that holds the
// child page or the page's assets is refused, and so, inside a form, is a live site that reads
// the form or holds a field, and a form or field attribute that reads a live variable. A live
// site that does not read the form is live inside a form as anywhere else.
func TestLiveBlocksRefused(t *testing.T) {
	const live = `<ssr:var name="on" type="bool" reactive="true"/><ssr:var name="xs" type="[]int"/>`
	for _, c := range []struct {
		src  string
		line int
		want string
	}{
		{"<div ssr:if=\"on\">\n<ssr:content/></div>", 4, "<ssr:content/> is in a block that the live variable on renders again"},
		{"<p ssr:for=\"x in xs\" class=\"{{ on }}\">\n<ssr:assets/></p>", 4, "<ssr:assets/> is in a block that the live variable on renders again"},
		{"<ssr:form name=\"f\">\n<p>{{ on ? form.GetError() : '' }}</p></ssr:form>", 4, `"form" reads the form`},
		{"<ssr:form name=\"f\">\n<div class=\"{{ on }}\"><ssr:input name=\"a\"/></div></ssr:form>", 4, `<ssr:input name="a"> reads the form`},
		{"<ssr:form name=\"f\"><div ssr:if=\"on\">\n<p>{{ form.Name.GetError() }}</p></div></ssr:form>", 4, `"form" reads the form`},
		{"<ssr:form name=\"f\"><p ssr:for=\"x in xs\">{{ on }}\n<ssr:textarea name=\"t\"/></p></ssr:form>", 4, `<ssr:textarea name="t"> reads the form`},
		{"<ssr:form name=\"f\"><p ssr:if=\"on\">\n<ssr:select name=\"s\"/></p></ssr:form>", 4, `<ssr:select name="s"> reads the form`},
		{"<ssr:form name=\"f\"><p ssr:if=\"on && input.HasError()\">x</p>\n</ssr:form>", 3, `"input" reads the form`},
		{"<ssr:form name=\"f\" class=\"{{ on }}\">\n</ssr:form>", 3, `<ssr:form name="f"> reads the live variable on in an attribute`},
		{"<ssr:form name=\"f\">\n<ssr:input name=\"a\" class=\"{{ on ? 'x' : '' }}\"/></ssr:form>", 4, `<ssr:input name="a"> reads the live variable on in an attribute`},
	} {
		_, err := parseSource(t, "<div>\n\n"+c.src+"\n</div>\n"+live)
		if syntaxLine(err) != c.line || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a syntax error on line %d saying %s", c.src, err, c.line, c.want)
		}
	}

	for _, src := range []string{
		`<ssr:form name="f"><p>{{ on }}</p><div class="{{ on }}">{{ on ? 'a' : 'b' }}</div><p ssr:if="on">x</p><p ssr:for="x in xs">{{ on }}</p></ssr:form>`,
		`<ssr:form name="f"><p ssr:if="form.IsValidated()">{{ form.GetError() }}</p><ssr:input name="a" class="{{ input.HasError() ? 'x' : '' }}"/></ssr:form>`,
		`<div ssr:if="on"><p>x</p></div><ssr:content/><ssr:assets/>`,
	} {
		if _, err := parseSource(t, live+src); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}

	tpl, err := parseSource(t, live+`<ssr:form name="f"><p>{{ on }}</p></ssr:form>`)
	if err != nil {
		t.Fatal(err)
	}
	if b := tpl.AnnotateBindings(map[string]bool{"on": true}, func(refs []string, _ string) string { return refs[0] }, "r"); len(b) != 1 || b["r.on"] == nil {
		t.Errorf("the bindings of a live value in a form are %v, want r.on", b)
	}
	if n := tpl.CollectReactiveNodes()["r.on"]; n == nil {
		t.Error("the live value in a form is not collected as a live site")
	} else if _, ok := n.(*node.Expression); !ok {
		t.Errorf("the live site in a form is %T, want the expression", n)
	}
}
