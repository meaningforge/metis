package expression

import "strings"

// SemanticType is the analyzer-level type of an expression. It is intentionally
// warehouse-neutral; renderer-specific physical types remain outside the
// semantic analyzer.
type SemanticType string

const (
	TypeUnknown   SemanticType = "UNKNOWN"
	TypeNull      SemanticType = "NULL"
	TypeBoolean   SemanticType = "BOOLEAN"
	TypeInteger   SemanticType = "INTEGER"
	TypeDecimal   SemanticType = "DECIMAL"
	TypeString    SemanticType = "STRING"
	TypeDate      SemanticType = "DATE"
	TypeTime      SemanticType = "TIME"
	TypeTimestamp SemanticType = "TIMESTAMP"
	TypeVariant   SemanticType = "VARIANT"
	TypeArray     SemanticType = "ARRAY"
)

type AggregationState string

const (
	AggregationScalar    AggregationState = "SCALAR"
	AggregationAggregate AggregationState = "AGGREGATE"
	AggregationMixed     AggregationState = "MIXED"
)

type BoundSymbolKind string

const (
	BoundColumn BoundSymbolKind = "COLUMN"
	BoundMetric BoundSymbolKind = "METRIC"
	BoundLocal  BoundSymbolKind = "LOCAL"
)

type BoundSymbol struct {
	Kind        BoundSymbolKind
	Qualifier   string
	Name        string
	Type        SemanticType
	Nullability Nullability
	Aggregation AggregationState
}

type FunctionResolutionEvidence struct {
	Name           string
	Kind           FunctionKind
	ArgumentTypes  []SemanticType
	ParameterTypes []SemanticType
	Coercions      []FunctionArgumentCoercion
	Cost           int
}

type CastResolutionEvidence struct {
	From        SemanticType
	To          SemanticType
	Kind        ExplicitCastKind
	Nullability Nullability
	Span        SourceSpan
}

type PathResolutionEvidence struct {
	Resolution PathAccessResolution
	Span       SourceSpan
}

type TypedExpression struct {
	Expr                Expr
	Type                SemanticType
	Nullability         Nullability
	Aggregation         AggregationState
	Symbols             []BoundSymbol
	BindingResolutions  []SymbolBindingEvidence
	FunctionResolutions []FunctionResolutionEvidence
	CastResolutions     []CastResolutionEvidence
	PathResolutions     []PathResolutionEvidence
	Deterministic       bool
	// RowDependent distinguishes a raw row-level scalar from a literal/constant
	// scalar. It lets SUM(x)+1 remain legal while rejecting SUM(x)+raw_column.
	RowDependent bool
}

// SymbolResolver is the semantic binding boundary. Parser/analyzer code does
// not know SemanticManifest internals; SemanticManifest adapts its indexes to this interface.
type SymbolResolver interface {
	Resolve(parts []string) (BoundSymbol, bool)
}

type FunctionKind string

const (
	FunctionScalar    FunctionKind = "SCALAR"
	FunctionAggregate FunctionKind = "AGGREGATE"
)

// TypeConstraint validates one function argument. Unknown types are accepted so
// semantic analysis can be incrementally enriched as source schemas become typed.
type TypeConstraint func(SemanticType) bool

func AnyType(SemanticType) bool { return true }
func NumericType(t SemanticType) bool {
	return t == TypeUnknown || t == TypeNull || t == TypeInteger || t == TypeDecimal
}
func StringType(t SemanticType) bool {
	return t == TypeUnknown || t == TypeNull || t == TypeString
}
func BooleanType(t SemanticType) bool {
	return t == TypeUnknown || t == TypeNull || t == TypeBoolean
}

type FunctionSignature struct {
	Name         string
	Kind         FunctionKind
	MinArgs      int
	MaxArgs      int // -1 means unbounded
	ArgTypes     []TypeConstraint
	VariadicType TypeConstraint
	// ArgTargets and VariadicTarget are optional analyzer-level target types used
	// by overload resolution. They describe the semantic type a compatible
	// argument is resolved to after an allowed implicit coercion. TypeConstraint
	// remains the extensibility boundary for generic or warehouse-native
	// signatures that do not have one concrete target type.
	ArgTargets     []SemanticType
	VariadicTarget SemanticType
	ReturnType     func([]SemanticType) SemanticType
	Deterministic  bool
}

