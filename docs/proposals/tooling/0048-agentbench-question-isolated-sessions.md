# RFC-0048: AgentBench question-isolated Agent sessions

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-26
- **Last updated:** 2026-08-27
- **Scope:** `tests/agentbench`, live Agent drivers, benchmark collection order
- **Supersedes:** RFC-0046

## Summary

Give every benchmark question an independent external-Agent process and
conversation against a shared read-only arm workspace. Reuse that conversation only
for the same question's optional repair. Keep the Metis runtime, MCP listener, DuckDB
execution seam and model identity stable for the complete collection. Every
Agent process receives a clean benchmark-owned runtime environment.

Collect each repetition in scenario order and run the raw-assets arm followed
immediately by the Metis arm for the same scenario. Final artifacts remain
normalized by scenario and repetition for direct horizontal comparison.

## Motivation

RFC-0046 attempted to reduce repeated startup and YAML-discovery cost by asking
all questions in one multi-turn conversation. A live Claude diagnostic showed
the opposite result: independent questions accumulated prior prompts, tool
results, and reasoning in the model context. Later questions became slower and
more expensive, and their answers were no longer independent observations.

Agent process startup is smaller than the cost and experimental bias of an
ever-growing conversation. Shared services can still avoid the expensive
semantic-runtime reload without sharing model context.

## Design

The collection loop owns one question lifecycle:

1. expose the shared read-only arm assets and open a fresh Agent session with a
   temporary writable home;
2. prepare the question's canonical DuckDB fixture;
3. ask the frozen business question and score its result;
4. if eligible, send one repair turn in the same session and workspace;
5. journal the scored attempt and close the Agent session;
6. open the opposite arm for the same scenario/repetition;
7. advance only after the raw/Metis pair completes.

The 10-call budget remains per attempt. With at most two attempts, a question
can use at most 20 successful tool calls. A tool-budget failure is journaled,
that question's process is closed, and collection continues with a clean
session. Agent/provider unavailability still aborts collection after journaling
the failed question.

The raw arm materializes the same complete canonical Ossie project once in one
shared read-only workspace and updates only the harness-owned `schema.sql`
between questions. The Metis arm needs no semantic workspace and receives only
an empty read-only cwd plus the same long-lived benchmark MCP endpoint. Agent
customizations are disabled: Codex uses a read-only sandbox and temporary home
containing only authentication; Claude uses safe mode, read-only discovery
tools, and provider-only connection variables. Every process gets an isolated
temporary `HOME/TMPDIR`. Raw exposes no MCP server; only the benchmark-owned
`metis` server distinguishes Path B.

Prompt protocol `v0.12` separates these results from RFC-0046's conversation-
dependent `v0.10` evidence and the non-isolated `v0.11` diagnostics.

## Alternatives

- **One conversation per repetition:** rejected because context and tool output
  accumulate across independent questions.
- **Restart every few questions:** rejected because group boundaries create an
  arbitrary context advantage and questions within a group remain dependent.
- **Reuse one process with a reset command:** not portable across Codex, Claude,
  and generic drivers; a fresh native session is the common isolation boundary.

## Rollout and migration

Existing `v0.10` and `v0.11` artifacts remain readable but are not comparable
with `v0.12`.
No external-Agent driver protocol changes are required: `AgentDriver.Open`,
`AgentSession.Run`, and `AgentSession.Close` now execute once per question
rather than once per repetition.

## Test and acceptance criteria

- Every arm/question/repetition opens and closes exactly one Agent session.
- A repair reuses only its own question's session and workspace.
- No Agent context or Agent-created workspace state crosses a question boundary.
- Journal order is raw then Metis for every identical scenario/repetition pair.
- Final arm artifacts align by scenario and repetition.
- Shared Metis/MCP/DuckDB runtime state is not reloaded per Agent session.
- Tool-budget failures remain question-local and collection continues.

## Documentation updates

`tests/benchmarks/agent-semantic/README.md` defines the current executable
benchmark contract. RFC-0046 and the proposal index identify this RFC as the
superseding lifecycle decision.
