# RFC-0066: dbt-aligned semantic discovery

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-01
- **Last updated:** 2026-09-01
- **Scope:** Primary Agent semantic discovery surface and AgentBench evidence
- **Supersedes:** Dimension-tool naming in RFC-0051

## Summary

Restore `get_dimensions` as the Agent-facing compatibility-discovery tool,
retain `get_dimension` for exact selected-object detail, and adopt bounded
progressive-disclosure lessons demonstrated by dbt MCP without copying dbt's
MetricFlow-specific wire format.

Improve deterministic discovery by ranking exact and multi-term matches ahead
of partial descriptive matches, indexing custom-calendar canonical grain names
and physical-bucket aliases, trimming detail from unconstrained metric catalog
listings, and retaining bounded redacted MCP arguments in benchmark tool traces.

## External evidence

The dbt MCP Semantic Layer surface at upstream commit
`d16db88e4e2c60d44ab0aaa5fcd942c6e5248aeb` exposes:

```text
list_metrics(search?: string | string[], meta_filter?: object)
get_dimensions(metrics: string[], search?: string)
get_dimension_values(dimension, metrics?, ...)
```

Its implementation and contract tests demonstrate several mature design
choices relevant to Metis:

- `get_dimensions` means dimensions compatible with selected metrics;
- broad metric listings progressively omit expensive columns;
- narrow metric results retain richer descriptions and related dimensions;
- multi-term metric search is bounded, ORed, and deduplicated;
- distinct values remain a separate execution-backed tool.

Metis adopts the naming and progressive response shape. It does not adopt CSV
responses, generic metadata filters, or `get_dimension_values`: typed canonical
refs and complete `canonical_groupings[].group_by` objects are required for
safe Metis compilation, model narrowing has explicit semantics, and distinct
values require a separately governed execution contract.

## Contract

`get_dimensions` accepts exactly one anchor:

- one or more compatible canonical metric refs; or
- one canonical model ref for metric-free discovery.

It returns compact typed summaries. `get_dimension(ref)` remains optional and
returns the selected dimension's authored detail. No compatibility alias named
`list_dimensions` is registered.

Search remains deterministic and case-insensitive. Identifier separators such
as `_`, `-`, and `.` normalize to token boundaries. Results preserve OR
discovery, but exact canonical/name matches rank above substring matches,
primary identity matches rank above description/AI-context matches, and items
matching all supplied terms receive a deterministic bonus. Ties sort by
canonical ref. Model ranking awards the all-term bonus only when the terms
co-occur in one contained semantic asset; it does not assemble relevance from
unrelated fields. At most 20 distinct non-empty search terms are accepted.

Custom-calendar dimensions are searchable by logical grain, canonical group-by
ref/type, and physical bucket. A search for `fiscal_week` therefore ranks the
canonical base-time dimension carrying the `fiscal_week` grouping ahead of a
plain physical bucket field.

An unconstrained `list_metrics` call remains valid. It behaves as a broad
identity index and omits descriptions and dimension hints. Focused search or
model scope retains richer summaries subject to the existing encoded-size
budget. If identities alone exceed that budget, the lowest-ranked tail is
removed and `truncated=true` directs narrower retrieval. This reduces context
without prescribing Agent call order. Unsearched multi-model results interleave
models before trimming so one model cannot consume the entire response budget.

AgentBench `tool_trace[].arguments` stores only bounded JSON argument summaries
for Metis MCP calls. Sensitive-key values are redacted and summaries over 4 KiB
collapse to size-only evidence. Native Agent tool inputs such as shell commands
are not persisted in this field.

## Acceptance criteria

- MCP exposes `get_dimensions` and `get_dimension`, not `list_dimensions`.
- compile remediation names `get_dimensions`.
- `fiscal_week` finds and ranks its canonical custom-calendar base dimension.
- multi-term metric search ranks the most specific metric first.
- unconstrained metric listings report omitted detail.
- successful Metis MCP traces preserve bounded redacted arguments.
- service, MCP, observability, conformance, and AgentBench tests pass.
