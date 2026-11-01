//go:generate goyacc -o texty.go -v texty.output text.y

package template

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/htmlutils"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
	"github.com/sergei-svistunov/go-ssr/pkg/form"
)

var reWhitespace = regexp.MustCompile(`\s+`)

// SsrBindOnPrimitiveError is returned by the parser when ssr:bind is found on
// a GoSSR form primitive (<ssr:input>, <ssr:select>, <ssr:textarea>).
// The generator formats it as error E06.
type SsrBindOnPrimitiveError struct {
	File    string
	Line    int
	Element string // "input", "select", or "textarea"
}

func (e *SsrBindOnPrimitiveError) Error() string {
	return fmt.Sprintf("%s:%d:1: %s", e.File, e.Line, e.Message())
}

// Message returns the explanation without the position prefix, so callers that
// report their own location do not duplicate it.
func (e *SsrBindOnPrimitiveError) Message() string {
	return fmt.Sprintf("reactive-bindings: ssr:bind is not valid on GoSSR form primitive <ssr:%s>; use a native HTML <%s> element instead",
		e.Element, e.Element)
}

type Template struct {
	nodes       []node.Node
	variables   []Variable
	forms       []*Form
	contentNode *node.SsrContent
	ssrBindRefs []SsrBindRef
	// bindElements are the elements with ssr:bind.
	bindElements []*node.HtmlElement
	calls        []*node.SsrCall
}

type Variable struct {
	File           string
	Line           int
	Name           string
	Type           string
	Reactive       bool
	ClientWritable bool
}

// SsrBindRef records an ssr:bind attribute found on a native HTML element.
// It is used by the generator for validation (E05, E06 checks).
type SsrBindRef struct {
	File    string
	Line    int
	VarName string
}

type Form struct {
	Name     string
	Node     *node.SsrForm
	Elements []*FormElement
}

type FormElement struct {
	Name       string
	Nodes      []node.Node
	Type       FormElementType
	IsRequired bool
	IsMultiple bool
	GoType     string
}

type FormElementType uint8

const (
	FormElementUnknown FormElementType = iota
	FormElementInput
	FormElementInputFile
	FormElementTextarea
	FormElementSelect
)

var formElementGoTypes = map[string]struct{}{
	"string":  {},
	"int":     {},
	"int8":    {},
	"int16":   {},
	"int32":   {},
	"int64":   {},
	"uint":    {},
	"uint8":   {},
	"uint16":  {},
	"uint32":  {},
	"uint64":  {},
	"float32": {},
	"float64": {},
	"bool":    {},
}

func (v *Variable) FilePos() string {
	return fmt.Sprintf("//line %s:%d", v.File, v.Line)
}

