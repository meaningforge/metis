package expression

// EvidenceSymbolResolver resolves one identifier with the binding evidence that
// explains the selected semantic scope. SemanticAnalyzer integration is kept as
// a separate step so resolver precedence can be tested independently.
type EvidenceSymbolResolver interface {
	ResolveWithEvidence(parts []string, span SourceSpan) (BoundSymbol, SymbolBindingEvidence, error)
}

// PrecedenceResolver composes an explicit semantic scope chain with an existing
// SymbolResolver fallback. Explicit scopes always win. Ambiguity at the nearest
// matching explicit scope fails closed and must never fall through to the
// fallback resolver.
type PrecedenceResolver struct {
	scope    *SymbolScope
	fallback SymbolResolver
}

func NewPrecedenceResolver(scope *SymbolScope, fallback SymbolResolver) *PrecedenceResolver {
	return &PrecedenceResolver{scope: scope, fallback: fallback}
}

// Resolve preserves the existing SymbolResolver contract for compatibility.
// Callers that need diagnostics/evidence should use ResolveWithEvidence.
func (r *PrecedenceResolver) Resolve(parts []string) (BoundSymbol, bool) {
	symbol, _, err := r.ResolveWithEvidence(parts, SourceSpan{})
	return symbol, err == nil
}

func (r *PrecedenceResolver) ResolveWithEvidence(parts []string, span SourceSpan) (BoundSymbol, SymbolBindingEvidence, error) {
	ref := referenceFromParts(parts)
	if r != nil && r.scope != nil {
		symbol, evidence, err := r.scope.Resolve(ref, span)
		if err == nil {
			return symbol, evidence, nil
		}
		bindingErr, ok := err.(*ScopeBindingError)
		if !ok || bindingErr.Code != ScopeBindingUnbound {
			return BoundSymbol{}, evidence, err
		}
	}

	if r != nil && r.fallback != nil {
		if symbol, ok := r.fallback.Resolve(parts); ok {
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
	}

	evidence := SymbolBindingEvidence{Reference: ref, Span: span, Reason: BindingReasonUnbound}
	return BoundSymbol{}, evidence, &ScopeBindingError{Code: ScopeBindingUnbound, Evidence: evidence}
}

func referenceFromParts(parts []string) Reference {
	if len(parts) == 0 {
		return Reference{}
	}
	if len(parts) == 1 {
		return Reference{Name: parts[0]}
	}
	return Reference{Qualifier: parts[0], Name: parts[1]}
}
