package template

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)

var YyLexDebug = false //|| true

type exprLex struct {
	text       string
	insideExpr bool
	err        error
	result     *node.Content
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
	x.err = fmt.Errorf("%s", s)
}

func (x *exprLex) Lex(yylval *yySymType) int {
	for {
		if len(x.text) == 0 {
			return 0
		}

		if x.insideExpr {
			for _, token := range simpleTokens {
				if strings.HasPrefix(x.text, token.token) {
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
			case ' ', '\t', '\r', '\n':
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

					if YyLexDebug {
						fmt.Println("RAW_EXPR_START ", yylval.string)
					}

					return RAW_EXPR_START
				}
				yylval.string = "{{"
				x.text = x.text[2:]
				x.insideExpr = true

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

	return 0
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
