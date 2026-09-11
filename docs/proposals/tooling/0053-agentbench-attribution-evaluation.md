# RFC-0053: AgentBench Attribution Evaluation

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-28
- **Last updated:** 2026-09-01
- **Scope:** `tests/agentbench/attribution`, benchmark-private attribution adapter, live attribution evaluation artifacts
- **Supersedes:** None
- **Related:** RFC-0048, RFC-0051, RFC-0052

> **Current parameter transport:** SQL and parameters are now carried separately
> to database drivers. The broker materialization described below is historical;
> see the [S2SBench contract](../../specs/testing/s2sbench-cli.md).

## Summary

RFC-0053 introduced a separate AgentBench frame comparing the same Agent using
ordinary Metis semantic compile loops with the same Agent using RFC-0052's
deterministic attribution bundle. The experiment measured correctness, tool
calls, token usage, reconciliation failures, and final-answer consistency
without modifying the frozen v0 raw-assets-versus-Metis experiment or adding a
production Agent-facing tool.

The two experimental arms were:

```text
compile_loop
  production nine-tool Metis discovery/compile surface
  + benchmark-owned execution/result feedback

attribution_bundle
  identical production discovery surface
  + one benchmark-private adapter over the internal attribution compiler
  + benchmark-owned execution/result feedback
```

The evaluation is now closed. The accepted four-scenario diagnostic produced a
clear product-direction signal, so no five-repetition formal paid collection
will be run.

Post-implementation note: after RFC-0057 productized `attribute_metric`, the
benchmark-private adapter, broker, tools, runner, and live wiring were retired.
The frozen frame, oracle, artifact validation, and accepted evidence remain as
historical assets; the active attribution AgentBench now compares production
`query_metrics` with production `attribute_metric`.

## Accepted diagnostic result

The accepted Pi 0.84.1 + DeepSeek `deepseek-v4-flash` one-repetition diagnostic
covered all four frozen semantic shapes:

| Arm | Correct | Failed | Tool calls | Context tokens | Output tokens | Reconciliation failures |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `compile_loop` | 0/4 | 4/4 | 44 | 137,075 | 11,785 | 0 |
| `attribution_bundle` | 4/4 | 0/4 | 4 | 39,970 | 5,228 | 0 |

The bundle arm therefore reduced observed tool calls from 44 to 4 while
answering all four scenarios correctly. The compile-loop arm exhausted or
failed within the frozen interaction budget on every scenario.

This remains descriptive directional evidence, not a statistical claim. The
model version is a provider alias rather than an immutable model revision, and
the collection has one repetition. Those limitations are preserved in the
committed evidence rather than hidden by a stronger claim.

## Decision

The evidence is sufficient for the architectural question RFC-0053 was created
to answer:

> A deterministic attribution bundle should be productized rather than asking
> an Agent to manually orchestrate independent ordinary compile calls.

The next product step is a separate public-interface RFC. That RFC may expose a
bounded Agent-facing attribution operation while preserving RFC-0052's
compile-only ownership. RFC-0053 itself does not define the production MCP or
REST contract.

A five-repetition formal collection would add provider cost and statistical
appearance without changing the product decision supported by the current
result. The live formal runner is therefore retired. Smoke and one-pass
diagnostic modes remain available only for harness debugging and historical
reproduction work; ordinary CI continues to invoke no paid Agent/provider.

The frozen benchmark data model retains its original benchmark run-mode value
for artifact/schema compatibility. That value is not reachable from the live
`attribution-run` command.

## Motivation

RFC-0052 has deterministic semantic, physical, and three-engine evidence for
metric change attribution. It deliberately left public interface design
unresolved until Agent evidence showed whether the primitive improved real
analytical work.

The existing AgentBench v0 frame could not answer that question. It compares raw
semantic assets with production Metis for one final SQL query, forbids the Agent
from reading warehouse results, and freezes a 25-question corpus and prompt
protocol. Attribution requires a multi-step analytical loop, controlled result
feedback, multiple independent dimension queries, and scoring of reconciliation
evidence. Mutating v0 would invalidate its historical comparison.

## Design

### Separate frozen identity

The attribution experiment uses its own package, schema version, prompt version,
scenario set, manifest, artifacts, and report. It imports shared neutral types
such as Agent identity and task budget where useful, but v0 manifests and
reports remain byte- and behavior-compatible.

Both arms receive the same Agent/model identity, question, project, physical
fixture, execution broker, attempt/tool budget, and repetitions. The only
intentional difference is availability of the benchmark-private deterministic
attribution compile adapter.

### No production interface expansion in this RFC

The attribution adapter lives under `tests/agentbench`. It calls the same
internal resolved-request, attribution-plan, independent `SemanticPlan`, and
physical compilation contracts proven by RFC-0052. It is not registered in
`app/mcp`, `app/rest`, or `app/service/semantic`, and it is not part of the production
nine-tool vocabulary.

The harness, not Metis, executes physical queries and returns bounded normalized
result evidence to the Agent. Neither arm receives database credentials or
direct database access.

### Frozen scenarios

The frame contains four semantic shapes:

