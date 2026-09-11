# Metric scale semantic extension

`metric_scale` is Metis's first concrete semantic-critical Ossie custom extension. RFC-0021 defines the capability lifecycle and RFC-0028 defines this extension's normative semantics; this document records the runtime implementation boundary for maintainers.

## Contract

A metric may carry the preserved Ossie extension payload:

```json
{"kind":"metric_scale","version":"1","factor":2}
```

with `vendor_name: metis.semantic`.

`extension.MetricScaleInterpreter` recognizes that explicit vendor contract only at metric scope and emits the critical capability requirement `semantic.metric_scale` with identity `metis.semantic/metric_scale/metric` and version `1`.

Unknown vendors and unknown `metis.semantic` kinds remain opaque-preserved. Invalid recognized payloads fail closed rather than falling back to metadata-only success.

## Runtime flow

```text
Ossie CustomExtension
  -> MetricScaleInterpreter
  -> critical Requirement
  -> selected-target Registry resolution
  -> MetricScaleEvidence
  -> target-selected ResolvedExpression
  -> SemanticPlan projection/evaluation expression
  -> SQLPlan / dialect rendering
  -> database execution
```

A selected target is supported only after an exact `MetricScaleRegistration` for its engine and dialect. Registrations do not leak across targets.

For a supported target, Resolver applies the scale after selecting and analyzing the metric expression:

```text
(<selected metric expression>) * <factor>
```

The transformed expression retains the existing SemanticManifest analysis and carries typed `MetricScaleEvidence` across the Resolver-to-Planner boundary. `ResolvedExpression` stores that evidence behind a pointer-backed evidence set so existing planner equality/deduplication behavior remains comparable and deterministic while evidence survives ordinary value copies.

## Correctness evidence

Unit coverage proves interpreter classification, typed evidence, target isolation, unsupported-capability failure, and value-changing SQL lowering.

The real-engine harness separately registers the capability for each selected target and executes the same scaled metric against Doris and ClickHouse. A source sum of `30` with factor `2` must return `60` on both engines. This is result-level evidence that the extension is not merely preserved metadata.

## Boundaries

The implementation does not modify generated Ossie bindings, introduce a second semantic schema, add target-specific analyzer semantics, execute databases inside Metis Core, or introduce statistics/CBO/join-order responsibilities.

Normative references: `docs/proposals/semantic/0021-semantic-critical-extension-capabilities.md` and `docs/proposals/semantic/0028-semantic-critical-metric-scale.md`.
