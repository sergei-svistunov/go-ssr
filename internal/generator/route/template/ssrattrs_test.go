package template

import (
	"errors"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// An <ssr:form> with ssr:if, ssr:else-if or ssr:else is a branch of a condition, as any
// element with the attribute is, and the attribute is not one of the form's.
func TestFormIsABranch(t *testing.T) {
	for _, c := range []struct {
		src    string
		branch func(*node.SsrCondition) node.Node
	}{
		{`<ssr:form name="f" ssr:if="a" class="c"></ssr:form>`, func(c *node.SsrCondition) node.Node { return c.Conditions[0].Body }},
		{`<p ssr:if="a">x</p> <ssr:form name="f" ssr:else-if="b" class="c"></ssr:form>`, func(c *node.SsrCondition) node.Node { return c.Conditions[1].Body }},
		{`<p ssr:if="a">x</p> <ssr:form name="f" ssr:else class="c"></ssr:form>`, func(c *node.SsrCondition) node.Node { return c.ElseBody }},
	} {
		tpl, err := parseSource(t, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		cond, ok := tpl.nodes[0].(*node.SsrCondition)
		if len(tpl.nodes) != 1 || !ok {
			t.Fatalf("%s: nodes %#v, want one condition", c.src, tpl.nodes)
		}
		form, ok := c.branch(cond).(*node.SsrForm)
		if !ok {
			t.Fatalf("%s: the branch is %#v, want the form", c.src, c.branch(cond))
		}
		if len(form.Attributes) != 2 || form.Attributes[0].Key != "name" || form.Attributes[1].Key != "class" {
			t.Errorf("%s: form attributes %v, want name and class", c.src, form.Attributes)
		}
	}

	tpl, err := parseSource(t, `<ssr:form name="f" ssr:if="a"></ssr:form> <p ssr:else>x</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if cond, ok := tpl.nodes[0].(*node.SsrCondition); len(tpl.nodes) != 1 || !ok || cond.ElseBody == nil {
		t.Errorf("an element with ssr:else after a form with ssr:if: nodes %#v, want one condition with an else", tpl.nodes)
	}
}

// Every GoSSR tag refuses, at its line, an ssr: attribute the generator does not handle on
// it, rather than writing it into the page or dropping it.
func TestSsrTagsRefuseUnknownSsrAttributes(t *testing.T) {
	for _, src := range []string{
		`<ssr:form name="f" ssr:for="x in xs"></ssr:form>`,
		`<ssr:form name="f" ssr:x="1"></ssr:form>`,
		`<ssr:form name="f" ssr:bind="v"></ssr:form>`,
		`<ssr:form name="f" ssr:if="a" ssr:else></ssr:form>`,
		`<p ssr:if="a">x</p><ssr:form name="f" ssr:else-if="b" ssr:else></ssr:form>`,
		`<ssr:form name="f"><ssr:input name="a" ssr:if="b"/></ssr:form>`,
		`<ssr:form name="f"><ssr:select name="a" ssr:for="x in xs"/></ssr:form>`,
		`<ssr:form name="f"><ssr:textarea name="a" ssr:else/></ssr:form>`,
		`<ssr:var name="a" type="int" ssr:if="b"/>`,
		`<ssr:content ssr:if="b"/>`,
		`<ssr:assets ssr:if="b"/>`,
	} {
		_, err := parseSource(t, "<div>\n\n"+src+"\n</div>")
		if line := syntaxLine(err); line != 3 || !strings.Contains(err.Error(), "ssr:") {
			t.Errorf("%s: %v, want a syntax error on line 3 naming the attribute", src, err)
		}
	}

	_, err := parseSource(t, "<div>\n\n<ssr:call name=\"c\" in=\"struct{}\" out=\"struct{}\" ssr:if=\"b\"/>\n</div>")
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != "E19" || coded.Line != 3 {
		t.Errorf("<ssr:call ssr:if>: %v, want E19 on line 3", err)
	}
}

// A live update renders a block again from the route's data alone, so an <ssr:form> in a
// block that reads a live variable is refused at the form's line; a condition that reads no
// live variable is not a block.
func TestFormInLiveBlockRefused(t *testing.T) {
	const live = `<ssr:var name="on" type="bool" reactive="true"/><ssr:var name="xs" type="[]int" reactive="true"/>`
	for _, src := range []string{
		"<ssr:form name=\"f\" ssr:if=\"on\"></ssr:form>",
		"<div ssr:if=\"on\">\n<ssr:form name=\"f\"></ssr:form></div>",
		"<p ssr:if=\"!on\">x</p>\n<ssr:form name=\"f\" ssr:else></ssr:form>",
		"<ul ssr:for=\"x in xs\">\n<li><ssr:form name=\"f\"></ssr:form></li></ul>",
		"<div class=\"{{ on ? 'a' : 'b' }}\">\n<ssr:form name=\"f\"></ssr:form></div>",
		"<p ssr:if=\"true\">{{ on }}</p>\n<ssr:form name=\"f\" ssr:else></ssr:form>",
	} {
		_, err := parseSource(t, "<div>\n\n"+src+"\n</div>\n"+live)
		if line := strings.Count(src, "\n") + 3; syntaxLine(err) != line || !strings.Contains(err.Error(), `<ssr:form name="f">`) {
			t.Errorf("%s: %v, want a syntax error on line %d naming the form", src, err, line)
		}
	}

	for _, src := range []string{
		live + `<ssr:form name="f" ssr:if="show"></ssr:form>`,
		live + `<div ssr:if="show"><ssr:form name="f"></ssr:form></div><p>{{ on }}</p>`,
		`<ssr:var name="on" type="bool"/><ssr:form name="f" ssr:if="on"></ssr:form>`,
	} {
		if _, err := parseSource(t, src); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// An element takes one of ssr:if, ssr:else-if, ssr:else and ssr:for: two of them on one
// element are refused at its line, since they would add it to the page twice.
func TestElementTakesOneConditionOrLoop(t *testing.T) {
	for _, src := range []string{
		`<p ssr:if="a" ssr:for="x in xs">x</p>`,
		`<p ssr:for="x in xs" ssr:if="a">x</p>`,
		`<p ssr:if="a" ssr:else>x</p>`,
		`<p ssr:if="a">x</p><p ssr:else-if="b" ssr:for="x in xs">x</p>`,
		`<p ssr:if="a">x</p><p ssr:else ssr:for="x in xs">x</p>`,
		`<p ssr:if="a">x</p><p ssr:else-if="b" ssr:else>x</p>`,
	} {
		_, err := parseSource(t, "<div>\n\n"+src+"\n</div>")
		if syntaxLine(err) != 3 || !strings.Contains(err.Error(), "<p> has both ssr:") {
			t.Errorf("%s: %v, want a syntax error on line 3 naming both attributes", src, err)
		}
	}
}