func Parse(filename string, imageResolver func(string) string) (*Template, error) {
	tplFile, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not open template file: %w", err)
	}
	defer tplFile.Close()

	filename = filepath.Base(filename)

	tok := html.NewTokenizer(tplFile)
	curLine := 1
	rootNode := node.HtmlElement{}
	stack := NodesStack{&rootNode}
	var (
		variables    []Variable
		forms        []*Form
		activeForm   *Form
		contentNode  *node.SsrContent
		ssrBindRefs  []SsrBindRef
		bindElements []*node.HtmlElement
		calls        []*node.SsrCall
		// wrapped are the elements with ssr:if, ssr:else-if, ssr:else or ssr:for.
		wrapped = map[*node.HtmlElement]bool{}
		// rcdata holds the values in the text of <title> and <textarea>, by element.
		rcdata []rcdataValue

		// raw is the element whose text the tokenizer reads next without parsing markup.
		raw       string
		rawLine   int
		rawClosed bool // its start tag ends with "/>"
		rawData   bool // it is a <script> data block
	)

	for {
		tokenType := tok.Next()
		tokenLines := bytes.Count(tok.Raw(), []byte{'\n'})
		inRaw, inRawLine, inRawClosed, inRawData := raw, rawLine, rawClosed, rawData
		raw = ""
		if inRaw == "style" && tokenType != html.TextToken {
			return nil, refuse(filename, inRawLine, "E16", fixInline, "a <style> element")
		}
		switch tokenType {
		case html.ErrorToken:
			if errors.Is(tok.Err(), io.EOF) {
				if err := checkLiveRCDATA(filename, rcdata, variables); err != nil {
					return nil, err
				}
				if err := checkLiveBlocks(rootNode.Children, variables); err != nil {
					return nil, err
				}
				return &Template{
					nodes:        rootNode.Children,
					variables:    variables,
					forms:        forms,
					contentNode:  contentNode,
					ssrBindRefs:  ssrBindRefs,
					bindElements: bindElements,
					calls:        calls,
				}, nil
			}
			return nil, syntaxErrf(filename, curLine, "cannot parse HTML: %v", tok.Err())
		case html.TextToken:
			text := string(tok.Text())
			preserve := stack.isWhitespacePreserved()
			if !preserve && strings.TrimSpace(text) == "" {
				break
			}
			nodes, err := parseText(text, filename, curLine, false)
			if err != nil {
				return nil, err
			}
			if !preserve {
				for _, n := range nodes {
					if t, ok := n.(*node.Text); ok {
						t.Text = reWhitespace.ReplaceAllString(t.Text, " ")
					}
				}
			}
			if err := checkTextValues(filename, inRaw, nodes); err != nil {
				return nil, err
			}
			if rcdataTags[inRaw] {
				for _, n := range nodes {
					if e, ok := n.(*node.Expression); ok {
						rcdata = append(rcdata, rcdataValue{inRaw, e})
					}
				}
			}
			switch {
			case inRaw == "style":
				return nil, refuse(filename, inRawLine, "E16", fixInline, "a <style> element")
			case inRaw == "script" && !inRawData && strings.TrimSpace(text) != "":
				msg := "<script> has an inline body"
				if inRawClosed {
					msg = "<script/> does not end the element, so the text after it is an inline script body"
				}
				return nil, refuse(filename, inRawLine, "E16", fixInline, msg)
			case (undecodedTags[inRaw] || inRaw == "script") && hasMarkup(text):
				return nil, syntaxErrf(filename, inRawLine, "markup inside <%s>, which some browsers parse and others show as text; it may hold only plain text", inRaw)
			}
			stack.Top().AddChildren(nodes...)

		case html.StartTagToken, html.SelfClosingTagToken:
			tagName, hasAttrs := tok.TagName()
			if hasValueMarker(string(tagName)) {
				return nil, refuse(filename, curLine, "E17", fixName, "a value in the tag name <"+string(tagName)+">")
			}
			attrs, err := readAttrs(tok, hasAttrs, filename, curLine, string(tagName))
			if err != nil {
				return nil, err
			}

			if bytes.HasPrefix(tagName, []byte("ssr:")) {
				ssrTag := string(tagName[4:])
				if !ssrTags[ssrTag] {
					return nil, syntaxErrf(filename, curLine, "unknown GoSSR tag <%s>; the tags are ssr:var, ssr:content, ssr:assets, ssr:form, ssr:input, ssr:select, ssr:textarea and ssr:call", tagName)
				}
				cond, err := ssrTagCondition(filename, curLine, ssrTag, attrs)
				if err != nil {
					return nil, err
				}
				rendered := ssrTag == "form" || ssrTag == "input" || ssrTag == "select" || ssrTag == "textarea"
				attrsMap := map[string]string{}
				attributes := make([]node.HtmlAttribute, 0, len(attrs))
				for _, a := range attrs {
					attrsMap[a.key] = a.value
					if strings.HasPrefix(a.key, "ssr:") {
						continue
					}
					var nodes []node.Node
					var err error
					switch {
					case rendered:
						nodes, err = attrValues(filename, curLine, ssrTag, a.key, a.value, a.quoted)
					case len(a.value) > 0:
						nodes, err = parseText(a.value, filename, curLine, false)
					}
					if err != nil {
						return nil, err
					}

					attributes = append(attributes, node.HtmlAttribute{
						Key:    a.key,
						Values: nodes,
					})
				}

				switch string(tagName[4:]) {
				case "var":
					if attrsMap["name"] == "" {
						return nil, syntaxErrf(filename, curLine, "<ssr:var> is missing the required name attribute")
					}
					if attrsMap["type"] == "" {
						return nil, syntaxErrf(filename, curLine, "<ssr:var name=%q> is missing the required type attribute", attrsMap["name"])
					}
					if err := checkDeclared(filename, curLine, "<ssr:var> name", attrsMap["name"]); err != nil {
						return nil, err
					}
					field := ExportedName(attrsMap["name"])
					if i := slices.IndexFunc(variables, func(v Variable) bool { return ExportedName(v.Name) == field }); i >= 0 {
						return nil, syntaxErrf(filename, curLine, "<ssr:var name=%q> becomes the field %s of RouteData, as <ssr:var name=%q> on line %d does; pick another name", attrsMap["name"], field, variables[i].Name, variables[i].Line)
					}
					variables = append(variables, Variable{
						File:           filename,
						Line:           curLine,
						Name:           attrsMap["name"],
						Type:           attrsMap["type"],
						Reactive:       attrsMap["reactive"] == "true",
						ClientWritable: attrsMap["client-writable"] == "true",
					})
				case "content":
					contentNode = &node.SsrContent{
						BaseNode: node.BaseNode{File: filename, Line: curLine},
						Default:  attrsMap["default"],
					}
					stack.Top().AddChildren(contentNode)
				case "assets":
					stack.Top().AddChildren(&node.SsrAssets{BaseNode: node.BaseNode{File: filename, Line: curLine}})
				case "call":
					call, err := parseCall(filename, curLine, attrs, stack, wrapped, calls)
					if err != nil {
						return nil, err
					}
					calls = append(calls, call)
				case "form":
					if activeForm != nil {
						return nil, syntaxErrf(filename, curLine, "<ssr:form> cannot be nested inside another <ssr:form>")
					}

					if err := checkIdent(filename, curLine, "<ssr:form> name", attrsMap["name"]); err != nil {
						return nil, err
					}
					if i := slices.IndexFunc(forms, func(f *Form) bool { return ExportedName(f.Name) == ExportedName(attrsMap["name"]) }); i >= 0 {
						return nil, syntaxErrf(filename, curLine, "<ssr:form name=%q> has the generated names of <ssr:form name=%q> on line %d, such as Form%sValues; pick another name", attrsMap["name"], forms[i].Name, forms[i].Node.Line, ExportedName(attrsMap["name"]))
					}

					if attrsMap["enctype"] != "" && attrsMap["enctype"] != node.FormEncUrlEncoded && attrsMap["enctype"] != node.FormEncTypeMultipart {
						return nil, syntaxErrf(filename, curLine, "<ssr:form> has an invalid enctype, must be '%s' or '%s'",
							node.FormEncUrlEncoded, node.FormEncTypeMultipart)
					}

					formNode := &node.SsrForm{
						BaseNode:   node.BaseNode{File: filename, Line: curLine},
						Name:       attrsMap["name"],
						EncType:    attrsMap["enctype"],
						Attributes: attributes,
					}
					forms = append(forms, &Form{Name: attrsMap["name"], Node: formNode})
					activeForm = forms[len(forms)-1]

					if cond != nil {
						if err := addBranch(stack, filename, curLine, cond.key[len("ssr:"):], cond.value, formNode); err != nil {
							return nil, err
						}
					} else {
						stack.Top().AddChildren(formNode)
					}
					stack.Push(formNode)
				case "input", "textarea", "select":
					if activeForm == nil {
						return nil, syntaxErrf(filename, curLine, "<ssr:%s> must be inside an <ssr:form>", tagName[4:])
					}
					// ssr:bind is invalid on GoSSR form primitives (E06).
					if _, hasBind := attrsMap["ssr:bind"]; hasBind {
						elemName := string(tagName[4:])
						return nil, &SsrBindOnPrimitiveError{
							File:    filename,
							Line:    curLine,
							Element: elemName,
						}
					}

					if err := checkIdent(filename, curLine, "<ssr:"+string(tagName[4:])+"> name", attrsMap["name"]); err != nil {
						return nil, err
					}
					if field := ExportedName(attrsMap["name"]); field == "BaseFormValues" {
						return nil, syntaxErrf(filename, curLine, "<ssr:%s name=%q> becomes the field BaseFormValues, which every form's values embed; pick another name", tagName[4:], attrsMap["name"])
					} else if _, ok := reflect.TypeFor[*form.BaseFormValues]().MethodByName(field); ok {
						return nil, syntaxErrf(filename, curLine, "<ssr:%s name=%q> becomes the field %s, which hides the method %s of every form's values; pick another name", tagName[4:], attrsMap["name"], field, field)
					} else if i := slices.IndexFunc(activeForm.Elements, func(e *FormElement) bool { return e.Name != attrsMap["name"] && ExportedName(e.Name) == field }); i >= 0 {
						return nil, syntaxErrf(filename, curLine, "<ssr:%s name=%q> becomes the field %s of the form's values, as the element named %q does; pick another name", tagName[4:], attrsMap["name"], field, activeForm.Elements[i].Name)
					}

					_, isRequired := attrsMap["required"]
					_, isMultiple := attrsMap["multiple"]
					goType := attrsMap["gotype"]
					if goType == "" {
						goType = "string"
					}
					if _, exists := formElementGoTypes[goType]; !exists {
						return nil, syntaxErrf(filename, curLine, "unknown gotype '%s' on <ssr:%s name=%q>", goType, tagName[4:], attrsMap["name"])
					}

					var (
						n        node.Node
						nodeType FormElementType
					)

					switch string(tagName[4:]) {
					case "input":
						inputType := strings.ToLower(attrsMap["type"])
						if !inputTypes[inputType] {
							return nil, syntaxErrf(filename, curLine, "unknown type %q on <ssr:input name=%q>", attrsMap["type"], attrsMap["name"])
						}
						attrsMap["type"] = inputType
						goValue := ""
						if inputType == "checkbox" || inputType == "radio" {
							v, ok := goLiteral(goType, attrsMap["value"])
							if !ok {
								return nil, syntaxErrf(filename, curLine, "value %q of <ssr:input name=%q> is not a %s", attrsMap["value"], attrsMap["name"], goType)
							}
							goValue = v
						}
						if attrsMap["type"] == "file" {
							if activeForm.Node.EncType == "" {
								activeForm.Node.EncType = node.FormEncTypeMultipart
							}
							if activeForm.Node.EncType != node.FormEncTypeMultipart {
								return nil, syntaxErrf(filename, curLine, "invalid enctype '%s' for an input with type file, must be '%s'",
									activeForm.Node.EncType, node.FormEncTypeMultipart)
							}
						}

						n = &node.SsrInput{
							BaseNode:   node.BaseNode{File: filename, Line: curLine},
							Name:       attrsMap["name"],
							Type:       attrsMap["type"],
							Value:      attrsMap["value"],
							GoValue:    goValue,
							Attributes: attributes,
						}
						if attrsMap["type"] == "file" {
							nodeType = FormElementInputFile
						} else {
							nodeType = FormElementInput
						}
					case "textarea":
						n = &node.SsrTextarea{
							BaseNode:   node.BaseNode{File: filename, Line: curLine},
							Name:       attrsMap["name"],
							Attributes: attributes,
						}
						nodeType = FormElementTextarea
					case "select":
						n = &node.SsrSelect{
							BaseNode:   node.BaseNode{File: filename, Line: curLine},
							Name:       attrsMap["name"],
							GoType:     goType,
							Attributes: attributes,
							Multiple:   isMultiple,
						}
						nodeType = FormElementSelect
					}

					var elementByName *FormElement
					for _, e := range activeForm.Elements {
						if e.Name == attrsMap["name"] {
							elementByName = e
							break
						}
					}
					if elementByName == nil {
						activeForm.Elements = append(activeForm.Elements, &FormElement{
							Name:       attrsMap["name"],
							Nodes:      []node.Node{n},
							Type:       nodeType,
							IsRequired: isRequired,
							GoType:     goType,
							IsMultiple: isMultiple,
						})
					} else if elementByName.Type == nodeType && nodeType == FormElementInput {
						if goType != elementByName.GoType {
							return nil, syntaxErrf(filename, curLine, "form elements with name '%s' have different gotypes", elementByName.Name)
						}
						switch attrsMap["type"] {
						case "checkbox":
							elementByName.IsMultiple = true
							elementByName.Nodes[0].(*node.SsrInput).Multiple = true
							n.(*node.SsrInput).Multiple = true
							elementByName.Nodes = append(elementByName.Nodes, n)
						case "radio":
							elementByName.Nodes = append(elementByName.Nodes, n)
						default:
							return nil, syntaxErrf(filename, curLine, "form contains at least 2 elements with name '%s'", attrsMap["name"])
						}
					} else {
						return nil, syntaxErrf(filename, curLine, "form contains at least 2 elements with name '%s'", attrsMap["name"])
					}

					stack.Top().AddChildren(n)
				}

				continue
			}

			tag := string(tagName)
			n := &node.HtmlElement{
				BaseNode: node.BaseNode{File: filename, Line: curLine},
				TagName:  tag,
				// The tokenizer reads the text after "<title/>" as the element's, as browsers do.
				SelfClosed: tokenType == html.SelfClosingTagToken && !rawTags[tag],
			}
			// wrapper is the element's ssr:if, ssr:else-if, ssr:else or ssr:for.
			wrapper := ""
			for _, a := range attrs {
				key, value := []byte(a.key), []byte(a.value)

				// Process SSR attributes
				if bytes.HasPrefix(key, []byte("ssr:")) {
					switch key := string(key[4:]); key {
					case "for", "if", "else", "else-if":
						if wrapper != "" {
							return nil, syntaxErrf(filename, curLine, "<%s> has both %s and %s; put one on an element inside the other, such as <div ssr:if=\"…\"><p ssr:for=\"…\">", tag, wrapper, a.key)
						}
						wrapper = a.key
					}
					switch key := string(key[4:]); key {
					case "bind":
						// Record ssr:bind reference for generator validation (E05, E06).
						ssrBindRefs = append(ssrBindRefs, SsrBindRef{
							File:    filename,
							Line:    curLine,
							VarName: string(value),
						})
						// Preserve the ssr:bind attribute in the rendered HTML so the
						// TypeScript runtime's wireSsrBindElements() can discover it via
						// el.getAttribute('ssr:bind'). Without this the attribute is
						// consumed silently and the input is never wired.
						n.Attributes = append(n.Attributes, node.HtmlAttribute{
							Key:    "ssr:bind",
							Values: []node.Node{&node.Text{Text: string(value)}},
						})
						bindElements = append(bindElements, n)
						continue
					case "for":
						if err := checkLoopNames(filename, curLine, string(value)); err != nil {
							return nil, err
						}
						expr, err := parseText(string(value), filename, curLine, true)
						if err != nil {
							return nil, err
						}
						loop, ok := expr[0].(*node.Loop)
						if !ok {
							return nil, syntaxErrf(filename, curLine, "invalid ssr:for expression %q, expected \"item in collection\" or \"i, item in collection\"", string(value))
						}
						loop.Children = []node.Node{n}
						stack.Top().AddChildren(loop)
					case "if", "else", "else-if":
						if err := addBranch(stack, filename, curLine, key, string(value), n); err != nil {
							return nil, err
						}
					default:
						return nil, syntaxErrf(filename, curLine, "unknown GoSSR attribute \"ssr:%s\"", key)
					}
					continue
				}

				// Fix assets for local images
				if string(key) == "src" && n.TagName == "img" {
					value = []byte(imageResolver(string(value)))
				}

				nodes, err := attrValues(filename, curLine, tag, string(key), string(value), a.quoted)
				if err != nil {
					return nil, err
				}

				n.Attributes = append(n.Attributes, node.HtmlAttribute{
					Key:    string(key),
					Values: nodes,
				})
			}

			if wrapper != "" {
				wrapped[n] = true
			} else {
				stack.Top().AddChildren(n)
			}
			switch {
			case rawTags[tag]:
				raw, rawLine, rawClosed = tag, curLine, tokenType == html.SelfClosingTagToken
				rawData = tag == "script" && isDataBlock(n)
				stack.Push(n)
			case tokenType == html.StartTagToken && !htmlutils.VoidElements[tag]:
				stack.Push(n)
			}
		case html.EndTagToken:
			tagName, _ := tok.TagName()
			name := string(tagName)
			// An end tag closes the nearest open element of its name and every element
			// opened after it; one that matches no open element is ignored.
			for i := len(stack) - 1; i > 0; i-- {
				if tagOf(stack[i]) != name {
					continue
				}
				for _, open := range stack[i:] {
					if _, ok := open.(*node.SsrForm); ok {
						activeForm = nil
					}
				}
				stack = stack[:i]
				break
			}

		case html.CommentToken:
			// Ignore comments
		case html.DoctypeToken:
			stack.Top().AddChildren(&node.HtmlRaw{
				BaseNode: node.BaseNode{File: filename, Line: curLine},
				Data:     string(tok.Raw()),
			})
		}
		curLine += tokenLines
	}
}

