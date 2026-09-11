package expression

import "testing"

func TestSourceSpanValidityAndEmptiness(t *testing.T) {
	for _, tc := range []struct {
		span  SourceSpan
		valid bool
		empty bool
	}{
		{SourceSpan{Start: 0, End: 0}, true, true},
		{SourceSpan{Start: 2, End: 5}, true, false},
		{SourceSpan{Start: -1, End: 2}, false, false},
		{SourceSpan{Start: 5, End: 4}, false, true},
	} {
		if got := tc.span.Valid(); got != tc.valid {
			t.Fatalf("%#v.Valid() = %v, want %v", tc.span, got, tc.valid)
		}
		if got := tc.span.Empty(); got != tc.empty {
			t.Fatalf("%#v.Empty() = %v, want %v", tc.span, got, tc.empty)
		}
	}
}

func TestMergeSpanCoversBothRangesAndIgnoresEmptyInputs(t *testing.T) {
	if got, want := MergeSpan(SourceSpan{Start: 5, End: 9}, SourceSpan{Start: 2, End: 7}), (SourceSpan{Start: 2, End: 9}); got != want {
		t.Fatalf("MergeSpan() = %#v, want %#v", got, want)
	}
	if got, want := MergeSpan(SourceSpan{}, SourceSpan{Start: 4, End: 8}), (SourceSpan{Start: 4, End: 8}); got != want {
		t.Fatalf("MergeSpan(empty, b) = %#v, want %#v", got, want)
	}
	if got, want := MergeSpan(SourceSpan{Start: 4, End: 8}, SourceSpan{Start: -1, End: 3}), (SourceSpan{Start: 4, End: 8}); got != want {
		t.Fatalf("MergeSpan(a, invalid) = %#v, want %#v", got, want)
	}
}

func TestSpanOfAndSetExprSpanUseNodeMetadata(t *testing.T) {
	expr := &IdentifierExpr{Parts: []string{"orders", "amount"}}
	span := SourceSpan{Start: 2, End: 15}
	if got := setExprSpan(expr, span); got != expr {
		t.Fatal("setExprSpan changed expression identity")
	}
	if got := SpanOf(expr); got != span {
		t.Fatalf("SpanOf() = %#v, want %#v", got, span)
	}
	if got := SpanOf(nil); got != (SourceSpan{}) {
		t.Fatalf("SpanOf(nil) = %#v, want zero span", got)
	}
}
