# RFC-0021: Semantic-Critical Extension Capability Contract

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** Ossie extension preservation, semantic validation, target capability negotiation, `SemanticEngine` integration
- **Supersedes:** None

## Summary

Metis will preserve Ossie extensions by default while adding an explicit, fail-closed contract for extensions whose meaning is required to compile a query correctly.

The contract has exactly three outcomes for an observed extension in a selected compile context:

1. **supported** — Metis and the selected semantic engine declare the capability and compatible version required to interpret the extension;
2. **opaque-preserved** — the extension is not semantically required by Metis, so its payload is retained without interpretation;
3. **unsupported-critical** — the extension declares semantics that are required for correctness, but the selected compile path cannot satisfy the required capability; validation or compilation fails explicitly.

This RFC does **not** define a second semantic schema. Apache Ossie remains the semantic source of truth. Metis-generated Ossie bindings remain generated artifacts and MUST NOT be hand-edited to add Metis-only extension fields.

## Architectural boundary

Metis needs to distinguish preservation from interpretation.

Preservation means an unknown extension can survive load, bootstrap, snapshotting, discovery, and later serialization without Metis understanding its semantics.

Interpretation means the extension changes semantic resolution, planning, expression meaning, or another correctness-critical compile decision. Interpretation requires an explicit capability contract.

Metis MUST NOT infer semantic meaning from extension names, vendor prefixes, arbitrary JSON/YAML keys, or implementation-specific heuristics.

The selected target database remains responsible for physical execution and cost-based optimization. Extension capability negotiation MUST NOT introduce statistics, cost models, physical join ordering, access-path selection, or database execution into Metis Core.

## No second schema

RFC-0021 introduces an **internal capability inventory**, not a new serialized model format.

An internal extension observation may carry normalized metadata such as:

```text
identity
scope
version
criticality
required capability
raw payload reference
```

These are runtime descriptors used to reason about existing Ossie extension material. They are not new top-level Ossie fields and are not persisted as a competing semantic model.

Metis MUST derive extension observations from one of these explicit sources:

- extension structure already represented by the pinned Ossie contract;
- metadata defined by the extension's own vendor contract and decoded by an extension-aware `Interpreter`;
- a registered Metis `extension.Interpreter` for a known extension vendor/identity.

Metis MUST NOT guess that an otherwise opaque unknown extension is semantic-critical. If no explicit criticality/requirement contract exists, the extension remains opaque-preserved.

## Extension identity

Capability matching requires a stable identity. The implementation SHOULD normalize an extension identity from the information available in the Ossie extension envelope and its owning scope. At minimum the identity must be deterministic and distinguish independently versioned extension kinds.

A conceptual identity is:

```text
namespace / kind / version / scope
```

The exact Go representation is implementation-defined, but matching MUST NOT depend on map iteration order or raw source ordering.

Version matching MUST be explicit. An `Interpreter` may require an exact version or a declared compatible range. Missing or unparsable version information MUST NOT be silently coerced into a supported version for a semantic-critical extension.

## Criticality

Criticality answers one question:

> Would ignoring this extension allow Metis to produce a semantically incorrect query while appearing successful?

A semantic-critical extension can affect, for example:

- metric meaning or evaluation rules;
- relationship semantics;
- filter semantics;
- time/calendar semantics;
- target-sensitive semantic lowering required before SQL rendering.

A non-critical extension may carry descriptive, UI, catalog, lineage, or vendor metadata that does not alter Metis compile semantics.

Criticality MUST come from an explicit extension contract or registered `Interpreter`. Metis MUST NOT maintain a hand-written global blacklist of field names that attempts to guess criticality.

## Capability inventory

Metis SHOULD expose a typed internal registry that answers whether an extension requirement is supported for a selected compile path.

A conceptual requirement is:

```text
required capability
extension identity
compatible version constraint
scope
```

