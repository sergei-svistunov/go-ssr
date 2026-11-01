package template

import (
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

// The fix lines of the context refusals.
const (
	fixScriptValue = "pass the value through a data-* attribute and read it in the page's TypeScript"
	fixHandler     = "remove the attribute and add the listener in the page's TypeScript"
	fixStyleAttr   = "move the style into a stylesheet and switch classes instead"
	fixRawValue    = "use {{ }}, which escapes the value"
	fixScriptURL   = "link to a page, or handle the click in the page's TypeScript"
	fixURLScheme   = "start the attribute with a fixed scheme or path, or put the whole URL in one value"
	fixInline      = "move it into an asset next to the template; the Content-Security-Policy blocks inline code"
	fixName        = "write the name literally"
	fixFixedAttr   = "write the attribute literally, or remove it"
)

func fixUnquoted(attr string) string { return "quote the value: " + attr + `="{{ … }}"` }

func fixRawText(tag string) string {
	return "move the value out of <" + tag + ">; its text is shown undecoded"
}

// rawTags are the elements whose text the tokenizer reads up to their end tag without
// parsing markup, even after a start tag written as "<tag/>".
var rawTags = map[string]bool{
	"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true,
	"script": true, "style": true, "textarea": true, "title": true, "xmp": true,
}

// undecodedTags are the raw-text elements whose text the browser shows as it is, so no
// escaper can make a value safe inside them.
var undecodedTags = map[string]bool{
	"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true, "xmp": true,
}

// rcdataTags are the elements whose text the browser decodes but never parses as markup.
var rcdataTags = map[string]bool{"title": true, "textarea": true}

var urlAttrs = map[string]bool{
	"action": true, "archive": true, "background": true, "cite": true, "classid": true, "codebase": true,
	"data": true, "formaction": true, "href": true, "icon": true, "longdesc": true, "manifest": true,
	"poster": true, "profile": true, "src": true, "usemap": true, "xmlns": true,
}

// codeTags are the elements whose attributes load code or change how the page is read, so
// none of them takes a value.
var codeTags = map[string]bool{
	"script": true, "base": true, "meta": true, "link": true, "object": true, "embed": true, "applet": true,
	"animate": true, "set": true, "animatemotion": true, "animatetransform": true,
}

// codeAttrs take no value on any element.
var codeAttrs = map[string]bool{"srcdoc": true, "srcset": true, "imagesrcset": true, "http-equiv": true}

// animationTags set an attribute of their parent element, such as href, to their own values.
var animationTags = map[string]bool{"animate": true, "set": true, "animatemotion": true, "animatetransform": true}

// urlSpace is what browsers strip from both ends of a URL.
const urlSpace = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f" +
	"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x20"

func refuse(file string, line int, code, fix, message string) *CodedError {
	return &CodedError{File: file, Line: line, Code: code, Message: message, Fix: fix}
}

// hasMarkup reports whether s holds what the tokenizer would read as a tag or a comment: "<"
// followed by a letter, "/" or "!".
func hasMarkup(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if c := s[i+1]; s[i] == '<' && (c == '/' || c == '!' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return true
		}
	}
	return false
}

func hasValueMarker(s string) bool { return strings.Contains(s, "{{") || strings.Contains(s, "}}") }

// localName returns name without its namespace prefix, such as "href" for "xlink:href".
func localName(name string) string {
	return name[strings.LastIndexByte(name, ':')+1:]
}

func isURLAttr(name string) bool {
	n := strings.TrimPrefix(localName(name), "data-")
	return urlAttrs[n] || strings.Contains(n, "src") || strings.Contains(n, "uri") || strings.Contains(n, "url")
}

