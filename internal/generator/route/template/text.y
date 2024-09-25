%{
package template;

import (
        __yyfmt__ "fmt"
        "github.com/sergei-svistunov/go-ssr/internal/generator/route/template/node"
)
%}

%union {
        string                  string
        Node                    node.Node
        NodeContent             *node.Content
        NodeExpressionsList     *node.ExpressionsList
}

%token  TEXT EXPR_START EXPR_END RAW_EXPR_START IDENTIFIER STRING NUMBER IN
%token  EQ NE GE LE OR AND NOT

%type   <string>                TEXT STRING IDENTIFIER NUMBER
%type   <NodeContent>           top content
%type   <Node>                  expr loop
%type   <NodeExpressionsList>   expr_list

%left '+' '-' '*' '/' '%' '>' '<' EQ NE GE LE OR AND
%left NOT

%%

top:            content                                 { yylex.(*exprLex).result = $$  }

content:        TEXT                                    { $$ = &node.Content{[]node.Node{&node.Text{$1}}} }
|               EXPR_START expr EXPR_END                { $$ = &node.Content{[]node.Node{&node.Expression{$2}}} }
|               RAW_EXPR_START expr EXPR_END            { $$ = &node.Content{[]node.Node{&node.RawExpression{$2}}} }
|               content TEXT                            { $1.Children = append($1.Children, &node.Text{$2}); $$ = $1 }
|               content EXPR_START expr EXPR_END        { $1.Children = append($1.Children, &node.Expression{$3}); $$ = $1 }
|               content RAW_EXPR_START expr EXPR_END    { $1.Children = append($1.Children, &node.RawExpression{$3}); $$ = $1 }
|               loop                                    { $$ = &node.Content{[]node.Node{$1}} }
|               expr                                    { $$ = &node.Content{[]node.Node{$1}} }

expr:           IDENTIFIER                              { $$ = &node.Variable{Name: $1} }
|               STRING                                  { $$ = &node.String{$1} }
|               NUMBER                                  { $$ = &node.Number{$1} }
|               expr EQ expr                            { $$ = &node.Operator{"==", $1, $3} }
|               expr NE expr                            { $$ = &node.Operator{"!=", $1, $3} }
|               expr '+' expr                           { $$ = &node.Operator{"+", $1, $3} }
|               expr '-' expr                           { $$ = &node.Operator{"-", $1, $3} }
|               expr '*' expr                           { $$ = &node.Operator{"*", $1, $3} }
|               expr '/' expr                           { $$ = &node.Operator{"/", $1, $3} }
|               expr '%' expr                           { $$ = &node.Operator{"%", $1, $3} }
|               expr '<' expr                           { $$ = &node.Operator{"<", $1, $3} }
|               expr LE expr                            { $$ = &node.Operator{"<=", $1, $3} }
|               expr '>' expr                           { $$ = &node.Operator{">", $1, $3} }
|               expr GE expr                            { $$ = &node.Operator{">=", $1, $3} }
|               expr AND expr                           { $$ = &node.Operator{"&&", $1, $3} }
|               expr OR expr                            { $$ = &node.Operator{"||", $1, $3} }
|               NOT expr                                { $$ = &node.Operator{"!", nil, $2} }
|               '(' expr ')'                            { $$ = &node.Parentheses{$2} }
|               expr '.' IDENTIFIER                     { $$ = &node.StructField{$1, $3} }
|               expr '[' expr ']'                       { $$ = &node.Indexed{$1, $3} }
|               expr '(' expr_list ')'                  { $$ = &node.Function{$1, $3} }

expr_list:                                              { $$ = &node.ExpressionsList{} }
|               expr                                    { $$ = &node.ExpressionsList{[]node.Node{$1}} }
|               expr_list ',' expr                      { $1.Values = append($1.Values, $3); $$ = $1 }

loop:           IDENTIFIER IN expr                      { $$ = &node.Loop{Variable: $1, Array: $3} }
|               IDENTIFIER ',' IDENTIFIER IN expr       { $$ = &node.Loop{Index: $1, Variable: $3, Array: $5} }

%%