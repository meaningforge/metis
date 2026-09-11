# RFC-0024: Scope-aware Symbol Binding

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** expression semantic binding
- **Supersedes:** None

## Summary

Phase I2 makes semantic symbol binding explicitly scope-aware, deterministic, and explainable. Metis batch-binds non-local expression references against one semantic model and now uses the same shared scope contract for lexical lambda binding and typed analysis.

RFC-0024 introduces an engine-neutral lexical scope model and `SymbolBindingEvidence`. The nearest scope containing a matching reference shadows outer scopes. Multiple matches at that same nearest scope are an ambiguity; an outer scope must never be used as a semantic tie-breaker.

Tracking: #262, #264.

## Motivation

Complex expressions need one deterministic answer to questions such as:

- does a lambda parameter shadow a catalog field with the same name?
- when can an unqualified field be resolved safely?
- how is a qualified reference distinguished from a local name?
- which semantic scope supplied the chosen symbol?
- which candidates made a reference ambiguous?

`ReferenceResolver` batch-binds external references and Catalog owns model-specific ambiguity rules. Phase I2 removes the remaining independent lambda-local precedence logic from `SemanticAnalyzer` and makes binding decisions reusable typed evidence.

Without a shared scope contract, later semi-structured expressions, planner explainability, and nested semantic contexts would risk reintroducing independent per-reference guessing.

## Design

### Scope kinds

The common expression layer defines semantic scope kinds:

- `LAMBDA`: lexical function/lambda parameters;
- `METRIC`: metric-level semantic names;
- `QUERY`: query/model relation symbols;
- `EXTERNAL`: an outer resolver boundary when no nearer semantic scope matches.

These names describe evidence. The enum does not by itself define precedence. Precedence comes from the explicit parent chain built for a semantic context.

For typed semantic analysis the constructed chain is `LAMBDA > METRIC > QUERY > EXTERNAL`.

### Nearest-scope shadowing

Resolution starts at the current scope and walks outward.

For each scope:

1. collect every symbol in that scope matching the reference;
2. if there are no matches, continue to the parent;
3. if exactly one match exists, select it and stop;
4. if multiple matches exist, fail as ambiguous and stop.

Once a scope has any matching candidate, outer scopes are not considered. Registration order is never a tie-breaker.

Qualified references match both qualifier and name. Unqualified references match by name and can therefore surface ambiguity when multiple symbols in the same scope share that name.

### Binding evidence

Each resolution retains `SymbolBindingEvidence` containing:

- the common `Reference`;
- the common-AST `SourceSpan`;
- deterministic candidates at the nearest matching scope;
- the selected symbol and selected scope when resolution succeeds;
- a stable reason describing selected, ambiguous, or unbound resolution.

Candidate ordering is deterministic so evidence can be compared in tests and surfaced in explain/debug flows without depending on map or registration order.

Typed analysis retains these values in `TypedExpression.BindingResolutions`. Lambda-local bindings remain explainable through this evidence even though lambda-local symbols are removed from the external `Symbols` set.

### Resolver precedence boundary

`PrecedenceResolver` composes two layers without merging their semantics:

1. an explicit `SymbolScope` chain;
2. an existing `SymbolResolver` fallback.

The explicit scope chain is authoritative. If it resolves a symbol, the fallback is not consulted. If the nearest explicit scope is ambiguous, resolution fails closed and the fallback is not consulted. The fallback is used only when every explicit scope is unbound for that reference.

Fallback success is represented as `ScopeExternal` evidence so callers receive one uniform binding explanation regardless of which resolver layer selected the symbol.

`EvidenceSymbolResolver` exposes this result together with `SourceSpan`-bearing `SymbolBindingEvidence`, while `PrecedenceResolver.Resolve` preserves the existing `SymbolResolver` compatibility contract for callers that do not yet consume evidence.

### Analyzer integration

`SemanticAnalyzer` uses one shared `SymbolScope` head instead of maintaining a separate stack of private local maps.

When its resolver exposes canonical symbols, analyzer construction lifts metric symbols into `METRIC` scope and column symbols into `QUERY` scope. The original resolver remains the `EXTERNAL` fallback. Entering a lambda pushes a `LAMBDA` scope whose parent is the current scope; leaving the lambda restores the parent. Nested lambdas therefore capture outer locals through the same parent chain used by all other semantic scopes.

