# Native Expression Parser Specification

This document is normative for the Metis native expression parser and its reference-extraction boundary. Keywords **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are used normatively.

## 1. Processing contract

```text
Expression text
    -> Lexer
    -> Pratt Parser + DialectProfile
    -> Common Expression AST
    -> ReferenceCollector
    -> []expression.Reference
    -> expression.Bind + SemanticManifest ReferenceResolver
    -> BoundExpression
    -> TypedExpression
    -> SemanticManifest MetricAnalysis / MetricDependency
```

The parser MUST be independent of SemanticManifest. The ReferenceCollector MUST be independent of SemanticManifest. SemanticManifest MUST remain authoritative for semantic symbol binding.

## 2. Parser scope

The parser MUST be expression-only. It MUST NOT parse complete SELECT statements, DDL, DML, transaction statements, permissions, or other database statement grammar merely to support semantic dependency analysis.

The common parser SHOULD support general expression constructs before additional dialect-specific surface syntax is added.

## 3. Stable common AST primitives

The common AST includes these primitives:

```text
IdentifierExpr
LiteralExpr
WildcardExpr
FunctionCallExpr
ApplyExpr
UnaryExpr
BinaryExpr
BetweenExpr
InExpr
IsNullExpr
CaseExpr
CastExpr
IntervalExpr
TupleExpr
LambdaExpr
IndexExpr
PathAccessExpr
```

Dialect profiles MUST map surface syntax to common AST nodes when those nodes preserve the structure required for semantic analysis. A dialect-specific AST node MUST NOT be introduced solely because one warehouse uses a different token sequence for an already modeled concept.

## 4. Required structural behavior

### 4.1 Wildcard

`COUNT(*)` MUST represent `*` as `WildcardExpr`, not as an identifier. `WildcardExpr` contributes no semantic field reference.

### 4.2 Parameterized cast types

`CAST(orders.amount AS DECIMAL(18,2))` MUST produce a `CastExpr` whose type representation preserves `DECIMAL(18,2)` rather than truncating it to `DECIMAL`.

Equivalent shorthand syntax such as `payload:amount::NUMBER(18,2)` MUST preserve the parameterized type in the same common cast representation. Type names and type parameters MUST NOT become semantic references.

### 4.3 Interval

`orders.created_at + INTERVAL 7 DAY` MUST model the interval as `IntervalExpr` or an equivalent common interval structure. Only `orders.created_at` contributes a semantic field reference.

### 4.4 Tuple, grouping, and lambda parameters

Parentheses MUST distinguish ordinary grouping from tuple-like expression lists when required by grammar.

`(x, y) -> x + y + orders.tax` MUST produce a `LambdaExpr` with parameters `[x, y]`. Lambda parameters are lexical bindings and MUST NOT become SemanticManifest references.

### 4.5 Operator precedence

The Pratt parser MUST preserve deterministic binding power. For example:

```sql
a + b * c = d OR e AND f
```

MUST be structurally equivalent to:

```text
OR(
  EQ(ADD(a, MUL(b, c)), d),
  AND(e, f)
)
```

Changing precedence or associativity is a parser-contract change and MUST be accompanied by conformance updates and review.

### 4.6 Lambda lexical scope

Lambda parameters MUST be treated as lexical locals by ReferenceCollector. Nested lambda scopes MUST support shadowing.

For:

```sql
arrayMap(x -> arrayMap(x -> x + orders.tax, orders.inner), orders.outer)
```

ReferenceCollector MUST collect exactly:

```text
orders.tax
orders.inner
orders.outer
```

### 4.7 Relational qualification

`orders.amount` MUST remain a relational `IdentifierExpr(parts=[orders, amount])` and MUST collect the qualified reference `(orders, amount)`.

### 4.8 Semi-structured path access

`payload:customer.region::STRING` MUST be represented using common path and cast nodes, structurally equivalent to:

```text
CastExpr
  Expr: PathAccessExpr
    Base: IdentifierExpr(payload)
    Segments:
      Key(customer)
      Key(region)
  Type: STRING
```

