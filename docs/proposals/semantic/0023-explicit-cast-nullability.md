# RFC-0023: Explicit Cast and Nullability Semantics

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** expression semantic analysis
- **Supersedes:** None

## Summary

Phase I1 separates authored explicit casts from implicit function coercion and introduces the warehouse-neutral nullability model used by typed expression analysis.

I1.3 established the standalone resolution contract. I1.4 integrates it into `SemanticAnalyzer`, adds typed cast evidence and `INVALID_CAST` diagnostics with `SourceSpan`, and propagates nullability conservatively through typed expression forms.

Tracking: #262, #263.

## Motivation

RFC-0022 defines deterministic function overload resolution and a deliberately narrow implicit coercion lattice. Explicit `CAST(...)` / `::TYPE` syntax has a different semantic contract: the author has requested a conversion, so valid explicit narrowing or cross-domain conversions must not be rejected merely because they are forbidden as implicit coercions.

The analyzer also needs an explicit way to carry whether a typed value is known non-null, may be null, or has unknown nullability. Without that distinction, later planner rewrites cannot safely reason about null-preserving expressions.

## Design

### Explicit casts are separate from implicit coercion

`ResolveExplicitCast` is an engine-neutral semantic contract. It does not reuse function coercion cost rules and does not choose physical SQL syntax.

The initial admissible cast families are intentionally small:

- identity casts between equal concrete semantic types;
- `NULL` and `UNKNOWN` sources to a concrete target;
- numeric casts between `INTEGER` and `DECIMAL`, including authored narrowing;
- string/scalar conversions for boolean, numeric, and temporal scalar types;
- `DATE <-> TIMESTAMP` temporal casts;
- semi-structured `VARIANT` to scalar casts needed by typed path access.

Container-crossing casts such as `ARRAY -> DECIMAL` fail closed. Unknown or `NULL` targets are invalid semantic cast targets.

This contract only states that the semantic conversion is admissible. Dialect adapters and capability checks remain responsible for whether and how a target engine renders the physical conversion.

### Analyzer integration and evidence

`SemanticAnalyzer` resolves every common-AST `CastExpr` through `ResolveExplicitCast`. Unsupported or invalid conversions fail as `INVALID_CAST` and retain the cast node's `SourceSpan`.

A successful cast appends `CastResolutionEvidence` to `TypedExpression` containing source type, target type, semantic cast family, resulting nullability, and source span. Nested evidence is preserved through compound expressions.

### Nullability

The nullability lattice is:

- `NON_NULL`: known not to produce SQL NULL;
- `NULLABLE`: may produce SQL NULL;
- `UNKNOWN`: the semantic boundary does not know enough to prove either case.

The zero/default value normalizes to `UNKNOWN`, so existing bindings remain backward-compatible until schema nullability is supplied by binding metadata.

The analyzer applies conservative propagation: literals are known, explicit casts preserve source nullability, `IS NULL` is non-null, ordinary binary/predicate expressions fail-closed across their operands, scalar functions conservatively merge arguments, aggregate results remain unknown unless proven by a future signature contract, and missing `CASE ELSE` makes the result nullable. Index and semi-structured path access remain unknown pending their dedicated Phase I3 contract.

`MergeNullability` returns nullable if any input is nullable, non-null only when all inputs are non-null, and unknown otherwise.

## Boundary rules

- no dialect-specific branches in semantic analysis;
- no physical cast syntax selection in this package;
- no widening of the implicit function coercion lattice;
- no CBO, statistics, cost model, or join-order behavior;
- no database execution inside Metis.

## Acceptance criteria

- explicit `DECIMAL -> INTEGER` is admissible even though the same conversion is not an implicit function coercion;
- existing authored string-to-number casts remain semantically admissible;
- semi-structured `VARIANT -> scalar` casts have a common semantic path;
- invalid targets and unsupported container-crossing casts fail as `INVALID_CAST` with `SourceSpan`;
- typed cast evidence survives compound analysis;
- nullability is preserved across explicit casts and literal NULL remains nullable;
- `IS NULL` is a non-null boolean;
- `CASE` without `ELSE` is nullable;
- aggregate result nullability remains fail-closed as unknown;
- all touched Go files are gofmt-clean and correctness/conformance gates remain green.

## Current specification

The implemented contract is normative in `docs/specs/semantic/typed-expression-analysis.md`.
