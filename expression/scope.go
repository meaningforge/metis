package expression

import (
	"fmt"
	"sort"
	"strings"
)

// ScopeKind identifies the semantic namespace that contributed a binding.
// The scope chain, rather than this enum, defines precedence: the nearest
// scope with a matching name shadows all outer scopes.
type ScopeKind string

const (
	ScopeLambda   ScopeKind = "LAMBDA"
	ScopeMetric   ScopeKind = "METRIC"
	ScopeQuery    ScopeKind = "QUERY"
	ScopeExternal ScopeKind = "EXTERNAL"
)

// BindingReason is retained in binding evidence so later analyzer/planner
// diagnostics can explain why a symbol was selected or rejected.
type BindingReason string

const (
	BindingReasonNearestScope BindingReason = "NEAREST_SCOPE_MATCH"
	BindingReasonAmbiguous    BindingReason = "AMBIGUOUS_NEAREST_SCOPE"
	BindingReasonUnbound      BindingReason = "UNBOUND"
)

// SymbolBindingCandidate records one symbol considered at the winning scope.
// Candidates from shadowed outer scopes are deliberately excluded: once a
// scope contains the requested name, lexical shadowing prevents outer lookup.
type SymbolBindingCandidate struct {
	Scope  ScopeKind
	Symbol BoundSymbol
}

// SymbolBindingEvidence is the engine-neutral explanation of one binding
// decision. Span belongs to the common expression AST and is preserved here so
// analyzer diagnostics can be produced without re-resolving the symbol.
type SymbolBindingEvidence struct {
	Reference     Reference
	Span          SourceSpan
	Candidates    []SymbolBindingCandidate
	Selected      BoundSymbol
	SelectedScope ScopeKind
	Found         bool
	Reason        BindingReason
}

type ScopeBindingErrorCode string

const (
	ScopeBindingUnbound   ScopeBindingErrorCode = "UNBOUND"
	ScopeBindingAmbiguous ScopeBindingErrorCode = "AMBIGUOUS"
)

// ScopeBindingError is intentionally analyzer-neutral. I2.2 maps it to stable
// SemanticError codes while retaining this evidence and its SourceSpan.
type ScopeBindingError struct {
	Code     ScopeBindingErrorCode
	Evidence SymbolBindingEvidence
}

func (e *ScopeBindingError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("scope binding %s for %s at [%d,%d)", e.Code, referenceName(e.Evidence.Reference), e.Evidence.Span.Start, e.Evidence.Span.End)
}

// SymbolScope is one lexical semantic scope. Symbols are immutable after
// construction so resolution remains deterministic and safe to reuse.
type SymbolScope struct {
	kind    ScopeKind
	parent  *SymbolScope
	symbols []BoundSymbol
}

func NewSymbolScope(kind ScopeKind, parent *SymbolScope, symbols ...BoundSymbol) *SymbolScope {
	owned := append([]BoundSymbol(nil), symbols...)
	return &SymbolScope{kind: kind, parent: parent, symbols: owned}
}

func (s *SymbolScope) Kind() ScopeKind {
	if s == nil {
		return ""
	}
	return s.kind
}

func (s *SymbolScope) Parent() *SymbolScope {
	if s == nil {
		return nil
	}
	return s.parent
}

func (s *SymbolScope) Symbols() []BoundSymbol {
	if s == nil {
		return nil
	}
	return append([]BoundSymbol(nil), s.symbols...)
}

// Resolve walks from the current scope outward. The first scope containing at
// least one matching symbol wins. A single candidate is selected; multiple
// candidates at that same nearest scope are an ambiguity and outer scopes are
// never consulted as a tie-breaker.
func (s *SymbolScope) Resolve(ref Reference, span SourceSpan) (BoundSymbol, SymbolBindingEvidence, error) {
	for scope := s; scope != nil; scope = scope.parent {
		candidates := scope.matchingCandidates(ref)
		if len(candidates) == 0 {
			continue
		}
		evidence := SymbolBindingEvidence{
			Reference:  ref,
			Span:       span,
			Candidates: candidates,
		}
		if len(candidates) > 1 {
			evidence.Reason = BindingReasonAmbiguous
			return BoundSymbol{}, evidence, &ScopeBindingError{Code: ScopeBindingAmbiguous, Evidence: evidence}
		}
		selected := candidates[0]
		evidence.Selected = selected.Symbol
		evidence.SelectedScope = selected.Scope
		evidence.Found = true
		evidence.Reason = BindingReasonNearestScope
		return selected.Symbol, evidence, nil
	}

	evidence := SymbolBindingEvidence{Reference: ref, Span: span, Reason: BindingReasonUnbound}
	return BoundSymbol{}, evidence, &ScopeBindingError{Code: ScopeBindingUnbound, Evidence: evidence}
}

func (s *SymbolScope) matchingCandidates(ref Reference) []SymbolBindingCandidate {
	candidates := make([]SymbolBindingCandidate, 0)
	for _, symbol := range s.symbols {
		if !symbolMatchesReference(symbol, ref) {
			continue
		}
		candidates = append(candidates, SymbolBindingCandidate{Scope: s.kind, Symbol: symbol})
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i].Symbol, candidates[j].Symbol
		if left.Qualifier != right.Qualifier {
			return left.Qualifier < right.Qualifier
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Kind < right.Kind
	})
	return candidates
}

func symbolMatchesReference(symbol BoundSymbol, ref Reference) bool {
	if !strings.EqualFold(symbol.Name, ref.Name) {
		return false
	}
	if ref.Qualifier == "" {
		return true
	}
	return strings.EqualFold(symbol.Qualifier, ref.Qualifier)
}

func referenceName(ref Reference) string {
	if ref.Qualifier == "" {
		return ref.Name
	}
	return ref.Qualifier + "." + ref.Name
}
