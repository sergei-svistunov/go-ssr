//go:generate goyacc -o texty.go -v texty.output text.y

package template

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/sergei-svistunov/go-ssr/internal/generator/gobuf"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/htmlutils"
	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

type Template struct {
	nodes       []node.Node
	variables   []Variable
	forms       []*Form
	contentNode *node.SsrContent
}

type Variable struct {
	File string
	Line int
	Name string
	Type string
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
				}, nil
			}
			return nil, fmt.Errorf("%s:%d: cannot parse HTML: %v", filename, curLine, tok.Err())
		case html.TextToken:
			nodes, err := parseText(string(tok.Text()), filename, curLine, false)
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
						File: filename,
						Line: curLine,
						Name: attrsMap["name"],
						Type: attrsMap["type"],
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

func (t *Template) WriteGoCode(buf *gobuf.GoBuf) {
	for _, n := range t.nodes {
		n.WriteGoCode(buf)
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

	return lexer.result.Children, nil
}
