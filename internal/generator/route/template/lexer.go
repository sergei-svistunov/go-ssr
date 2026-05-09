package template

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

var YyLexDebug = false //|| true

type exprLex struct {
	filename   string
	text       string
	insideExpr bool
	curLine    int
	err        *SyntaxError
	result     *node.Content

	// exprSources accumulates the raw source text of each {{ ... }} and {{$ ... }}
	// expression in lexing order. After yyParse, parseText walks the AST in the
	// same order and assigns each source to the corresponding Expression or
	// RawExpression node so that the reactive analysis pass can compute per-site
	// SHA-256 binding keys without hash collisions.
	exprSources    []string
	exprTextAtOpen string // snapshot of x.text just after the opening {{ delimiter
}

type SyntaxError struct {
	Filename string
	Line     int
	Message  string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.Filename, e.Line, e.Message)
}

var simpleTokens = []struct {
	token string
	value int
}{
	{"==", EQ},
	{"!=", NE},
	{">=", GE},
	{"<=", LE},
	{"&&", AND},
	{"||", OR},
	{"}}", EXPR_END},
	{"!", NOT},
}

var reTokens = []struct {
	re    *regexp.Regexp
	value int
}{
	{regexp.MustCompile(`^(?i)IN(?:\s|$)`), IN},
	{regexp.MustCompile(`^"[^\\"]*(?:\\.[^\\"]*)*"`), STRING},
	{regexp.MustCompile("^`[^`]*(?:\\.[^`]*)*`"), STRING},
	{regexp.MustCompile("^'[^']*(?:\\.[^']*)*'"), STRING},
	{regexp.MustCompile(`^-?\d+(?:[.,]\d+)?`), NUMBER},
	{regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`), IDENTIFIER},
}

func (x *exprLex) Error(s string) {
	x.err = &SyntaxError{x.filename, x.curLine, s}
}

func (x *exprLex) Lex(yylval *yySymType) int {
	for {
		if len(x.text) == 0 {
			return 0
		}

		if x.insideExpr {
			for _, token := range simpleTokens {
				if strings.HasPrefix(x.text, token.token) {
					if token.value == EXPR_END {
						// Capture the expression source: everything consumed since
						// exprTextAtOpen up to (but not including) the current "}}"
						// position. The source is what was between {{ and }}.
						consumed := len(x.exprTextAtOpen) - len(x.text)
						src := strings.TrimSpace(x.exprTextAtOpen[:consumed])
						x.exprSources = append(x.exprSources, src)
						x.exprTextAtOpen = ""
					}

					x.text = x.text[len(token.token):]
					yylval.string = token.token

					if YyLexDebug {
						fmt.Println("SIMPLE TOKEN ", tokenName(token.value))
					}

					if token.value == EXPR_END {
						x.insideExpr = false
					}

					return token.value
				}
			}

			for _, token := range reTokens {
				if m := token.re.FindString(x.text); m != "" {
					x.text = x.text[len(m):]

					yylval.string = string(m)

					if YyLexDebug {
						fmt.Println("RE TOKEN ", tokenName(token.value), yylval.string)
					}

					return token.value
				}
			}

			c := x.text[0]
			x.text = x.text[1:]

			switch c {
			case ' ', '\t', '\r':
				continue
			case '\n':
				x.curLine++
				continue
			default:
				if YyLexDebug {
					fmt.Println("TOKEN ", string(c))
				}
				return int(c)
			}
		} else {
			exprPos := strings.Index(x.text, "{{")
			if exprPos == 0 {
				if len(x.text) > 2 && x.text[2] == '$' {
					yylval.string = "{{$"
					x.text = x.text[3:]
					x.insideExpr = true
					x.exprTextAtOpen = x.text // record text right after {{$

					if YyLexDebug {
						fmt.Println("RAW_EXPR_START ", yylval.string)
					}

					return RAW_EXPR_START
				}
				yylval.string = "{{"
				x.text = x.text[2:]
				x.insideExpr = true
				x.exprTextAtOpen = x.text // record text right after {{

				if YyLexDebug {
					fmt.Println("EXPR_START ", yylval.string)
				}

				return EXPR_START

			} else if exprPos < 0 {
				exprPos = len(x.text)
			}

			yylval.string = x.text[:exprPos]
			x.text = x.text[exprPos:]

			if YyLexDebug {
				fmt.Println("TEXT ", yylval.string)
			}

			return TEXT
		}
	}
}

func bn(lexer yyLexer) node.BaseNode {
	x := lexer.(*exprLex)
	return node.BaseNode{x.filename, x.curLine}
}

func tokenName(c int) string {
	if c > yyPrivate {
		c -= yyPrivate - 1
	}
	if c >= 0 && c < len(yyToknames) {
		if yyToknames[c] != "" {
			return yyToknames[c]
		}
	}
	return fmt.Sprintf("tok-%v", c)

}
