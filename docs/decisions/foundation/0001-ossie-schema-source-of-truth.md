# ADR-0001: Apache Ossie Schema Is the Semantic Model Source of Truth

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-09
**Last reviewed:** 2026-08-14

## Context

Metis started M1 with a hand-written `ossie/model.go` containing only the subset of Apache Ossie needed by the initial fixture. That was useful for bootstrapping but is not acceptable as a long-term architecture.

If Metis manually evolves this structure, it will eventually create a de-facto Metis dialect of Ossie: new Apache Ossie fields may be ignored, enum values may drift, required fields may differ, or vendor extension semantics may be represented incorrectly.

This directly conflicts with the `Ossie-first` and `extension-preserving` principles.

Apache Ossie already publishes a machine-readable JSON Schema at:

```text
apache/ossie/core-spec/osi-schema.json
```

The schema is therefore the authoritative definition of the interchange model.

## Decision

Metis treats the **official Apache Ossie JSON Schema as the only source of truth for the semantic definition model**.

Metis must not independently design or extend an Ossie object model.

The pipeline is:

```text
Apache Ossie
core-spec/osi-schema.json
          │
          ▼
 pinned upstream commit
          │
          ▼
  cmd/ossiegen
          │
          ▼
 generated Go binding
          │
          ▼
       Loader
          │
          ▼
  Catalog / Runtime Views
```

The generated Go binding is an implementation convenience, not a second specification.

## Rules

1. `core-spec/osi-schema.json` is authoritative.
2. Go structs representing Ossie documents must be schema-derived.
3. `ossie/model.go` is generated output and must not be edited manually.
4. Metis-specific semantic fields must never be added to the Ossie binding.
5. Metis runtime state belongs in Catalog / Resolver / Planner types, not Ossie types.
6. A new Apache Ossie spec version requires an explicit schema-sync change.
7. Unknown top-level/core semantic properties must fail validation rather than be silently ignored.
8. `custom_extensions` must round-trip without semantic reinterpretation by Metis Core.
9. AIContext remains extensible exactly where the official schema permits additional properties.

## M1.1 Implementation

M1.1 transitions from:

```text
hand-written Ossie subset
        │
        ▼
      Loader
```

to:

```text
official Ossie schema
        │
        ▼
pinned upstream commit
        │
        ▼
 cmd/ossiegen
        │
        ▼
generated Go binding
        │
        ▼
 strict Loader
```

The binding matches the current official Ossie `0.2.0.dev0` schema for Document, SemanticModel, Dataset, Field, Dimension, Metric, Relationship, Expression, DialectExpression, AIContext, CustomExtension, Dialect and DataType.

The current upstream schema is pinned by `tools/update-ossie-binding.sh` to a concrete Apache Ossie commit rather than `main`. Upgrading Ossie therefore requires an intentional pin change.

`cmd/ossiegen` is a small Go-native generator owned by Metis. It consumes only the JSON Schema constructs currently present in the Ossie core schema and fails on unsupported definition shapes rather than silently producing weak bindings.

CI runs the same generation path and requires `ossie/model.go` to remain byte-for-byte clean after regeneration.

## Schema Validation vs Semantic Validation

These must remain separate.

### Apache Ossie schema validation

Answers:

> Is this document structurally valid according to Apache Ossie?

Examples include correct spec version, required fields, legal dialect values, legal datatype values, additionalProperties rules and extension payload shape.

### Metis semantic validation

Answers:

> Is this valid semantic input for Metis planning and compilation?

Examples include duplicate dataset names, relationship target existence, relationship column cardinality and later metric/dimension resolution constraints.

Metis must not use semantic validation to redefine Apache Ossie structural rules.

## Schema Upgrade Workflow

An Apache Ossie upgrade is explicit:

```text
update OSSIE_COMMIT
        │
        ▼
make ossie-sync
        │
        ▼
fetch pinned upstream schema
        │
        ▼
regenerate ossie/model.go
        │
        ▼
inspect generated diff
        │
        ▼
run compatibility tests
        │
        ▼
review + commit
```

This workflow is not part of the default per-change CI gate. Run it when the
pinned Ossie commit, binding generator, or generated binding contract changes;
review the generated diff as upgrade evidence.

`tools/update-ossie-binding.sh` is the single entry point for this workflow.

## Why Not Maintain a Metis Semantic Model?

Doing so would create:

```text
Ossie
  ↓
Metis Semantic IR
  ↓
Runtime Plan
```

This creates an unnecessary second semantic standard and forces every new Ossie feature to be re-modeled by Metis.

Instead:

```text
Ossie
  ↓
Catalog indexes / handles
  ↓
ResolvedSemanticQuery
  ↓
SemanticPlan
```

Catalog/runtime objects may index and reference Ossie objects, but do not redefine the semantic interchange model.

## Why Not Use `map[string]any` Everywhere?

Using only raw maps would guarantee schema fidelity but would make Resolver and Planner implementation unnecessarily error-prone and remove compile-time type safety.

Metis therefore uses this compromise:

> **Schema is canonical; generated Go types are projections of the schema.**

If there is disagreement between generated Go code and the Apache Ossie schema, the schema wins.

## Extension Preservation

The official schema defines `custom_extensions` explicitly. Metis keeps the extension payload in its official representation and does not invent a Metis-specific extension structure.

An engine may declare that it understands a vendor extension. If an extension is semantic-critical and the selected engine cannot understand it, compilation must fail explicitly rather than silently downgrade semantics.

## Consequences

### Positive

- Apache Ossie evolution does not require Metis to redesign a semantic model.
- Structural compatibility is reviewable against one upstream schema.
- Metis cannot quietly invent incompatible fields.
- Vendor extensions keep their official wire representation.
- Go code retains type-safe access for Catalog / Resolver development.
- Binding regeneration is deterministic and CI-enforced.

### Cost

- Schema upgrades become deliberate repository changes.
- Generated binding changes may be noisy.
- `cmd/ossiegen` must evolve if Apache Ossie adopts JSON Schema constructs it does not currently support.

These costs are preferable to semantic model drift.

## Guardrail

Code review should reject any change that manually adds a semantic-definition field to Metis without first pointing to the corresponding field in Apache Ossie.

The architectural rule is:

> **If Apache Ossie can express it, Metis consumes it. If Apache Ossie cannot express it, Metis does not quietly create a competing semantic standard.**
