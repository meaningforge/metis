# RFC-0074: S2SBench diagnostic fidelity and token efficiency

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-03
- **Last updated:** 2026-09-03
- **Scope:** `tests/s2sbench`, Agent semantic discovery, and benchmark-only execution evidence
- **Supersedes:** None; refines RFC-0072's collection and reporting frame

## Problem

The first semantic-interface comparison exposed useful failures, but its
single end-to-end verdict could not distinguish governed execution from the
Agent's final textual response. A shared deadline could also expire during repair
and be reported as an execution failure. The Metis arm received a redundant
physical schema file, broad dimension discovery responses consumed context,
and the one-repetition formal frame overstated noisy model outcomes.

## Decision

The active harness is named **S2SBench**. This supersedes the AgentBench product
name and `tests/agentbench` path used by RFC-0041 through RFC-0072 while
preserving those RFCs as historical records. Current code, operator commands,
and developer documentation MUST use `s2sbench` and `tests/s2sbench`.

S2SBench now:

- propagates the remaining question deadline into every Agent turn and records
  bounded failure details under the actual failing phase;
- reports first-attempt readiness, semantic-intent success, handoff success,
  and strict end-to-end success independently;
- exposes the complete configured production MCP tool set without a
  benchmark-specific allowlist;
- treats the structured `query_metrics` result as the Metis arm's primary
  scored output and consumes `compile_sql` directly as fallback evidence,
  without requiring the Agent to copy SQL or parameters;
- wraps only the benchmark DuckDB Driver and snapshots the exact
  `CompiledQuery` received by `Executor.Execute`, correlating it to the
  structured result by `query_id`; the public response and production
  observability contracts remain unchanged;
- gives `schema.sql` only to the OKF arm; the Metis arm receives semantic
  discovery through MCP and the common task contract;
- uses five independent repetitions for the formal suite while smoke,
  diagnostic, and focused-case suites remain one-repetition diagnostics.

The Agent semantic surface now bounds `get_dimensions` to 20 items by default
and 100 maximum, with an opaque cursor scoped to the same semantic anchor and
search. `list_metrics` exposes exact authored-definition equivalents as hints
without selecting among business identities. Integer fields labelled as time
dimensions remain groupable dimensions but advertise no built-in calendar
grain and reject grain-bearing compile requests.

## Compatibility

The prompt, arm-report, and comparison-report schema versions are advanced.
Artifacts from earlier versions must not be pooled with the new evidence.
Both arms remain bound to the same physical fixture and result oracle. The
Agent's compact post-tool text is evidence, but it is not a second lossy data
transport for a successful structured Metis response.

## Validation

Service tests cover time-grain rejection, metric-definition equivalence, and
cursor pagination. Harness tests cover remaining-deadline propagation,
arm-specific workspaces, layered reporting, structured query/compile response
evidence, full runtime tool exposure, and five-repetition formal manifests.
