# RFC-0028: Semantic-Critical Metric Scale Lowering

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-18
- **Last updated:** 2026-09-03
- **Scope:** Phase I6 semantic-critical extension end-to-end lowering
- **Supersedes:** None
- **Depends on:** RFC-0021

## Summary

Phase I6 proves the RFC-0021 extension lifecycle with one real semantic-critical custom extension whose meaning changes query results. The `metis.semantic` metric extension kind `metric_scale` declares a positive multiplicative factor that is applied to the selected, typed metric expression before Planner lowering.

The extension payload remains inside Ossie `CustomExtension.data`; no generated Ossie binding is changed and no second semantic schema is introduced.

Example payload:

```json
{"kind":"metric_scale","version":"1","factor":2}
```

## Capability contract

`MetricScaleInterpreter` recognizes the extension only at metric scope and emits a critical requirement with identity `metis.semantic/metric_scale/metric`, version `1`, and capability `semantic.metric_scale`.

A selected target must explicitly register that exact capability and version. Missing target support remains `unsupported-critical` and is surfaced through the RFC-0021 `UNSUPPORTED_SEMANTIC_EXTENSION` boundary. A registration for one dialect cannot satisfy another dialect.

Unknown vendors and unknown kinds remain opaque-preserved under RFC-0021.

## Typed semantic evidence

The interpreter decodes the payload into `MetricScaleEvidence`, containing the extension identity, owning metric, version, and finite positive factor. Resolver retains that evidence on the target-selected `ResolvedExpression`, which is the value already passed across the Resolver -> Planner boundary.

The selected metric expression is lowered as:

```text
(<selected metric expression>) * <factor>
```

The original Catalog binding/type analysis remains attached to the resolved expression, while `MetricScaleEvidence` records why the value semantics changed. Planner therefore receives both the transformed semantic expression and typed extension evidence rather than an opaque metadata success.

## Safety

- factor must be finite and greater than zero;
- multiple scale extensions on the same metric fail explicitly;
- an unsupported selected target fails before physical SQL is emitted through CompileService;
- capability registrations are target-isolated;
- lowering is engine-neutral arithmetic and introduces no dialect-specific analyzer branch;
- no statistics, CBO, cost model, join-order search, or database execution is added to Metis Core.

## Acceptance

Phase I6 is complete when tests prove:

- Interpreter -> Requirement is semantic-critical and deterministic;
- missing capability fails closed and exact selected-target registration succeeds;
- typed `MetricScaleEvidence` survives Resolver -> Planner;
- generated SQL materially changes metric value semantics;
- Doris and ClickHouse execute the scaled metric correctly for registered targets;
- current semantic-extension design/spec documentation reflects this implemented contract.