// dropURLTabs removes the tabs and newlines browsers drop anywhere in a URL.
func dropURLTabs(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

// runsCode reports whether a URL with the scheme s runs code.
func runsCode(s string) bool {
	s = strings.ToLower(dropURLTabs(strings.Trim(s, urlSpace)))
	return s == "javascript" || s == "vbscript"
}

// valueScheme reports whether a value may follow the literal scheme s: http, https or mailto.
func valueScheme(s string) bool {
	switch strings.ToLower(dropURLTabs(strings.Trim(s, urlSpace))) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// isScriptURL reports whether the literal URL u runs code.
func isScriptURL(u string) bool {
	u = strings.TrimLeft(u, urlSpace)
	i := strings.IndexAny(u, ":/?#")
	return i >= 0 && u[i] == ':' && runsCode(u[:i])
}

// literalText returns the text of values without their {{ }}.
func literalText(values []node.Node) string {
	var b strings.Builder
	for _, v := range values {
		if t, ok := v.(*node.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// checkTextValues refuses a value in text the browser does not read as plain text, and a
// {{$ }} in text the browser never reads as markup. raw is the element whose text the
// tokenizer reads without parsing markup, or "".
func checkTextValues(file, raw string, nodes []node.Node) error {
	for _, n := range nodes {
		var line int
		markup := false
		switch v := n.(type) {
		case *node.Expression:
			line, v.Esc = v.Line, node.EscText
		case *node.RawExpression:
			line, markup = v.Line, true
		default:
			continue
		}
		switch {
		case raw == "script" || raw == "style":
			return refuse(file, line, "E08", fixScriptValue, "a value in the body of <"+raw+">")
		case undecodedTags[raw]:
			return refuse(file, line, "E12", fixRawText(raw), "a value inside <"+raw+">")
		case markup && rcdataTags[raw]:
			return refuse(file, line, "E13", fixRawValue, "{{$ }} inside <"+raw+">, whose text is never markup")
		}
	}
	return nil
}

// rcdataValue is a value in the text of a <title> or <textarea>.
type rcdataValue struct {
	tag  string
	expr *node.Expression
}

// checkLiveRCDATA refuses a value of a live variable in the text of <title> or <textarea>:
// a live site needs a wrapper element, which there would show as text.
func checkLiveRCDATA(file string, values []rcdataValue, vars []Variable) error {
	live := map[string]bool{}
	for _, v := range vars {
		if v.Reactive {
			live[v.Name] = true
		}
	}
	for _, v := range values {
		if refs := v.expr.CollectVarRefs(live); len(refs) > 0 {
			return syntaxErrf(file, v.expr.Line, "the live variable %s inside <%s>, whose text a live update cannot change; show it outside <%s> or drop reactive=\"true\"", refs[0], v.tag, v.tag)
		}
	}
	return nil
}

// attrValues parses the value of an attribute, refusing one that could run code or change
// how the page is read, and sets the escaper of every value in it. tag is the element the
// browser sees, name the attribute's lower-case name, and quoted whether its value is quoted
// in the template.
func attrValues(file string, line int, tag, name, value string, quoted bool) ([]node.Node, error) {
	local := localName(name)
	switch {
	case hasValueMarker(name):
		return nil, refuse(file, line, "E17", fixName, "a value in the attribute name "+name+" of <"+tag+">")
	case strings.HasPrefix(local, "on"):
		return nil, refuse(file, line, "E09", fixHandler, "<"+tag+"> has the event handler attribute "+name)
	case local == "style":
		return nil, refuse(file, line, "E10", fixStyleAttr, "<"+tag+"> has a style attribute")
	case !quoted && hasValueMarker(value):
		return nil, refuse(file, line, "E11", fixUnquoted(name), "the "+name+" attribute of <"+tag+"> holds a value but is not quoted")
	}
	if value == "" {
		return nil, checkLiteralAttr(file, line, tag, name, nil)
	}
	values, err := parseText(value, file, line, false)
	if err != nil {
		return nil, err
	}

	var exprs []*node.Expression
	for _, v := range values {
		switch x := v.(type) {
		case *node.Expression:
			exprs = append(exprs, x)
		case *node.RawExpression:
			return nil, refuse(file, line, "E13", fixRawValue, "{{$ }} in the "+name+" attribute of <"+tag+">")
		}
	}
	if len(exprs) == 0 {
		return values, checkLiteralAttr(file, line, tag, name, values)
	}
	switch {
	case codeTags[tag] || codeAttrs[name]:
		return nil, refuse(file, line, "E18", fixFixedAttr, "the "+name+" attribute of <"+tag+"> holds a value")
	case isURLAttr(name):
		return values, urlEscapers(file, line, tag, name, values)
	}
	for _, e := range exprs {
		e.Esc = node.EscAttr
	}
	return values, nil
}

// checkLiteralAttr refuses an attribute without values that holds inline code: a srcdoc, or
// a URL that runs a script.
func checkLiteralAttr(file string, line int, tag, name string, values []node.Node) error {
	lit := literalText(values)
	switch {
	case name == "srcdoc":
		return refuse(file, line, "E18", fixFixedAttr, "<"+tag+"> has a srcdoc attribute, which holds a whole page inline")
	case isURLAttr(name) && isScriptURL(lit):
		return refuse(file, line, "E14", fixScriptURL, "the "+name+" attribute of <"+tag+"> runs a script")
	case animationTags[tag]:
		for item := range strings.SplitSeq(lit, ";") {
			if isScriptURL(item) {
				return refuse(file, line, "E14", fixScriptURL, "the "+name+" attribute of <"+tag+"> sets a URL that runs a script")
			}
		}
	}
	return nil
}

// urlEscapers sets the escaper of every value in a URL attribute. A value may be the whole
// URL; otherwise the literal text before the first value must fix the scheme. A value right
// after a lone leading "/" gets the escaper that keeps the URL from starting with "//".
func urlEscapers(file string, line int, tag, name string, values []node.Node) error {
	var lead strings.Builder
	first := 0
	for i, v := range values {
		t, ok := v.(*node.Text)
		if !ok {
			first = i
			break
		}
		lead.WriteString(t.Text)
	}
	prefix := strings.TrimLeft(lead.String(), urlSpace)
	schemeErr := refuse(file, line, "E15", fixURLScheme, "the scheme of the "+name+" attribute of <"+tag+"> depends on a value")

	if prefix == "" {
		for _, v := range values[first+1:] {
			if t, ok := v.(*node.Text); !ok || strings.Trim(t.Text, urlSpace) != "" {
				return schemeErr
			}
		}
		values[first].(*node.Expression).Esc = node.EscURL
		return nil
	}

	i := strings.IndexAny(prefix, ":/?#")
	switch {
	case i < 0:
		return schemeErr
	case prefix[i] == ':' && runsCode(prefix[:i]):
		return refuse(file, line, "E14", fixScriptURL, "the "+name+" attribute of <"+tag+"> runs a script")
	case prefix[i] == ':' && !valueScheme(prefix[:i]):
		return refuse(file, line, "E14", fixScriptURL, "the "+name+" attribute of <"+tag+"> puts a value after the scheme "+prefix[:i]+":, which can carry a page or a script")
	}

	query := false
	for _, v := range values {
		switch x := v.(type) {
		case *node.Text:
			query = query || strings.ContainsAny(x.Text, "?#")
		case *node.Expression:
			x.Esc = node.EscURLPart
			if query {
				x.Esc = node.EscURLQuery
			}
		}
	}
	if dropURLTabs(prefix) == "/" {
		values[first].(*node.Expression).Esc = node.EscURLPathStart
	}
	return nil
}

// scannedAttr is an attribute of a start tag as the template spells it.
type scannedAttr struct {
	name   string
	quoted bool
}

func isHTMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }

func asciiLower(b []byte) string {
	s := []byte(string(b))
	for i, c := range s {
		if 'A' <= c && c <= 'Z' {
			s[i] = c + 'a' - 'A'
		}
	}
	return string(s)
}

// scanAttrs lists the attributes of the start tag raw in source order, reading them as the
// x/net/html tokenizer does, so that the list lines up with what Tokenizer.TagAttr returns
// and tells whether each value was quoted.
func scanAttrs(raw []byte) []scannedAttr {
	n := len(raw)
	skipSpace := func(i int) int {
		for i < n && isHTMLSpace(raw[i]) {
			i++
		}
		return i
	}
	i := 1
	for i < n && !isHTMLSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	var attrs []scannedAttr
	for {
		for i < n && (isHTMLSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= n || raw[i] == '>' {
			return attrs
		}
		// The first byte belongs to the name even when it is "=".
		start := i
		i++
		for i < n && !isHTMLSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' && raw[i] != '=' {
			i++
		}
		a := scannedAttr{name: asciiLower(raw[start:i])}
		if j := skipSpace(i); j < n && raw[j] == '=' {
			j = skipSpace(j + 1)
			if j < n && (raw[j] == '"' || raw[j] == '\'') {
				q := raw[j]
				for j++; j < n && raw[j] != q; j++ {
				}
				a.quoted = true
				j++
			} else {
				for j < n && !isHTMLSpace(raw[j]) && raw[j] != '>' {
					j++
				}
			}
			i = j
		}
		attrs = append(attrs, a)
	}
}
