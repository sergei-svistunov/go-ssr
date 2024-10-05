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

%left '+' '-' '*' '/' '%' '>' '<' EQ NE GE LE OR AND '?' ':'
%left NOT

%%

top:            content                                 { yylex.(*exprLex).result = $$  }

content:        TEXT                                    { $$ = &node.Content{bn(yylex), []node.Node{&node.Text{bn(yylex), $1}}} }
|               EXPR_START expr EXPR_END                { $$ = &node.Content{bn(yylex), []node.Node{&node.Expression{bn(yylex), $2}}} }
|               RAW_EXPR_START expr EXPR_END            { $$ = &node.Content{bn(yylex), []node.Node{&node.RawExpression{bn(yylex), $2}}} }
|               content TEXT                            { $1.Children = append($1.Children, &node.Text{bn(yylex), $2}); $$ = $1 }
|               content EXPR_START expr EXPR_END        { $1.Children = append($1.Children, &node.Expression{bn(yylex), $3}); $$ = $1 }
|               content RAW_EXPR_START expr EXPR_END    { $1.Children = append($1.Children, &node.RawExpression{bn(yylex), $3}); $$ = $1 }
|               loop                                    { $$ = &node.Content{bn(yylex), []node.Node{$1}} }
|               expr                                    { $$ = &node.Content{bn(yylex), []node.Node{$1}} }

expr:           IDENTIFIER                              { $$ = &node.Variable{bn(yylex), $1} }
|               STRING                                  { $$ = &node.String{bn(yylex), $1} }
|               NUMBER                                  { $$ = &node.Number{bn(yylex), $1} }
|               expr EQ expr                            { $$ = &node.Operator{bn(yylex), "==", $1, $3} }
|               expr NE expr                            { $$ = &node.Operator{bn(yylex), "!=", $1, $3} }
|               expr '+' expr                           { $$ = &node.Operator{bn(yylex), "+", $1, $3} }
|               expr '-' expr                           { $$ = &node.Operator{bn(yylex), "-", $1, $3} }
|               expr '*' expr                           { $$ = &node.Operator{bn(yylex), "*", $1, $3} }
|               expr '/' expr                           { $$ = &node.Operator{bn(yylex), "/", $1, $3} }
|               expr '%' expr                           { $$ = &node.Operator{bn(yylex), "%", $1, $3} }
|               expr '<' expr                           { $$ = &node.Operator{bn(yylex), "<", $1, $3} }
|               expr LE expr                            { $$ = &node.Operator{bn(yylex), "<=", $1, $3} }
|               expr '>' expr                           { $$ = &node.Operator{bn(yylex), ">", $1, $3} }
|               expr GE expr                            { $$ = &node.Operator{bn(yylex), ">=", $1, $3} }
|               expr AND expr                           { $$ = &node.Operator{bn(yylex), "&&", $1, $3} }
|               expr OR expr                            { $$ = &node.Operator{bn(yylex), "||", $1, $3} }
|               NOT expr                                { $$ = &node.Operator{bn(yylex), "!", nil, $2} }
|               '(' expr ')'                            { $$ = &node.Parentheses{bn(yylex), $2} }
|               expr '.' IDENTIFIER                     { $$ = &node.StructField{bn(yylex), $1, $3} }
|               expr '[' expr ']'                       { $$ = &node.Indexed{bn(yylex), $1, $3} }
|               expr '(' expr_list ')'                  { $$ = &node.Function{bn(yylex), $1, $3} }
|               expr '?' expr ':' expr                  { $$ = &node.TernaryIf{bn(yylex), $1, $3, $5} }

expr_list:                                              { $$ = &node.ExpressionsList{bn(yylex), nil} }
|               expr                                    { $$ = &node.ExpressionsList{bn(yylex), []node.Node{$1}} }
|               expr_list ',' expr                      { $1.Values = append($1.Values, $3); $$ = $1 }

loop:           IDENTIFIER IN expr                      { $$ = &node.Loop{bn(yylex), "", $1, $3, nil} }
|               IDENTIFIER ',' IDENTIFIER IN expr       { $$ = &node.Loop{bn(yylex), $1, $3, $5, nil} }

%%