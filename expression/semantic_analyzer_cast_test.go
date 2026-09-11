package expression

import "testing"

func TestSemanticAnalyzerRetainsExplicitCastEvidenceAndNullability(t *testing.T) {
	ast, err := Parse("orders.amount::NUMBER(18,2)", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.amount": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "amount",
			Type:        TypeString,
			Nullability: NullabilityNullable,
		},
	}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
	if got.Nullability != NullabilityNullable {
		t.Fatalf("nullability=%s", got.Nullability)
	}
	if len(got.CastResolutions) != 1 {
		t.Fatalf("cast resolutions=%#v", got.CastResolutions)
	}
	ev := got.CastResolutions[0]
	if ev.From != TypeString || ev.To != TypeDecimal || ev.Kind != ExplicitCastTextual {
		t.Fatalf("cast evidence=%#v", ev)
	}
	if ev.Span != SpanOf(ast) {
		t.Fatalf("span=%#v want=%#v", ev.Span, SpanOf(ast))
	}
}

func TestSemanticAnalyzerRejectsUnsupportedExplicitCast(t *testing.T) {
	ast, err := Parse("CAST(orders.items AS DECIMAL)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.items": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "items",
			Type:        TypeArray,
			Nullability: NullabilityNonNull,
		},
	}
	_, err = NewSemanticAnalyzer(r, nil).Analyze(ast)
	se, ok := err.(*SemanticError)
	if !ok || se.Code != ErrInvalidCast {
		t.Fatalf("err=%T %v", err, err)
	}
	if se.Span != SpanOf(ast) {
		t.Fatalf("span=%#v want=%#v", se.Span, SpanOf(ast))
	}
}

func TestSemanticAnalyzerExplicitNullCastIsNullable(t *testing.T) {
	ast, err := Parse("CAST(NULL AS INTEGER)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewSemanticAnalyzer(nil, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeInteger || got.Nullability != NullabilityNullable {
		t.Fatalf("got type=%s nullability=%s", got.Type, got.Nullability)
	}
}

func TestSemanticAnalyzerIsNullIsNonNullBoolean(t *testing.T) {
	ast, err := Parse("orders.amount IS NULL", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.amount": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "amount",
			Type:        TypeDecimal,
			Nullability: NullabilityNullable,
		},
	}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeBoolean || got.Nullability != NullabilityNonNull {
		t.Fatalf("got type=%s nullability=%s", got.Type, got.Nullability)
	}
}

func TestSemanticAnalyzerBinaryNullabilityFailsClosed(t *testing.T) {
	ast, err := Parse("orders.amount + 1", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.amount": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "amount",
			Type:        TypeDecimal,
			Nullability: NullabilityNullable,
		},
	}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nullability != NullabilityNullable {
		t.Fatalf("nullability=%s", got.Nullability)
	}
}

func TestSemanticAnalyzerCaseWithoutElseIsNullable(t *testing.T) {
	ast, err := Parse("CASE WHEN orders.paid THEN 1 END", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.paid": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "paid",
			Type:        TypeBoolean,
			Nullability: NullabilityNonNull,
		},
	}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nullability != NullabilityNullable {
		t.Fatalf("nullability=%s", got.Nullability)
	}
}

func TestSemanticAnalyzerScalarAndAggregateNullability(t *testing.T) {
	lowerAST, err := Parse("LOWER(orders.status)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.status": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "status",
			Type:        TypeString,
			Nullability: NullabilityNonNull,
		},
		"orders.amount": {
			Kind:        BoundColumn,
			Qualifier:   "orders",
			Name:        "amount",
			Type:        TypeDecimal,
			Nullability: NullabilityNonNull,
		},
	}
	lower, err := NewSemanticAnalyzer(r, nil).Analyze(lowerAST)
	if err != nil {
		t.Fatal(err)
	}
	if lower.Nullability != NullabilityNonNull {
		t.Fatalf("lower nullability=%s", lower.Nullability)
	}

	sumAST, err := Parse("SUM(orders.amount)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := NewSemanticAnalyzer(r, nil).Analyze(sumAST)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Nullability != NullabilityUnknown {
		t.Fatalf("sum nullability=%s", sum.Nullability)
	}
}
