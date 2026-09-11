package expression

import (
	"fmt"
	"strings"
)

type SemanticErrorCode string

const (
	ErrUnboundSymbol        SemanticErrorCode = "UNBOUND_SYMBOL"
	ErrUnknownFunction      SemanticErrorCode = "UNKNOWN_FUNCTION"
	ErrInvalidFunctionArity SemanticErrorCode = "INVALID_FUNCTION_ARITY"
	ErrAmbiguousFunction    SemanticErrorCode = "AMBIGUOUS_FUNCTION"
	ErrInvalidAggregation   SemanticErrorCode = "INVALID_AGGREGATION"
	ErrInvalidCast          SemanticErrorCode = "INVALID_CAST"
	ErrInvalidPathAccess    SemanticErrorCode = "INVALID_PATH_ACCESS"
	ErrTypeMismatch         SemanticErrorCode = "TYPE_MISMATCH"
)

type SemanticError struct {
	Code    SemanticErrorCode
	Message string
	Span    SourceSpan
}

func (e *SemanticError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s at [%d,%d)", e.Code, e.Message, e.Span.Start, e.Span.End)
}

type SemanticAnalyzerOptions struct {
	AllowUnknownFunctions bool
}

type SemanticAnalyzer struct {
	resolver  SymbolResolver
	functions *FunctionRegistry
	scope     *SymbolScope
	options   SemanticAnalyzerOptions
}

func NewSemanticAnalyzer(resolver SymbolResolver, functions *FunctionRegistry) *SemanticAnalyzer {
	return NewSemanticAnalyzerWithOptions(resolver, functions, SemanticAnalyzerOptions{})
}

func NewSemanticAnalyzerWithOptions(resolver SymbolResolver, functions *FunctionRegistry, options SemanticAnalyzerOptions) *SemanticAnalyzer {
	if functions == nil {
		functions = DefaultFunctionRegistry()
	}
	return &SemanticAnalyzer{resolver: resolver, functions: functions, scope: semanticScopeFromResolver(resolver), options: options}
}

func (a *SemanticAnalyzer) Analyze(expr Expr) (TypedExpression, error) {
	result, err := a.analyze(expr, false)
	if err != nil {
		return TypedExpression{}, err
	}
	result.Expr = expr
	return result, nil
}

