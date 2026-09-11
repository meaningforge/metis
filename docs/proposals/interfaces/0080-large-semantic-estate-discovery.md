# RFC-0080: Large Semantic Estate Discovery

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-03
- **Last updated:** 2026-09-08
- **Scope:** deterministic indexed discovery, pagination consistency, authorization-safe search, and scale budgets
- **Supersedes:** None

## Summary

Scale existing deterministic semantic discovery to large projects and many
projects through immutable generation-scoped indexes, bounded pagination, and
reproducible performance evidence. Existing retrieval ranking remains
non-authoritative and semantic resolution remains unchanged.

## Motivation

Current bounded discovery is correct for small estates, but repeated scans and
response assembly can become expensive at tens of thousands of models, metrics,
dimensions, and relationships. Scale work must not introduce an embedding
authority, leak hidden assets, or let reloads corrupt pagination.

## Design

Indexes are derived solely from one immutable SemanticManifest generation and
contain canonical identities, normalized authored terms, typed constraints,
compatibility edges, and governance-filter keys. They are disposable read
models, never semantic authority.

Search preserves current deterministic matching tiers and canonical-ref tie
breaking. It does not infer synonyms, interpret natural language, or select a
metric. External full-text services and vector databases are not required for
the first implementation.

Authorization and [asset visibility](../../specs/semantic/asset-governance.md) filters apply before scoring,
pagination, counts, truncation, and cursor creation. Cursors bind to project,
query shape, visible policy scope, and semantic generation. A cursor from a
different generation fails with a stable restart-pagination action rather than
silently skipping or duplicating assets.

The repository gains a deterministic synthetic estate generator and benchmarks
for 10k, 50k, and 100k assets. Acceptance budgets cover index build time,
resident bytes per asset, allocations per request, result latency, and encoded
response size. Numeric release gates are frozen from a named reference host
before implementation begins; timing gates run outside flaky unit-test paths,
while allocation and result-correctness gates remain in CI.

This RFC does not add cross-project analytical queries, federation, fuzzy
semantic inference, an external catalog authority, or unbounded result dumps.

## Frozen budgets and baseline

Budgets were frozen on an Apple M1 with 16 GiB RAM, Darwin arm64, and Go
1.26.1 before the indexed request path replaced the reference scan. The
deterministic generator creates ten assets per model: one model, one dataset,
four dimensions, and four metrics.

| Assets | Reference `list_models` | Reference `list_metrics` |
| ---: | ---: | ---: |
| 10,000 | 6.76 ms | 7.56 ms |
| 50,000 | 32.1 ms | 36.8 ms |
| 100,000 | 67.7 ms | 75.5 ms |

The frozen 100k acceptance budgets are: index build at most 1.5 seconds,
estimated retained index size at most 768 bytes per asset, focused indexed
request latency at most 1 millisecond, at most 64 allocations for
`list_models`, at most 160 allocations for `list_metrics`, and at most 2 KiB
for the encoded list response. Timing is recorded manually with
`BenchmarkLargeSemanticEstateDiscovery`; deterministic result equivalence,
allocation ceilings at 10k, retained-size estimation, and response size remain
in normal tests. The implementation measured a 1.17-second 100k build,
approximately 676 retained bytes per asset, and steady focused retrieval in
the 7–16 microsecond range on the reference host.

The in-process design follows Cube's useful boundary—compile a navigable
metadata catalog from the current model and apply visibility before exposing
it—without copying Cube's API vocabulary or introducing a second catalog
authority. Metis keeps Project-local generations and business-domain actions.

The implementation audit used Cube's [Views and view
folders](https://docs.cube.dev/docs/data-modeling/views) as evidence for a
compiled, navigable metadata projection and its [multi-tenant configuration
recipe](https://docs.cube.dev/recipes/configuration/multiple-sources-same-schema)
as evidence that caller context and cached metadata boundaries must align.
Metis does not copy Cube's transport-oriented API scopes, metadata response
shape, or tenant routing: Project resolution remains separate from
authorization, indexes and cursors remain Project-local, and the public tools
retain Metis's narrow domain DTOs.

## Alternatives

An external search cluster is deferred because it adds operational and
consistency authority before in-process limits are measured. Embedding search
is rejected as the default because it is probabilistic and cannot become
semantic selection authority. Increasing response limits is rejected because
it worsens Agent token consumption.

## Rollout and migration

1. Add the synthetic corpus and record the current baseline.
2. Freeze measurable budgets and result-equivalence fixtures.
3. Build immutable indexes without changing public results.
4. Introduce generation-bound cursors with explicit compatibility behavior.
5. Optimize response assembly and governance filtering.

All rollout steps are implemented. The discovery index is built before a
runtime generation is published and is rebuilt only for a different immutable
manifest.

## Test and acceptance criteria

- Indexed and reference implementations return identical ordered results.
- Repeated builds from identical content produce equivalent ordered results,
  pagination semantics, and cursor binding. Opaque cursor bytes may differ due
  to signing, expiry, or key rotation.
- Hidden assets cannot influence counts, rank, truncation, or cursor state.
- Old-generation cursors fail explicitly after activation.
- Search never changes resolver or compiler authority.
- 10k/50k/100k benchmark evidence meets frozen time, memory, allocation, and
  response-size budgets without raising discovery limits.

## Documentation updates

Implementation must update Agent semantic discovery, runtime observability,
testing architecture, and operational capacity guidance.
