package expression

import "testing"

func TestResolvePathAccessVariantKeyChain(t *testing.T) {
	got, err := ResolvePathAccess(TypeVariant, []PathSemanticStep{
		{Kind: PathSegmentKey, Key: "customer"},
		{Kind: PathSegmentKey, Key: "region"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResultType != TypeVariant || got.Nullability != NullabilityUnknown {
		t.Fatalf("resolution=%#v", got)
	}
	if len(got.Steps) != 2 || got.Steps[0].InputType != TypeVariant || got.Steps[1].OutputType != TypeVariant {
		t.Fatalf("steps=%#v", got.Steps)
	}
}

func TestResolvePathAccessRejectsKeyOnArray(t *testing.T) {
	_, err := ResolvePathAccess(TypeArray, []PathSemanticStep{{Kind: PathSegmentKey, Key: "region"}})
	assertPathResolutionFailure(t, err, PathResolutionInvalidBase, 0)
}

func TestResolvePathAccessVariantIndexSupportsIntegerAndString(t *testing.T) {
	for _, operand := range []SemanticType{TypeInteger, TypeString, TypeUnknown} {
		t.Run(string(operand), func(t *testing.T) {
			got, err := ResolvePathAccess(TypeVariant, []PathSemanticStep{{Kind: PathSegmentIndex, OperandType: operand}})
			if err != nil {
				t.Fatal(err)
			}
			if got.ResultType != TypeVariant {
				t.Fatalf("result type=%s", got.ResultType)
			}
		})
	}
}

func TestResolvePathAccessArrayIndexYieldsUnknownElementType(t *testing.T) {
	got, err := ResolvePathAccess(TypeArray, []PathSemanticStep{{Kind: PathSegmentIndex, OperandType: TypeInteger}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResultType != TypeUnknown {
		t.Fatalf("result type=%s", got.ResultType)
	}
}

func TestResolvePathAccessRejectsStringIndexOnArray(t *testing.T) {
	_, err := ResolvePathAccess(TypeArray, []PathSemanticStep{{Kind: PathSegmentIndex, OperandType: TypeString}})
	assertPathResolutionFailure(t, err, PathResolutionInvalidOperand, 0)
}

func TestResolvePathAccessRejectsScalarBase(t *testing.T) {
	_, err := ResolvePathAccess(TypeString, []PathSemanticStep{{Kind: PathSegmentKey, Key: "region"}})
	assertPathResolutionFailure(t, err, PathResolutionInvalidBase, 0)
}

func TestResolvePathAccessUnknownBaseRemainsUnknown(t *testing.T) {
	got, err := ResolvePathAccess(TypeUnknown, []PathSemanticStep{
		{Kind: PathSegmentKey, Key: "customer"},
		{Kind: PathSegmentDynamic, OperandType: TypeString},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResultType != TypeUnknown {
		t.Fatalf("result type=%s", got.ResultType)
	}
}

func TestResolvePathAccessRejectsInvalidDynamicOperand(t *testing.T) {
	_, err := ResolvePathAccess(TypeVariant, []PathSemanticStep{{Kind: PathSegmentDynamic, OperandType: TypeBoolean}})
	assertPathResolutionFailure(t, err, PathResolutionInvalidOperand, 0)
}

func TestResolvePathAccessRejectsEmptyKey(t *testing.T) {
	_, err := ResolvePathAccess(TypeVariant, []PathSemanticStep{{Kind: PathSegmentKey}})
	assertPathResolutionFailure(t, err, PathResolutionInvalidKey, 0)
}

func TestResolvePathAccessReportsFailingOrdinal(t *testing.T) {
	_, err := ResolvePathAccess(TypeVariant, []PathSemanticStep{
		{Kind: PathSegmentKey, Key: "items"},
		{Kind: PathSegmentIndex, OperandType: TypeBoolean},
	})
	assertPathResolutionFailure(t, err, PathResolutionInvalidOperand, 1)
}

func assertPathResolutionFailure(t *testing.T, err error, failure PathResolutionFailure, ordinal int) {
	t.Helper()
	pathErr, ok := err.(*PathResolutionError)
	if !ok {
		t.Fatalf("err=%T %v", err, err)
	}
	if pathErr.Failure != failure || pathErr.Ordinal != ordinal {
		t.Fatalf("path error=%#v", pathErr)
	}
}