Identifier analysis resolves through `PrecedenceResolver`, retains the returned binding evidence, and maps analyzer-neutral scope errors to stable semantic diagnostics. Query-scope ambiguity therefore fails before external fallback, while a qualified reference can bypass a non-matching metric/local scope and bind to the matching query symbol.

### Error layering

The scope/resolver abstraction returns analyzer-neutral `ScopeBindingError` values for ambiguous and unbound resolution. It does not invent user-facing semantic error codes.

Typed analysis maps ambiguity to `AMBIGUOUS_SYMBOL` and unbound resolution to `UNBOUND_SYMBOL`, preserving the original common-AST `SourceSpan`.

Catalog remains responsible for Catalog/model-specific candidate construction and dataset-boundary rules.

### Compatibility

Phase I2 does not replace the existing `ReferenceResolver`, `Bind`, `BoundExpression`, or `SymbolResolver` contracts. Evidence-aware resolution is additive, and legacy resolvers remain valid external fallback providers.

No physical dialect, execution binding, SQL lowering, or database execution behavior belongs in this scope model.

## Sequencing

### I2.1 — Scope model & binding evidence

- introduce `ScopeKind` and immutable `SymbolScope`;
- define deterministic nearest-scope resolution;
- define `SymbolBindingEvidence` and analyzer-neutral scope errors;
- cover shadowing, qualification, ambiguity, unbound references, SourceSpan retention, and ownership in unit tests.

### I2.2 — Resolver precedence hardening

- compose explicit scopes with the existing resolver boundary;
- make scope-first/fallback-second precedence explicit;
- fail closed on nearest-scope ambiguity instead of falling back;
- preserve SourceSpan-bearing evidence across explicit-scope and external-fallback resolution;
- retain the legacy `SymbolResolver` compatibility API.

### I2.3 — Lambda/query/metric scope integration

- integrate the shared scope/precedence contract with semantic analysis;
- map ambiguity/unbound failures to stable SourceSpan-bearing semantic diagnostics;
- retain binding evidence in typed analysis;
- remove duplicated local-scope guessing where the shared scope contract can own it;
- harden nested lambda capture and local-vs-catalog collisions;
- confirm metric/query/external precedence and dataset/model scope boundaries end to end.

## Alternatives

### Keep lambda scopes private to SemanticAnalyzer

Rejected because later semantic contexts would need to reproduce shadowing and evidence rules independently.

### Let Catalog decide all lexical scope behavior

Rejected because lexical lambda/local scope is expression semantics, while Catalog should remain responsible for project/model semantic indexes and model-specific candidate construction.

### Use registration order to resolve collisions

Rejected because semantic correctness must not depend on construction order.

### Search outer scopes or fallback after an inner ambiguity

Rejected because it violates lexical shadowing and can silently choose a semantically unrelated symbol.

## Acceptance criteria

I2.1 is complete when:

- nearest-scope shadowing is deterministic;
- qualified references can bypass non-matching inner local names;
- ambiguity is detected at the nearest matching scope;
- candidate evidence is deterministically ordered;
- unbound and ambiguous evidence retains `SourceSpan`;
- scope inputs/outputs are defensively owned.

I2.2 is complete when:

- explicit scope matches win without invoking fallback resolution;
- qualified references can fall through when an inner scope does not match;
- nearest-scope ambiguity fails closed without invoking fallback resolution;
- external fallback success is represented with deterministic `ScopeExternal` evidence;
- fallback misses retain the original `Reference` and `SourceSpan`;
- compatibility callers cannot accidentally treat ambiguity as success.

I2.3 is complete when:

- `SemanticAnalyzer` uses the shared scope parent chain for lambda locals;
- typed identifier resolution follows `LAMBDA > METRIC > QUERY > EXTERNAL`;
- nested lambda capture resolves outer locals through the shared scope chain;
- lambda locals do not leak into external bound-symbol output;
- successful typed bindings retain `SymbolBindingEvidence`;
- query/metric ambiguity fails closed before external fallback;
- ambiguous and unbound symbols surface stable SourceSpan-bearing semantic diagnostics;
- the normative typed-expression specification states the same precedence and evidence contract;
- existing correctness, conformance, E2E, Doris, ClickHouse, and container gates remain green.

## Documentation updates

The normative typed-expression specification is updated together with I2.3 so RFC-0024 is not the only statement of current binding behavior.