// addBranch adds n to the template as the branch that the attribute ssr:key with value opens:
// ssr:if starts a condition, ssr:else-if and ssr:else extend the one directly before n.
func addBranch(stack NodesStack, filename string, line int, key, value string, n node.Node) error {
	if key == "if" {
		expr, err := parseText(value, filename, line, true)
		if err != nil {
			return err
		}
		stack.Top().AddChildren(&node.SsrCondition{
			BaseNode: node.BaseNode{File: filename, Line: line},
			Conditions: []node.SsrConditionData{{
				BaseNode:  node.BaseNode{File: filename, Line: line},
				Condition: expr[0],
				Body:      n,
			}}},
		)
		return nil
	}
	elseErr := syntaxErrf(filename, line, "ssr:%s must directly follow an element with ssr:if or ssr:else-if", key)
	lastChild := stack.Top().LastChild()
	if lastChild == nil {
		return elseErr
	}
	for {
		nodeText, ok := lastChild.(*node.Text)
		if !ok || strings.TrimSpace(nodeText.Text) != "" {
			break
		}
		stack.Top().PopChild()
		lastChild = stack.Top().LastChild()
		if lastChild == nil {
			return elseErr
		}
	}
	nodeCond, ok := lastChild.(*node.SsrCondition)
	if !ok {
		return elseErr
	}
	if key == "else" {
		nodeCond.ElseBody = n
		return nil
	}
	expr, err := parseText(value, filename, line, true)
	if err != nil {
		return err
	}
	nodeCond.Conditions = append(nodeCond.Conditions, node.SsrConditionData{
		BaseNode:  node.BaseNode{File: filename, Line: line},
		Condition: expr[0],
		Body:      n,
	})
	return nil
}

