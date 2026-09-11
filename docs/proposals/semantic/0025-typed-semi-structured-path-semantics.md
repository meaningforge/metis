# RFC-0025: Typed Semi-structured Path Semantics

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** typed semantic analysis for semi-structured path access
- **Supersedes:** None

## Summary

Phase I3 turns Metis' existing common `PathAccessExpr` syntax into deterministic typed semantics suitable for expressions such as `payload:customer.region::STRING` without teaching the semantic analyzer Snowflake-specific syntax.

I3.1 introduced the engine-neutral `ResolvePathAccess` contract. I3.2 integrates that contract into `SemanticAnalyzer`, preserves path-resolution evidence alongside function/cast/binding evidence, and exposes stable SourceSpan-bearing `INVALID_PATH_ACCESS` diagnostics. I3.3 closes the lowering/capability boundary with end-to-end regression coverage: semantic path evidence may survive planning even when the selected physical dialect is not registered, and that unsupported physical capability must fail explicitly rather than leaking vendor syntax through fallback guessing.

Tracking: #262, #265.

## Existing architecture

Metis parses semi-structured syntax into common AST nodes:

```text
payload:customer.region::STRING
        |
        v
CastExpr
  PathAccessExpr
    Base: IdentifierExpr(payload)
    Segments:
      PathKeySegment(customer)
      PathKeySegment(region)
```

The parser therefore separates source syntax from semantics. Path keys are navigation metadata, not catalog/semantic references. Typed analysis no longer unconditionally treats every `PathAccessExpr` as `VARIANT`; it validates the typed base and segment operands through the shared path contract.

## Design principles

1. Path semantics are engine-neutral. A semantic rule MUST NOT branch on Snowflake, Doris, ClickHouse, or another physical dialect.
2. Parser syntax and semantic validity remain separate. Dialect profiles decide whether syntax can be parsed; `ResolvePathAccess` decides whether the typed traversal is semantically valid.
3. Unknown type information remains explicit. The analyzer MUST NOT invent object shape or array element types that are not present in the semantic model.
4. Physical renderability remains a compiler/renderer capability concern.
5. Explicit casts after traversal continue to use the existing Phase I1 explicit-cast contract.

## Semantic step model

The common typed contract represents one traversal as `PathSemanticStep` values:

- `KEY`: static object/path key;
- `INDEX`: indexed bracket access;
- `DYNAMIC`: path selection whose operand is another expression.

For `INDEX` and `DYNAMIC`, the analyzer supplies the already-analyzed operand type. The path resolver does not independently bind or analyze expressions.

Successful resolution retains ordered `PathStepResolution` evidence containing the ordinal, input type, operand type, and output type for each step.

## Type rules

### VARIANT

A static key on `VARIANT` returns `VARIANT`.

An index/dynamic step on `VARIANT` accepts `INTEGER`, `STRING`, or `UNKNOWN` operands and returns `VARIANT`. This expresses generic semi-structured navigation; target-specific physical support is checked later.

### ARRAY

A static key directly on `ARRAY` is invalid.

An index/dynamic step on `ARRAY` accepts `INTEGER` or `UNKNOWN`. Metis does not currently model array element types, so successful array indexing returns `UNKNOWN` rather than guessing an element type.

A later step may continue from that `UNKNOWN` value, but the type remains unknown until stronger schema evidence exists.

### UNKNOWN

A key on `UNKNOWN` is allowed and returns `UNKNOWN`.

Index/dynamic access on `UNKNOWN` accepts `INTEGER`, `STRING`, or `UNKNOWN` operands and also returns `UNKNOWN`.

This is deliberate compatibility with incrementally typed source schemas: uncertainty is retained rather than converted into a false `VARIANT` claim.

### Scalar types

Static key or indexed path traversal directly from scalar types such as `STRING`, `INTEGER`, `DECIMAL`, `BOOLEAN`, `DATE`, or `TIMESTAMP` fails explicitly.

An authored cast to `VARIANT` before navigation is a separate expression and may make the subsequent traversal valid if the explicit-cast contract permits it.

## Analyzer integration

`SemanticAnalyzer` types the path base first, then translates common AST path segments into `PathSemanticStep` values. INDEX and DYNAMIC operands are analyzed exactly once; their typed dependency/evidence is combined with the base expression before path resolution.

Successful resolution appends `PathResolutionEvidence` to `TypedExpression`, containing the engine-neutral `PathAccessResolution` plus the common-AST path span. This evidence is propagated through ordinary typed composition such as casts, binary expressions, function calls, CASE expressions, tuples, and apply/index wrappers.

The analyzer MUST use the original typed base as the path resolver input even when an index/dynamic operand contributes its own type. This prevents an `UNKNOWN` base from being accidentally replaced by an operand type during evidence/dependency combination.

## Failure contract

The analyzer-neutral path resolver uses stable `PathResolutionError` categories:

- `INVALID_BASE`
- `INVALID_KEY`
- `INVALID_OPERAND`
- `INVALID_SEGMENT_KIND`

`SemanticAnalyzer` maps these failures to stable `INVALID_PATH_ACCESS` semantic errors. Structural/key failures retain the full `PathAccessExpr` span. Invalid INDEX/DYNAMIC operand failures use the operand span when available.

