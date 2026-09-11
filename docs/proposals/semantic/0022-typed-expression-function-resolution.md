# RFC-0022: Typed Expression Function Resolution

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

Phase I hardens Metis expression analysis so function calls resolve against typed semantic signatures deterministically instead of relying on one boolean argument predicate per function name.

I1.1 introduced an overload-preserving registry contract, explicit analyzer-level parameter targets, deterministic exact-match/coercion scoring, and typed coercion evidence. I1.2 wired that contract into `SemanticAnalyzer`, mapped resolution failures to SourceSpan-bearing semantic diagnostics, and retained selected function-resolution evidence in typed-expression output.

Tracking: #262, #263.

## Motivation

Before I1, one function name mapped to one signature, `TypeConstraint` only answered yes/no, and compatible arguments did not retain evidence of how their types were reconciled. That allowed overload choice or implicit conversion behavior to become registration-order-dependent or non-explainable.

## Design

### Overload sets

`FunctionRegistry` preserves all signatures registered for a case-insensitive function name. Typed analyzer paths resolve the complete overload set; compatibility `Lookup` remains for older callers but is not the semantic tie-breaker.

### Parameter targets and resolution

`FunctionSignature` may declare semantic `ArgTargets` / `VariadicTarget` in addition to `TypeConstraint` predicates. Resolution evaluates arity, type constraints, and optional target types for every candidate. Lowest total coercion cost wins.

The warehouse-neutral implicit coercion lattice is intentionally narrow:

- exact type: cost 0;
- `NULL` to a concrete target: cost 1;
- `INTEGER` to `DECIMAL`: cost 1;
- `UNKNOWN` to a concrete target: cost 2;
- narrowing and cross-domain conversions: unsupported without an explicit cast.

Equal-cost best candidates fail as ambiguous. Registration order is never a semantic tie-breaker.

### Analyzer integration

`SemanticAnalyzer` first produces typed call arguments, then resolves the complete argument vector against arity-compatible overloads. Return-type inference uses resolved parameter types.

Stable diagnostics are:

- no known function -> `UNKNOWN_FUNCTION`;
- no arity-compatible overload -> `INVALID_FUNCTION_ARITY`;
- no type-compatible overload -> `TYPE_MISMATCH`;
- equal-cost best overloads -> `AMBIGUOUS_FUNCTION`.

All diagnostics retain common-AST `SourceSpan`. Aggregate calls remain fail-closed for nested aggregate arguments.

### Evidence

Successful resolution preserves original argument types, resolved parameter types, total coercion cost, and per-argument coercion evidence (`index`, `from`, `to`) as `FunctionResolutionEvidence` on `TypedExpression`. Compound expressions retain nested evidence so later planning/lowering does not re-run overload selection.

The evidence is engine-neutral and does not choose physical cast syntax or execution placement.

## Alternatives

Registration-order tie-breaking, renderer-owned overload selection, and a broad warehouse-specific implicit-cast matrix were rejected because they make semantic behavior non-deterministic or dialect-coupled.

## Test and acceptance criteria

- overload sets are preserved case-insensitively;
- exact matches beat coercible alternatives regardless of registration order;
- `INTEGER -> DECIMAL` produces explicit coercion evidence;
- equal-cost overloads fail as `AMBIGUOUS_FUNCTION` with `SourceSpan`;
- incompatible overload sets fail as `TYPE_MISMATCH` with `SourceSpan`;
- implicit narrowing such as `DECIMAL -> INTEGER` is rejected;
- analyzer return-type inference uses resolved parameter types;
- nested aggregate safety remains fail-closed;
- existing expression/analyzer behavior remains green.

## Current specification

The implemented function-resolution contract is normative in `docs/specs/semantic/typed-expression-analysis.md`. Explicit authored cast and nullability behavior is defined by RFC-0023 and the same current specification.
