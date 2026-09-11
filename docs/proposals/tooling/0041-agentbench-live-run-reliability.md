# RFC-0041: AgentBench live-run reliability and incremental evidence

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-25
- **Last updated:** 2026-08-25
- **Scope:** `tests/agentbench`, live AgentBench artifacts and operator workflow
- **Supersedes:** None

## Summary

Make paid AgentBench runs fail fast when the external Agent cannot start or
cannot use the requested model, reuse one loopback MCP listener for the whole
run, and persist one detailed evidence record after every completed question
repetition. Preserve the frozen five-repetition formal benchmark while adding
explicitly non-comparable diagnostic and single-scenario smoke modes.

## Motivation

The original live runner treated an external-Agent protocol failure as an
ordinary failed answer. A model/authentication incompatibility could therefore
consume both attempts for every question in both arms before the final report
revealed that no model inference had occurred.

The runner also restarted the Metis MCP HTTP listener for every scenario. The
fixture isolation was correct, but changing the port was unnecessary and made
the active endpoint difficult to observe. Finally, evidence existed only in
memory until both arms completed. A crash lost all completed work, and an
operator could not inspect the prompt, latency, SQL, actual result, and oracle
result while a long run was in progress.

## Design

### Fatal external-Agent errors

Agent drivers classify process startup, provider/model rejection, fatal stream
events, and wrapper process failure as external-Agent availability errors.
These errors retain the failed repetition as evidence, skip the repair attempt,
and abort the collection immediately. SQL validation, SQL execution, budget,
and ordinary answer failures remain scored benchmark behavior and may use the
frozen repair attempt.

### One MCP listener per live run

The Metis arm starts at most one loopback HTTP listener and one random bearer
token. The complete canonical multi-model project is loaded once and the same
authenticated handler, namespace, URL, and token serve the whole collection.
The default port is allocated once by the OS. `--mcp-port` allows an operator
to request a specific loopback port; a bind conflict fails before collection.
RFC-0042 defines the immutable semantic-world protocol in detail.

### Formal, diagnostic, and smoke runs

The formal run remains five repetitions with the frozen budget. A live run may
set `--repetitions 1`; its manifest is marked `diagnostic`, and it must not be
presented as a formal v0 benchmark result. No other repetition count is
accepted.

Supplying `--scenario <frozen-name>` selects exactly one member of the frozen
25-question sample, automatically uses one repetition, and marks the manifest
as `smoke`. Both arms still run, so a successful smoke collection contains two
repetition records. If a repetition needs repair, the existing two-attempt
limit still applies. Smoke results validate one end-to-end path and must not be
reported as diagnostic or formal benchmark evidence.

### Incremental evidence journal

Before collection, the runner creates `<output>.partial/` containing the
manifest and `attempts.jsonl`. After every completed repetition it appends and
syncs one record with:

- arm, scenario, repetition, and question;
- start time and elapsed milliseconds;
- generated/materialized SQL and semantic query when applicable;
- actual normalized result and canonical expected result;
- verdict, failure, usage, and compact retry evidence.

Fatal repetitions are journaled before the run aborts. On success the complete
arm artifacts and derived report are written into the same staging directory,
then the directory is atomically renamed to the requested output. On failure,
the `.partial` directory is retained for diagnosis.

## Alternatives

**Use one hard-coded MCP port.** Rejected as the default because concurrent
runs and unrelated local services would conflict. An explicit fixed-port flag
provides stable operator control without making concurrency unsafe by default.

**Reduce the formal benchmark to one repetition.** Rejected because a single
sample cannot expose run-to-run model spread. One repetition is supported only
as a diagnostic mode and is labeled in the manifest.

**Keep only the final atomic bundle.** Rejected because paid long-running work
must remain auditable after a crash or fatal Agent error. The final bundle is
still atomic; the partial journal is intentionally visible before completion.

## Rollout and migration

The manifest schema advances to `agent-semantic-v0.3` and adds `run_mode`.
Existing v0.2 artifacts remain historical evidence but are not accepted as new
collections. Result tooling must distinguish `benchmark`, `diagnostic`, and
`smoke` manifests.

RFC-0043 subsequently advances the manifest schema to `agent-semantic-v0.4`
and removes repeated prompts and raw Agent transcripts from persisted records.

## Test and acceptance criteria

- A Codex provider/model rejection aborts after the first external invocation.
- A fatal repetition is present in `attempts.jsonl` after the abort.
- A non-fatal invalid answer may still use the second frozen attempt.
- The MCP URL and loaded semantic project remain identical across scenarios.
- An explicitly requested MCP port is used or fails with a bind error.
- Diagnostic runs record one repetition per question and are labeled.
- Smoke runs accept exactly one frozen scenario and produce one record per arm.
- Formal runs retain the frozen five repetitions.
- Successful runs contain the four existing JSON files plus
  `attempts.jsonl`; incomplete runs retain `<output>.partial`.

## Documentation updates

`tests/benchmarks/agent-semantic/README.md` is the current operational contract
and documents modes, fail-fast behavior, stable-per-run MCP addressing, and the
incremental journal.
