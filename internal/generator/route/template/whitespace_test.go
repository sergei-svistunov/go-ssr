package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

func parseTemplate(t *testing.T, html string) *Template {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
	tpl, err := Parse(path, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	if tpl == nil {
		t.Fatal("template is nil")
	}
	return tpl
}

// collectTexts recursively collects all Text node values from the tree.
func collectTexts(nodes []node.Node) []string {
	var texts []string
	for _, n := range nodes {
		switch v := n.(type) {
		case *node.Text:
			texts = append(texts, v.Text)
		case *node.HtmlElement:
			texts = append(texts, collectTexts(v.Children)...)
		case *node.SsrCondition:
			for _, cond := range v.Conditions {
				if cond.Body != nil {
					if el, ok := cond.Body.(*node.HtmlElement); ok {
						texts = append(texts, collectTexts(el.Children)...)
					}
				}
			}
		case *node.Loop:
			texts = append(texts, collectTexts(v.Children)...)
		case *node.Content:
			texts = append(texts, collectTexts(v.Children)...)
		}
	}
	return texts
}

func TestWhitespace_IndentationRemoved(t *testing.T) {
	tpl := parseTemplate(t, `<div>
    <p>
        Hello
    </p>
</div>`)

	texts := collectTexts(tpl.nodes)
	for _, text := range texts {
		if strings.Contains(text, "\n") {
			t.Errorf("text node should not contain newline, got %q", text)
		}
		if strings.Contains(text, "    ") {
			t.Errorf("text node should not contain indentation, got %q", text)
		}
	}
}

func TestWhitespace_PreservedInPre(t *testing.T) {
	tpl := parseTemplate(t, `<pre>
    line 1
    line 2
</pre>`)

	texts := collectTexts(tpl.nodes)
	found := false
	for _, text := range texts {
		if strings.Contains(text, "line 1") && strings.Contains(text, "\n") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("whitespace inside <pre> should be preserved, got texts: %v", texts)
	}
}

func TestWhitespace_PreservedInTextarea(t *testing.T) {
	tpl := parseTemplate(t, `<textarea>
    some text
    more text
</textarea>`)

	texts := collectTexts(tpl.nodes)
	found := false
	for _, text := range texts {
		if strings.Contains(text, "some text") && strings.Contains(text, "\n") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("whitespace inside <textarea> should be preserved, got texts: %v", texts)
	}
}

func TestWhitespace_PreservedInScript(t *testing.T) {
	tpl := parseTemplate(t, `<script>
    const x = 1;
    if (x > 0) {}
</script>`)

	texts := collectTexts(tpl.nodes)
	found := false
	for _, text := range texts {
		if strings.Contains(text, "const x = 1;") && strings.Contains(text, "\n") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("whitespace inside <script> should be preserved, got texts: %v", texts)
	}
}

func TestWhitespace_PreservedInStyle(t *testing.T) {
	tpl := parseTemplate(t, `<style>
    .cls {
        color: red;
    }
</style>`)

	texts := collectTexts(tpl.nodes)
	found := false
	for _, text := range texts {
		if strings.Contains(text, ".cls") && strings.Contains(text, "\n") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("whitespace inside <style> should be preserved, got texts: %v", texts)
	}
}

func TestWhitespace_CollapsedInMixedContent(t *testing.T) {
	tpl := parseTemplate(t, `<p>
    Hello    World
</p>`)

	texts := collectTexts(tpl.nodes)
	found := false
	for _, text := range texts {
		if strings.Contains(text, "Hello World") {
			found = true
			if strings.Contains(text, "    ") {
				t.Error("multiple spaces should be collapsed to single space")
			}
			break
		}
	}
	if !found {
		t.Errorf("collapsed text should contain 'Hello World', got texts: %v", texts)
	}
}

func TestWhitespace_WhitespaceOnlyBetweenBlockElements(t *testing.T) {
	tpl := parseTemplate(t, `<div>
    <p>A</p>
    <p>B</p>
</div>`)

	texts := collectTexts(tpl.nodes)
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			t.Errorf("whitespace-only text node should have been removed, got %q", text)
		}
	}
}

func TestWhitespace_ExpressionPreservesAdjacentSpaces(t *testing.T) {
	tpl := parseTemplate(t, `<span>
    Hello {{ name }}!
</span>`)

	texts := collectTexts(tpl.nodes)
	foundHello := false
	for _, text := range texts {
		if strings.Contains(text, "Hello") {
			foundHello = true
			// Should be " Hello " — collapsed whitespace with content
			if strings.Contains(text, "\n") {
				t.Errorf("newlines should be collapsed, got %q", text)
			}
			break
		}
	}
	if !foundHello {
		t.Errorf("expected text containing 'Hello', got texts: %v", texts)
	}
}