// checkLiveBlocks refuses what a live update cannot render again from the route's data alone,
// which holds neither a form's values nor its token, nor the child page or the page's assets:
// an <ssr:form>, <ssr:content/> or <ssr:assets/> in a live block; inside a form, a live site
// that reads the form or holds one of its fields; and a form or field whose attribute reads a
// live variable. A live site is what AnnotateBindings makes one: a value that reads a live
// variable, and a block, an ssr:if chain or an ssr:for that reads one or an element with an
// attribute that does.
func checkLiveBlocks(nodes []node.Node, vars []Variable) error {
	live := map[string]bool{}
	for _, v := range vars {
		if v.Reactive {
			live[v.Name] = true
		}
	}
	if len(live) == 0 {
		return nil
	}
	var walk func(nodes []node.Node, inForm bool) error
	walk = func(nodes []node.Node, inForm bool) error {
		for _, n := range nodes {
			var refs []string
			var inner []node.Node
			switch v := n.(type) {
			case *node.Expression, *node.RawExpression:
				refs = v.CollectVarRefs(live)
			case *node.HtmlElement:
				refs, inner = v.CollectAttributeVarRefs(live), v.Children
			case *node.SsrCondition:
				refs = v.CollectVarRefs(live)
				for _, c := range v.Conditions {
					inner = append(inner, c.Body)
				}
				if v.ElseBody != nil {
					inner = append(inner, v.ElseBody)
				}
			case *node.Loop:
				refs, inner = v.CollectVarRefs(live), v.Children
			case *node.Content:
				inner = v.Children
			case *node.SsrForm, *node.SsrInput, *node.SsrSelect, *node.SsrTextarea:
				var attrs []node.Node
				if f, ok := v.(*node.SsrForm); ok {
					for _, a := range f.Attributes {
						attrs = append(attrs, a.Values...)
					}
				} else {
					attrs = subNodes(v)
				}
				if r := (&node.Content{Children: attrs}).CollectVarRefs(live); len(r) > 0 {
					file, line := v.(positioned).Position()
					return syntaxErrf(file, line, "%s reads the live variable %s in an attribute, and a live update cannot render a form or its fields; drop reactive=\"true\" or read the variable on an element inside the form", describeNode(v), r[0])
				}
				if f, ok := v.(*node.SsrForm); ok {
					if err := walk(f.Children, true); err != nil {
						return err
					}
				}
				continue
			}
			if len(refs) == 0 {
				if err := walk(inner, inForm); err != nil {
					return err
				}
				continue
			}
			if f := firstNode(inner, isUnrenderable); f != nil {
				file, line := f.(positioned).Position()
				return syntaxErrf(file, line, "%s is in a block that the live variable %s renders again, and a live update cannot render %s; move it out of the block or drop reactive=\"true\"", describeNode(f), refs[0], unrenderable(f))
			}
			if !inForm {
				continue
			}
			if f := firstNode([]node.Node{n}, readsForm); f != nil {
				file, line := f.(positioned).Position()
				return syntaxErrf(file, line, "%s reads the form in a site that the live variable %s renders again, and a live update has no form values; read the form outside the site or drop reactive=\"true\"", describeNode(f), refs[0])
			}
		}
		return nil
	}
	return walk(nodes, false)
}

