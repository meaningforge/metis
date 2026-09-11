package expression

import "fmt"

type Nullability string

const (
	NullabilityUnknown  Nullability = "UNKNOWN"
	NullabilityNonNull  Nullability = "NON_NULL"
	NullabilityNullable Nullability = "NULLABLE"
)

type ExplicitCastKind string

const (
	ExplicitCastIdentity       ExplicitCastKind = "IDENTITY"
	ExplicitCastNull           ExplicitCastKind = "NULL"
	ExplicitCastUnknown        ExplicitCastKind = "UNKNOWN_SOURCE"
	ExplicitCastNumeric        ExplicitCastKind = "NUMERIC"
	ExplicitCastTextual        ExplicitCastKind = "TEXTUAL"
	ExplicitCastTemporal       ExplicitCastKind = "TEMPORAL"
	ExplicitCastSemiStructured ExplicitCastKind = "SEMI_STRUCTURED"
)

type CastResolutionFailure string

const (
	CastResolutionInvalidTarget CastResolutionFailure = "INVALID_TARGET"
	CastResolutionUnsupported   CastResolutionFailure = "UNSUPPORTED"
)

type CastResolutionError struct {
	Failure CastResolutionFailure
	From    SemanticType
	To      SemanticType
}

func (e *CastResolutionError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("cannot resolve explicit cast from %s to %s: %s", e.From, e.To, e.Failure)
}

type ExplicitCastResolution struct {
	From        SemanticType
	To          SemanticType
	Kind        ExplicitCastKind
	Nullability Nullability
}

func ResolveExplicitCast(from, to SemanticType, sourceNullability Nullability) (ExplicitCastResolution, error) {
	if !isConcreteCastTarget(to) {
		return ExplicitCastResolution{}, &CastResolutionError{Failure: CastResolutionInvalidTarget, From: from, To: to}
	}

	resolution := ExplicitCastResolution{
		From:        from,
		To:          to,
		Nullability: normalizeNullability(sourceNullability),
	}

	switch {
	case from == TypeNull:
		resolution.Kind = ExplicitCastNull
		resolution.Nullability = NullabilityNullable
		return resolution, nil
	case from == TypeUnknown:
		resolution.Kind = ExplicitCastUnknown
		return resolution, nil
	case from == to:
		resolution.Kind = ExplicitCastIdentity
		return resolution, nil
	case isNumericType(from) && isNumericType(to):
		resolution.Kind = ExplicitCastNumeric
		return resolution, nil
	case isStringCast(from, to):
		resolution.Kind = ExplicitCastTextual
		return resolution, nil
	case isTemporalCast(from, to):
		resolution.Kind = ExplicitCastTemporal
		return resolution, nil
	case from == TypeVariant && isVariantScalarTarget(to):
		resolution.Kind = ExplicitCastSemiStructured
		return resolution, nil
	default:
		return ExplicitCastResolution{}, &CastResolutionError{Failure: CastResolutionUnsupported, From: from, To: to}
	}
}

func MergeNullability(values ...Nullability) Nullability {
	if len(values) == 0 {
		return NullabilityUnknown
	}
	allNonNull := true
	for _, value := range values {
		switch normalizeNullability(value) {
		case NullabilityNullable:
			return NullabilityNullable
		case NullabilityUnknown:
			allNonNull = false
		}
	}
	if allNonNull {
		return NullabilityNonNull
	}
	return NullabilityUnknown
}

func normalizeNullability(value Nullability) Nullability {
	switch value {
	case NullabilityNonNull, NullabilityNullable:
		return value
	default:
		return NullabilityUnknown
	}
}

func isConcreteCastTarget(t SemanticType) bool {
	return t != "" && t != TypeUnknown && t != TypeNull
}

func isNumericType(t SemanticType) bool {
	return t == TypeInteger || t == TypeDecimal
}

func isStringCast(from, to SemanticType) bool {
	if from == TypeString {
		switch to {
		case TypeBoolean, TypeInteger, TypeDecimal, TypeDate, TypeTime, TypeTimestamp, TypeVariant:
			return true
		}
	}
	if to == TypeString {
		switch from {
		case TypeBoolean, TypeInteger, TypeDecimal, TypeDate, TypeTime, TypeTimestamp, TypeVariant:
			return true
		}
	}
	return false
}

func isTemporalCast(from, to SemanticType) bool {
	return (from == TypeDate && to == TypeTimestamp) || (from == TypeTimestamp && to == TypeDate)
}

func isVariantScalarTarget(t SemanticType) bool {
	switch t {
	case TypeBoolean, TypeInteger, TypeDecimal, TypeString, TypeDate, TypeTime, TypeTimestamp:
		return true
	default:
		return false
	}
}
