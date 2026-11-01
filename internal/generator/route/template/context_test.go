package template

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// parseSource parses src as a template file.
func parseSource(t *testing.T, src string) (*Template, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return Parse(path, func(s string) string { return s })
}

// values returns the {{ }} and {{$ }} nodes of nodes in document order, attribute values
// included.
func values(nodes []node.Node) []node.Node {
	var out []node.Node
	for _, n := range nodes {
		switch v := n.(type) {
		case *node.Expression, *node.RawExpression:
			out = append(out, v)
		case *node.HtmlElement:
			for _, a := range v.Attributes {
				out = append(out, values(a.Values)...)
			}
			out = append(out, values(v.Children)...)
		case *node.SsrForm:
			for _, a := range v.Attributes {
				out = append(out, values(a.Values)...)
			}
			out = append(out, values(v.Children)...)
		case *node.SsrInput:
			for _, a := range v.Attributes {
				out = append(out, values(a.Values)...)
			}
		case *node.SsrTextarea:
			for _, a := range v.Attributes {
				out = append(out, values(a.Values)...)
			}
		case *node.SsrSelect:
			for _, a := range v.Attributes {
				out = append(out, values(a.Values)...)
			}
		}
	}
	return out
}

// escapers parses src and returns the escaper of each {{ }} in it.
func escapers(t *testing.T, src string) []node.Esc {
	t.Helper()
	tpl, err := parseSource(t, src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	var out []node.Esc
	for _, v := range values(tpl.nodes) {
		if e, ok := v.(*node.Expression); ok {
			out = append(out, e.Esc)
		}
	}
	return out
}

var (
	reWrite  = regexp.MustCompile(`_w\.Write\((_[0-9a-v]+)\)|render\.\w+\(_w, \w+\)`)
	reLitVar = regexp.MustCompile(`(_[0-9a-v]+)=\[\]byte\{([^}]*)\}`)
)

// rendered returns what the code generated for tpl writes, with each value shown as the
// call that writes it.
func rendered(t *testing.T, tpl *Template) string {
	t.Helper()
	buf := gobuf.New()
	tpl.WriteGoCode(buf)
	buf.WriteVars()
	src := buf.String()
	lits := map[string]string{}
	for _, m := range reLitVar.FindAllStringSubmatch(src, -1) {
		var b []byte
		for _, h := range strings.FieldsFunc(m[2], func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
			n, err := strconv.ParseUint(h, 0, 8)
			if err != nil {
				t.Fatal(err)
			}
			b = append(b, byte(n))
		}
		lits[m[1]] = string(b)
	}
	var out strings.Builder
	for _, m := range reWrite.FindAllStringSubmatch(src, -1) {
		if m[1] == "" {
			out.WriteString("[" + m[0] + "]")
			continue
		}
		out.WriteString(lits[m[1]])
	}
	return out.String()
}

// code returns the error code of err, or "" when err is not a *CodedError.
func code(err error) string {
	var c *CodedError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

func TestEscaperByContext(t *testing.T) {
	for src, want := range map[string]node.Esc{
		`<p>{{ x }}</p>`:                                                             node.EscText,
		`<p class="a {{ x }}"></p>`:                                                  node.EscAttr,
		`<p class='{{ x }}'></p>`:                                                    node.EscAttr,
		`<p title="{{ x }}"></p>`:                                                    node.EscAttr,
		`<a href="{{ x }}"></a>`:                                                     node.EscURL,
		`<a href=" {{ x }} "></a>`:                                                   node.EscURL,
		`<img src="{{ x }}">`:                                                        node.EscURL,
		`<button formaction="{{ x }}"></button>`:                                     node.EscURL,
		`<a data-src="{{ x }}"></a>`:                                                 node.EscURL,
		`<a data-href="{{ x }}"></a>`:                                                node.EscURL,
		`<div data-thumb-url="{{ x }}"></div>`:                                       node.EscURL,
		`<svg><a xlink:href="{{ x }}"></a></svg>`:                                    node.EscURL,
		`<a href="/u/{{ x }}"></a>`:                                                  node.EscURLPart,
		`<a href="u/{{ x }}"></a>`:                                                   node.EscURLPart,
		`<a href="mailto:{{ x }}"></a>`:                                              node.EscURLPart,
		`<a href="https://h.example/{{ x }}"></a>`:                                   node.EscURLPart,
		`<a href="HTTP://h.example/{{ x }}"></a>`:                                    node.EscURLPart,
		`<a href="ht&#x09;tps://h.example/{{ x }}"></a>`:                             node.EscURLPart,
		`<a href="/a:b/{{ x }}"></a>`:                                                node.EscURLPart,
		`<a href="/{{ x }}"></a>`:                                                    node.EscURLPathStart,
		`<a href=" /{{ x }}"></a>`:                                                   node.EscURLPathStart,
		"<a href=\"/\n{{ x }}\"></a>":                                                node.EscURLPathStart,
		`<img src="/{{ x }}">`:                                                       node.EscURLPathStart,
		`<a href="/f?q={{ x }}"></a>`:                                                node.EscURLQuery,
		`<a href="#{{ x }}"></a>`:                                                    node.EscURLQuery,
		`<ssr:form name="f" class="{{ x }}"></ssr:form>`:                             node.EscAttr,
		`<ssr:form name="f" action="/s/{{ x }}"></ssr:form>`:                         node.EscURLPart,
		`<ssr:form name="f"><ssr:input name="a" data-href="{{ x }}"/></ssr:form>`:    node.EscURL,
		`<ssr:form name="f"><ssr:textarea name="a" title="{{ x }}"/></ssr:form>`:     node.EscAttr,
		`<ssr:form name="f"><ssr:select name="a" data-src="/i/{{ x }}"/></ssr:form>`: node.EscURLPart,
	} {
		if got := escapers(t, src); len(got) != 1 || got[0] != want {
			t.Errorf("%s: escapers %v, want [%v]", src, got, want)
		}
	}
	for src, want := range map[string][]node.Esc{
		`<a href="/{{ x }}/{{ y }}"></a>`:       {node.EscURLPathStart, node.EscURLPart},
		`<a href="/f?q={{ x }}&p={{ y }}"></a>`: {node.EscURLQuery, node.EscURLQuery},
		`<a href="/n/{{ x }}#{{ y }}"></a>`:     {node.EscURLPart, node.EscURLQuery},
	} {
		got := escapers(t, src)
		if len(got) != len(want) {
			t.Errorf("%s: escapers %v, want %v", src, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: escapers %v, want %v", src, got, want)
			}
		}
	}

	tpl, err := parseSource(t, `<a class="{{ c }}" href="/u/{{ id }}">{{ name }}{{$ markup }}</a>`)
	if err != nil {
		t.Fatal(err)
	}
	want := `<a class="[render.Attr(_w, c)]" href="/u/[render.URLPart(_w, id)]">[render.Text(_w, name)][render.HTML(_w, markup)]</a>`
	if got := rendered(t, tpl); got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}
}

func TestContextRefusals(t *testing.T) {
	for src, want := range map[string]string{
		`<script>{{ x }}</script>`:                                                  "E08",
		`<script type="application/json">{{ x }}</script>`:                          "E08",
		`<style>{{ x }}</style>`:                                                    "E08",
		`<script>{{$ x }}</script>`:                                                 "E08",
		`<a onclick="go()"></a>`:                                                    "E09",
		`<a ONMOUSEOVER="x"></a>`:                                                   "E09",
		`<svg><a xlink:onclick="x"></a></svg>`:                                      "E09",
		`<ssr:form name="a" onsubmit="x()"></ssr:form>`:                             "E09",
		`<p style="color:red"></p>`:                                                 "E10",
		`<ssr:form name="a"><ssr:textarea name="t" style="height:9em"/></ssr:form>`: "E10",
		`<a class={{x}}></a>`:                                                       "E11",
		`<a class={{ x }}></a>`:                                                     "E11",
		`<a href=/u/{{x}}></a>`:                                                     "E11",
		`<noscript>{{ x }}</noscript>`:                                              "E12",
		`<iframe>{{ x }}</iframe>`:                                                  "E12",
		`<noembed>{{ x }}</noembed>`:                                                "E12",
		`<noframes>{{ x }}</noframes>`:                                              "E12",
		`<xmp>{{ x }}</xmp>`:                                                        "E12",
		`<plaintext>{{ x }}`:                                                        "E12",
		`<noscript>{{$ x }}</noscript>`:                                             "E12",
		`<a title="{{$ x }}"></a>`:                                                  "E13",
		`<title>{{$ x }}</title>`:                                                   "E13",
		`<textarea>{{$ x }}</textarea>`:                                             "E13",
		`<ssr:form name="a"><ssr:input name="n" placeholder="{{$ x }}"/></ssr:form>`:     "E13",
		`<a href="javascript:alert(1)"></a>`:                                             "E14",
		`<a href=" JavaScript:x"></a>`:                                                   "E14",
		`<a href="java&#x09;script:x"></a>`:                                              "E14",
		`<a href="vbscript:x"></a>`:                                                      "E14",
		`<a href="javascript:{{ x }}"></a>`:                                              "E14",
		`<a href="data:text/html,{{ x }}"></a>`:                                          "E14",
		`<img src="blob:{{ x }}">`:                                                       "E14",
		`<a href="tel:{{ x }}"></a>`:                                                     "E14",
		`<a href=" Data:{{ x }}"></a>`:                                                   "E14",
		`<a href="ftp://h/{{ y }}"></a>`:                                                 "E14",
		`<iframe src="javascript:x"></iframe>`:                                           "E14",
		`<ssr:form name="a" action="javascript:x"></ssr:form>`:                           "E14",
		`<svg><a><animate attributeName="href" values="javascript:alert(1)"/></a></svg>`: "E14",
		`<svg><set attributeName="href" to="/a; JavaScript:x"/></svg>`:                   "E14",
		`<a href="java{{ x }}"></a>`:                                                     "E15",
		`<a href="{{ a }}{{ b }}"></a>`:                                                  "E15",
		`<a href="{{ base }}/x"></a>`:                                                    "E15",
		`<a href="{{ p }}://{{ h }}"></a>`:                                               "E15",
		`<ssr:form name="a" action="{{ a }}{{ b }}"></ssr:form>`:                         "E15",
		`<script>alert(1)</script>`:                                                      "E16",
		`<script type="module">import "/a.js"</script>`:                                  "E16",
		`<script type="text/javascript"> x </script>`:                                    "E16",
		`<script src="/a.js"/><p>x</p>`:                                                  "E16",
		`<style>p{}</style>`:                                                             "E16",
		`<style></style>`:                                                                "E16",
		`<style> </style>`:                                                               "E16",
		`<style>`:                                                                        "E16",
		`<svg><style></style></svg>`:                                                     "E16",
		`<a {{x}}="1"></a>`:                                                              "E17",
		`<a {{ x }}="1"></a>`:                                                            "E17",
		`<div{{x}}></div>`:                                                               "E17",
		`<script src="{{ x }}"></script>`:                                                "E18",
		`<script type="{{ x }}"></script>`:                                               "E18",
		`<meta content="{{ x }}">`:                                                       "E18",
		`<base href="{{ x }}">`:                                                          "E18",
		`<link rel="{{ x }}" href="/a.css">`:                                             "E18",
		`<link rel="stylesheet" href="{{ x }}">`:                                         "E18",
		`<object data="{{ x }}"></object>`:                                               "E18",
		`<embed src="{{ x }}">`:                                                          "E18",
		`<applet code="{{ x }}"></applet>`:                                               "E18",
		`<img srcset="{{ x }}">`:                                                         "E18",
		`<link imagesrcset="{{ x }}">`:                                                   "E18",
		`<div http-equiv="{{ x }}"></div>`:                                               "E18",
		`<iframe srcdoc="{{ x }}"></iframe>`:                                             "E18",
		`<svg><animate values="{{ x }}"/></svg>`:                                         "E18",
		`<svg><set to="{{ x }}"/></svg>`:                                                 "E18",
		`<svg><animateMotion path="{{ x }}"/></svg>`:                                     "E18",
		`<svg><animateTransform from="{{ x }}"/></svg>`:                                  "E18",
		`<iframe srcdoc="<p>hi</p>"></iframe>`:                                           "E18",
		`<iframe SRCDOC=""></iframe>`:                                                    "E18",
	} {
		_, err := parseSource(t, src)
		if got := code(err); got != want {
			t.Errorf("%s: code %q (%v), want %s", src, got, err, want)
		}
	}
	for _, ok := range []string{
		`<script src="/a.js"></script>`,
		`<script> </script>`,
		"<script src=\"/a.js\">\n</script>",
		`<script></script>`,
		`<script type="application/json"> </script>`,
		`<script src="/a.js"/></script>`,
		`<script type="application/json">{"a": 1}</script>`,
		`<script type=" Application/LD+JSON ">{}</script>`,
		`<a href="/x">x</a>`,
		`<a href="https://x.example">x</a>`,
		`<a href="tel:+1">x</a>`,
		`<a href="#top">x</a>`,
		`<p data-on="1"></p>`,
		`<img src="data:image/png;base64,AAAA">`,
		`<svg><animate attributeName="x" values="0;10"/></svg>`,
		`<iframe src="/x"></iframe>`,
		`<link rel="stylesheet" href="/a.css">`,
		`<noscript>Turn on JavaScript &amp; reload</noscript>`,
		`<title>{{ x }}</title>`,
		`<textarea>{{ x }}</textarea>`,
		`<p>{{$ x }}</p>`,
		`<!-- <script>{{ x }}</script> -->`,
	} {
		if _, err := parseSource(t, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

// The fix line of a refusal names the attribute or the element it is about.
func TestRefusalFix(t *testing.T) {
	for src, want := range map[string]string{
		`<a class={{ x }}></a>`:        `quote the value: class="{{ … }}"`,
		`<noscript>{{ x }}</noscript>`: "move the value out of <noscript>; its text is shown undecoded",
		`<xmp>{{ x }}</xmp>`:           "move the value out of <xmp>; its text is shown undecoded",
	} {
		_, err := parseSource(t, src)
		var c *CodedError
		if !errors.As(err, &c) || c.Fix != want {
			t.Errorf("%s: %#v, want the fix %q", src, err, want)
		}
	}
}

func TestRCDATA(t *testing.T) {
	for _, src := range []string{`<title>{{ x }}</title>`, `<textarea>{{ x }}</textarea>`} {
		if got := escapers(t, src); len(got) != 1 || got[0] != node.EscText {
			t.Errorf("%s: escapers %v, want [EscText]", src, got)
		}
	}
	for _, src := range []string{`<title>{{$ x }}</title>`, `<textarea>{{$ x }}</textarea>`, "<title>a\n{{$ x }}</title>"} {
		if _, err := parseSource(t, src); code(err) != "E13" {
			t.Errorf("%s: %v, want E13", src, err)
		}
	}
}

// A live value inside <title> or <textarea> is refused: its wrapper element would show as
// text, and a live update cannot change the element's text safely.
func TestReactiveInRCDATARefused(t *testing.T) {
	const vars = `<ssr:var name="live" type="string" reactive="true"/><ssr:var name="x" type="string"/>` + "\n"
	for _, src := range []string{
		vars + "<title>{{ live }}</title>",
		vars + "<textarea>{{ live }}</textarea>",
		vars + "<title>a {{ x }}</title><title>{{ x + live }}</title>",
		"<textarea>\n</textarea><title>{{ live }}</title>\n" + vars,
	} {
		if _, err := parseSource(t, src); syntaxLine(err) != 2 || !strings.Contains(err.Error(), "live") {
			t.Errorf("%q: %v, want a syntax error at line 2", src, err)
		}
	}
	for _, src := range []string{
		vars + "<title>{{ x }}</title>",
		vars + "<textarea>{{ x }}</textarea><p>{{ live }}</p>",
	} {
		if _, err := parseSource(t, src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

// A template cannot claim an ssr:bind element for another route's client.
func TestBindRouteIsReserved(t *testing.T) {
	for _, src := range []string{`<input ssr:bind="x" data-ssr-route="00000000"/>`, "<p>\n<div DATA-SSR-ROUTE=\"a\"></div></p>"} {
		if _, err := parseSource(t, src); syntaxLine(err) == 0 || !strings.Contains(err.Error(), "data-ssr-route") {
			t.Errorf("%q: %v, want a syntax error", src, err)
		}
	}
}

// rawElements are the elements whose text x/net/html's tokenizer reads up to their end tag
// without parsing markup.
var rawElements = map[string]bool{
	"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true,
	"script": true, "style": true, "textarea": true, "title": true, "xmp": true,
}

const marker = "{{$ x }}"

// tokenizerContext returns where x/net/html's tokenizer puts the marker in src: in text,
// in the text of an element it reads without parsing markup, in a comment, in an attribute
// value, or in a tag or attribute name.
func tokenizerContext(t *testing.T, src string) string {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(src))
	open := ""
	for {
		tt := z.Next()
		tok := z.Token()
		switch tt {
		case html.ErrorToken:
			t.Fatalf("%s: the tokenizer never reads the marker", src)
		case html.TextToken:
			if strings.Contains(tok.Data, marker) {
				switch open {
				case "":
					return "text"
				case "title", "textarea":
					return "rcdata"
				case "script", "style":
					return "script"
				}
				return "raw text"
			}
		case html.CommentToken:
			if strings.Contains(tok.Data, marker) {
				return "comment"
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			if strings.Contains(tok.Data, "{{") {
				return "name"
			}
			for _, a := range tok.Attr {
				if strings.Contains(a.Key, "{{") {
					return "name"
				}
				if strings.Contains(a.Val, marker) {
					return "attribute"
				}
			}
		}
		open = ""
		if (tt == html.StartTagToken || tt == html.SelfClosingTagToken) && rawElements[tok.Data] {
			open = tok.Data
		}
	}
}

// parserOutcome returns what the parser does with the marker in src: writes it as markup,
// drops it, or refuses it with a code.
func parserOutcome(t *testing.T, src string) string {
	t.Helper()
	tpl, err := parseSource(t, src)
	if err != nil {
		if c := code(err); c != "" {
			return c
		}
		return err.Error()
	}
	for _, v := range values(tpl.nodes) {
		if _, ok := v.(*node.RawExpression); ok {
			return "markup"
		}
	}
	return "dropped"
}

// The parser puts every value in the context x/net/html's tokenizer puts it, so a value is
// escaped for the place the browser reads it: {{$ }} is markup only in text.
// The oracle is the tokenizer the parser itself uses, so it cannot see where browsers parse
// SVG and MathML content differently; TestRawTextHoldsNoMarkup covers that.
func TestParserAgreesWithBrowsers(t *testing.T) {
	outcome := map[string]string{
		"text":      "markup",
		"rcdata":    "E13",
		"script":    "E08",
		"raw text":  "E12",
		"comment":   "dropped",
		"attribute": "E13",
		"name":      "E17",
	}
	for _, src := range []string{
		`<p>{{$ x }}</p>`,
		`<a title="</script>">{{$ x }}</a>`,
		`<script src="/a.js" data-x="</script>"></script><p>{{$ x }}</p>`,
		`<p title="<!--">{{$ x }}</p>`,
		`<!-- <script> -->{{$ x }}`,
		`<!-- {{$ x }} -->`,
		`<p><!-- </p> -->{{$ x }}</p>`,
		`<title><b>{{$ x }}</b></title>`,
		`<title><!--</title>-->{{$ x }}`,
		`<title><!-- {{$ x }} --></title>`,
		`<title/>{{$ x }}</title>`,
		`<textarea/>{{$ x }}</textarea>`,
		`<textarea></textareax>{{$ x }}</textarea>`,
		`<textarea></textarea x="1">{{$ x }}`,
		`<textarea></TEXTAREA>{{$ x }}`,
		`<script/>{{$ x }}</script>`,
		`<script>"</scr" + "ipt>{{$ x }}"</script>`,
		`<style>{{$ x }}</style>`,
		`<noscript/>{{$ x }}`,
		`<noscript><p>{{$ x }}</p></noscript>`,
		`<xmp></xmp >{{$ x }}`,
		`<iframe>{{$ x }}</iframe>`,
		`<plaintext></plaintext>{{$ x }}`,
		`<p class="{{$ x }}"></p>`,
		`<p {{$ x }}="1"></p>`,
		`<p{{$ x }}></p>`,
	} {
		ctx := tokenizerContext(t, src)
		if got, want := parserOutcome(t, src), outcome[ctx]; got != want {
			t.Errorf("%s: the tokenizer reads the value as %s, so want %s; the parser gives %s", src, ctx, want, got)
		}
	}
}

// A self-closing tag does not end <title>, <textarea> or <script>, so the parser keeps the
// element open, as the browser does.
func TestRawSelfClosingTagsStayOpen(t *testing.T) {
	tpl, err := parseSource(t, `<div><textarea/>{{ x }}</textarea><p>after</p></div>`)
	if err != nil {
		t.Fatal(err)
	}
	want := "<div><textarea>\n[render.Text(_w, x)]</textarea><p>after</p></div>"
	if got := rendered(t, tpl); got != want {
		t.Errorf("rendered %s, want %s", got, want)
	}
}

// Text inside <ssr:form> is escaped like any other text, so an entity in the template stays
// text.
func TestFormTextIsEscaped(t *testing.T) {
	tpl, err := parseSource(t, `<ssr:form name="f">&lt;script&gt;x&lt;/script&gt;</ssr:form>`)
	if err != nil {
		t.Fatal(err)
	}
	tpl.forms[0].Node.DPCtxFieldName = "FormF"
	if got := rendered(t, tpl); strings.Contains(got, "<script>") || !strings.Contains(got, "&lt;script&gt;x&lt;/script&gt;</form>") {
		t.Errorf("rendered %s", got)
	}
}

// An end tag closes the nearest open element of its name, or nothing.
func TestEndTagsCloseTheirElement(t *testing.T) {
	tpl, err := parseSource(t, "<div><span>a</div><p>b</p></span></p>\n<ssr:form name=\"f\"><ssr:input name=\"n\"></ssr:input></ssr:form>")
	if err != nil {
		t.Fatal(err)
	}
	if len(tpl.nodes) != 3 {
		t.Fatalf("top-level nodes: %d, want div, p and the form", len(tpl.nodes))
	}
	div := tpl.nodes[0].(*node.HtmlElement)
	if len(div.Children) != 1 || div.Children[0].(*node.HtmlElement).TagName != "span" {
		t.Errorf("div children: %#v", div.Children)
	}
	if f, ok := tpl.nodes[2].(*node.SsrForm); !ok || len(f.Children) != 1 {
		t.Errorf("third node: %#v", tpl.nodes[2])
	}
}

// Browsers keep the first of two attributes with one name, other parsers the last.
func TestDuplicateAttributeIsRefused(t *testing.T) {
	for _, src := range []string{`<a b="1" b="2"></a>`, `<a B="1" b="{{ x }}"></a>`} {
		_, err := parseSource(t, `<ssr:var name="x" type="string"/>`+src)
		if err == nil || !strings.Contains(err.Error(), "has the attribute b twice") {
			t.Errorf("%s: %v", src, err)
		}
	}
}

func TestScannedAttrsMatchTokenizer(t *testing.T) {
	for _, src := range []string{
		`<a b="1" c='2' d=3 e f = "4" g=>`,
		`<a =b="1"/>`,
		`<a b="x"c="y">`,
		`<a / b="1" / c>`,
		"<a\tb\n=\n\"1\"\fc='>'>",
		`<a B="1" b="2">`,
		`<a b=>`,
	} {
		z := html.NewTokenizer(strings.NewReader(src))
		z.Next()
		tok := z.Token()
		var got []scannedAttr
		for _, a := range scanAttrs(z.Raw()) {
			if !a.dup {
				got = append(got, a)
			}
		}
		if len(got) != len(tok.Attr) {
			t.Errorf("%s: scanned %v, tokenizer %v", src, got, tok.Attr)
			continue
		}
		for i, a := range tok.Attr {
			if got[i].name != a.Key {
				t.Errorf("%s: scanned %v, tokenizer %v", src, got, tok.Attr)
			}
		}
	}
	got := scanAttrs([]byte(`<a b="1" c='2' d=3 e>`))
	for i, want := range []bool{true, true, false, false} {
		if got[i].quoted != want {
			t.Errorf("attribute %s quoted = %t, want %t", got[i].name, got[i].quoted, want)
		}
	}
}

// syntaxLine returns the line of err when it is a *SyntaxError, or 0.
func syntaxLine(err error) int {
	var e *SyntaxError
	if errors.As(err, &e) {
		return e.Line
	}
	return 0
}

// The text of a raw-text element holds no markup, since inside SVG or MathML the browser
// parses it as markup the generator never checked, and a tag's attributes are read the same
// way by every parser.
func TestRawTextHoldsNoMarkup(t *testing.T) {
	for _, src := range []string{
		`<xmp><b>x</b></xmp>`,
		`<noscript/><img src=x onerror=alert(1)>`,
		`<svg><noscript><img src=x onerror=alert(1)></noscript></svg>`,
		`<svg><iframe><img src=x onerror=alert(1)></iframe></svg>`,
		`<noembed><p>x</p></noembed>`,
		`<noframes><!-- x --></noframes>`,
		`<plaintext></plaintext>`,
		`<svg><script type="application/json"><img src=x onerror=alert(1)></script></svg>`,
		`<a href="/x" href="{{ y }}"></a>`,
		`<a B="1" b="2"></a>`,
		`<ssr:form name="f" class="a" CLASS="b"></ssr:form>`,
		"<a b\x00c=\"1\"></a>",
	} {
		_, err := parseSource(t, "<div>\n"+src+"\n</div>")
		if syntaxLine(err) != 2 {
			t.Errorf("%s: %v, want a syntax error at line 2", src, err)
		}
	}
	for _, ok := range []string{
		`<xmp>a < b, 1<2</xmp>`,
		`<noscript>Turn on JavaScript &amp; reload</noscript>`,
		`<script type="application/json">{"a": "1 < 2"}</script>`,
		`<title><b>x</b></title>`,
		`<textarea><b>x</b></textarea>`,
	} {
		if _, err := parseSource(t, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

// The literal attributes a form field copies from the template stay attribute values, and its
// type and checkbox or radio value are checked before they reach the page or the Go code.
func TestFormFieldLiterals(t *testing.T) {
	for _, src := range []string{
		`<ssr:form name="f"><ssr:input name="q" type="text&quot; autofocus onfocus=&quot;alert(1)"/></ssr:form>`,
		`<ssr:form name="f"><ssr:input name="q" type="texts"/></ssr:form>`,
		`<ssr:form name="f"><ssr:input name="q" type="radio" gotype="int" value="1 { panic(0) }"/></ssr:form>`,
		`<ssr:form name="f"><ssr:input name="q" type="checkbox" gotype="bool" value="yes"/></ssr:form>`,
		`<ssr:form name="f"><ssr:input name="q" type="radio" gotype="uint8" value="256"/></ssr:form>`,
		`<ssr:form name="f"><ssr:input name="q" type="radio" gotype="float64" value="NaN"/></ssr:form>`,
	} {
		_, err := parseSource(t, "<div>\n"+src+"\n</div>")
		if syntaxLine(err) != 2 {
			t.Errorf("%s: %v, want a syntax error at line 2", src, err)
		}
	}

	tpl, err := parseSource(t, `<ssr:form name="f">`+
		`<ssr:input name="a" type="Email"/>`+
		`<ssr:input name="r" type="radio" value="x&quot;y"/></ssr:form>`)
	if err != nil {
		t.Fatal(err)
	}
	tpl.forms[0].Node.DPCtxFieldName = "FormF"
	// A field's name is a Go name, which the parser checks; the nodes escape it all the same.
	tpl.forms[0].Node.Children = append(tpl.forms[0].Node.Children,
		&node.SsrInput{Name: `a"b`}, &node.SsrTextarea{Name: "t<"}, &node.SsrSelect{Name: `s"`, GoType: "string"})
	got := rendered(t, tpl)
	for _, want := range []string{`<input name="a" type="email"`, `<input name="r" type="radio" value="x&#34;y"`, `<input name="a&#34;b"`, `<textarea name="t&lt;">`, `<select name="s&#34;">`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %s, want %s", got, want)
		}
	}
	buf := gobuf.New()
	tpl.WriteGoCode(buf)
	if code := buf.String(); !strings.Contains(code, `input.GetValue() == "x\"y"`) {
		t.Errorf("the radio value is not a Go string literal:\n%s", code)
	}
}
