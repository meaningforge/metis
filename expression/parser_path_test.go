package expression

import (
	"reflect"
	"testing"
)

func TestParserSnowflakeVariantPathAndCastAST(t *testing.T) {
	expr, err := Parse("payload:customer.region::STRING", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	cast, ok := expr.(*CastExpr)
	if !ok {
		t.Fatalf("expr = %T, want *CastExpr", expr)
	}
	if cast.Type.Name != "STRING" || cast.Type.String() != "STRING" {
		t.Fatalf("cast type = %#v, want STRING", cast.Type)
	}
	path, ok := cast.Expr.(*PathAccessExpr)
	if !ok {
		t.Fatalf("cast expr = %T, want *PathAccessExpr", cast.Expr)
	}
	base, ok := path.Base.(*IdentifierExpr)
	if !ok || !reflect.DeepEqual(base.Parts, []string{"payload"}) {
		t.Fatalf("path base = %#v, want payload identifier", path.Base)
	}
	wantSegments := []PathSegment{PathKeySegment{Key: "customer"}, PathKeySegment{Key: "region"}}
	if !reflect.DeepEqual(path.Segments, wantSegments) {
		t.Fatalf("segments = %#v, want %#v", path.Segments, wantSegments)
	}
}

func TestParserSnowflakeVariantPathWithIndexAST(t *testing.T) {
	expr, err := Parse("payload:items[0].price::NUMBER", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	cast := expr.(*CastExpr)
	path := cast.Expr.(*PathAccessExpr)
	if len(path.Segments) != 3 {
		t.Fatalf("segments = %#v, want 3", path.Segments)
	}
	if key, ok := path.Segments[0].(PathKeySegment); !ok || key.Key != "items" {
		t.Fatalf("segment[0] = %#v, want Key(items)", path.Segments[0])
	}
	idx, ok := path.Segments[1].(PathIndexSegment)
	if !ok {
		t.Fatalf("segment[1] = %#v, want PathIndexSegment", path.Segments[1])
	}
	lit, ok := idx.Index.(*LiteralExpr)
	if !ok || lit.Value != "0" {
		t.Fatalf("index = %#v, want literal 0", idx.Index)
	}
	if key, ok := path.Segments[2].(PathKeySegment); !ok || key.Key != "price" {
		t.Fatalf("segment[2] = %#v, want Key(price)", path.Segments[2])
	}
}

func TestParserRelationalQualificationRemainsIdentifier(t *testing.T) {
	expr, err := Parse("orders.amount", Snowflake)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := expr.(*IdentifierExpr)
	if !ok || !reflect.DeepEqual(id.Parts, []string{"orders", "amount"}) {
		t.Fatalf("expr = %#v, want identifier [orders amount]", expr)
	}
}