A capability implementation is registered explicitly and duplicate normalized registrations MUST fail closed, following the same determinism rule as semantic-engine and SQL-dialect registries.

Capability registration MUST be separate from raw extension preservation: installing no `Interpreter` must never cause a non-critical unknown extension to be discarded.

## Selected SemanticEngine participation

Capability resolution is target-aware.

The compile path already selects an explicit `ExecutionBinding`, semantic engine, and dialect before target-specific compilation. RFC-0021 extends that boundary so the selected semantic engine can declare extension capabilities relevant to its semantic compilation responsibilities.

The implementation SHOULD prefer a small optional capability-provider contract over expanding the base `SemanticEngine` interface for every extension feature. Engines that do not participate in extension interpretation continue to compile ordinary Ossie semantics unchanged.

A semantic-critical requirement is **supported** only when all required target-aware capability checks succeed. A capability advertised by a different engine or dialect MUST NOT satisfy the selected compile path.

Agent-facing APIs MUST remain independent of engine-internal registry details. They receive stable support/unsupported diagnostics, not mutable implementation objects.

## Resolution states

For each observed extension relevant to a compile request, Metis resolves one of three states.

### supported

Conditions:

- the extension has an explicit semantic requirement;
- the selected compile path registers the required capability;
- extension identity and version constraints match;
- any `Interpreter` validation succeeds.

Behavior:

- the `Interpreter` may produce typed internal semantic evidence consumed by resolver/planner/compiler stages;
- raw extension material remains preserved unless the Ossie contract itself specifies otherwise.

### opaque-preserved

Conditions:

- Metis has no explicit semantic-critical requirement for the extension; or
- an extension-aware `Interpreter` classifies it as non-critical metadata.

Behavior:

- preserve raw extension data;
- do not allow it to alter semantic resolution or planning;
- do not advertise it as semantically supported merely because it was preserved.

### unsupported-critical

Conditions:

- an explicit extension contract says the extension is semantic-critical; and
- the selected compile path lacks the capability, has an incompatible version, or cannot validate the extension requirement.

Behavior:

- fail closed before producing physical SQL;
- emit a stable typed error containing bounded diagnostic fields such as extension identity, required capability, requested version, selected engine, and selected dialect;
- never silently downgrade the extension to opaque-preserved.

## Validation and compile lifecycle

The lifecycle is intentionally staged:

```text
Ossie load
  -> preserve extension payloads
Project bootstrap / snapshot
  -> inventory explicit extension observations through registered Interpreters
Target selection
  -> select ExecutionBinding + SemanticEngine + dialect
Semantic validation / compile
  -> resolve extension requirements against selected capabilities
  -> supported | opaque-preserved | unsupported-critical
```

Loader correctness and target capability correctness are separate concerns. The loader SHOULD preserve an unknown extension even when no current engine can interpret it, because the same snapshot may later be compiled through a capable `Interpreter` and selected target.

A target-aware compile MUST, however, reject any semantic-critical requirement that its selected path cannot satisfy.

Project-level validation MAY report capability gaps ahead of compilation when a target is explicitly known, but it MUST use the same resolver as compile rather than a second policy implementation.

## Stable errors

H6.2 MUST introduce or reuse a typed semantic error for unsupported critical extensions. String-only errors are insufficient as the public contract.

The stable envelope SHOULD distinguish at least:

```text
extension identity
required capability
required/observed version
selected engine
selected dialect
reason: capability_missing | version_incompatible | invalid_extension
```

Diagnostics MUST be deterministic and bounded. Raw extension payloads MUST NOT be echoed into Agent-facing errors by default.

## Determinism and duplicate registration

Extension capability resolution MUST be deterministic for identical project state and compile target.

Therefore:

- normalized extension identities must be stable;
- duplicate capability registrations fail explicitly;
- duplicate vendor `Interpreter` registrations fail explicitly;
- registration order must not decide which `Interpreter` wins;
- version matching must have one documented rule;
- unsupported-critical results must not depend on map iteration order.

