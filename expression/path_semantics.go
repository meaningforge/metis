package expression

import "fmt"

type PathSegmentKind string

const (
	PathSegmentKey     PathSegmentKind = "KEY"
	PathSegmentIndex   PathSegmentKind = "INDEX"
	PathSegmentDynamic PathSegmentKind = "DYNAMIC"
)

// PathSemanticStep is the analyzer-neutral typed input for one semi-structured
// path segment. OperandType is used by INDEX/DYNAMIC steps and is ignored for
// KEY steps. Key is used only for KEY steps.
type PathSemanticStep struct {
	Kind        PathSegmentKind
	Key         string
	OperandType SemanticType
}

type PathStepResolution struct {
	Ordinal     int
	Kind        PathSegmentKind
	InputType   SemanticType
	OperandType SemanticType
	OutputType  SemanticType
}

type PathAccessResolution struct {
	BaseType    SemanticType
	ResultType  SemanticType
	Nullability Nullability
	Steps       []PathStepResolution
}

type PathResolutionFailure string

const (
	PathResolutionInvalidBase    PathResolutionFailure = "INVALID_BASE"
	PathResolutionInvalidKey     PathResolutionFailure = "INVALID_KEY"
	PathResolutionInvalidOperand PathResolutionFailure = "INVALID_OPERAND"
	PathResolutionInvalidKind    PathResolutionFailure = "INVALID_SEGMENT_KIND"
)

type PathResolutionError struct {
	Failure PathResolutionFailure
	Ordinal int
	Input   SemanticType
	Step    PathSemanticStep
}

func (e *PathResolutionError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("path resolution %s at segment %d from %s", e.Failure, e.Ordinal, e.Input)
}

// ResolvePathAccess validates one complete semi-structured traversal without
// depending on SQL dialect syntax. Physical renderability remains a lowering
// concern. Unknown types are preserved explicitly rather than guessed.
func ResolvePathAccess(base SemanticType, steps []PathSemanticStep) (PathAccessResolution, error) {
	current := base
	resolution := PathAccessResolution{
		BaseType:    base,
		ResultType:  base,
		Nullability: NullabilityUnknown,
		Steps:       make([]PathStepResolution, 0, len(steps)),
	}

	for ordinal, step := range steps {
		input := current
		output, err := resolvePathStep(input, step, ordinal)
		if err != nil {
			return PathAccessResolution{}, err
		}
		resolution.Steps = append(resolution.Steps, PathStepResolution{
			Ordinal:     ordinal,
			Kind:        step.Kind,
			InputType:   input,
			OperandType: step.OperandType,
			OutputType:  output,
		})
		current = output
	}

	resolution.ResultType = current
	return resolution, nil
}

func resolvePathStep(input SemanticType, step PathSemanticStep, ordinal int) (SemanticType, error) {
	switch step.Kind {
	case PathSegmentKey:
		if step.Key == "" {
			return TypeUnknown, pathResolutionError(PathResolutionInvalidKey, ordinal, input, step)
		}
		switch input {
		case TypeVariant:
			return TypeVariant, nil
		case TypeUnknown:
			return TypeUnknown, nil
		default:
			return TypeUnknown, pathResolutionError(PathResolutionInvalidBase, ordinal, input, step)
		}
	case PathSegmentIndex, PathSegmentDynamic:
		return resolvePathOperandStep(input, step, ordinal)
	default:
		return TypeUnknown, pathResolutionError(PathResolutionInvalidKind, ordinal, input, step)
	}
}

func resolvePathOperandStep(input SemanticType, step PathSemanticStep, ordinal int) (SemanticType, error) {
	switch input {
	case TypeVariant:
		if !variantPathOperand(step.OperandType) {
			return TypeUnknown, pathResolutionError(PathResolutionInvalidOperand, ordinal, input, step)
		}
		return TypeVariant, nil
	case TypeArray:
		if !arrayPathOperand(step.OperandType) {
			return TypeUnknown, pathResolutionError(PathResolutionInvalidOperand, ordinal, input, step)
		}
		// Array element type is not modeled by SemanticType yet, so indexing an
		// ARRAY intentionally yields UNKNOWN rather than inventing an element type.
		return TypeUnknown, nil
	case TypeUnknown:
		if !unknownPathOperand(step.OperandType) {
			return TypeUnknown, pathResolutionError(PathResolutionInvalidOperand, ordinal, input, step)
		}
		return TypeUnknown, nil
	default:
		return TypeUnknown, pathResolutionError(PathResolutionInvalidBase, ordinal, input, step)
	}
}

func variantPathOperand(t SemanticType) bool {
	return t == TypeInteger || t == TypeString || t == TypeUnknown
}

func arrayPathOperand(t SemanticType) bool {
	return t == TypeInteger || t == TypeUnknown
}

func unknownPathOperand(t SemanticType) bool {
	return t == TypeInteger || t == TypeString || t == TypeUnknown
}

func pathResolutionError(failure PathResolutionFailure, ordinal int, input SemanticType, step PathSemanticStep) *PathResolutionError {
	return &PathResolutionError{Failure: failure, Ordinal: ordinal, Input: input, Step: step}
}
