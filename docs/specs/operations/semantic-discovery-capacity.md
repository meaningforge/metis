# Semantic discovery capacity

This specification defines the supported in-process capacity evidence for
deterministic semantic discovery.

Each Project generation builds its own immutable index before publication.
Memory therefore scales with the sum of simultaneously active Project
generations, including an old generation still pinned by an in-flight request
and a candidate being activated. Operators should provision for at least two
generations of the largest Project during activation. There is no global
generation or cross-Project index.

`BenchmarkLargeSemanticEstateDiscovery` produces deterministic 10k, 50k, and
100k estates and reports build time, estimated retained bytes per asset,
focused `list_models` and `list_metrics` latency, allocations, and candidate
count. The frozen Apple M1 / 16 GiB / Darwin arm64 / Go 1.26.1 100k budgets are:

| Measure | Budget |
| --- | ---: |
| index build | 1.5 s |
| retained index estimate | 768 B/asset |
| focused indexed request | 1 ms |
| focused `list_models` allocations | 64/request |
| focused `list_metrics` allocations | 160/request |
| encoded list response | 2 KiB |

Timing evidence is a manual release check because shared CI runners are noisy.
Normal tests enforce deterministic ordered equivalence, the allocation and
retained-size ceilings at 10k, response size, generation-bound cursor failure,
and visibility-safe pagination. Limits remain 50 list items and 100 semantic
search or dimension items; capacity work does not authorize unbounded dumps.

Activation failure during index construction leaves the current generation
unchanged. The existing activation duration and closed result observation cover
the build; no asset identity, query text, Project ID, principal, or policy tag
is added as a metric label.
