# Typed Expression Analysis Specification

This document is normative for Metis typed expression analysis. Keywords **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are used normatively.

## 1. Boundary

Typed expression analysis runs after common-AST parsing and semantic symbol binding and before semantic planning/lowering.

It MUST remain engine-neutral. It MUST NOT choose physical cast syntax, database execution placement, statistics, cost models, or join order.

## 2. Typed expression evidence

`TypedExpression` MUST carry the analyzer-level semantic type, aggregation state, determinism, row-dependence, bound symbols, symbol-binding evidence, function-resolution evidence, explicit-cast evidence, semi-structured path-resolution evidence, and nullability known at the analysis boundary.

Unknown information MUST be represented explicitly rather than guessed.

Evidence produced by a nested typed expression MUST remain available when that expression is wrapped by a cast, binary expression, function call, CASE, tuple, or other semantic composition that preserves the underlying dependency.

## 3. Scope-aware symbol binding

Typed analysis MUST use the shared scope/resolver contract rather than maintain an independent symbol-precedence implementation.

For analyzer-visible symbols the semantic precedence is:

1. `LAMBDA` lexical locals;
2. `METRIC` semantic names;
3. `QUERY` relation/model symbols;
4. `EXTERNAL` resolver fallback.

The nearest scope containing at least one matching candidate is authoritative. A single candidate MUST be selected. Multiple candidates in that nearest scope MUST fail closed as `AMBIGUOUS_SYMBOL`; outer scopes and external fallback MUST NOT be used as tie-breakers.

Qualified references MUST match qualifier and name and MAY therefore bypass nearer scopes whose symbols do not satisfy the qualifier. Unqualified references match by name and MUST surface same-scope ambiguity deterministically.

Every successful analyzer binding MUST preserve `SymbolBindingEvidence`, including the reference, original common-AST `SourceSpan`, deterministic nearest-scope candidates, selected symbol, selected scope, and selection reason. Unbound and ambiguous failures MUST preserve the same source span in their semantic diagnostics.

Lambda parameters MUST use `LAMBDA` scopes. Nested lambdas MUST capture outer lambda locals through the scope parent chain. Lambda locals MUST NOT leak into the external `Symbols` set of the analyzed expression, although their binding evidence MAY remain available for explain/debug consumers.

Existing bound/query symbols MAY be lifted into explicit `METRIC` and `QUERY` scopes when the resolver can expose its canonical bound symbols. The original resolver remains the `EXTERNAL` fallback for names not represented by explicit scopes.

## 4. Function resolution

Known functions MUST resolve against the complete overload set for the case-insensitive function name.

Resolution MUST be deterministic:

- exact semantic-type matches beat implicit coercions;
- the warehouse-neutral implicit coercion lattice is limited to `NULL`/`UNKNOWN` adaptation to concrete parameters and `INTEGER -> DECIMAL` widening;
- narrowing and cross-domain conversions MUST NOT be introduced implicitly;
- equal-cost best overloads MUST fail as `AMBIGUOUS_FUNCTION`;
- no compatible overload MUST fail as `TYPE_MISMATCH`;
- arity mismatch MUST fail as `INVALID_FUNCTION_ARITY`.

Successful resolution MUST preserve original argument types, resolved parameter types, coercion evidence, and total resolution cost.

## 5. Explicit cast semantics

Authored `CAST(expr AS type)` and `expr::type` MUST use the explicit-cast contract rather than the implicit function-coercion lattice.

The analyzer MUST call the common `ResolveExplicitCast` contract and MUST fail unsupported or invalid conversions as `INVALID_CAST` with the `CastExpr` `SourceSpan`.

Successful casts MUST preserve typed evidence containing source type, target type, semantic cast family, resulting nullability, and source span.

Explicit authored narrowing such as `DECIMAL -> INTEGER` MAY be semantically admissible even though the same conversion is forbidden implicitly.

Physical renderability remains a target-capability/lowering concern.

## 6. Semi-structured path semantics

`PathAccessExpr` MUST be analyzed through the shared engine-neutral `ResolvePathAccess` contract. The analyzer MUST NOT infer path validity from the parser dialect profile or unconditionally convert a path result to `VARIANT`.

