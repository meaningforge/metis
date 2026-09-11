package expression

import "testing"

func TestSemanticAnalyzerComposesVariantPathWithExplicitCast(t *testing.T) {
	ast, err := Parse("payload:customer.region::STRING", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	resolver := testResolver{"payload": {Kind: BoundColumn, Name: "payload", Type: TypeVariant}}

	got, err := NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeString {
		t.Fatalf("type=%s", got.Type)
	}
	if len(got.PathResolutions) != 1 {
		t.Fatalf("path resolutions=%#v", got.PathResolutions)
	}
	path := got.PathResolutions[0]
	if path.Resolution.BaseType != TypeVariant || path.Resolution.ResultType != TypeVariant || len(path.Resolution.Steps) != 2 {
		t.Fatalf("path resolution=%#v", path)
	}
	if path.Resolution.Steps[0].Kind != PathSegmentKey || path.Resolution.Steps[1].Kind != PathSegmentKey {
		t.Fatalf("path steps=%#v", path.Resolution.Steps)
	}
	if len(got.CastResolutions) != 1 {
		t.Fatalf("cast resolutions=%#v", got.CastResolutions)
	}
	cast := got.CastResolutions[0]
	if cast.From != TypeVariant || cast.To != TypeString || cast.Kind != ExplicitCastTextual {
		t.Fatalf("cast resolution=%#v", cast)
	}
}

func TestSemanticAnalyzerRejectsPathAccessOnScalarBase(t *testing.T) {
	ast, err := Parse("payload:customer", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	resolver := testResolver{"payload": {Kind: BoundColumn, Name: "payload", Type: TypeString}}

	_, err = NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	semanticErr, ok := err.(*SemanticError)
	if !ok || semanticErr.Code != ErrInvalidPathAccess {
		t.Fatalf("err=%T %v", err, err)
	}
	if semanticErr.Span != SpanOf(ast) {
		t.Fatalf("span=%#v want=%#v", semanticErr.Span, SpanOf(ast))
	}
}

func TestSemanticAnalyzerPreservesUnknownPathBase(t *testing.T) {
	ast, err := Parse("payload:customer.region", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	resolver := testResolver{"payload": {Kind: BoundColumn, Name: "payload", Type: TypeUnknown}}

	got, err := NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeUnknown {
		t.Fatalf("type=%s", got.Type)
	}
	if len(got.PathResolutions) != 1 || got.PathResolutions[0].Resolution.ResultType != TypeUnknown {
		t.Fatalf("path resolutions=%#v", got.PathResolutions)
	}
}

func TestSemanticAnalyzerRejectsStringIndexForArrayPath(t *testing.T) {
	base := &IdentifierExpr{Parts: []string{"items"}}
	index := &LiteralExpr{Value: "'sku'"}
	ast := &PathAccessExpr{Base: base, Segments: []PathSegment{PathIndexSegment{Index: index}}}
	resolver := testResolver{"items": {Kind: BoundColumn, Name: "items", Type: TypeArray}}

	_, err := NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	semanticErr, ok := err.(*SemanticError)
	if !ok || semanticErr.Code != ErrInvalidPathAccess {
		t.Fatalf("err=%T %v", err, err)
	}
	if semanticErr.Span != SpanOf(index) {
		t.Fatalf("span=%#v want=%#v", semanticErr.Span, SpanOf(index))
	}
}

func TestSemanticAnalyzerCarriesPathEvidenceThroughBinaryExpression(t *testing.T) {
	ast, err := Parse("payload:amount::NUMBER + 1", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	resolver := testResolver{"payload": {Kind: BoundColumn, Name: "payload", Type: TypeVariant}}

	got, err := NewSemanticAnalyzer(resolver, nil).Analyze(ast)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeDecimal {
		t.Fatalf("type=%s", got.Type)
	}
	if len(got.PathResolutions) != 1 || len(got.CastResolutions) != 1 {
		t.Fatalf("path=%#v cast=%#v", got.PathResolutions, got.CastResolutions)
	}
}
