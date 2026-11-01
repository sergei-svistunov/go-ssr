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

content:        TEXT                                    { $$ = &node.Content{BaseNode: bn(yylex), Children: []node.Node{&node.Text{BaseNode: bn(yylex), Text: $1}}} }
|               EXPR_START expr EXPR_END                { $$ = &node.Content{BaseNode: bn(yylex), Children: []node.Node{&node.Expression{BaseNode: bn(yylex), Value: $2}}} }
|               RAW_EXPR_START expr EXPR_END            { $$ = &node.Content{BaseNode: bn(yylex), Children: []node.Node{&node.RawExpression{BaseNode: bn(yylex), Value: $2}}} }
|               content TEXT                            { $1.Children = append($1.Children, &node.Text{BaseNode: bn(yylex), Text: $2}); $$ = $1 }
|               content EXPR_START expr EXPR_END        { $1.Children = append($1.Children, &node.Expression{BaseNode: bn(yylex), Value: $3}); $$ = $1 }
|               content RAW_EXPR_START expr EXPR_END    { $1.Children = append($1.Children, &node.RawExpression{BaseNode: bn(yylex), Value: $3}); $$ = $1 }
|               loop                                    { $$ = &node.Content{BaseNode: bn(yylex), Children: []node.Node{$1}} }
|               expr                                    { $$ = &node.Content{BaseNode: bn(yylex), Children: []node.Node{$1}} }

expr:           IDENTIFIER                              { $$ = &node.Variable{BaseNode: bn(yylex), Name: $1} }
|               STRING                                  { $$ = &node.String{BaseNode: bn(yylex), Text: $1} }
|               NUMBER                                  { $$ = &node.Number{BaseNode: bn(yylex), Text: $1} }
|               expr EQ expr                            { $$ = &node.Operator{BaseNode: bn(yylex), Op: "==", Left: $1, Right: $3} }
|               expr NE expr                            { $$ = &node.Operator{BaseNode: bn(yylex), Op: "!=", Left: $1, Right: $3} }
|               expr '+' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "+", Left: $1, Right: $3} }
|               expr '-' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "-", Left: $1, Right: $3} }
|               expr '*' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "*", Left: $1, Right: $3} }
|               expr '/' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "/", Left: $1, Right: $3} }
|               expr '%' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "%", Left: $1, Right: $3} }
|               expr '<' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "<", Left: $1, Right: $3} }
|               expr LE expr                            { $$ = &node.Operator{BaseNode: bn(yylex), Op: "<=", Left: $1, Right: $3} }
|               expr '>' expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: ">", Left: $1, Right: $3} }
|               expr GE expr                            { $$ = &node.Operator{BaseNode: bn(yylex), Op: ">=", Left: $1, Right: $3} }
|               expr AND expr                           { $$ = &node.Operator{BaseNode: bn(yylex), Op: "&&", Left: $1, Right: $3} }
|               expr OR expr                            { $$ = &node.Operator{BaseNode: bn(yylex), Op: "||", Left: $1, Right: $3} }
|               NOT expr                                { $$ = &node.Operator{BaseNode: bn(yylex), Op: "!", Right: $2} }
|               '(' expr ')'                            { $$ = &node.Parentheses{BaseNode: bn(yylex), Value: $2} }
|               expr '.' IDENTIFIER                     { $$ = &node.StructField{BaseNode: bn(yylex), Expr: $1, FieldName: $3} }
|               expr '[' expr ']'                       { $$ = &node.Indexed{BaseNode: bn(yylex), Expr: $1, Index: $3} }
|               expr '(' expr_list ')'                  { $$ = &node.Function{BaseNode: bn(yylex), Expr: $1, Arguments: $3} }
|               expr '?' expr ':' expr                  { $$ = &node.TernaryIf{BaseNode: bn(yylex), Cond: $1, T: $3, F: $5} }

expr_list:                                              { $$ = &node.ExpressionsList{BaseNode: bn(yylex)} }
|               expr                                    { $$ = &node.ExpressionsList{BaseNode: bn(yylex), Values: []node.Node{$1}} }
|               expr_list ',' expr                      { $1.Values = append($1.Values, $3); $$ = $1 }

loop:           IDENTIFIER IN expr                      { $$ = &node.Loop{BaseNode: bn(yylex), Variable: $1, Array: $3} }
|               IDENTIFIER ',' IDENTIFIER IN expr       { $$ = &node.Loop{BaseNode: bn(yylex), Index: $1, Variable: $3, Array: $5} }

%%