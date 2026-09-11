# RFC-0043: AgentBench compact result artifacts

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-25
- **Last updated:** 2026-08-25
- **Scope:** `tests/agentbench`, live journals, AgentBench artifact schema
- **Supersedes:** RFC-0041's verbose persisted prompt/transcript evidence

## Summary

Make AgentBench result bundles readable and bounded by omitting repeated full
prompts and raw structured Agent transcripts from default persisted artifacts.
Keep the outcome evidence needed to inspect semantic correctness, performance,
usage, and repairs.

The manifest schema advances to `agent-semantic-v0.4`.

## Motivation

One three-record Codex sample produced a 156 KiB incremental journal. The final
transcript appeared once at the record top level and again in `trace`; those two
copies consumed roughly 132 KiB, or 83 percent of the file. SQL, actual result,
and expected result together used about 5 KiB.

Raw event streams make the ordinary result difficult to inspect and scale
linearly across the 250-repetition formal collection. Repeating the full frozen
prompt in every record also adds noise. The benchmark question is answered by
the generated query and its scored result, not by preserving every streaming
event emitted by a particular Agent CLI.

## Compact persisted contract

Every journal and arm-artifact record retains:

- arm, scenario, repetition, question, start time, and duration;
- final attempt index, SQL, and Metis semantic query when applicable;
- normalized actual result and canonical expected result;
- verdict, failure reason, tool-call count, and token counts;
- compact per-attempt repair evidence: SQL/semantic query, timing, usage,
  result, and error.

It does not serialize:

- the full prompt on every record or trace entry;
- raw Codex, Claude Code, or generic-wrapper interaction transcripts.

`PromptVersion`, the question, run mode, model identity, and documented arm
protocol identify the frozen input. A repair's triggering failure remains in
the compact trace.

Drivers may still retain prompts and transcripts in memory while parsing and
validating one invocation. The persistence boundary removes them before JSONL
or final JSON encoding.

## Alternatives

**Keep transcript once instead of twice.** This removes the accidental
duplication but still leaves raw event streams as the dominant artifact cost.

**Compress transcripts inside the main bundle.** Compression reduces disk
space but does not improve readability and complicates incremental inspection.

**Persist transcripts behind a default-on flag.** Rejected because the common
result remains noisy. A future explicit debug export may be added if a concrete
protocol-forensics need appears.

## Test and acceptance criteria

- JSON artifacts contain question, SQL, actual result, expected result,
  duration, verdict, and compact repair evidence.
- JSON artifacts contain neither `prompt` nor `transcript` fields, including
  inside `trace`.
- In-process driver/parser tests can still inspect the raw transcript before
  persistence.
- Usage reporting and first-try/repair accounting remain unchanged.
- Existing v0.3 partial evidence remains historical and is not rewritten.