// positioned is a node with the place in the template it comes from.
type positioned interface{ Position() (string, int) }

// firstNode returns the first of nodes and the nodes inside them, in document order, that
// matches, or nil.
func firstNode(nodes []node.Node, match func(node.Node) bool) node.Node {
	for _, n := range nodes {
		if match(n) {
			return n
		}
		if f := firstNode(subNodes(n), match); f != nil {
			return f
		}
	}
	return nil
}

// isUnrenderable reports whether n is a node that a live update cannot render.
func isUnrenderable(n node.Node) bool { return unrenderable(n) != "" }

// unrenderable names what a live update would have to render to render n, or returns "".
func unrenderable(n node.Node) string {
	switch n.(type) {
	case *node.SsrForm:
		return "a form"
	case *node.SsrContent:
		return "the child page"
	case *node.SsrAssets:
		return "the page's assets"
	}
	return ""
}

// readsForm reports whether n reads the values of the form it is in: a value named form,
// input or textarea, or a form field.
func readsForm(n node.Node) bool {
	switch v := n.(type) {
	case *node.Variable:
		return v.Name == "form" || v.Name == "input" || v.Name == "textarea"
	case *node.SsrInput, *node.SsrSelect, *node.SsrTextarea:
		return true
	}
	return false
}

// describeNode names n in an error message.
func describeNode(n node.Node) string {
	switch v := n.(type) {
	case *node.SsrForm:
		return fmt.Sprintf("<ssr:form name=%q>", v.Name)
	case *node.SsrInput:
		return fmt.Sprintf("<ssr:input name=%q>", v.Name)
	case *node.SsrSelect:
		return fmt.Sprintf("<ssr:select name=%q>", v.Name)
	case *node.SsrTextarea:
		return fmt.Sprintf("<ssr:textarea name=%q>", v.Name)
	case *node.SsrContent:
		return "<ssr:content/>"
	case *node.SsrAssets:
		return "<ssr:assets/>"
	case *node.Variable:
		return fmt.Sprintf("%q", v.Name)
	}
	return "this"
}

// ssrTags are the GoSSR tags, without their ssr: prefix.
var ssrTags = map[string]bool{
	"var": true, "content": true, "assets": true, "form": true,
	"input": true, "select": true, "textarea": true, "call": true,
}

// ssrTagCondition refuses an ssr: attribute that the GoSSR tag <ssr:tag> does not take, and
// returns the one ssr:if, ssr:else-if or ssr:else of an <ssr:form>, or nil. ssr:bind on a form
// field and every attribute of <ssr:call> are checked with their tags.
func ssrTagCondition(filename string, line int, tag string, attrs []tagAttr) (*tagAttr, error) {
	if tag == "call" {
		return nil, nil
	}
	var cond *tagAttr
	for i, a := range attrs {
		if !strings.HasPrefix(a.key, "ssr:") {
			continue
		}
		switch {
		case tag == "form" && (a.key == "ssr:if" || a.key == "ssr:else-if" || a.key == "ssr:else"):
			if cond != nil {
				return nil, syntaxErrf(filename, line, "<ssr:form> has both %s and %s; it takes one of ssr:if, ssr:else-if and ssr:else", cond.key, a.key)
			}
			cond = &attrs[i]
		case a.key == "ssr:bind" && (tag == "input" || tag == "select" || tag == "textarea"):
		case tag == "form":
			return nil, syntaxErrf(filename, line, "unknown GoSSR attribute %q on <ssr:form>; it takes ssr:if, ssr:else-if and ssr:else", a.key)
		default:
			return nil, syntaxErrf(filename, line, "unknown GoSSR attribute %q on <ssr:%s>, which takes no ssr: attribute", a.key, tag)
		}
	}
	return cond, nil
}

const fixCall = `declare <ssr:call name="lowerCamel" in="Type" out="Type"/> once, at the template's top level`

var reCallName = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// parseCall reads the <ssr:call> with attrs at line. It refuses the tag inside an element
// with ssr:if, ssr:else-if, ssr:else or ssr:for or inside an <ssr:form>, and a name that
// one of calls, those declared before it, has.
func parseCall(filename string, line int, attrs []tagAttr, stack NodesStack, wrapped map[*node.HtmlElement]bool, calls []*node.SsrCall) (*node.SsrCall, error) {
	call := &node.SsrCall{BaseNode: node.BaseNode{File: filename, Line: line}}
	set := map[string]bool{}
	for _, a := range attrs {
		switch a.key {
		case "name":
			call.Name = a.value
		case "in":
			call.In = a.value
		case "out":
			call.Out = a.value
		default:
			return nil, refuse(filename, line, "E19", fixCall, fmt.Sprintf("<ssr:call> has the unknown attribute %q", a.key))
		}
		set[a.key] = true
	}
	switch {
	case call.Name == "":
		return nil, refuse(filename, line, "E19", fixCall, "<ssr:call> has no name")
	case !reCallName.MatchString(call.Name):
		return nil, refuse(filename, line, "E19", fixCall, fmt.Sprintf("<ssr:call> has the name %q; a name is an ASCII letter in lower case followed by letters and digits", call.Name))
	}
	for _, t := range []struct{ attr, value string }{{"in", call.In}, {"out", call.Out}} {
		if !set[t.attr] || strings.TrimSpace(t.value) == "" {
			return nil, refuse(filename, line, "E19", fixCall, fmt.Sprintf("<ssr:call name=%q> has no %s type; write struct{} for none", call.Name, t.attr))
		}
		if !isGoType(t.value) {
			return nil, refuse(filename, line, "E19", fixCall, fmt.Sprintf("<ssr:call name=%q>: the %s type %q is not a Go type", call.Name, t.attr, t.value))
		}
	}
	for _, open := range stack {
		el, isElement := open.(*node.HtmlElement)
		if _, isForm := open.(*node.SsrForm); isForm || isElement && wrapped[el] {
			return nil, refuse(filename, line, "E19", fixCall, fmt.Sprintf("<ssr:call name=%q> is inside ssr:if, ssr:for or <ssr:form>", call.Name))
		}
	}
	for _, c := range calls {
		if c.Name == call.Name {
			return nil, refuse(filename, line, "E19", fixCall, fmt.Sprintf("<ssr:call name=%q> is declared twice; the first is on line %d", call.Name, c.Line))
		}
	}
	return call, nil
}