## Compatibility

Existing unknown non-critical extensions remain preserved and do not become errors merely because RFC-0021 is implemented.

Existing Ossie models that use no semantic-critical extensions behave unchanged.

The implementation MUST NOT modify generated Ossie bindings by hand. If a future pinned Ossie release adds standardized extension metadata, Metis should regenerate bindings through the existing schema workflow and adapt the internal inventory to that source of truth.

## Conformance requirements

H6.2 implementation must add executable tests proving:

- unknown non-critical extension payload survives load/bootstrap unchanged;
- a registered compatible critical extension resolves as supported;
- a critical extension with no selected-engine capability fails closed;
- an incompatible critical-extension version fails closed;
- a capability registered for another engine/dialect does not leak into the selected target;
- duplicate capability registration is rejected deterministically;
- duplicate vendor `Interpreter` registration is rejected deterministically;
- Agent-facing unsupported diagnostics are typed and bounded;
- generated Ossie bindings are untouched.

Where an extension can affect generated SQL or result semantics, representative compiler and real-engine differential evidence SHOULD be added. A metadata-only opaque-preservation case does not require database execution merely to prove round-trip preservation.

## Non-goals

RFC-0021 does not introduce:

- a second Metis semantic schema;
- a dynamic extension marketplace or plugin distribution system;
- arbitrary runtime code loading;
- database credentials or SQL execution inside Metis;
- statistics-driven optimization or a cost-based optimizer;
- physical operator, access-path, exchange, or distribution planning;
- automatic interpretation of unknown vendor payloads.

## Delivery plan

Phase H6 is split deliberately:

1. **H6.1 — RFC:** review and accept this capability contract before production behavior changes.
2. **H6.2 — runtime contract:** implement the smallest typed inventory/registry and fail-closed validation path required by the accepted RFC, with conformance coverage.
3. **H6.3 — lifecycle closure:** mark RFC-0021 `Implemented` only after code/tests/specs reflect the accepted behavior.

H6.1 being merged as `Draft` or `Accepted` is not evidence that runtime enforcement exists.

## Implementation evidence

RFC-0021 is implemented by the Phase H6 runtime sequence:

- **H6.2.1 — capability model (#257):** typed `extension` identity, requirement, target, registration, registry, deterministic three-state resolution, exact version matching, target isolation, and duplicate registration rejection;
- **H6.2.2 — resolver/compile integration (#259):** explicit vendor `Interpreter` inventory, unknown-vendor opaque preservation, duplicate Interpreter rejection, and shared Compile/Validate/Explain capability enforcement after target selection and before planning/physical SQL;
- **H6.2.3 — Agent-facing errors (#260):** `unsupported-critical` maps to `UNSUPPORTED_SEMANTIC_EXTENSION` with bounded typed details shared by REST and MCP while preserving the internal `extension.UnsupportedError` in the Go error chain.

The implementation leaves generated Ossie bindings unchanged, introduces no second semantic schema, performs no database execution, and adds no cost-based optimizer responsibilities.

Developer-facing explanations of the runtime model and Agent-facing error boundary live in `docs/design/semantic/extensions.md` and `docs/design/semantic/extension-errors.md`.

## Alternatives rejected

### Reject every unknown extension

This breaks Ossie extension preservation and makes Metis unnecessarily hostile to descriptive/vendor metadata that does not affect compilation.

### Preserve every unknown extension and always compile

This can silently ignore semantics that are required for correctness, violating fail-closed behavior.

### Add Metis-only extension fields to generated Ossie structs

This forks the semantic source of truth and would be overwritten by schema regeneration.

### Let interpreters overwrite registrations by order

This makes semantic behavior depend on initialization order and is incompatible with deterministic compilation.

### Treat extension handling as a database optimizer concern

Semantic extension meaning belongs before physical database optimization. It must not become a path to introducing cost models or target-database execution into Metis Core.