func (s FunctionSignature) AcceptsArg(index int, typ SemanticType) bool {
	if index < len(s.ArgTypes) && s.ArgTypes[index] != nil {
		return s.ArgTypes[index](typ)
	}
	if s.VariadicType != nil {
		return s.VariadicType(typ)
	}
	return true
}

func (s FunctionSignature) TargetType(index int) SemanticType {
	if index < len(s.ArgTargets) {
		return s.ArgTargets[index]
	}
	return s.VariadicTarget
}

type FunctionRegistry struct {
	byName map[string][]FunctionSignature
}

func NewFunctionRegistry(signatures ...FunctionSignature) *FunctionRegistry {
	r := &FunctionRegistry{byName: map[string][]FunctionSignature{}}
	for _, s := range signatures {
		key := strings.ToUpper(s.Name)
		r.byName[key] = append(r.byName[key], s)
	}
	return r
}

// Lookup preserves the original single-signature API for existing callers.
// New analyzer code that needs overload resolution should use Signatures and
// ResolveFunctionSignatures instead of relying on registration order.
func (r *FunctionRegistry) Lookup(name string) (FunctionSignature, bool) {
	if r == nil {
		return FunctionSignature{}, false
	}
	sigs := r.byName[strings.ToUpper(name)]
	if len(sigs) == 0 {
		return FunctionSignature{}, false
	}
	return sigs[0], true
}

func (r *FunctionRegistry) Signatures(name string) []FunctionSignature {
	if r == nil {
		return nil
	}
	sigs := r.byName[strings.ToUpper(name)]
	return append([]FunctionSignature(nil), sigs...)
}

func DefaultFunctionRegistry() *FunctionRegistry {
	numeric := func(args []SemanticType) SemanticType {
		for _, t := range args {
			if t == TypeDecimal {
				return TypeDecimal
			}
		}
		for _, t := range args {
			if t == TypeInteger {
				return TypeInteger
			}
		}
		return TypeUnknown
	}
	first := func(args []SemanticType) SemanticType {
		if len(args) == 0 {
			return TypeUnknown
		}
		return args[0]
	}
	common := func(args []SemanticType) SemanticType {
		result := TypeUnknown
		for _, typ := range args {
			result = commonType(result, typ)
		}
		return result
	}
	return NewFunctionRegistry(
		FunctionSignature{Name: "SUM", Kind: FunctionAggregate, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{NumericType}, ReturnType: numeric, Deterministic: true},
		FunctionSignature{Name: "SUMIF", Kind: FunctionAggregate, MinArgs: 2, MaxArgs: 2, ArgTypes: []TypeConstraint{NumericType, BooleanType}, ReturnType: first, Deterministic: true},
		FunctionSignature{Name: "AVG", Kind: FunctionAggregate, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{NumericType}, ReturnType: func([]SemanticType) SemanticType { return TypeDecimal }, Deterministic: true},
		FunctionSignature{Name: "MIN", Kind: FunctionAggregate, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{AnyType}, ReturnType: first, Deterministic: true},
		FunctionSignature{Name: "MAX", Kind: FunctionAggregate, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{AnyType}, ReturnType: first, Deterministic: true},
		FunctionSignature{Name: "COUNT", Kind: FunctionAggregate, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{AnyType}, ReturnType: func([]SemanticType) SemanticType { return TypeInteger }, Deterministic: true},
		FunctionSignature{Name: "LOWER", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{StringType}, ArgTargets: []SemanticType{TypeString}, ReturnType: func([]SemanticType) SemanticType { return TypeString }, Deterministic: true},
		FunctionSignature{Name: "UPPER", Kind: FunctionScalar, MinArgs: 1, MaxArgs: 1, ArgTypes: []TypeConstraint{StringType}, ArgTargets: []SemanticType{TypeString}, ReturnType: func([]SemanticType) SemanticType { return TypeString }, Deterministic: true},
		FunctionSignature{Name: "COALESCE", Kind: FunctionScalar, MinArgs: 1, MaxArgs: -1, VariadicType: AnyType, ReturnType: common, Deterministic: true},
	)
}
