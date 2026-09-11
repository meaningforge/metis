# Agent Query Conformance Specification

## Contract

Phase E Agent query conformance proves that the public structured query contract, validation diagnostics, repair evidence, explanation, and compilation remain one semantic system rather than transport- or operation-specific interpretations.

The canonical in-process corpus lives under `tests/conformance/agentquery/` and runs through the same project bootstrap/runtime wiring used by public service entrypoints.

## Required success evidence

Representative valid requests MUST prove:

- `Validate` returns `valid=true` with no diagnostics;
- `Explain` succeeds for the same `CompileRequest`;
- `Compile` succeeds for the same `CompileRequest`;
- typed metric, dimension, filter, ordering, and limit fields retain their semantic meaning without raw SQL escape hatches.

The implication contracts are:

```text
Compile success => Validate success
Explain success => Validate success
```

## Required failure evidence

Representative invalid requests MUST prove stable semantic failure identity across operations where the same semantic boundary is reached.

Coverage includes, as applicable to the current corpus:

- unknown metric and dimension identity;
- invalid filter/operator shape;
- invalid positive-limit requirements;
- ambiguous/unreachable relationship and advanced semantic failures already covered by the canonical semantic corpus;
- unsupported target/expression failures already covered by target-resolution/compiler conformance.

For a matching failure, Validate's diagnostic code MUST equal the stable `serrors` code surfaced by Explain and Compile. Authoritative structured details MUST not be replaced by transport prose.

## Repair evidence

Unknown-asset diagnostics MAY include deterministic bounded candidate suggestions derived from the existing discovery read model.

Conformance MUST verify at least one repair case and MUST NOT interpret a suggestion as successful validation of the original request. Suggestions are evidence for a client-controlled resubmission only.

## Transport relationship

REST and MCP transport tests prove that validate/compile/explain bind the shared service DTOs. The Agent query corpus is transport-neutral and therefore does not duplicate semantic algorithms per protocol.

## Repository gates

Agent query contract changes remain subject to normal repository correctness gates, including Go formatting, docs-check, `make check`, canonical conformance, and relevant Doris/ClickHouse real-engine execution. The Agent query corpus complements those gates; it does not replace physical-result correctness evidence.
