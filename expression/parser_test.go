package expression

import (
	"reflect"
	"testing"
)

func TestParserOperatorPrecedence(t *testing.T) {
	expr, err := Parse("a + b * c = d OR e AND f", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := expr.(*BinaryExpr)
	if !ok || root.Op != "OR" {
		t.Fatalf("root = %#v, want OR", expr)
	}
	left := root.Left.(*BinaryExpr)
	plus := left.Left.(*BinaryExpr)
	if left.Op != "=" || plus.Op != "+" || plus.Right.(*BinaryExpr).Op != "*" || root.Right.(*BinaryExpr).Op != "AND" {
		t.Fatalf("unexpected precedence tree: %#v", expr)
	}
}

func TestParserParenthesesOverridePrecedence(t *testing.T) {
	expr, err := Parse("(a + b) * c", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := expr.(*BinaryExpr)
	if !ok || root.Op != "*" {
		t.Fatalf("root = %#v, want binary *", expr)
	}
	left, ok := root.Left.(*BinaryExpr)
	if !ok || left.Op != "+" {
		t.Fatalf("left = %#v, want binary +", root.Left)
	}
}

func TestParserUnaryAndNestedCall(t *testing.T) {
	expr, err := Parse("-COALESCE(a, b + 1)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	unary, ok := expr.(*UnaryExpr)
	if !ok || unary.Op != "-" {
		t.Fatalf("expr = %#v, want unary -", expr)
	}
	call, ok := unary.Expr.(*FunctionCallExpr)
	if !ok || call.Name != "COALESCE" || len(call.Args) != 2 {
		t.Fatalf("unary child = %#v, want COALESCE with two args", unary.Expr)
	}
	if nested, ok := call.Args[1].(*BinaryExpr); !ok || nested.Op != "+" {
		t.Fatalf("second arg = %#v, want binary +", call.Args[1])
	}
}

func TestParserParameterizedCastType(t *testing.T) {
	ast, err := Parse("CAST(orders.amount AS DECIMAL(18,2))", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	cast, ok := ast.(*CastExpr)
	if !ok || cast.Type.Name != "DECIMAL" || len(cast.Type.Args) != 2 {
		t.Fatalf("cast = %#v", ast)
	}
	if cast.Type.Args[0].Value != "18" || cast.Type.Args[1].Value != "2" {
		t.Fatalf("cast type args = %#v", cast.Type.Args)
	}
}

func TestParserNestedTypeRef(t *testing.T) {
	ast, err := Parse("CAST(v AS Nullable(Decimal(18,2)))", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	cast, ok := ast.(*CastExpr)
	if !ok || cast.Type.Name != "Nullable" || len(cast.Type.Args) != 1 || cast.Type.Args[0].Type == nil {
		t.Fatalf("nested type = %#v", ast)
	}
	nested := cast.Type.Args[0].Type
	if nested.Name != "Decimal" || len(nested.Args) != 2 {
		t.Fatalf("nested type arg = %#v", nested)
	}
}

func TestParserMultiParameterLambdaAST(t *testing.T) {
	ast, err := Parse("(x, y) -> x + y + orders.tax", ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	lambda, ok := ast.(*LambdaExpr)
	if !ok || !reflect.DeepEqual(lambda.Params, []string{"x", "y"}) {
		t.Fatalf("ast = %#v, want lambda(x,y)", ast)
	}
}

func TestParserCastAndLiteralKinds(t *testing.T) {
	for _, input := range []string{"CAST(amount AS DECIMAL(18, 2))", "NULL", "TRUE", "FALSE", "'paid'", "42.5"} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input, ANSI); err != nil {
				t.Fatalf("Parse(%q): %v", input, err)
			}
		})
	}
}

func TestParserPreservesSourceSpans(t *testing.T) {
	input := "orders.amount + tax"
	expr, err := Parse(input, ANSI)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := SpanOf(expr), (SourceSpan{Start: 0, End: len(input)}); got != want {
		t.Fatalf("span = %#v, want %#v", got, want)
	}
	root := expr.(*BinaryExpr)
	left := SpanOf(root.Left)
	if input[left.Start:left.End] != "orders.amount" {
		t.Fatalf("left span = %#v", left)
	}
}

func TestParserTypedErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		code  ParseErrorCode
	}{
		{name: "empty parentheses", input: "()", code: ParseInvalidSyntax},
		{name: "missing closing parenthesis", input: "SUM(amount", code: ParseUnexpectedEOF},
		{name: "trailing token", input: "amount 1", code: ParseUnexpectedToken},
		{name: "missing expression", input: "amount +", code: ParseUnexpectedEOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input, ANSI)
			if err == nil {
				t.Fatalf("Parse(%q) unexpectedly succeeded", tt.input)
			}
			parseErr, ok := err.(*ParseError)
			if !ok || parseErr.Code != tt.code || !parseErr.Span.Valid() {
				t.Fatalf("error = %#v, want code %q with valid span", err, tt.code)
			}
		})
	}
}

func TestParserErrorCarriesExpectedToken(t *testing.T) {
	_, err := Parse("CASE WHEN a THEN b", ANSI)
	pe, ok := err.(*ParseError)
	if !ok || pe.Code != ParseUnexpectedEOF || len(pe.Expected) == 0 || pe.Expected[0] != "END" {
		t.Fatalf("error = %#v, want unexpected EOF expecting END", err)
	}
}

func TestParserRejectsTrailingTokenAtItsSourceSpan(t *testing.T) {
	_, err := Parse("amount 1", ANSI)
	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error = %T, want *ParseError", err)
	}
	if got, want := parseErr.Span, (SourceSpan{Start: 7, End: 8}); got != want || parseErr.Found != "1" {
		t.Fatalf("error = %#v, want trailing token 1 at %#v", parseErr, want)
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	for _, seed := range []string{"", "a + b * c", "SUM(CASE WHEN status = 'paid' THEN amount ELSE 0 END)", "payload:customer.region::STRING", "arrayMap(x -> x.price, orders.items)", "CAST(amount AS DECIMAL(18, 2))", "((((", "'unterminated"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		for _, dialect := range []DialectProfile{ANSI, ClickHouse} {
			_, _ = Parse(input, dialect)
		}
	})
}
