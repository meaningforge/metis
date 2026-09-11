# Semantic extension runtime

This guide explains how Metis handles Ossie `CustomExtension` values at runtime. RFC-0021 is the normative capability contract; this document is the developer-facing mental model for implementing and reviewing extension support.

## The short version

Keep this model in mind:

```text
CustomExtension
      |
      v
Interpreter
      |
      v
Requirement        <- want
      |
      v
Registry
      |
      +---- searches Registrations <- have
      |
      v
Resolution         <- result
```

In one sentence:

> `Requirement` is what the model needs, `Registration` is what the selected Renderer can provide, `Registry` matches the two, and `Resolution` is the result.

## What is `CustomExtension`?

`CustomExtension` is the Ossie extension envelope for information outside the standard semantic fields. Metis preserves that material even when it does not understand the vendor-specific payload.

Preservation does not imply semantic support. An unknown extension must not silently change resolution, planning, or generated SQL merely because Metis can load it.

The current runtime observes custom extensions attached to these Ossie owners:

```text
SemanticModel
Dataset
Field
Metric
Relationship
```

The observation includes the owner location, for example `scope=metric, owner=revenue`, without introducing a second serialized semantic schema.

## Interpreter: understand an explicit vendor contract

`extension.Interpreter` is the boundary that may assign semantic meaning to a known vendor extension.

```go
type Interpreter interface {
    Vendor() ossie.Vendor
    Requirements(Location, ossie.CustomExtension) ([]Requirement, error)
}
```

An Interpreter answers:

> What semantic capabilities does this known extension require?

Metis does not infer criticality from vendor names, payload keys, or heuristics. If no Interpreter is registered for a vendor, the extension remains opaque-preserved.

When an interpreted extension changes planning or generated SQL, that semantic
effect must be materialized into `ResolvedExpression` or another explicit typed
plan field before physical lowering. Runtime `ExtensionEvidence` may retain the
capability decision and provenance for validation or Explain, but it is not a
hidden lowering input and does not independently define structural plan
identity.

Interpreters are therefore intentionally explicit. Adding one means Metis has chosen to understand a vendor contract rather than merely preserve its payload.

## Requirement: the demand side (`want`)

An Interpreter produces zero or more `Requirement` values.

Conceptually:

```text
Identity
Version
Capability
Critical
```

For example, a vendor extension may mean:

```text
identity   = acme/fiscal_calendar/metric
version    = 1
capability = fiscal_calendar
critical   = true
```

This says what correct semantic compilation requires. It does not say that every Metis Renderer supports the requirement.

A non-critical requirement remains opaque-preserved. A critical requirement must be satisfied by the selected Renderer or compilation fails closed.

## Registration: the supply side (`have`)

A `Registration` is one capability support declaration.

For example:

```go
extension.Registration{
    Identity:   acmeFiscalCalendar,
    Version:    "1",
    Capability: "fiscal_calendar",
    Dialect:    "CLICKHOUSE",
}
```

This means:

> The selected ClickHouse Renderer supports version 1 of this extension capability.

A Registration is data. It is not the registry itself.

## Registry: the capability catalog and matcher

`extension.Registry` owns capability Registrations and resolves requirements
through the selected Renderer's capability evidence. It does not select or
look up a Renderer.

Think of it as a table:

| Extension identity | Capability | Version | Renderer dialect |
| --- | --- | --- | --- |
| `acme/fiscal_calendar/metric` | `fiscal_calendar` | `1` | `clickhouse` |
| `acme/metric_window/metric` | `metric_window` | `2` | `doris` |

Each row is a `Registration`; the whole table plus deterministic matching behavior is the `Registry`.

Duplicate normalized registrations fail closed. Registration order must never decide semantic behavior.

## Renderer support is intentionally partial

A semantic extension does **not** need to be supported by every Renderer.

For example, an extension capability may only be implemented for ClickHouse:

| Capability | Doris | ClickHouse | Snowflake |
| --- | --- | --- | --- |
| `bitmap_metric` | unsupported | supported | unsupported |

That is valid. Metis must not add fake Doris or Snowflake Registrations merely to make the matrix complete.

For a critical `bitmap_metric` requirement:

```text
                         Requirement
                      bitmap_metric v1
                             |
                +------------+------------+
                |                         |
                v                         v
      Renderer=ClickHouse           Renderer=Doris
                |                         |
       Registration found          no Registration
                |                         |
                v                         v
            supported            unsupported-critical
                                      fail closed
```

Capabilities are isolated by the selected Renderer. A Registration for
ClickHouse cannot satisfy a Doris compile request, and capability matching MUST
NOT perform a second Renderer lookup.

This lets Metis remain engine-neutral without reducing every semantic feature to the lowest common denominator across databases.

## Resolution: the result

The Registry produces one of three RFC-0021 states:

- `supported`: the critical requirement has a matching capability Registration in the selected Renderer's evidence for the requested version;
- `opaque-preserved`: the extension is not semantically required by Metis and remains preserved without interpretation;
- `unsupported-critical`: correctness requires the extension semantics, but the selected compile path cannot satisfy them.

Typical fail-closed reasons include `capability_missing` and `version_incompatible`.

Raw extension payloads are not included in unsupported diagnostics by default.

## End-to-end example

Suppose an Ossie model contains a known vendor extension representing a fiscal calendar.

```text
Ossie CustomExtension
        |
        | vendor=acme
        v
extension.Interpreter
        |
        | understands the explicit acme contract
        v
Requirement
        |
        | want: fiscal_calendar v1, critical=true
        v
Registry.Resolve(requirement, selectedTarget)
        |
        +---- Registration: ClickHouse has fiscal_calendar v1
        |
        v
Resolution
```

With `selectedTarget=clickhouse`, the result is `supported`.

With `selectedTarget=doris` and no Doris Registration, the result is `unsupported-critical` and Metis fails before physical SQL is produced.

If the vendor is unknown and therefore has no registered Interpreter, Metis does not guess that it is a fiscal calendar extension. The payload remains opaque-preserved.

## Responsibilities at a glance

| Type | Responsibility | Mental model |
| --- | --- | --- |
| `CustomExtension` | Ossie extension material | preserved input |
| `Identity` | stable extension coordinates | what extension? |
| `Interpreter` | understands an explicit vendor contract | interpret |
| `Inventory` | finds registered Interpreters for observed extensions | interpretation catalog |
| `Requirement` | capability demanded by extension semantics | want |
| `Target` | selected engine + dialect | where? |
| `Registration` | one target capability declaration | have |
| `Registry` | stores declarations and matches requirements | catalog + matcher |
| `Resolution` | support decision | result |
| `UnsupportedError` | bounded fail-closed evidence | diagnostic |

## Adding support for a new semantic extension

When implementing a new extension, keep the responsibilities separate:

1. Confirm the semantics come from an explicit Ossie/vendor contract. Do not infer semantics from arbitrary payload shapes.
2. Implement an `extension.Interpreter` for that vendor contract.
3. Produce the smallest explicit `Requirement` needed for correctness.
4. Register capability support only for engine/dialect targets that actually implement it.
5. Add tests for supported targets, unsupported targets, version mismatch, duplicate registration, and unknown-vendor preservation as applicable.
6. Add compiler or real-engine evidence when the extension changes generated SQL or result semantics.

Do not modify generated Ossie bindings by hand, introduce a parallel Metis semantic schema, or add database execution/cost-based optimization to the extension runtime.

## Related contract

See RFC-0021, `docs/proposals/semantic/0021-semantic-critical-extension-capabilities.md`, for the normative semantic-critical extension capability contract.
