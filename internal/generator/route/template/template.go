//go:generate goyacc -o texty.go -v texty.output text.y

package template

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/htmlutils"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
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
	return fmt.Sprintf("%s:%d:1: reactive-bindings: ssr:bind is not valid on GoSSR form primitive <ssr:%s>; use a native HTML <%s> element instead",
		e.File, e.Line, e.Element, e.Element)
}

type Template struct {
	nodes       []node.Node
	variables   []Variable
	forms       []*Form
	contentNode *node.SsrContent
	ssrBindRefs []SsrBindRef
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
		variables   []Variable
		forms       []*Form
		activeForm  *Form
		contentNode *node.SsrContent
		ssrBindRefs []SsrBindRef
	)

	for {
		tokenType := tok.Next()
		tokenLines := bytes.Count(tok.Raw(), []byte{'\n'})
		switch tokenType {
		case html.ErrorToken:
			if tok.Err() == io.EOF {
				return &Template{
					nodes:       rootNode.Children,
					variables:   variables,
					forms:       forms,
					contentNode: contentNode,
					ssrBindRefs: ssrBindRefs,
				}, nil
			}
			return nil, fmt.Errorf("%s:%d: cannot parse HTML: %v", filename, curLine, tok.Err())
		case html.TextToken:
			text := string(tok.Text())
			if !stack.isWhitespacePreserved() {
				if strings.TrimSpace(text) == "" {
					break
				}
				text = reWhitespace.ReplaceAllString(text, " ")
			}
			nodes, err := parseText(text, filename, curLine, false)
			if err != nil {
				return nil, err
			}
			stack.Top().AddChildren(nodes...)

		case html.StartTagToken, html.SelfClosingTagToken:
			tagName, hasAttrs := tok.TagName()

			if bytes.HasPrefix(tagName, []byte("ssr:")) {
				attrsMap := map[string]string{}
				attributes := make([]node.HtmlAttribute, 0, len(attrsMap))
				for {
					key, value, more := tok.TagAttr()
					attrsMap[string(key)] = string(value)
					var nodes []node.Node
					if len(value) > 0 {
						n, err := parseText(string(value), filename, curLine, false)
						if err != nil {
							return nil, err
						}
						nodes = n
					}

					attributes = append(attributes, node.HtmlAttribute{
						Key:    string(key),
						Values: nodes,
					})
					if !more {
						break
					}
				}

				switch string(tagName[4:]) {
				case "var":
					if attrsMap["name"] == "" {
						return nil, fmt.Errorf("missing var name attribute")
					}
					if attrsMap["type"] == "" {
						return nil, fmt.Errorf("missing var type attribute")
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
						BaseNode: node.BaseNode{filename, curLine},
						Default:  attrsMap["default"],
					}
					stack.Top().AddChildren(contentNode)
				case "assets":
					stack.Top().AddChildren(&node.SsrAssets{node.BaseNode{filename, curLine}})
				case "form":
					if activeForm != nil {
						return nil, fmt.Errorf("<ssr:form> is inside <ssr:form>")
					}

					if attrsMap["enctype"] != "" && attrsMap["enctype"] != node.FormEncUrlEncoded && attrsMap["enctype"] != node.FormEncTypeMultipart {
						return nil, fmt.Errorf("<ssr:form> has an invalid enctype, must be '%s' or '%s'",
							node.FormEncUrlEncoded, node.FormEncTypeMultipart)
					}

					formNode := &node.SsrForm{
						BaseNode:   node.BaseNode{filename, curLine},
						Name:       attrsMap["name"],
						EncType:    attrsMap["enctype"],
						Attributes: attributes,
					}
					forms = append(forms, &Form{Name: attrsMap["name"], Node: formNode})
					activeForm = forms[len(forms)-1]

					stack.Top().AddChildren(formNode)
					stack.Push(formNode)
				case "input", "textarea", "select":
					if activeForm == nil {
						return nil, fmt.Errorf("<ssr:%s> is not inside <ssr:form>", tagName[4:])
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

					_, isRequired := attrsMap["required"]
					_, isMultiple := attrsMap["multiple"]
					goType := attrsMap["gotype"]
					if goType == "" {
						goType = "string"
					}
					if _, exists := formElementGoTypes[goType]; !exists {
						return nil, fmt.Errorf("unknown gotype '%s'", goType)
					}

					var (
						n        node.Node
						nodeType FormElementType
					)

					switch string(tagName[4:]) {
					case "input":
						if attrsMap["type"] == "file" {
							if activeForm.Node.EncType == "" {
								activeForm.Node.EncType = node.FormEncTypeMultipart
							}
							if activeForm.Node.EncType != node.FormEncTypeMultipart {
								return nil, fmt.Errorf("invalid enctype '%s' for an input with type file, must be '%s'",
									activeForm.Node.EncType, node.FormEncTypeMultipart)
							}
						}

						n = &node.SsrInput{
							BaseNode:   node.BaseNode{filename, curLine},
							Name:       attrsMap["name"],
							Type:       attrsMap["type"],
							Value:      attrsMap["value"],
							Attributes: attributes,
						}
						if attrsMap["type"] == "file" {
							nodeType = FormElementInputFile
						} else {
							nodeType = FormElementInput
						}
					case "textarea":
						n = &node.SsrTextarea{
							BaseNode:   node.BaseNode{filename, curLine},
							Name:       attrsMap["name"],
							Attributes: attributes,
						}
						nodeType = FormElementTextarea
					case "select":
						n = &node.SsrSelect{
							BaseNode:   node.BaseNode{filename, curLine},
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
							return nil, fmt.Errorf("form elements with name '%s' have different gotypes", elementByName.Name)
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
							return nil, fmt.Errorf("form contains at least 2 elements with name '%s'", attrsMap["name"])
						}
					} else {
						return nil, fmt.Errorf("form contains at least 2 elements with name '%s'", attrsMap["name"])
					}

					stack.Top().AddChildren(n)
				default:
					return nil, fmt.Errorf("invalid tag name: %s", tagName)
				}

				continue
			}

			n := &node.HtmlElement{
				BaseNode:   node.BaseNode{filename, curLine},
				TagName:    string(tagName),
				SelfClosed: tokenType == html.SelfClosingTagToken,
			}
			hasWrapper := false
			if hasAttrs {
				for {
					key, value, more := tok.TagAttr()

					// Process SSR attributes
					if bytes.HasPrefix(key, []byte("ssr:")) {
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
							// consumed silently and the input is never wired (Bug 1).
							n.Attributes = append(n.Attributes, node.HtmlAttribute{
								Key: "ssr:bind",
								Values: []node.Node{&node.Text{Text: string(value)}},
							})
							if !more {
								break
							}
							continue
						case "for":
							expr, err := parseText(string(value), filename, curLine, true)
							if err != nil {
								return nil, err
							}
							loop, ok := expr[0].(*node.Loop)
							if !ok {
								return nil, fmt.Errorf("invalid loop expression")
							}
							loop.Children = []node.Node{n}
							hasWrapper = true
							stack.Top().AddChildren(loop)
						case "if":
							expr, err := parseText(string(value), filename, curLine, true)
							if err != nil {
								return nil, err
							}
							hasWrapper = true
							stack.Top().AddChildren(&node.SsrCondition{
								BaseNode: node.BaseNode{filename, curLine},
								Conditions: []node.SsrConditionData{{
									BaseNode:  node.BaseNode{filename, curLine},
									Condition: expr[0],
									Body:      n,
								}}},
							)

						case "else", "else-if":
							lastChild := stack.Top().LastChild()
							if lastChild == nil {
								return nil, fmt.Errorf("invalid else condition place")
							}
							for {
								nodeText, ok := lastChild.(*node.Text)
								if !ok || strings.TrimSpace(nodeText.Text) != "" {
									break
								}
								stack.Top().PopChild()
								lastChild = stack.Top().LastChild()
								if lastChild == nil {
									return nil, fmt.Errorf("invalid else condition place")
								}
							}
							nodeCond, ok := lastChild.(*node.SsrCondition)
							if !ok {
								return nil, fmt.Errorf("invalid else condition place")
							}
							hasWrapper = true
							if key == "else" {
								nodeCond.ElseBody = n
							} else {
								expr, err := parseText(string(value), filename, curLine, true)
								if err != nil {
									return nil, err
								}
								nodeCond.Conditions = append(nodeCond.Conditions, node.SsrConditionData{
									BaseNode:  node.BaseNode{filename, curLine},
									Condition: expr[0],
									Body:      n,
								})
							}
						default:
							return nil, fmt.Errorf("invalid attribute name: \"%s\"", key)
						}
						if more {
							continue
						}
						break
					}

					// Fix assets for local images
					if string(key) == "src" && n.TagName == "img" {
						value = []byte(imageResolver(string(value)))
					}

					var nodes []node.Node
					if len(value) > 0 {
						n, err := parseText(string(value), filename, curLine, false)
						if err != nil {
							return nil, err
						}
						nodes = n
					}

					n.Attributes = append(n.Attributes, node.HtmlAttribute{
						Key:    string(key),
						Values: nodes,
					})
					if !more {
						break
					}
				}
			}

			if !hasWrapper {
				stack.Top().AddChildren(n)
			}
			if tokenType == html.StartTagToken && !htmlutils.VoidElements[n.TagName] {
				stack.Push(n)
			}
		case html.EndTagToken:
			tagName, _ := tok.TagName()
			if string(tagName) == "ssr:form" {
				activeForm = nil
			}
			stack.Pop()

		case html.CommentToken:
			// Ignore comments
		case html.DoctypeToken:
			stack.Top().AddChildren(&node.HtmlRaw{
				BaseNode: node.BaseNode{filename, curLine},
				Data:     string(tok.Raw()),
			})
		}
		curLine += tokenLines
	}
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

// GetSsrBindRefs returns all ssr:bind references found on native HTML elements.
func (t *Template) GetSsrBindRefs() []SsrBindRef { return t.ssrBindRefs }

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
// references; it returns the LOCAL (un-prefixed) binding key string.
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
			// NOTE: v.Source must be non-empty for composite (multi-ref) expressions.
			localKey := keyer(refs, v.Source)
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