func TestWhitespace_NestedPreInDiv(t *testing.T) {
	tpl := parseTemplate(t, `<div>
    <pre>
        preserved
    </pre>
</div>`)

	texts := collectTexts(tpl.nodes)
	foundPreserved := false
	for _, text := range texts {
		if strings.Contains(text, "preserved") {
			foundPreserved = true
			if !strings.Contains(text, "\n") {
				t.Error("whitespace inside nested <pre> should be preserved")
			}
			break
		}
	}
	if !foundPreserved {
		t.Errorf("expected text 'preserved' inside <pre>, got texts: %v", texts)
	}
}

func TestWhitespace_EmptyElementsNoExtraSpaces(t *testing.T) {
	tpl := parseTemplate(t, `<ul>
    <li>A</li>
    <li>B</li>
    <li>C</li>
</ul>`)

	texts := collectTexts(tpl.nodes)
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			t.Errorf("whitespace-only text between list items should be removed, got %q", text)
		}
	}
}

func TestWhitespace_DoctypeNotAffected(t *testing.T) {
	tpl := parseTemplate(t, `<!DOCTYPE html>
<html>
<head></head>
<body></body>
</html>`)

	// DOCTYPE creates an HtmlRaw node, not a Text node
	foundDoctype := false
	for _, n := range tpl.nodes {
		if raw, ok := n.(*node.HtmlRaw); ok {
			if strings.Contains(raw.Data, "DOCTYPE") {
				foundDoctype = true
				break
			}
		}
	}
	if !foundDoctype {
		t.Error("DOCTYPE should be preserved as HtmlRaw node")
	}
}

func TestWhitespace_ScriptWithExpression(t *testing.T) {
	tpl := parseTemplate(t, `<script>
    console.log("{{title}}")
</script>`)

	texts := collectTexts(tpl.nodes)
	foundConsole := false
	for _, text := range texts {
		if strings.Contains(text, "console.log") {
			foundConsole = true
			if !strings.Contains(text, "\n") {
				t.Error("script whitespace should be preserved even with expressions")
			}
			break
		}
	}
	if !foundConsole {
		t.Errorf("expected script content preserved, got texts: %v", texts)
	}
}

func TestWhitespace_BlankLinesRemoved(t *testing.T) {
	tpl := parseTemplate(t, `<div>

    <p>text</p>

</div>`)

	texts := collectTexts(tpl.nodes)
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			t.Errorf("blank lines should be removed, got whitespace-only text: %q", text)
		}
	}
}

func TestWhitespace_InlineTextContent(t *testing.T) {
	tpl := parseTemplate(t, `<li>Login: <strong>username</strong></li>`)

	texts := collectTexts(tpl.nodes)
	foundLogin := false
	for _, text := range texts {
		if strings.Contains(text, "Login: ") {
			foundLogin = true
			break
		}
	}
	if !foundLogin {
		t.Errorf("inline text 'Login: ' should be preserved, got texts: %v", texts)
	}
}

func TestWhitespace_AttributeValuesUnchanged(t *testing.T) {
	tpl := parseTemplate(t, `<div class="foo   bar" data-value="a  b"></div>`)

	for _, n := range tpl.nodes {
		el, ok := n.(*node.HtmlElement)
		if !ok {
			continue
		}
		for _, attr := range el.Attributes {
			if attr.Key == "class" {
				if len(attr.Values) != 1 {
					t.Fatalf("expected 1 value node for class attr, got %d", len(attr.Values))
				}
				textNode, ok := attr.Values[0].(*node.Text)
				if !ok {
					t.Fatal("expected text node in class attr")
				}
				if textNode.Text != "foo   bar" {
					t.Errorf("attribute value should not be modified, got %q", textNode.Text)
				}
			}
		}
	}
}

func TestWhitespace_SsrElseStillWorks(t *testing.T) {
	tpl := parseTemplate(t, `<div ssr:if="a">yes</div>
<div ssr:else>no</div>`)

	if tpl == nil {
		t.Fatal("template should parse without error")
	}

	// Verify the condition node was created properly
	foundCondition := false
	for _, n := range tpl.nodes {
		if _, ok := n.(*node.SsrCondition); ok {
			foundCondition = true
			break
		}
	}
	if !foundCondition {
		t.Error("ssr:if/else should create a condition node")
	}
}
