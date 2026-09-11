# Agent Context Conformance Specification

## Purpose

Agent-facing semantic context and explanation are public structured contracts. Their correctness is proven independently of SQL rendering and real-engine execution by canonical conformance tests under `tests/conformance/agentcontext`.

## Canonical evidence

The corpus MUST use reusable semantic fixtures and assert deterministic structured output for representative semantics, including:

- relationship compatibility, unreachable paths, and ambiguity;
- time alignment and time offset;
- metric fill policy;
- semi-additive selection and composability;
- derived and conversion metric composition;
- metric-definition and query filters;
- grouping, ordering, and limits.

Assertions MUST target stable Agent-facing DTO fields, semantic step kinds, issue codes, and relationship-path evidence. Tests MUST NOT make internal `SemanticPlan` or SQLPlan structure part of the public contract.

Equivalent requests over the same fixture MUST produce equivalent structured output. Repeated execution is part of the determinism contract.

## Transport parity

REST and MCP parity MUST be proven against the same service-level canonical expectation.

A transport parity test MUST:

1. execute the canonical request directly through the shared service;
2. execute the equivalent REST request through the real handler;
3. initialize MCP and execute the equivalent tool call through the real Streamable HTTP handler;
4. decode protocol envelopes and compare the complete structured Agent-facing payload.

Transport tests MUST NOT replace semantic assertions with field-presence greps. JSON and valid Streamable HTTP SSE framing are protocol representations of the same semantic payload and MUST decode to equivalent structured content.

## CI contract

Agent-context conformance is part of the required offline correctness gate through the existing conformance test tree. Changes to discovery, Semantic Context, Explain Query, Resolver/Planner evidence, REST, or MCP that alter the public contract MUST keep this corpus green or update the normative specification and canonical expectations intentionally.

The standard repository gates remain authoritative:

```bash
make fmt-check
make check
make smoke
```

Relevant real-engine gates remain independent evidence for physical execution semantics; Agent-context conformance does not replace them.