// isGoType reports whether s is written as a Go type.
func isGoType(s string) bool {
	e, err := parser.ParseExpr(s)
	return err == nil && isTypeExpr(e)
}

func isTypeExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident, *ast.StructType, *ast.InterfaceType, *ast.FuncType:
		return true
	case *ast.SelectorExpr:
		_, ok := t.X.(*ast.Ident)
		return ok
	case *ast.ParenExpr:
		return isTypeExpr(t.X)
	case *ast.StarExpr:
		return isTypeExpr(t.X)
	case *ast.ArrayType:
		return isTypeExpr(t.Elt)
	case *ast.MapType:
		return isTypeExpr(t.Key) && isTypeExpr(t.Value)
	case *ast.ChanType:
		return isTypeExpr(t.Value)
	case *ast.IndexExpr:
		return isTypeExpr(t.X) && isTypeExpr(t.Index)
	case *ast.IndexListExpr:
		if !isTypeExpr(t.X) {
			return false
		}
		for _, i := range t.Indices {
			if !isTypeExpr(i) {
				return false
			}
		}
		return true
	}
	return false
}

// tagAttr is an attribute of the current start tag.
type tagAttr struct {
	key, value string
	quoted     bool
}

// readAttrs returns the attributes of the current start tag of tok. It refuses a tag whose
// attributes the tokenizer and scanAttrs read differently, since then it cannot tell which
// values are quoted.
func readAttrs(tok *html.Tokenizer, hasAttrs bool, filename string, line int, tag string) ([]tagAttr, error) {
	scanned := scanAttrs(tok.Raw())
	// Browsers keep the first of two attributes with one name, other parsers the last.
	for _, a := range scanned {
		if a.dup {
			return nil, syntaxErrf(filename, line, "<%s> has the attribute %s twice", tag, a.name)
		}
	}
	var attrs []tagAttr
	for more := hasAttrs; more; {
		var k, v []byte
		k, v, more = tok.TagAttr()
		attrs = append(attrs, tagAttr{key: string(k), value: string(v)})
	}
	if len(attrs) != len(scanned) {
		return nil, syntaxErrf(filename, line, "cannot read the attributes of <%s>", tag)
	}
	for i := range attrs {
		if attrs[i].key != scanned[i].name {
			return nil, syntaxErrf(filename, line, "cannot read the attributes of <%s>", tag)
		}
		if strings.ContainsRune(attrs[i].key, 0) {
			return nil, syntaxErrf(filename, line, "<%s> has an attribute name with a NUL character", tag)
		}
		if attrs[i].key == bindRouteAttr {
			return nil, syntaxErrf(filename, line, "<%s> sets %s, which the generator writes on elements with ssr:bind; remove it", tag, bindRouteAttr)
		}
		attrs[i].quoted = scanned[i].quoted
	}
	return attrs, nil
}

// inputTypes are the type values of an HTML <input>, "" for the default.
var inputTypes = map[string]bool{
	"": true, "button": true, "checkbox": true, "color": true, "date": true, "datetime-local": true,
	"email": true, "file": true, "hidden": true, "image": true, "month": true, "number": true,
	"password": true, "radio": true, "range": true, "reset": true, "search": true, "submit": true,
	"tel": true, "text": true, "time": true, "url": true, "week": true,
}

// goLiteral returns the Go literal of value as a goType, the type of a form field, and whether
// value is one.
func goLiteral(goType, value string) (string, bool) {
	bits := 0
	if i := strings.IndexAny(goType, "0123456789"); i >= 0 {
		bits, _ = strconv.Atoi(goType[i:])
	}
	switch goType {
	case "string":
		return strconv.Quote(value), true
	case "bool":
		b, err := strconv.ParseBool(value)
		return strconv.FormatBool(b), err == nil
	case "int", "int8", "int16", "int32", "int64":
		i, err := strconv.ParseInt(value, 10, bits)
		return strconv.FormatInt(i, 10), err == nil
	case "uint", "uint8", "uint16", "uint32", "uint64":
		u, err := strconv.ParseUint(value, 10, bits)
		return strconv.FormatUint(u, 10), err == nil
	case "float32", "float64":
		f, err := strconv.ParseFloat(value, bits)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return "", false
		}
		return strconv.FormatFloat(f, 'g', -1, bits), true
	}
	return "", false
}

// isDataBlock reports whether the <script> element n holds data rather than code.
func isDataBlock(n *node.HtmlElement) bool {
	for _, a := range n.Attributes {
		if a.Key == "type" {
			switch strings.ToLower(strings.Trim(literalText(a.Values), " \t\n\f\r")) {
			case "application/json", "application/ld+json":
				return true
			}
		}
	}
	return false
}

func tagOf(n node.WithChildren) string {
	switch n := n.(type) {
	case *node.HtmlElement:
		return n.TagName
	case *node.SsrForm:
		return "ssr:form"
	}
	return ""
}

func (t *Template) GetVariables() []Variable {
	sort.Slice(t.variables, func(i, j int) bool {
		return t.variables[i].Name < t.variables[j].Name
	})
	return t.variables
}

func (t *Template) GetForms() []*Form {
	sort.Slice(t.forms, func(i, j int) bool {
		return t.forms[i].Name < t.forms[j].Name
	})
	return t.forms
}

func (t *Template) GetContentNode() *node.SsrContent { return t.contentNode }

// GetCalls returns the template's <ssr:call> declarations, sorted by name.
func (t *Template) GetCalls() []*node.SsrCall {
	sort.Slice(t.calls, func(i, j int) bool { return t.calls[i].Name < t.calls[j].Name })
	return t.calls
}

// GetSsrBindRefs returns all ssr:bind references found on native HTML elements.
func (t *Template) GetSsrBindRefs() []SsrBindRef { return t.ssrBindRefs }