The path keys `customer` and `region` MUST NOT become semantic field references. The base `payload` remains a semantic-reference candidate.

Indexed paths such as `payload:items[0].price::NUMBER` MUST preserve typed key/index path segments. Index or dynamic expressions MUST be traversed if they contain expressions; static path keys MUST NOT be bound through SemanticManifest.

## 5. ReferenceCollector contract

ReferenceCollector MUST walk the common AST and return lexical unbound references only.

It MUST NOT return function names, type names, wildcard tokens, static semi-structured path keys, lambda-local symbols, literal contents, or interval unit names.

It MUST traverse expression-bearing children including function arguments, operators, CASE branches, cast operands, index expressions, dynamic path segments, and lambda bodies under the correct lexical scope.

Duplicate references MAY be deduplicated deterministically before SemanticManifest binding.

## 6. SemanticManifest binding contract

SemanticManifest binding occurs after parsing and reference collection.

Qualified references MUST be validated against canonical dataset and field indexes.

Unqualified references from one expression MUST be bound as one semantic scope rather than independently guessing a dataset per field. If no dataset contains the complete required field set, binding MUST fail. If multiple datasets satisfy the complete field set, binding MUST fail with `AMBIGUOUS_EXPRESSION_REFERENCE`.

The parser and ReferenceCollector MUST NOT perform this binding themselves.

## 7. Failure semantics

Production expression analysis MUST use the Metis native parser as its authoritative syntax path.

There MUST NOT be an implicit regexp, token-scan, or heuristic fallback after parser failure.

Unsupported syntax MUST fail deterministically with `EXPRESSION_PARSE_FAILED` and enough context to identify the metric, dialect, and cause at the SemanticManifest boundary.

A new unsupported expression MUST be handled by adding a conformance case and extending the parser/profile deliberately, not by textual guessing.

## 8. Dialect profile rules

Dialect profiles MAY enable surface syntax extensions. They MUST NOT implement semantic resolution.

A dialect profile SHOULD be small and declarative where possible. Shared grammar and AST primitives MUST be preferred over copied parser implementations.

Before adding a new dialect-specific grammar feature, contributors MUST determine whether the syntax can lower into an existing common AST primitive.

Metis SHOULD prefer hardening shared parser core behavior over rapidly increasing profile coverage.

## 9. Conformance corpus

The parser, lexer, scope, and reference tests under `expression/` form the
normative regression corpus, including `parser_test.go`, `lexer_test.go`,
`scope_test.go`, and `references_test.go`.

Changes to lexer/tokenization, Pratt precedence or associativity, common AST definitions, parser grammar, dialect profile hooks/capabilities, ReferenceCollector traversal/scope behavior, or cast/path/index/lambda handling MUST pass this corpus.

The corpus MUST test both parse structure and collected references where both matter.

The baseline includes:

```text
COUNT(*)
CAST(orders.amount AS DECIMAL(18,2))
payload:amount::NUMBER(18,2)
orders.created_at + INTERVAL 7 DAY
arrayMap((x, y) -> x + y + orders.tax, orders.a, orders.b)
nested lambda shadowing
CASE + BETWEEN + NOT IN
quoted identifiers
operator precedence
ClickHouse parametric aggregate syntax
payload:customer.region::STRING
payload:items[0].price::NUMBER
```

## 10. Differential validation

Mature parsers and real database engines MAY be used as test oracles. They are not required production runtime dependencies.

For ANSI/MySQL-compatible subsets, a mature parser such as TiDB MAY be used for differential tests. For warehouse-specific syntax, official grammar corpora and real-engine conformance MAY be used.

Differential testing validates the Metis native parser; it does not transfer semantic authority away from SemanticManifest.

## 11. Evolution rule

When a production or benchmark expression fails to parse, contributors SHOULD follow this sequence:

```text
capture failing expression
    -> add conformance case
    -> classify as common syntax or dialect surface syntax
    -> extend common AST/core grammar or existing profile
    -> assert AST shape + collected references
    -> run conformance gates
```

Do not broaden multiple dialect profiles opportunistically in the same change unless the new primitive is required to prove the common core contract.
