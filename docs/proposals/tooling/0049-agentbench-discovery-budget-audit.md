# RFC-0049: AgentBench discovery sufficiency and budget audit correctness

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-27
- **Last updated:** 2026-08-27
- **Scope:** `tests/agentbench`, Pi/generic drivers, Agent-facing dimension discovery
- **Supersedes:** None
- **Related:** RFC-0044, RFC-0048

## Summary

Close the AgentBench failures exposed by the first Pi/DeepSeek full diagnostic after RFC-0048:

1. preserve compact structured tool traces for generic/Pi runs;
2. make terminal tool-budget overrun accounting deterministic and archivable;
3. ensure `get_dimensions` returns the minimum semantic evidence required to author the next `compile` request without trial-and-error, especially time-dimension type and valid grains;
4. preserve a semantic distinction between dense-calendar and sparse/offset time behavior without leaking planner implementation flags.

The governing principle is:

> **Every Agent-authored compile field must be discoverable from the preceding semantic surface or mechanically derivable from a canonical ref.**

This RFC does not add `search_semantics` to the primary MCP surface.

> **Current interface refinement:** RFC-0051 moves time-grain detail from the
> compact `get_dimensions` index to `get_dimension` and moves relationship
> evidence to `get_relationships`. The AgentBench audit requirements remain.

## Motivation

A fiscal-calendar AgentBench case exhausted its 20-call tool budget without producing SQL. Metis compilation was correct when given the canonical request, but the Agent-facing discovery surface omitted information required to construct that request:

- two metrics shared the same display name in different models;
- `get_dimensions` exposed only ref/name/model/description;
- the Agent still had to infer that `calendar_day` was a time dimension;
- it had to guess that `fiscal_week` was a valid grain;
- it had to distinguish a dense fiscal-calendar semantic from an offset/sparse alternative.

The Pi driver also observed 22 tool starts after terminating on the first over-budget event, and the final artifact validator rejected the completed 50/50 run because it only accepted exactly `budget + 1` as a terminal overrun.

The benchmark therefore exposed two independent problems: semantic discovery insufficiency and harness/audit correctness.

## Decisions

### 1. Structured generic tool trace

The generic driver protocol carries compact tool evidence separately from the diagnostic transcript. The normalized `AgentResult.ToolTrace` is populated when the wrapper reports it.

Tool trace remains bounded metadata, not raw prompts/responses.

### 2. Frozen terminal overrun accounting

A driver freezes the reported tool count at the first observed over-budget start: `budget + 1`. Events already buffered after that boundary may be retained only as diagnostic transcript evidence and do not increase normalized accounting.

Artifact validation accepts a terminal `ToolBudgetExceededError` only when the normalized final count is exactly `budget + 1`.

### 3. Discovery sufficiency

`get_dimensions` exposes semantic metadata needed to author `compile`:

- semantic dimension kind/type;
- valid time grains for time dimensions;
- compact semantic capability/evidence where required to distinguish materially different query behavior.

The API remains compact: it does not expose planner internals, physical SQL details, or the entire semantic graph.

### 4. Dense-calendar evidence is semantic, not implementation detail

If the authored model distinguishes a dense calendar/population-preserving time semantic from an offset/sparse semantic, the Agent surface should describe the semantic consequence (for example preserving empty fiscal periods), not leak an internal `dense_mapping` planner flag.

## Acceptance criteria

- Pi/generic runs persist compact structured tool names/statuses through `AgentResult.ToolTrace`.
- A driver that sees arbitrary buffered starts after the first overrun reports `budget + 1`, raises `ToolBudgetExceededError`, and produces an artifact accepted by `NewArmRunArtifact`.
- The previously completed 50/50 diagnostic shape can be archived when its terminal overrun is normalized.
- `get_dimensions` tells an Agent whether a dimension is time-based and which grains are accepted by `compile`.
- The fiscal-calendar smoke can select the intended semantic path without repeated compile probing caused by missing type/grain metadata.
- Existing RFC-0044 primary tool names and staged flow remain unchanged.