1. additive continuing/entry/exit contributions;
2. additive positive/negative offsets with zero total delta;
3. ratio symmetric rate/mix plus entry/exit effects;
4. ratio segment and total zero-denominator undefined evidence.

At least one scenario requests two dimensions. Each dimension remains an
independent attribution and is scored against independently calculated ground
truth. Questions ask for business change evidence without naming Metis planner
or SQL implementation vocabulary.

### Evidence and scoring

Each attempt records arm, scenario, repetition, final retry index, duration,
tool calls, context/output tokens, verdict, reconciliation-failure count, and a
canonical final-answer fingerprint. Raw prompts and transcripts remain
diagnostic-only.

The derived report contains, per arm:

- correct, wrong, and failed attempt counts and accuracy;
- first-try correct count;
- total tool calls and context/output tokens;
- reconciliation failures observed in returned evidence;
- scenarios whose canonical final answer differs across repetitions.

Failed attempts remain in the accuracy denominator. Reports are descriptive and
do not claim statistical significance.

### Reconciliation failure

A reconciliation failure is an Agent answer that presents decomposition as
defined but whose complete segment effects do not reconcile to the stated total
change under the scenario's numeric tolerance, or that presents undefined
runtime evidence as a defined zero. Harness/transport failures are recorded as
failed attempts, not reconciliation failures.

## Alternatives

### Extend the frozen v0 benchmark

Rejected because it would change the question corpus, allowed interaction, final
answer type, and tool protocol of an existing experiment.

### Add a production attribution MCP tool before evaluation

Rejected when this RFC began because Phase F existed to gather evidence for
that interface decision. The accepted diagnostic now supplies that directional
evidence; production interface design remains a separate RFC so experimental
harness details do not become an accidental public contract.

### Compare raw assets with attribution

Rejected because it would conflate two effects: semantic resolution through
Metis and deterministic attribution planning. The controlled comparison isolates
ordinary Metis orchestration versus the new primitive.

### Run the five-repetition formal paid collection

Rejected after the accepted diagnostic. The result is sufficiently separated
for the product decision (`compile_loop` 0/4 versus `attribution_bundle` 4/4,
44 versus 4 tool calls), while a larger paid run would not remove the model-alias
reproducibility limitation or define the production interface. The formal live
entry point is closed to prevent accidental provider spend.

### Let the Agent connect directly to DuckDB

Rejected because credentials/execution are outside Metis and direct access would
make tool availability and unrestricted data exploration part of the treatment.

## Rollout and closure

Phase F1 added the frozen manifest/artifact/report contract and deterministic
tests. Phase F2a added the benchmark-private planner adapter, DuckDB execution
broker, planted fixtures, and evidence oracle. Phase F2b added the two Agent
interaction loops and smoke/diagnostic live runner.

The F2a adapter compiles every requested dimension independently through the
RFC-0052 planner and native engine. The broker materializes returned parameters
and executes only those physical DuckDB queries. Four isolated fixtures prove
additive population union, offsetting zero-total behavior, symmetric ratio
mix/rate with entry/exit, and explicit segment/total undefined ratios. The
oracle compares independent planted ground truth, audits reconciliation
separately, and fingerprints normalized evidence.

The F2b collector interleaves `compile_loop` then `attribution_bundle` for each
scenario/repetition, opens a clean Agent session for every scored question,
permits at most one same-session repair, and accounts tool calls and tokens
cumulatively across both turns. Both arms receive the same production Metis
discovery/compile server plus the same benchmark-only, bounded DuckDB
result-feedback tool. Only the treatment arm receives the benchmark-private
compile-and-execute attribution-bundle tool. The local live runner writes an
fsynced attempt journal before atomically committing both arm artifacts and the
derived report. Broker preparation and execution share one serialization gate
so terminating an Agent during an in-flight query cannot race the next arm's
fixture rebuild.

PR #491 committed the accepted diagnostic evidence and deterministic rebuild
gate. The evaluation closes with that evidence. The formal live runner is no
longer exposed; smoke/diagnostic support and the benchmark package remain as
historical validation assets.

This RFC does not migrate or invalidate AgentBench v0 artifacts.

## Test and acceptance criteria

- attribution manifests reject unequal arms, unknown scenarios, invalid modes,
  incomplete identity, and arbitrary out-of-budget records; the normalized
  first terminal call above the tool limit is retained only as a failed attempt;
- artifacts require exactly one record per scenario/repetition in canonical
  order and preserve wrong versus failed outcomes;
- reports deterministically derive all five required metric groups;
- final-answer inconsistency and reconciliation failures cannot be omitted or
  converted into correctness;
- the live harness gives both arms the same execution/result broker and budget;
- the benchmark-private adapter does not appear in production MCP/REST/service
  packages or the RFC-0051 tool vocabulary;
- smoke covers one additive and one ratio scenario before diagnostic runs;
- accepted diagnostic evidence deterministically rebuilds from committed arm
  artifacts;
- live `benchmark` mode is rejected before executable/provider resolution;
- no paid Agent run occurs in ordinary CI.

## Documentation updates

Implementation and closure update:

```text
tests/benchmarks/agent-attribution/README.md
tests/benchmarks/CONFORMANCE.md
docs/proposals/README.md
```

The production attribution interface is intentionally delegated to a separate
interface RFC.
