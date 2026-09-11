# RFC-0046: AgentBench multi-turn Agent sessions

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Superseded
- **Owners:** Metis maintainers
- **Created:** 2026-08-26
- **Last updated:** 2026-09-03
- **Scope:** `tests/agentbench`, live Agent drivers, benchmark prompt protocol
- **Supersedes:** None
- **Superseded by:** [`RFC-0048`](0048-agentbench-question-isolated-sessions.md)

> Superseded after live diagnostics demonstrated that carrying independent
> questions through one conversation caused cumulative context growth, higher
> latency and token usage, and cross-question contamination. RFC-0048 retains
> shared benchmark services but restores a fresh Agent conversation per
> question.

## Summary

Run each repetition of each AgentBench arm as one multi-turn Agent process and
conversation. Start the Agent when the repetition sends its first question,
send every subsequent question as another user turn, retain per-turn evidence
and budget accounting, and close the process after that repetition has asked
all selected questions and repairs. The next repetition starts clean.

Path A and Path B remain separate sessions. They never share conversation state,
workspace state, tools, or credentials.

## Motivation

The previous driver boundary launched a fresh non-interactive Agent process for
every question. Path A consequently made the Agent rediscover and reload the
same complete Ossie YAML project on every invocation. Besides startup latency,
that repeatedly charged invariant semantic context and did not resemble a human
asking several analytical questions in one project conversation.

The semantic project and Metis runtime are already immutable across a
collection. Agent lifecycle should match that boundary.

## Design

`AgentDriver.Open` creates an `AgentSession` per arm repetition. `AgentSession.Run` represents one
user turn and returns usage, tool calls, transcript, and final output for only
that turn. `AgentSession.Close` owns process shutdown. The collection loop
closes the session on success and on early failure.

The built-in drivers use their native streaming protocols:

- Codex uses one app-server process, one thread, and repeated `turn/start`
  requests.
- Claude Code uses one `--input-format stream-json` process and repeated user
  messages.
- The generic driver protocol advances to `agentbench-driver-v2`, a JSONL
  request/response stream over one wrapper process.

Each arm repetition owns one stable workspace. Path A writes the canonical model files
once and refreshes only `schema.sql` after fixture preparation. Path B keeps its
workspace empty. The first turn carries the full frozen arm protocol; later
turns carry the next business question plus compact reminders about the stable
semantic interface and refreshed physical schema.

Tool-call limits, context/output tokens, SQL execution, repair accounting,
journaling, and scoring remain per question. A repair is another turn in the
same conversation and receives only its own failure, never oracle feedback.

## Experimental consequence

Answers inside one arm repetition are intentionally conversation-dependent. This changes
the experiment from independent cold-start invocations to a realistic project
conversation. Later questions may benefit from semantic context established by
earlier questions; that benefit is the behavior this lifecycle is intended to
measure. Prompt protocol version `v0.10` prevents comparison with cold-start
collections. Repetitions remain independent samples because each starts with a
new process, conversation, and workspace.

## Failure behavior

A process/provider failure ends the arm as before. Session close errors fail
collection when no earlier error already explains the failure. Configuration
that must remain process-static (workspace, MCP endpoint, environment, output
schema) is checked on every turn and fails closed if it changes.

## Acceptance criteria

- A full dual-arm collection opens and closes exactly two Agent sessions per
  repetition.
- Every selected question still produces one independently journaled attempt
  per configured repetition.
- Repair turns reuse the same session and workspace.
- Raw Ossie files are materialized once per arm; `schema.sql` is refreshed for
  each current fixture.
- Per-turn tool and token accounting does not accumulate prior-turn values.
- Raw and Metis sessions remain isolated.
- Codex and Claude sessions inherit the caller's normal customer environment.
  The raw arm freezes its MCP-server baseline; the Metis arm must expose exactly
  that same baseline plus the benchmark-owned `metis` server. A caller baseline
  that already contains `metis` fails closed.