The analyzer MUST first type the path base. Static key segments are translated directly to typed `KEY` steps. `INDEX` and `DYNAMIC` segment operands MUST be analyzed exactly once and their resulting semantic types supplied to the path resolver.

The common path type rules are:

- key access on `VARIANT` returns `VARIANT`;
- integer/string/unknown indexed or dynamic access on `VARIANT` returns `VARIANT`;
- static key access directly on `ARRAY` is invalid;
- integer/unknown indexed or dynamic access on `ARRAY` succeeds but returns `UNKNOWN` because element type is not yet modeled;
- path access from `UNKNOWN` preserves `UNKNOWN` rather than guessing `VARIANT`;
- direct path access from scalar types fails closed.

Successful path analysis MUST preserve ordered `PathResolutionEvidence`, including the engine-neutral `PathAccessResolution` and the common-AST path span. This evidence MUST survive subsequent casts and ordinary typed expression composition.

Path-resolution failures MUST surface as `INVALID_PATH_ACCESS`. The diagnostic MUST retain the `PathAccessExpr` span for structural/key failures and SHOULD use the index/dynamic operand span when the failure is caused by an invalid typed operand.

An expression such as `payload:customer.region::STRING` is semantically a `PathAccessExpr` followed by the existing explicit `CastExpr` contract. Path analysis and cast analysis MUST therefore retain distinct evidence and failure boundaries.

Physical path syntax/renderability remains a target capability/lowering concern; no dialect-specific semantic analyzer branch is permitted.

## 7. Nullability

Analyzer nullability uses exactly three states:

- `NON_NULL`: proven not to produce SQL NULL at this expression boundary;
- `NULLABLE`: may produce SQL NULL;
- `UNKNOWN`: insufficient semantic information to prove either state.

The zero/default symbol nullability MUST normalize to `UNKNOWN` for backward compatibility.

The analyzer MUST apply these conservative rules:

- literal `NULL` is `NULLABLE`;
- non-null literals are `NON_NULL`;
- bound symbols use their declared nullability or `UNKNOWN`;
- explicit casts preserve source nullability, with literal NULL casts remaining `NULLABLE`;
- `IS NULL` / `IS NOT NULL` is always a `NON_NULL` boolean;
- ordinary binary/predicate expressions merge operand nullability fail-closed;
- scalar functions conservatively merge argument nullability unless a future signature contract defines stronger semantics;
- aggregate function result nullability remains `UNKNOWN` unless a future signature contract proves otherwise;
- `CASE` result nullability derives only from result branches; a missing `ELSE` makes the result `NULLABLE`;
- index and semi-structured path traversal remain `UNKNOWN` unless a future schema/capability contract proves stronger behavior.

`MergeNullability` MUST return `NULLABLE` if any input is nullable, `NON_NULL` only when all inputs are non-null, and `UNKNOWN` otherwise.

## 8. Aggregation safety

Typed function/cast/path analysis MUST preserve existing aggregation safety. Nested aggregate calls and aggregate/raw-row mixes MUST continue to fail closed.

A cast or path traversal MUST NOT erase the aggregation state or row-dependence of its base expression. Index/dynamic path operands participate in normal typed aggregation/dependency combination.

## 9. Diagnostics

Typed semantic failures MUST retain common-AST `SourceSpan` information.

The stable error codes covered by this specification are:

- `UNKNOWN_FUNCTION`
- `INVALID_FUNCTION_ARITY`
- `AMBIGUOUS_FUNCTION`
- `AMBIGUOUS_SYMBOL`
- `TYPE_MISMATCH`
- `INVALID_CAST`
- `INVALID_PATH_ACCESS`
- `INVALID_AGGREGATION`
- `UNBOUND_SYMBOL`

## 10. Evolution

Dialect-specific analyzer branches MUST NOT be added to model warehouse syntax. New syntax belongs in parser dialect profiles when necessary; new semantic behavior belongs in shared analyzer contracts or explicit target capability/lowering contracts.

Unknown warehouse-native functions MAY remain supported only through the existing explicit compatibility option and MUST keep unknown return type, unknown nullability, and non-assumed determinism.
