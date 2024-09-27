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
	contentNode *node.SsrContent
}

type Variable struct {
	File string
	Line int
	Name string
	Type string
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
					contentNode: contentNode,
				}, nil
			}
			return nil, fmt.Errorf("%s:%d: cannot parse HTML: %v", filename, curLine, tok.Err())
		case html.TextToken:
			nodes, err := parseText(string(tok.Text()), filename, curLine, false)
			if err != nil {
				return nil, err
			}
			stack.Top().Children = append(stack.Top().Children, nodes...)

		case html.StartTagToken, html.SelfClosingTagToken:
			tagName, hasAttrs := tok.TagName()

			if bytes.HasPrefix(tagName, []byte("ssr:")) {
				attrs := map[string]string{}
				for {
					key, value, more := tok.TagAttr()
					attrs[string(key)] = string(value)
					if !more {
						break
					}
				}

				switch string(tagName[4:]) {
				case "var":
					if attrs["name"] == "" {
						return nil, fmt.Errorf("missing var name attribute")
					}
					if attrs["type"] == "" {
						return nil, fmt.Errorf("missing var type attribute")
					}
					variables = append(variables, Variable{
						File: filename,
						Line: curLine,
						Name: attrs["name"],
						Type: attrs["type"],
					})
				case "content":
					contentNode = &node.SsrContent{
						BaseNode: node.BaseNode{filename, curLine},
						Default:  attrs["default"],
					}
					stack.Top().Children = append(stack.Top().Children, contentNode)
				case "assets":
					stack.Top().Children = append(stack.Top().Children, &node.SsrAssets{node.BaseNode{filename, curLine}})
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
							stack.Top().Children = append(stack.Top().Children, loop)
						case "if":
							expr, err := parseText(string(value), filename, curLine, true)
							if err != nil {
								return nil, err
							}
							hasWrapper = true
							stack.Top().Children = append(stack.Top().Children, &node.SsrCondition{
								BaseNode: node.BaseNode{filename, curLine},
								Conditions: []node.SsrConditionData{{
									BaseNode:  node.BaseNode{filename, curLine},
									Condition: expr[0],
									Body:      n,
								}}},
							)

						case "else", "else-if":
							if len(stack.Top().Children) == 0 {
								return nil, fmt.Errorf("invalid else condition place")
							}
							if nodeText, ok := stack.Top().Children[len(stack.Top().Children)-1].(*node.Text); ok && strings.TrimSpace(nodeText.Text) == "" {
								stack.Top().Children = stack.Top().Children[:len(stack.Top().Children)-1]
							}
							if len(stack.Top().Children) == 0 {
								return nil, fmt.Errorf("invalid else condition place")
							}
							nodeCond, ok := stack.Top().Children[len(stack.Top().Children)-1].(*node.SsrCondition)
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
				stack.Top().Children = append(stack.Top().Children, n)
			}
			if tokenType == html.StartTagToken && !htmlutils.VoidElements[n.TagName] {
				stack.Push(n)
			}
		case html.EndTagToken:
			stack.Pop()

		case html.CommentToken:
			// Ignore comments
		case html.DoctypeToken:
			stack.Top().Children = append(stack.Top().Children, &node.HtmlRaw{
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
