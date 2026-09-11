package expression

import "fmt"

type FunctionResolutionFailure string

const (
	FunctionResolutionNoMatch   FunctionResolutionFailure = "NO_MATCH"
	FunctionResolutionAmbiguous FunctionResolutionFailure = "AMBIGUOUS"
)

type FunctionResolutionError struct {
	Failure  FunctionResolutionFailure
	Name     string
	ArgTypes []SemanticType
}

func (e *FunctionResolutionError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("cannot resolve function %s%v: %s", e.Name, e.ArgTypes, e.Failure)
}

type FunctionArgumentCoercion struct {
	Index int
	From  SemanticType
	To    SemanticType
}

type ResolvedFunctionSignature struct {
	Signature      FunctionSignature
	ArgumentTypes  []SemanticType
	ParameterTypes []SemanticType
	Coercions      []FunctionArgumentCoercion
	Cost           int
}

// ResolveFunctionSignatures selects one signature deterministically. Exact
// semantic-type matches win over implicit coercions. Equal-cost matches are
// rejected instead of relying on registration order.
func ResolveFunctionSignatures(name string, signatures []FunctionSignature, argTypes []SemanticType) (ResolvedFunctionSignature, error) {
	matches := make([]ResolvedFunctionSignature, 0, len(signatures))
	bestCost := -1
	for _, sig := range signatures {
		resolved, ok := matchFunctionSignature(sig, argTypes)
		if !ok {
			continue
		}
		if bestCost < 0 || resolved.Cost < bestCost {
			bestCost = resolved.Cost
			matches = matches[:0]
			matches = append(matches, resolved)
			continue
		}
		if resolved.Cost == bestCost {
			matches = append(matches, resolved)
		}
	}
	if len(matches) == 0 {
		return ResolvedFunctionSignature{}, &FunctionResolutionError{Failure: FunctionResolutionNoMatch, Name: name, ArgTypes: append([]SemanticType(nil), argTypes...)}
	}
	if len(matches) > 1 {
		return ResolvedFunctionSignature{}, &FunctionResolutionError{Failure: FunctionResolutionAmbiguous, Name: name, ArgTypes: append([]SemanticType(nil), argTypes...)}
	}
	return matches[0], nil
}

func matchFunctionSignature(sig FunctionSignature, argTypes []SemanticType) (ResolvedFunctionSignature, bool) {
	if len(argTypes) < sig.MinArgs || (sig.MaxArgs >= 0 && len(argTypes) > sig.MaxArgs) {
		return ResolvedFunctionSignature{}, false
	}
	resolved := ResolvedFunctionSignature{
		Signature:      sig,
		ArgumentTypes:  append([]SemanticType(nil), argTypes...),
		ParameterTypes: make([]SemanticType, 0, len(argTypes)),
	}
	for i, actual := range argTypes {
		if !sig.AcceptsArg(i, actual) {
			return ResolvedFunctionSignature{}, false
		}
		target := sig.TargetType(i)
		if target == "" || target == TypeUnknown {
			resolved.ParameterTypes = append(resolved.ParameterTypes, actual)
			continue
		}
		cost, ok := implicitFunctionCoercionCost(actual, target)
		if !ok {
			return ResolvedFunctionSignature{}, false
		}
		resolved.ParameterTypes = append(resolved.ParameterTypes, target)
		resolved.Cost += cost
		if actual != target {
			resolved.Coercions = append(resolved.Coercions, FunctionArgumentCoercion{Index: i, From: actual, To: target})
		}
	}
	return resolved, true
}

// implicitFunctionCoercionCost is deliberately small and warehouse-neutral.
// Physical-engine cast behavior is not inferred here. The analyzer may widen an
// INTEGER to DECIMAL and may defer NULL/UNKNOWN values to a concrete parameter
// target; narrowing and cross-domain coercions require an explicit cast.
func implicitFunctionCoercionCost(from, to SemanticType) (int, bool) {
	if from == to {
		return 0, true
	}
	if from == TypeNull {
		return 1, true
	}
	if from == TypeInteger && to == TypeDecimal {
		return 1, true
	}
	if from == TypeUnknown {
		return 2, true
	}
	return 0, false
}
