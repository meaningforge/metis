# RFC-0019: Agent Semantic Discovery and Context Quality

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** `catalog`, `app/service/semantic`, REST/MCP discovery/context interfaces, Agent-facing conformance
- **Supersedes:** None

## Summary

Metis will strengthen the deterministic discovery and bounded semantic-context layer used by Agents before they construct `query.SemanticQuery`.

Phase F does not add natural-language interpretation or a second semantic resolver. It improves how an Agent finds relevant semantic assets, understands why they matched, determines whether candidate dimensions are compatible with one or more metrics, and obtains bounded evidence suitable for constructing or repairing a typed semantic query.

The intended workflow remains:

```text
Agent intent
  -> SearchSemantics
  -> bounded discovery evidence
  -> SemanticContext
  -> multi-metric compatibility evidence
  -> SemanticQuery
  -> Validate / Explain / Compile
```

All compatibility and relationship claims MUST reuse the same catalog relationship truth already used by semantic planning. Search relevance may rank candidates, but ranking MUST NOT silently select semantic meaning or bypass validation.

## Goals

Phase F establishes the following guarantees:

1. semantic search results are deterministic, bounded, and explain why a candidate matched;
2. ranking prefers stronger semantic identity matches over weak description matches without introducing nondeterministic external dependencies;
3. semantic context remains explicitly focused and bounded rather than returning the full model by default;
4. Agents can ask whether dimensions are compatible with a set of metrics, not only one metric at a time;
5. compatibility distinguishes `compatible`, `unreachable`, and `ambiguous` using the existing relationship/path resolver;
6. discovery evidence can be reused by structured diagnostics without creating another search or compatibility engine;
7. REST and MCP expose the same transport-neutral service contracts;
8. conformance tests freeze ordering, boundedness, evidence shape, and cross-service semantic consistency.

## Discovery contract

`SearchSemantics` remains lexical and deterministic in Phase F. Embeddings, vector databases, hosted rerankers, and LLM-based ranking are not required core dependencies.

Search ranking SHOULD consider stable evidence categories such as:

```text
exact qualified identity
exact name
name prefix / token match
ontology mapping
semantic description match
```

The precise scoring implementation is internal, but result ordering for the same catalog snapshot and request MUST be deterministic.

Each returned match SHOULD expose bounded structured evidence describing why it matched. Evidence is explanatory only; it is not semantic resolution.

A result MAY contain fields such as:

```text
match_reasons[]
  kind
  value?
```

Reason kinds MUST be stable enough for Agent consumption and tests. Raw internal scoring implementation details need not become public API.

## Bounded semantic context

`SemanticContext` continues to require explicit focus through one or more metric or dimension identities. Metis MUST NOT return an unbounded full-model dump as the default Agent context.

Context may include:

- metric identity, type, dependencies, source datasets, and semantic constraints;
- dimension identity, dataset, type, and compatibility evidence;
- only relationships required to explain the requested compatibility evidence;
- bounded compatible-dimension pages with deterministic cursors.

Existing RFC-0016 semantic-context guarantees remain authoritative and are strengthened rather than replaced.

## Multi-metric compatibility

Agent queries commonly contain multiple metrics. Phase F therefore defines compatibility over a metric set.

For a candidate dimension and requested metric set, Metis MUST preserve evidence for each metric and provide a deterministic aggregate status suitable for query construction.

The aggregate contract is fail-closed:

```text
all metrics compatible       -> compatible
any metric ambiguous         -> ambiguous
otherwise any unreachable    -> unreachable
```

Per-metric evidence remains available so an Agent can explain or repair the request. Aggregate compatibility MUST reuse existing metric source and relationship-path resolution; Phase F MUST NOT implement a second graph resolver.

## Discovery and diagnostics reuse

Structured repair suggestions introduced by RFC-0018 may reuse the shared discovery read model and ranking evidence. They MUST remain bounded and advisory.

Discovery ranking MUST NOT cause Metis to:

- auto-correct a metric or dimension;
- choose between ambiguous identities;
- choose a relationship path on behalf of the query resolver;
- mutate `SemanticQuery`;
- treat a high search score as proof of semantic validity.

## Transport parity

REST and MCP adapters MUST call the same `DiscoveryService` and `SemanticContextService` contracts. Transport-specific ranking, filtering, compatibility, or evidence interpretation is forbidden.

## Non-goals

Phase F does not add:

- NL2SQL or natural-language understanding inside Metis Core;
- embeddings or vector search as a required dependency;
- database execution or result analysis;
- authorization-aware ranking beyond existing project/model isolation;
- automatic typo correction or semantic selection;
- a new relationship graph or compatibility engine;
- raw SQL discovery or schema crawling from physical databases;
- additional SQL dialects merely to satisfy Phase F.

## Acceptance

RFC-0019 is complete when:

- deterministic search evidence and ranking behavior are specified and tested;
- bounded context behavior remains stable and deterministic;
- multi-metric dimension compatibility exists with per-metric evidence and fail-closed aggregate status;
- discovery/context service DTOs remain transport-neutral across REST and MCP;
- RFC-0018 diagnostics reuse shared discovery evidence where applicable;
- Agent discovery/context conformance covers ranking, boundedness, ambiguity, unreachable dimensions, and multi-metric queries;
- existing semantic, optimizer, canonical, Doris, and ClickHouse correctness gates remain green.