## Path + cast composition

An expression such as `payload:customer.region::STRING` is analyzed in two independent semantic stages:

1. `PathAccessExpr` validates traversal from the typed `payload` base and retains path evidence;
2. `CastExpr` consumes the path result type and applies the existing explicit-cast contract.

For a `VARIANT` payload, the path result remains `VARIANT`, and the authored `::STRING` cast resolves as the existing `TEXTUAL` cast family. Both evidence records are retained in the final `TypedExpression`. Other `VARIANT` scalar targets, such as timestamp, continue to use the existing `SEMI_STRUCTURED` cast family where defined by the Phase I1 cast contract.

## Lowering and physical capability boundary

Typed path validity does not imply that the native engine has a renderer for the expression's selected physical dialect. Catalog may parse, bind, and type a declared Snowflake expression and Resolver/Planner may carry its `ResolvedExpression.Analysis` without losing path/cast evidence; compiler capability remains a later boundary.

A selected dialect that is not registered by the native SQL renderer MUST fail as `UNSUPPORTED_DIALECT` before SQL text is emitted. Metis MUST NOT add a renderer solely to make a semantically valid path expression physically renderable.

Likewise, target-expression selection MUST NOT treat a foreign-only physical expression as an implicit portable fallback. If a model declares only a Snowflake path expression and the compile target is a registered target such as ClickHouse, selection MUST fail as `UNSUPPORTED_EXPRESSION` rather than forwarding Snowflake syntax to that target.

These failures are capability outcomes, not semantic-analysis failures. `SemanticAnalyzer` therefore remains dialect-neutral and does not branch on registered renderer availability.

## Nullability

Path traversal remains conservatively `UNKNOWN` nullability. A missing key, out-of-range index, or vendor-specific semi-structured behavior can produce NULL-like results, and the common semantic contract does not claim stronger guarantees without schema/capability evidence.

## Sequencing

### I3.1 — typed path access semantics — implemented

- define the engine-neutral path-step and resolution contract;
- validate VARIANT/ARRAY/UNKNOWN bases and index operands;
- retain ordered type-resolution evidence;
- cover invalid scalar bases and invalid steps.

### I3.2 — analyzer integration and diagnostics — implemented

- translate common AST `PathSegment` values into typed path steps;
- analyze index/dynamic operands once and feed their types into `ResolvePathAccess`;
- retain and propagate path evidence in `TypedExpression`;
- map path failures to stable SourceSpan-bearing `INVALID_PATH_ACCESS` errors;
- verify `payload:customer.region::STRING` composes with explicit cast semantics.

### I3.3 — lowering-oriented regression coverage — implemented

- exercise representative Snowflake-style path + cast syntax through Catalog binding/typed analysis and Resolver/Planner boundaries;
- verify path/cast typed evidence survives into the planned `ResolvedExpression`;
- verify an unregistered selected physical dialect fails explicitly as `UNSUPPORTED_DIALECT`;
- verify a registered target rejects a foreign-only Snowflake expression as `UNSUPPORTED_EXPRESSION` instead of guessing a fallback;
- add no new dialect or renderer.

## Non-goals

Phase I3 does not:

- infer JSON/object schemas from runtime data;
- introduce warehouse-specific semantic analyzer branches;
- add a new physical dialect;
- add query execution;
- add statistics, CBO, or cost-based rewrites.

## Acceptance criteria for I3.1

I3.1 is complete when:

- key chains on `VARIANT` resolve deterministically to `VARIANT`;
- static key access directly on `ARRAY` fails;
- integer array indexing succeeds but returns `UNKNOWN` element type;
- incompatible array/variant index operands fail explicitly;
- scalar bases fail explicitly;
- `UNKNOWN` bases preserve uncertainty rather than being guessed as `VARIANT`;
- failure evidence records the exact failing ordinal;
- required correctness and real-engine regression gates remain green.

## Acceptance criteria for I3.2

I3.2 is complete when:

- production `SemanticAnalyzer` calls `ResolvePathAccess` for `PathAccessExpr`;
- INDEX/DYNAMIC operands are typed exactly once;
- `TypedExpression` retains path resolution evidence and does not drop it through ordinary composition;
- scalar bases and incompatible path operands fail as `INVALID_PATH_ACCESS` with SourceSpan;
- `UNKNOWN` bases remain `UNKNOWN`;
- `payload:customer.region::STRING` retains distinct path and explicit-cast evidence;
- existing binding/function/cast/aggregation behavior stays green;
- no dialect-specific analyzer branch is introduced.

## Acceptance criteria for I3.3

I3.3 is complete when:

- representative Snowflake-style path + cast syntax is parsed, bound, and typed through the production Catalog analyzer;
- `PathResolutionEvidence` and `CastResolutionEvidence` survive Resolver and Planner into the selected planned expression;
- native compilation of that selected but unregistered Snowflake dialect fails explicitly as `UNSUPPORTED_DIALECT` before SQL rendering;
- a registered ClickHouse target rejects the Snowflake-only expression as `UNSUPPORTED_EXPRESSION` before planning/lowering;
- no new renderer, dialect-specific analyzer branch, or database execution behavior is introduced;
- required correctness, E2E, and real-engine regression gates remain green.
