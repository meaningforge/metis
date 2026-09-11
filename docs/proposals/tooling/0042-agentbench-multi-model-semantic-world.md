# RFC-0042: AgentBench multi-model semantic world

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-25
- **Last updated:** 2026-08-25
- **Scope:** `tests/agentbench`, conformance fixtures, AgentBench prompt protocol
- **Supersedes:** RFC-0041's original per-scenario semantic-handler lifecycle

## Summary

Make AgentBench test model discovery and routing instead of doing that work for
the Agent. Both arms receive the same complete canonical multi-model semantic
project. Path A receives it as raw read-only Ossie files; Path B receives it
through one immutable production Metis MCP runtime. The prompt supplies only
the project namespace and never the current scenario's model.

The semantic world is global and loaded once. Only DuckDB data and the expected
result remain scenario-scoped.

## Motivation

The earlier live harness exposed only the semantic model associated with the
current fixture and supplied that model name in the Metis prompt. This
pre-routed every question and removed semantic discovery, ambiguity resolution,
and model selection from the system under test. It also made both arms unlike a
real project containing multiple plausible metrics and dimensions.

Metis's Agent-facing value includes deterministic discovery and context within
a project, not merely compilation after a model has already been selected.
Selecting the wrong model or failing to resolve ambiguity must therefore be a
scored outcome.

## Design

### Canonical semantic-model registry

The conformance fixture registry derives a deterministic list unique by
project/model. Multiple physical fixture IDs may share one semantic model. If
two entries claim the same project/model but contain different documents, the
benchmark fails before collection rather than choosing one.

### Path A: complete raw project

The raw-arm session workspace contains a read-only `models/` directory with
every canonical unique Ossie document. The directory is materialized once;
`schema.sql` is refreshed for the current DuckDB fixture before each question.
It contains no MCP configuration or bearer credential. The Agent must select
and interpret the relevant model before authoring SQL. RFC-0046 supersedes the
earlier fresh-workspace-per-invocation lifecycle.

### Path B: immutable Metis project

At run startup the harness writes one `metis.yaml` referencing every canonical
model and one DuckDB execution binding. It invokes
`bootstrap.LoadProjectRuntime` once and gives its Discovery and Compile services
to one production MCP handler. One listener, endpoint, project, and bearer token
remain unchanged until collection ends.

The prompt contains the business question and project only. The Agent must use
Metis search/context tools to choose the model and return the compile result's
physical query. The harness does not infer or stamp a model from the scenario.
The bearer token is passed only in Path B child-process environment overrides,
so the concurrently running server is not an accessible semantic interface for
Path A.

### Independent physical lifecycle

Before each scenario, DuckDB drops and recreates the physical schema and seeds
that scenario's fixture. This remains scenario-scoped because different fixture
IDs may exercise different data against the same semantic model. It does not
cause any semantic runtime reload.

## Protocol and artifact impact

The frozen prompt protocol advances to `v0.7`. Existing artifacts retain their
historical prompt version and are not comparable with v0.7 collections. The
manifest schema did not change for this protocol correction because
project/model selection remains visible in semantic query, SQL, and incremental
attempt evidence. RFC-0043 later compacts those artifacts and advances their
schema independently.

## Alternatives

**Expose only the scenario model.** Rejected because it turns the experiment
into pre-routed compilation and hides cross-model ambiguity.

**Load all models but include `model:` in the prompt.** Rejected for the same
reason; availability without required discovery does not test routing.

**Reload the full runtime for every scenario.** Rejected because the semantic
world is immutable and scenario preparation owns only physical data. Reloading
adds latency and lifecycle failure modes without changing experiment input.

## Test and acceptance criteria

- Canonical models are sorted and unique by project/model; conflicting shared
  definitions fail closed.
- Path A receives all canonical model documents and current `schema.sql`, with
  neither MCP endpoint nor token.
- Path B receives an empty workspace, one MCP endpoint, request-scoped token,
  and a prompt containing project but no model hint.
- Production bootstrap successfully loads the complete project once.
- The MCP endpoint and semantic model set stay unchanged across questions.
- Scenario preparation changes only DuckDB physical data.
- Client-level adapters also require the Agent-produced semantic query to select
  a model; the harness never stamps the scenario model.
