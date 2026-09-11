package expression

import "testing"

func TestTypeRefStringFormatsNestedAndValueArguments(t *testing.T) {
	typ := TypeRef{
		Name: "Nullable",
		Args: []TypeArg{{
			Type: &TypeRef{
				Name: "Decimal",
				Args: []TypeArg{{Value: "18"}, {Value: "2"}},
			},
		}},
	}
	if got, want := typ.String(), "Nullable(Decimal(18,2))"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestTypeRefStringWithoutArgumentsReturnsName(t *testing.T) {
	if got, want := TypeName("STRING").String(), "STRING"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestTypeRefSpanReturnsSourceSpan(t *testing.T) {
	typ := TypeRef{Name: "DECIMAL", Source: SourceSpan{Start: 7, End: 20}}
	if got, want := typ.Span(), (SourceSpan{Start: 7, End: 20}); got != want {
		t.Fatalf("Span() = %#v, want %#v", got, want)
	}
}