// bindRouteAttr names the route whose client runtime owns an ssr:bind element.
const bindRouteAttr = "data-ssr-route"

// MarkBindRoute writes data-ssr-route="routeKey" on every element with ssr:bind,
// so that only the client runtime of the route with that key wires it.
func (t *Template) MarkBindRoute(routeKey string) {
	for _, el := range t.bindElements {
		attr := node.HtmlAttribute{Key: bindRouteAttr, Values: []node.Node{&node.Text{Text: routeKey}}}
		if i := slices.IndexFunc(el.Attributes, func(a node.HtmlAttribute) bool { return a.Key == bindRouteAttr }); i >= 0 {
			el.Attributes[i] = attr
		} else {
			el.Attributes = append(el.Attributes, attr)
		}
	}
}

// GetNodes returns the top-level AST nodes of the template.
// Used by the generator's CollectVarRefs analysis pass.
func (t *Template) GetNodes() []node.Node { return t.nodes }

// CollectAllVarRefs returns the union of all reactive variable names referenced
// anywhere in the template AST (including conditions, loop arrays, etc.).
func (t *Template) CollectAllVarRefs(reactive map[string]bool) []string {
	sets := make([][]string, len(t.nodes))
	for i, n := range t.nodes {
		sets[i] = n.CollectVarRefs(reactive)
	}
	return node.UnionRefs(sets...)
}

func (t *Template) WriteGoCode(buf *gobuf.GoBuf) {
	for _, n := range t.nodes {
		n.WriteGoCode(buf)
	}
}

// CollectReactiveNodes walks the annotated template AST and returns every
// reactive binding site node keyed by its binding/block key. This is used by
// the code generator to emit per-site renderBlock_KEY helper functions.
//
// Must be called after AnnotateBindings has been run on the template.
func (t *Template) CollectReactiveNodes() map[string]node.Node {
	result := make(map[string]node.Node)
	collectReactiveNodesIn(t.nodes, result)
	return result
}

func collectReactiveNodesIn(nodes []node.Node, result map[string]node.Node) {
	for _, n := range nodes {
		collectReactiveNode(n, result)
	}
}

func collectReactiveNode(n node.Node, result map[string]node.Node) {
	switch v := n.(type) {
	case *node.Expression:
		if v.BindingKey != "" {
			result[v.BindingKey] = v
		}
	case *node.RawExpression:
		if v.BindingKey != "" {
			result[v.BindingKey] = v
		}
	case *node.SsrCondition:
		if v.BlockKey != "" {
			result[v.BlockKey] = v
		}
		// Always recurse into branches so nested blocks are found too.
		for _, c := range v.Conditions {
			collectReactiveNode(c.Body, result)
		}
		if v.ElseBody != nil {
			collectReactiveNode(v.ElseBody, result)
		}
	case *node.Loop:
		if v.BlockKey != "" {
			result[v.BlockKey] = v
		}
		// Recurse into loop body so nested blocks are found.
		collectReactiveNodesIn(v.Children, result)
	case *node.HtmlElement:
		if v.BlockKey != "" {
			result[v.BlockKey] = v
		}
		collectReactiveNodesIn(v.Children, result)
	case *node.Content:
		collectReactiveNodesIn(v.Children, result)
	case *node.SsrForm:
		collectReactiveNodesIn(v.Children, result)
	}
}

// blockNodeKey returns a SHA256[:16] hex key for a reactive block node
// (SsrCondition or Loop). The hash input is "file:line:type:ordinal" where
// ordinal is a per-template counter that increments each time a new block key
// is assigned. This guarantees uniqueness even when two reactive blocks share
// the same source line (e.g., nested inline ssr:if attributes). The ordinal
// is deterministic within one AnnotateBindings call because the AST is walked
// depth-first in a fixed order.
func blockNodeKey(file string, line int, nodeType string, ordinal int) string {
	input := file + ":" + strconv.Itoa(line) + ":" + nodeType + ":" + strconv.Itoa(ordinal)
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum[:8]) // 8 bytes = 16 hex chars
}

// AnnotateBindings walks the template AST and sets BindingKey on every
// Expression and RawExpression node that references at least one reactive
// variable, and sets BlockKey on SsrCondition and Loop nodes whose subtrees
// reference reactive variables. Returns the complete binding-key→vars map
// that the generated server code uses to re-render sites on state change.
//
// reactive is the map of reactive variable names (built from <ssr:var reactive="true">).
// keyer is called with the sorted slice of reactive var names the expression
// references and its source, prefixed with "$" for a {{$ }} site; it returns
// the LOCAL (un-prefixed) binding key string.
// routeKeyPrefix is prepended to every emitted key to produce the namespaced
// wire key (routeKey + "." + localKey). Pass "" for tests or
// single-route pages where namespacing is still applied consistently.
//
// The returned map uses NAMESPACED keys (routeKeyPrefix + "." + localKey)
// as map keys, with the reactive var names as values.
//
// Inner-wrapper suppression rule: when a node is already covered by
// an enclosing reactive block (a SsrCondition or Loop with BlockKey set), the
// inner Expression / RawExpression nodes are NOT given a BindingKey and are
// NOT registered in the dependency map. The outer block's renderBlock_<KEY>
// re-renders the entire subtree on state change.
func (t *Template) AnnotateBindings(reactive map[string]bool, keyer func(refs []string, exprSrc string) string, routeKeyPrefix string) map[string][]string {
	bindings := make(map[string][]string)
	counter := 0
	annotateBindingsNodes(t.nodes, reactive, keyer, routeKeyPrefix, bindings, false, &counter)
	return bindings
}

