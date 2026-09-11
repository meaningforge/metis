package expression

import "testing"

type testResolver map[string]BoundSymbol

func (r testResolver) Resolve(parts []string) (BoundSymbol, bool) {
	key := ""
	for i, p := range parts {
		if i > 0 {
			key += "."
		}
		key += p
	}
	s, ok := r[key]
	return s, ok
}

func TestSemanticAnalyzerInfersAggregateTypeAndSymbols(t *testing.T) {
	ast, err := Parse("SUM(orders.amount)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal}}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
	if got.Aggregation != AggregationAggregate {
		t.Fatalf("aggregation=%s", got.Aggregation)
	}
	if len(got.Symbols) != 1 || got.Symbols[0].Name != "amount" {
		t.Fatalf("symbols=%#v", got.Symbols)
	}
}

func TestSemanticAnalyzerRejectsNestedAggregate(t *testing.T) {
	ast, err := Parse("SUM(AVG(orders.amount))", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal}}
	_, err = NewSemanticAnalyzer(r, nil).Analyze(ast)
	se, ok := err.(*SemanticError)
	if !ok || se.Code != ErrInvalidAggregation {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestSemanticAnalyzerRejectsAggregateScalarMix(t *testing.T) {
	ast, err := Parse("SUM(orders.amount) + orders.customer_id", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.amount":      {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
		"orders.customer_id": {Kind: BoundColumn, Qualifier: "orders", Name: "customer_id", Type: TypeInteger},
	}
	_, err = NewSemanticAnalyzer(r, nil).Analyze(ast)
	se, ok := err.(*SemanticError)
	if !ok || se.Code != ErrInvalidAggregation {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestSemanticAnalyzerAllowsAggregatePlusConstant(t *testing.T) {
	ast, err := Parse("SUM(orders.amount) + 1", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal}}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Aggregation != AggregationAggregate {
		t.Fatalf("aggregation=%s", got.Aggregation)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
}

func TestSemanticAnalyzerValidatesFunctionArgumentTypes(t *testing.T) {
	ast, err := Parse("SUM(orders.status)", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{"orders.status": {Kind: BoundColumn, Qualifier: "orders", Name: "status", Type: TypeString}}
	_, err = NewSemanticAnalyzer(r, nil).Analyze(ast)
	se, ok := err.(*SemanticError)
	if !ok || se.Code != ErrTypeMismatch {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestSemanticAnalyzerInfersCaseCommonType(t *testing.T) {
	ast, err := Parse("CASE WHEN orders.paid THEN orders.amount ELSE 0 END", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"orders.paid":   {Kind: BoundColumn, Qualifier: "orders", Name: "paid", Type: TypeBoolean},
		"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal},
	}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
}

func TestSemanticAnalyzerRejectsNonBooleanCaseCondition(t *testing.T) {
	ast, err := Parse("CASE WHEN orders.amount THEN 1 ELSE 0 END", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeDecimal}}
	_, err = NewSemanticAnalyzer(r, nil).Analyze(ast)
	se, ok := err.(*SemanticError)
	if !ok || se.Code != ErrTypeMismatch {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestSemanticAnalyzerResolvesLambdaLocalsWithoutSemanticManifestLookup(t *testing.T) {
	ast, err := Parse("arrayMap(x -> x + orders.tax, orders.values)", ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewFunctionRegistry(FunctionSignature{
		Name: "arrayMap", Kind: FunctionScalar, MinArgs: 2, MaxArgs: 2,
		ArgTypes: []TypeConstraint{AnyType, AnyType}, ReturnType: func([]SemanticType) SemanticType { return TypeArray }, Deterministic: true,
	})
	r := testResolver{
		"orders.tax":    {Kind: BoundColumn, Qualifier: "orders", Name: "tax", Type: TypeDecimal},
		"orders.values": {Kind: BoundColumn, Qualifier: "orders", Name: "values", Type: TypeArray},
	}
	got, err := NewSemanticAnalyzer(r, registry).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Symbols) != 2 {
		t.Fatalf("symbols=%#v", got.Symbols)
	}
	for _, sym := range got.Symbols {
		if sym.Kind == BoundLocal {
			t.Fatalf("lambda local leaked into bound symbols: %#v", got.Symbols)
		}
	}
}

func TestSemanticAnalyzerCompatibleModeKeepsUnknownWarehouseFunctionTyped(t *testing.T) {
	ast, err := Parse("warehouseNative(amount, status = 'paid')", ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"amount": {Kind: BoundColumn, Name: "amount", Type: TypeDecimal},
		"status": {Kind: BoundColumn, Name: "status", Type: TypeString},
	}
	got, err := NewSemanticAnalyzerWithOptions(r, nil, SemanticAnalyzerOptions{AllowUnknownFunctions: true}).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeUnknown {
		t.Fatalf("type=%s", got.Type)
	}
	if got.Deterministic {
		t.Fatal("unknown function must not be assumed deterministic")
	}
	if len(got.Symbols) != 2 {
		t.Fatalf("symbols=%#v", got.Symbols)
	}
}

func TestSemanticAnalyzerCarriesMetricReferences(t *testing.T) {
	ast, err := Parse("revenue / orders.order_count", ANSI)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{
		"revenue":            {Kind: BoundMetric, Name: "revenue", Type: TypeDecimal, Aggregation: AggregationAggregate},
		"orders.order_count": {Kind: BoundColumn, Qualifier: "orders", Name: "order_count", Type: TypeInteger},
	}
	_, err = NewSemanticAnalyzer(r, nil).Analyze(ast)
	se, ok := err.(*SemanticError)
	if !ok || se.Code != ErrInvalidAggregation {
		t.Fatalf("expected metric/raw-column aggregation error, got %T %v", err, err)
	}
}

func TestSemanticAnalyzerTypedCast(t *testing.T) {
	ast, err := Parse("orders.amount::NUMBER(18,2)", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	r := testResolver{"orders.amount": {Kind: BoundColumn, Qualifier: "orders", Name: "amount", Type: TypeString}}
	got, err := NewSemanticAnalyzer(r, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
}
