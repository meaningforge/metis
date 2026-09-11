package expression

const ErrAmbiguousSymbol SemanticErrorCode = "AMBIGUOUS_SYMBOL"

// resolveAnalyzerSymbol is the single semantic binding entry point used by the
// analyzer. Evidence-aware resolvers preserve their scope decision; legacy
// resolvers remain compatible and are represented as an external scope hit.
func resolveAnalyzerSymbol(resolver SymbolResolver, parts []string, span SourceSpan) (BoundSymbol, SymbolBindingEvidence, error) {
	ref := referenceFromParts(parts)
	if resolver == nil {
		evidence := SymbolBindingEvidence{Reference: ref, Span: span, Reason: BindingReasonUnbound}
		return BoundSymbol{}, evidence, &ScopeBindingError{Code: ScopeBindingUnbound, Evidence: evidence}
	}

	if evidenceResolver, ok := resolver.(EvidenceSymbolResolver); ok {
		return evidenceResolver.ResolveWithEvidence(parts, span)
	}

	if symbol, ok := resolver.Resolve(parts); ok {
		candidate := SymbolBindingCandidate{Scope: ScopeExternal, Symbol: symbol}
		evidence := SymbolBindingEvidence{
			Reference:     ref,
			Span:          span,
			Candidates:    []SymbolBindingCandidate{candidate},
			Selected:      symbol,
			SelectedScope: ScopeExternal,
			Found:         true,
			Reason:        BindingReasonNearestScope,
		}
		return symbol, evidence, nil
	}

	evidence := SymbolBindingEvidence{Reference: ref, Span: span, Reason: BindingReasonUnbound}
	return BoundSymbol{}, evidence, &ScopeBindingError{Code: ScopeBindingUnbound, Evidence: evidence}
}

func semanticBindingError(err error, evidence SymbolBindingEvidence) error {
	bindingErr, ok := err.(*ScopeBindingError)
	if !ok {
		return err
	}

	message := referenceName(evidence.Reference)
	switch bindingErr.Code {
	case ScopeBindingAmbiguous:
		return &SemanticError{Code: ErrAmbiguousSymbol, Message: "ambiguous symbol " + message, Span: evidence.Span}
	case ScopeBindingUnbound:
		return &SemanticError{Code: ErrUnboundSymbol, Message: "cannot bind symbol " + message, Span: evidence.Span}
	default:
		return err
	}
}

// symbolProvider is implemented by BoundExpression and any future resolver that
// can expose the canonical symbols already established for an expression.
type symbolProvider interface {
	Symbols() []BoundSymbol
}

// semanticScopeFromResolver lifts already-bound symbols into the shared lexical
// scope model. Metric names intentionally sit closer than query columns, while
// the original resolver remains the external fallback for names not represented
// by the explicit scope chain.
func semanticScopeFromResolver(resolver SymbolResolver) *SymbolScope {
	provider, ok := resolver.(symbolProvider)
	if !ok {
		return nil
	}

	var metrics []BoundSymbol
	var query []BoundSymbol
	for _, symbol := range provider.Symbols() {
		switch symbol.Kind {
		case BoundMetric:
			metrics = append(metrics, symbol)
		case BoundColumn:
			query = append(query, symbol)
		}
	}

	var scope *SymbolScope
	if len(query) > 0 {
		scope = NewSymbolScope(ScopeQuery, nil, query...)
	}
	if len(metrics) > 0 {
		scope = NewSymbolScope(ScopeMetric, scope, metrics...)
	}
	return scope
}

func (a *SemanticAnalyzer) resolveSymbol(parts []string, span SourceSpan) (BoundSymbol, SymbolBindingEvidence, error) {
	resolver := NewPrecedenceResolver(a.scope, a.resolver)
	return resolveAnalyzerSymbol(resolver, parts, span)
}
