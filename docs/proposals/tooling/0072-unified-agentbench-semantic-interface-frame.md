# RFC-0072: Unified AgentBench Semantic-Interface Frame

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-02
- **Last updated:** 2026-09-02
- **Scope:** `tests/agentbench`, live AgentBench artifacts and operator CLI
- **Supersedes:** RFC-0068 experiment orchestration and the AgentBench v0 comparison frame

> **Historical naming:** this RFC was authored while the harness was named
> AgentBench. RFC-0074 renamed the active harness and current paths/commands to
> S2SBench and `tests/s2sbench`. The legacy terms below describe the accepted
> migration history; they are not current developer instructions.

## Decision

AgentBench uses one single-arm collection and two-input analysis framework for
two Agent-facing interfaces:

1. `okf`: Agent reads an audited OKF bundle generated from physical source
   catalog metadata and authors SQL;
2. `metis-mcp`: Agent uses the production Metis MCP surface and returns the
   compiled physical query.

The framework remains entirely under `tests/agentbench`. It is not part of the
offline `metis` CLI or Metis production runtime.

## Frame

A run selects exactly one built-in suite or generated workload bundle, one
registered arm, and optionally an ordered subset of suite scenarios. The
manifest freezes Agent/model identity,
prompt and projection versions, budgets, scenario identity/order, statistical
settings, and the audited fact-ledger digest.

All arms use the same physical DuckDB schema, fixture lifecycle, normalized
result oracle, question numbering, prompt envelope, tool budget, timeout, and
repair policy. An arm adapter controls only the semantic interface exposed to
the Agent. The Metis adapter reuses the production MCP/bootstrap/execution
runtime already used by AgentBench; the OKF adapter materializes only its
read-only catalog-derived knowledge bundle.

## CLI

The canonical operator surface is:

```text
agentbench run --suite <suite> --arm <arm> ... --output <dir>
agentbench gen --provider <provider> --scale <scale> [--output <dir>]
agentbench run --suite-path <generated-dir> --arm <arm> ... --output <dir>
agentbench analyze --input <okf-dir> --input <metis-mcp-dir>
```

`gen` defaults its output to `.workload` in the current working directory and
refuses to overwrite an existing bundle. An explicit `--output` selects a
different new directory.

Every run is single-arm. Interrupted runs resume from a strict, fsynced JSONL
journal with `--resume`; completed question batches are not rerun. Analysis
accepts exactly the OKF and Metis MCP arms, rejects frame or ledger mismatches, and
aligns outcomes horizontally by one-based question number while verifying the
complete scenario/question/stratum identity.

The earlier `readiness-*` commands are removed rather than retained as aliases.

The CLI root and every exposed subcommand use Cobra-native flag declaration,
validation, help, and completion; there is no secondary standard-library
`flag.FlagSet` parser or disabled Cobra parsing path. Generation is a
first-class subcommand. Generated
workloads are immutable, digest-verified bundles containing canonical semantic
assets, a physical source-catalog snapshot, catalog-derived OKF v0.2 concepts
and indexes, structured queries, natural-language questions, CSV and Parquet data,
a ready DuckDB database, a portable DuckDB loader, provider-owned reference
SQL, and frozen normalized oracles. The first registered provider uses a pinned
Apache Ossie TPC-DS example as a semantic enrichment template. It creates the complete 24-table TPC-DS schema and
data through DuckDB's `tpcds` `dsdgen` implementation (TPC-DS kit 2.10.0 in
the pinned dependency); it does not create a
parallel hand-written or random "TPC-DS-like" fixture.

For each semantic question the provider's checked-in generation logic writes
an inspectable `reference.sql`. The oracle is the normalized result of that SQL
on the generated TPC-DS data. Metis compilation is an independent cross-check:
generation fails unless Metis SQL returns the same typed values under the
shared numerical tolerance. Metis output never defines its own oracle.

This is deliberately not described as an official TPC benchmark result or as
qualification against the current TPC-DS release. TPC-DS
qualification answer sets apply to the standard query templates and
qualification parameters, whereas these questions exercise metrics defined by
the Apache Ossie example. The bundle records both that distinction and the
exact DuckDB/TPC-DS extension version. OKF generation follows the source adapter
shape of Google's reference generator: enumerate physical assets, read catalog
metadata, emit one concept per dataset/table, and regenerate progressive indexes.
It is deterministic and invokes no LLM; Ossie is not an OKF projection input.
The physical database and exported CSV/Parquet assets retain all 24 standard
tables. Experiment materials are scoped to the 5 tables named by the pinned
Apache Ossie example. Independently, the provider generates
`models/ossie-tpcds-example.ossie.yaml` from those scoped physical tables and all their
columns, overlays only template semantics whose physical references resolve,
and validates the output through the Ossie loader and catalog binding checks.
The OKF arm receives the same 5-table catalog scope. The Metis MCP arm loads
the generated Ossie asset; it is never converted into OKF.
Provider-specific generation and reference SQL remain behind the workload provider boundary; single-arm
collection, representation projection, resume, and analysis operate only on
the generic bundle contract.

## Compatibility

Earlier RFC-0068 and AgentBench v0 result artifacts are removed and are not
accepted by the v2 frame. The existing
drivers, MCP runtime, execution fixture, scoring, and incremental journal are
reused; only experiment orchestration and artifact shape change.