func (a *SemanticAnalyzer) analyze(expr Expr, insideAggregate bool) (TypedExpression, error) {
	switch e := expr.(type) {
	case *IdentifierExpr:
		if a.resolver == nil && a.scope == nil {
			return TypedExpression{Expr: e, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: true, RowDependent: true}, nil
		}
		sym, evidence, err := a.resolveSymbol(e.Parts, SpanOf(e))
		if err != nil {
			return TypedExpression{}, semanticBindingError(err, evidence)
		}
		if sym.Aggregation == "" {
			sym.Aggregation = AggregationScalar
		}
		if insideAggregate && sym.Aggregation == AggregationAggregate {
			return TypedExpression{}, &SemanticError{Code: ErrInvalidAggregation, Message: "aggregate metric " + sym.Name + " cannot be nested inside an aggregate function", Span: SpanOf(e)}
		}
		out := typedSymbol(e, sym)
		out.BindingResolutions = []SymbolBindingEvidence{evidence}
		return out, nil
	case *LiteralExpr:
		typ := literalType(e.Value)
		return TypedExpression{Expr: e, Type: typ, Nullability: literalNullability(typ), Aggregation: AggregationScalar, Deterministic: true}, nil
	case *WildcardExpr:
		return TypedExpression{Expr: e, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: true}, nil
	case *CastExpr:
		child, err := a.analyze(e.Expr, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		target := semanticTypeFromTypeRef(e.Type)
		resolved, err := ResolveExplicitCast(child.Type, target, child.Nullability)
		if err != nil {
			return TypedExpression{}, &SemanticError{Code: ErrInvalidCast, Message: fmt.Sprintf("invalid explicit cast from %s to %s", child.Type, target), Span: SpanOf(e)}
		}
		child.Expr = e
		child.Type = target
		child.Nullability = resolved.Nullability
		child.CastResolutions = append(child.CastResolutions, castResolutionEvidence(resolved, SpanOf(e)))
		return child, nil
	case *UnaryExpr:
		child, err := a.analyze(e.Expr, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		child.Expr = e
		if strings.EqualFold(e.Op, "NOT") {
			if !booleanCompatible(child.Type) {
				return TypedExpression{}, typeError(e, "NOT expects BOOLEAN")
			}
			child.Type = TypeBoolean
		}
		return child, nil
	case *BinaryExpr:
		return a.analyzeBinary(e, insideAggregate)
	case *FunctionCallExpr:
		return a.analyzeFunction(e, insideAggregate)
	case *ApplyExpr:
		callee, err := a.analyze(e.Callee, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		out := callee
		out.Expr = e
		for _, arg := range e.Args {
			v, err := a.analyze(arg, insideAggregate)
			if err != nil {
				return TypedExpression{}, err
			}
			out = combineTyped(out, v)
		}
		return out, nil
	case *BetweenExpr:
		return a.analyzeBoolean([]Expr{e.Expr, e.Low, e.High}, insideAggregate, e)
	case *InExpr:
		return a.analyzeBoolean(append([]Expr{e.Expr}, e.Values...), insideAggregate, e)
	case *IsNullExpr:
		v, err := a.analyze(e.Expr, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		v.Expr = e
		v.Type = TypeBoolean
		v.Nullability = NullabilityNonNull
		return v, nil
	case *CaseExpr:
		return a.analyzeCase(e, insideAggregate)
	case *IntervalExpr:
		v, err := a.analyze(e.Value, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		v.Expr = e
		return v, nil
	case *TupleExpr:
		out := TypedExpression{Expr: e, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: true}
		for _, item := range e.Items {
			v, err := a.analyze(item, insideAggregate)
			if err != nil {
				return TypedExpression{}, err
			}
			out = combineTyped(out, v)
		}
		return out, nil
	case *LambdaExpr:
		locals := make([]BoundSymbol, 0, len(e.Params))
		for _, param := range e.Params {
			locals = append(locals, BoundSymbol{Kind: BoundLocal, Name: param, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar})
		}
		parentScope := a.scope
		a.scope = NewSymbolScope(ScopeLambda, parentScope, locals...)
		body, err := a.analyze(e.Body, insideAggregate)
		a.scope = parentScope
		if err != nil {
			return TypedExpression{}, err
		}
		body.Expr = e
		body.Symbols = removeLocalSymbols(body.Symbols, e.Params)
		return body, nil
	case *IndexExpr:
		o, err := a.analyze(e.Object, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		i, err := a.analyze(e.Index, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		out := combineTyped(o, i)
		out.Expr = e
		out.Type = TypeUnknown
		out.Nullability = NullabilityUnknown
		return out, nil
	case *PathAccessExpr:
		return a.analyzePathAccess(e, insideAggregate)
	default:
		return TypedExpression{Expr: expr, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: true}, nil
	}
}

func (a *SemanticAnalyzer) analyzeBinary(e *BinaryExpr, insideAggregate bool) (TypedExpression, error) {
	left, err := a.analyze(e.Left, insideAggregate)
	if err != nil {
		return TypedExpression{}, err
	}
	right, err := a.analyze(e.Right, insideAggregate)
	if err != nil {
		return TypedExpression{}, err
	}
	op := strings.ToUpper(e.Op)
	if (op == "AND" || op == "OR") && (!booleanCompatible(left.Type) || !booleanCompatible(right.Type)) {
		return TypedExpression{}, typeError(e, op+" expects BOOLEAN operands")
	}
	if isArithmetic(op) && (!numericCompatible(left.Type) || !numericCompatible(right.Type)) {
		return TypedExpression{}, typeError(e, op+" expects numeric operands")
	}
	agg := mergeExpressionAggregation(left, right)
	if agg == AggregationMixed && !insideAggregate {
		return TypedExpression{}, &SemanticError{Code: ErrInvalidAggregation, Message: "expression mixes aggregated and row-level values", Span: SpanOf(e)}
	}
	return TypedExpression{Expr: e, Type: binaryResultType(e.Op, left.Type, right.Type), Nullability: MergeNullability(left.Nullability, right.Nullability), Aggregation: agg, Symbols: mergeSymbols(left.Symbols, right.Symbols), BindingResolutions: appendBindingResolutions(left.BindingResolutions, right.BindingResolutions), FunctionResolutions: appendFunctionResolutions(left.FunctionResolutions, right.FunctionResolutions), CastResolutions: appendCastResolutions(left.CastResolutions, right.CastResolutions), PathResolutions: appendPathResolutions(left.PathResolutions, right.PathResolutions), Deterministic: left.Deterministic && right.Deterministic, RowDependent: left.RowDependent || right.RowDependent}, nil
}

func (a *SemanticAnalyzer) analyzeFunction(e *FunctionCallExpr, insideAggregate bool) (TypedExpression, error) {
	signatures := a.functions.Signatures(e.Name)
	if len(signatures) == 0 {
		if !a.options.AllowUnknownFunctions {
			return TypedExpression{}, &SemanticError{Code: ErrUnknownFunction, Message: "unknown function " + e.Name, Span: SpanOf(e)}
		}
		out := TypedExpression{Expr: e, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: false}
		for _, arg := range e.Args {
			v, err := a.analyze(arg, insideAggregate)
			if err != nil {
				return TypedExpression{}, err
			}
			out = combineTyped(out, v)
		}
		out.Type = TypeUnknown
		out.Nullability = NullabilityUnknown
		return out, nil
	}

	arityCandidates := make([]FunctionSignature, 0, len(signatures))
	for _, sig := range signatures {
		if functionAcceptsArity(sig, len(e.Args)) {
			arityCandidates = append(arityCandidates, sig)
		}
	}
	if len(arityCandidates) == 0 {
		return TypedExpression{}, &SemanticError{Code: ErrInvalidFunctionArity, Message: fmt.Sprintf("no overload of %s accepts %d arguments", e.Name, len(e.Args)), Span: SpanOf(e)}
	}

	args := make([]TypedExpression, 0, len(e.Args))
	argTypes := make([]SemanticType, 0, len(e.Args))
	for _, arg := range e.Args {
		v, err := a.analyze(arg, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		args = append(args, v)
		argTypes = append(argTypes, v.Type)
	}

	resolved, err := ResolveFunctionSignatures(e.Name, arityCandidates, argTypes)
	if err != nil {
		resolutionErr, ok := err.(*FunctionResolutionError)
		if ok && resolutionErr.Failure == FunctionResolutionAmbiguous {
			return TypedExpression{}, &SemanticError{Code: ErrAmbiguousFunction, Message: fmt.Sprintf("ambiguous overload of %s for argument types %v", e.Name, argTypes), Span: SpanOf(e)}
		}
		return TypedExpression{}, &SemanticError{Code: ErrTypeMismatch, Message: fmt.Sprintf("no compatible overload of %s for argument types %v", e.Name, argTypes), Span: SpanOf(e)}
	}

	sig := resolved.Signature
	if sig.Kind == FunctionAggregate && insideAggregate {
		return TypedExpression{}, &SemanticError{Code: ErrInvalidAggregation, Message: "nested aggregate function " + e.Name, Span: SpanOf(e)}
	}

	out := TypedExpression{Expr: e, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: sig.Deterministic}
	argNullability := make([]Nullability, 0, len(args))
	for i, v := range args {
		if sig.Kind == FunctionAggregate && (v.Aggregation == AggregationAggregate || v.Aggregation == AggregationMixed) {
			return TypedExpression{}, &SemanticError{Code: ErrInvalidAggregation, Message: "aggregate argument cannot contain an aggregate value", Span: SpanOf(e.Args[i])}
		}
		out.Symbols = mergeSymbols(out.Symbols, v.Symbols)
		out.BindingResolutions = appendBindingResolutions(out.BindingResolutions, v.BindingResolutions)
		out.FunctionResolutions = appendFunctionResolutions(out.FunctionResolutions, v.FunctionResolutions)
		out.CastResolutions = appendCastResolutions(out.CastResolutions, v.CastResolutions)
		out.PathResolutions = appendPathResolutions(out.PathResolutions, v.PathResolutions)
		out.Deterministic = out.Deterministic && v.Deterministic
		out.RowDependent = out.RowDependent || v.RowDependent
		argNullability = append(argNullability, v.Nullability)
		if sig.Kind != FunctionAggregate {
			out.Aggregation = mergeExpressionAggregation(out, v)
		}
	}
	if sig.Kind == FunctionAggregate {
		out.Aggregation = AggregationAggregate
		out.RowDependent = false
		out.Nullability = NullabilityUnknown
	} else {
		out.Nullability = MergeNullability(argNullability...)
	}
	out.Type = sig.ReturnType(resolved.ParameterTypes)
	out.FunctionResolutions = append(out.FunctionResolutions, functionResolutionEvidence(resolved))
	return out, nil
}

func functionAcceptsArity(sig FunctionSignature, count int) bool {
	return count >= sig.MinArgs && (sig.MaxArgs < 0 || count <= sig.MaxArgs)
}

func functionResolutionEvidence(resolved ResolvedFunctionSignature) FunctionResolutionEvidence {
	return FunctionResolutionEvidence{Name: resolved.Signature.Name, Kind: resolved.Signature.Kind, ArgumentTypes: append([]SemanticType(nil), resolved.ArgumentTypes...), ParameterTypes: append([]SemanticType(nil), resolved.ParameterTypes...), Coercions: append([]FunctionArgumentCoercion(nil), resolved.Coercions...), Cost: resolved.Cost}
}

func castResolutionEvidence(resolved ExplicitCastResolution, span SourceSpan) CastResolutionEvidence {
	return CastResolutionEvidence{From: resolved.From, To: resolved.To, Kind: resolved.Kind, Nullability: resolved.Nullability, Span: span}
}

func (a *SemanticAnalyzer) analyzeCase(e *CaseExpr, insideAggregate bool) (TypedExpression, error) {
	out := TypedExpression{Expr: e, Type: TypeUnknown, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: true}
	if e.Operand != nil {
		v, err := a.analyze(e.Operand, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		out = combineTyped(out, v)
	}
	resultType := TypeUnknown
	resultNullability := make([]Nullability, 0, len(e.Whens)+1)
	for _, w := range e.Whens {
		condition, err := a.analyze(w.Condition, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		if e.Operand == nil && !booleanCompatible(condition.Type) {
			return TypedExpression{}, typeError(w.Condition, "CASE WHEN condition must be BOOLEAN")
		}
		out = combineTyped(out, condition)
		result, err := a.analyze(w.Result, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		out = combineTyped(out, result)
		resultType = commonType(resultType, result.Type)
		resultNullability = append(resultNullability, result.Nullability)
	}
	if e.Else != nil {
		v, err := a.analyze(e.Else, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		out = combineTyped(out, v)
		resultType = commonType(resultType, v.Type)
		resultNullability = append(resultNullability, v.Nullability)
	} else {
		resultNullability = append(resultNullability, NullabilityNullable)
	}
	out.Type = resultType
	out.Nullability = MergeNullability(resultNullability...)
	if out.Aggregation == AggregationMixed && !insideAggregate {
		return TypedExpression{}, &SemanticError{Code: ErrInvalidAggregation, Message: "CASE mixes aggregated and row-level results", Span: SpanOf(e)}
	}
	return out, nil
}

func (a *SemanticAnalyzer) analyzeBoolean(parts []Expr, insideAggregate bool, root Expr) (TypedExpression, error) {
	out := TypedExpression{Expr: root, Type: TypeBoolean, Nullability: NullabilityUnknown, Aggregation: AggregationScalar, Deterministic: true}
	nullability := make([]Nullability, 0, len(parts))
	for _, p := range parts {
		v, err := a.analyze(p, insideAggregate)
		if err != nil {
			return TypedExpression{}, err
		}
		out = combineTyped(out, v)
		nullability = append(nullability, v.Nullability)
	}
	out.Type = TypeBoolean
	out.Nullability = MergeNullability(nullability...)
	if out.Aggregation == AggregationMixed && !insideAggregate {
		return TypedExpression{}, &SemanticError{Code: ErrInvalidAggregation, Message: "predicate mixes aggregated and row-level values", Span: SpanOf(root)}
	}
	return out, nil
}

func typedSymbol(expr Expr, sym BoundSymbol) TypedExpression {
	agg := sym.Aggregation
	if agg == "" {
		agg = AggregationScalar
	}
	return TypedExpression{Expr: expr, Type: sym.Type, Nullability: normalizeNullability(sym.Nullability), Aggregation: agg, Symbols: []BoundSymbol{sym}, Deterministic: true, RowDependent: sym.Kind == BoundColumn || sym.Kind == BoundLocal}
}

func combineTyped(a, b TypedExpression) TypedExpression {
	a.Aggregation = mergeExpressionAggregation(a, b)
	a.Symbols = mergeSymbols(a.Symbols, b.Symbols)
	a.BindingResolutions = appendBindingResolutions(a.BindingResolutions, b.BindingResolutions)
	a.FunctionResolutions = appendFunctionResolutions(a.FunctionResolutions, b.FunctionResolutions)
	a.CastResolutions = appendCastResolutions(a.CastResolutions, b.CastResolutions)
	a.PathResolutions = appendPathResolutions(a.PathResolutions, b.PathResolutions)
	a.Nullability = MergeNullability(a.Nullability, b.Nullability)
	a.Deterministic = a.Deterministic && b.Deterministic
	a.RowDependent = a.RowDependent || b.RowDependent
	if a.Type == TypeUnknown {
		a.Type = b.Type
	}
	return a
}

func appendBindingResolutions(a, b []SymbolBindingEvidence) []SymbolBindingEvidence {
	out := append([]SymbolBindingEvidence{}, a...)
	return append(out, b...)
}

func appendFunctionResolutions(a, b []FunctionResolutionEvidence) []FunctionResolutionEvidence {
	out := append([]FunctionResolutionEvidence{}, a...)
	return append(out, b...)
}

func appendCastResolutions(a, b []CastResolutionEvidence) []CastResolutionEvidence {
	out := append([]CastResolutionEvidence{}, a...)
	return append(out, b...)
}

func appendPathResolutions(a, b []PathResolutionEvidence) []PathResolutionEvidence {
	out := append([]PathResolutionEvidence{}, a...)
	return append(out, b...)
}

func mergeExpressionAggregation(a, b TypedExpression) AggregationState {
	if a.Aggregation == AggregationMixed || b.Aggregation == AggregationMixed {
		return AggregationMixed
	}
	if a.Aggregation == AggregationAggregate && b.Aggregation == AggregationAggregate {
		return AggregationAggregate
	}
	if a.Aggregation == AggregationAggregate {
		if b.RowDependent {
			return AggregationMixed
		}
		return AggregationAggregate
	}
	if b.Aggregation == AggregationAggregate {
		if a.RowDependent {
			return AggregationMixed
		}
		return AggregationAggregate
	}
	return AggregationScalar
}

func mergeSymbols(a, b []BoundSymbol) []BoundSymbol {
	out := append([]BoundSymbol{}, a...)
	seen := map[string]struct{}{}
	for _, s := range out {
		seen[symbolKey(s)] = struct{}{}
	}
	for _, s := range b {
		k := symbolKey(s)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

func symbolKey(s BoundSymbol) string { return string(s.Kind) + "|" + s.Qualifier + "|" + s.Name }

func removeLocalSymbols(symbols []BoundSymbol, params []string) []BoundSymbol {
	locals := map[string]struct{}{}
	for _, p := range params {
		locals[strings.ToLower(p)] = struct{}{}
	}
	out := make([]BoundSymbol, 0, len(symbols))
	for _, s := range symbols {
		if s.Kind == BoundLocal {
			if _, ok := locals[strings.ToLower(s.Name)]; ok {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func commonType(a, b SemanticType) SemanticType {
	if a == TypeUnknown || a == TypeNull {
		return b
	}
	if b == TypeUnknown || b == TypeNull {
		return a
	}
	if a == b {
		return a
	}
	if (a == TypeInteger && b == TypeDecimal) || (a == TypeDecimal && b == TypeInteger) {
		return TypeDecimal
	}
	return TypeUnknown
}

func literalType(v string) SemanticType {
	u := strings.ToUpper(strings.TrimSpace(v))
	if u == "NULL" {
		return TypeNull
	}
	if u == "TRUE" || u == "FALSE" {
		return TypeBoolean
	}
	if strings.HasPrefix(v, "'") {
		return TypeString
	}
	if strings.Contains(v, ".") {
		return TypeDecimal
	}
	return TypeInteger
}

func literalNullability(t SemanticType) Nullability {
	if t == TypeNull {
		return NullabilityNullable
	}
	return NullabilityNonNull
}

func semanticTypeFromTypeRef(t TypeRef) SemanticType {
	switch strings.ToUpper(t.Name) {
	case "BOOL", "BOOLEAN":
		return TypeBoolean
	case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT":
		return TypeInteger
	case "DECIMAL", "NUMBER", "NUMERIC", "FLOAT", "DOUBLE", "REAL":
		return TypeDecimal
	case "STRING", "VARCHAR", "CHAR", "TEXT":
		return TypeString
	case "DATE":
		return TypeDate
	case "TIME":
		return TypeTime
	case "TIMESTAMP", "DATETIME":
		return TypeTimestamp
	case "VARIANT", "OBJECT", "JSON":
		return TypeVariant
	case "ARRAY":
		return TypeArray
	default:
		return TypeUnknown
	}
}

func binaryResultType(op string, l, r SemanticType) SemanticType {
	switch strings.ToUpper(op) {
	case "=", "!=", "<>", "<", "<=", ">", ">=", "AND", "OR", "LIKE", "ILIKE", "NOT LIKE", "NOT ILIKE":
		return TypeBoolean
	case "+", "-", "*", "/", "%":
		return commonType(l, r)
	default:
		return commonType(l, r)
	}
}

func booleanCompatible(t SemanticType) bool {
	return t == TypeUnknown || t == TypeNull || t == TypeBoolean
}

func numericCompatible(t SemanticType) bool {
	return t == TypeUnknown || t == TypeNull || t == TypeInteger || t == TypeDecimal
}

func isArithmetic(op string) bool {
	switch op {
	case "+", "-", "*", "/", "%":
		return true
	default:
		return false
	}
}

func typeError(expr Expr, message string) *SemanticError {
	return &SemanticError{Code: ErrTypeMismatch, Message: message, Span: SpanOf(expr)}
}