func annotateBindingsNodes(nodes []node.Node, reactive map[string]bool, keyer func([]string, string) string, routeKeyPrefix string, bindings map[string][]string, insideBlock bool, counter *int) {
	for _, n := range nodes {
		annotateBindingsNode(n, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
	}
}

// namespacedKey prepends the routeKeyPrefix to a local key to produce a
// fully-namespaced wire key. If routeKeyPrefix is empty, localKey is returned
// as-is (for backwards compatibility in contexts where no prefix is set).
func namespacedKey(routeKeyPrefix, localKey string) string {
	if routeKeyPrefix == "" {
		return localKey
	}
	return routeKeyPrefix + "." + localKey
}

func annotateBindingsNode(n node.Node, reactive map[string]bool, keyer func([]string, string) string, routeKeyPrefix string, bindings map[string][]string, insideBlock bool, counter *int) {
	switch v := n.(type) {
	case *node.Expression:
		if insideBlock {
			// Suppressed: covered by enclosing reactive block.
			// BindingKey stays empty — no <span data-ssr-bind> wrapper emitted.
			return
		}
		refs := v.CollectVarRefs(reactive)
		if len(refs) > 0 {
			// NOTE: v.Source must be non-empty for composite (multi-ref) expressions
			// so that keyer produces a unique SHA-256 key per expression site.
			// v.Source is populated at parse time by assignExprSources in parseText.
			localKey := keyer(refs, v.Source)
			nsKey := namespacedKey(routeKeyPrefix, localKey)
			v.BindingKey = nsKey
			bindings[nsKey] = refs
		}
	case *node.RawExpression:
		if insideBlock {
			// Suppressed: covered by enclosing reactive block.
			return
		}
		refs := v.CollectVarRefs(reactive)
		if len(refs) > 0 {
			// The "$" keeps the key apart from a {{ }} site of the same source,
			// whose markup differs.
			localKey := keyer(refs, "$"+v.Source)
			nsKey := namespacedKey(routeKeyPrefix, localKey)
			v.BindingKey = nsKey
			bindings[nsKey] = refs
		}
	case *node.HtmlElement:
		// If any attribute value references a reactive variable, the whole
		// element becomes a reactive block. Attribute substrings cannot be
		// patched individually, so we wrap the element in <ssr-block> and
		// re-render the entire element on any input-variable change.
		// Children recurse with insideBlock=true so inner expression sites
		// don't double-patch — the outer block re-render covers them.
		if !insideBlock {
			if attrRefs := v.CollectAttributeVarRefs(reactive); len(attrRefs) > 0 {
				*counter++
				localKey := blockNodeKey(v.File, v.Line, "HtmlElement", *counter)
				nsKey := namespacedKey(routeKeyPrefix, localKey)
				v.BlockKey = nsKey
				bindings[nsKey] = v.CollectVarRefs(reactive)
				annotateBindingsNodes(v.Children, reactive, keyer, routeKeyPrefix, bindings, true, counter)
				return
			}
		}
		annotateBindingsNodes(v.Children, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
	case *node.Content:
		annotateBindingsNodes(v.Children, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
	case *node.SsrForm:
		annotateBindingsNodes(v.Children, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
	case *node.SsrCondition:
		refs := v.CollectVarRefs(reactive)
		if len(refs) > 0 && !insideBlock {
			// This conditional block is reactive AND not already covered by
			// an enclosing reactive block — assign its own BlockKey.
			// The ordinal counter ensures uniqueness even for same-line blocks.
			*counter++
			localKey := blockNodeKey(v.File, v.Line, "SsrCondition", *counter)
			nsKey := namespacedKey(routeKeyPrefix, localKey)
			v.BlockKey = nsKey
			bindings[nsKey] = refs
			// Recurse into branches with insideBlock=true to suppress inner
			// Expression / nested SsrCondition / nested Loop wrappers - they
			// would emit redundant <ssr-block>s and, inside table descendants,
			// trigger HTML foster-parenting which duplicates rendered rows.
			// The outer block's CollectVarRefs already covers the entire
			// subtree, so any change to a deeply-nested reactive var still
			// re-renders the right ancestor.
			for _, c := range v.Conditions {
				annotateBindingsNode(c.Body, reactive, keyer, routeKeyPrefix, bindings, true, counter)
			}
			if v.ElseBody != nil {
				annotateBindingsNode(v.ElseBody, reactive, keyer, routeKeyPrefix, bindings, true, counter)
			}
		} else {
			// Either non-reactive, or reactive-but-already-covered. Recurse
			// preserving the current insideBlock so the inner-suppression rule
			// propagates correctly.
			for _, c := range v.Conditions {
				annotateBindingsNode(c.Body, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
			}
			if v.ElseBody != nil {
				annotateBindingsNode(v.ElseBody, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
			}
		}
	case *node.Loop:
		refs := v.CollectVarRefs(reactive)
		if len(refs) > 0 && !insideBlock {
			// Reactive loop, not already covered — assign its own BlockKey.
			*counter++
			localKey := blockNodeKey(v.File, v.Line, "Loop", *counter)
			nsKey := namespacedKey(routeKeyPrefix, localKey)
			v.BlockKey = nsKey
			bindings[nsKey] = refs
			// Recurse into body with insideBlock=true to suppress inner spans.
			annotateBindingsNodes(v.Children, reactive, keyer, routeKeyPrefix, bindings, true, counter)
		} else {
			// Either non-reactive, or covered by an outer reactive block (in
			// which case emitting our own <ssr-block> would be redundant and
			// inside <tbody> would be foster-parented out by the HTML parser,
			// duplicating the rows in the rendered DOM).
			annotateBindingsNodes(v.Children, reactive, keyer, routeKeyPrefix, bindings, insideBlock, counter)
		}
	}
}

func parseText(text string, filename string, fileLine int, insideExpr bool) ([]node.Node, error) {
	lexer := &exprLex{text: text, filename: filename, curLine: fileLine, insideExpr: insideExpr}
	yyErrorVerbose = true
	//yyDebug = 5
	yyParse(lexer)

	if lexer.err != nil {
		return nil, lexer.err
	}
	if err := checkExprNames(lexer.result.Children); err != nil {
		return nil, err
	}

	// Populate Source on every Expression/RawExpression node in left-to-right
	// order so the reactive analysis pass can build per-site SHA-256 keys. The
	// lexer collected sources in the same order expressions appear in the AST.
	if len(lexer.exprSources) > 0 {
		idx := 0
		assignExprSources(lexer.result.Children, lexer.exprSources, &idx)
	}

	return lexer.result.Children, nil
}

// assignExprSources walks a slice of nodes and assigns Source strings to
// Expression and RawExpression nodes in document order. The sources slice was
// collected by exprLex in lexing order which matches document (DFS left-to-right)
// order for a flat content list.
func assignExprSources(nodes []node.Node, sources []string, idx *int) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *node.Expression:
			if *idx < len(sources) {
				v.Source = sources[*idx]
				*idx++
			}
		case *node.RawExpression:
			if *idx < len(sources) {
				v.Source = sources[*idx]
				*idx++
			}
		case *node.Content:
			assignExprSources(v.Children, sources, idx)
		case *node.HtmlElement:
			assignExprSources(v.Children, sources, idx)
		}
	}
}
