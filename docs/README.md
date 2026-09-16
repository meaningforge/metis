# Metis Core Documentation

Metis is an open-source, Ossie-first, engine-neutral semantic layer runtime
engine. Agents reason about analytical intent; Metis resolves its meaning,
compiles SQL, and optionally executes bounded semantic queries through database
drivers. Start with the [project quickstart](../README.md).

## Reading paths

- **Understand the engine:** [architecture overview](design/architecture.md),
  [compilation pipeline](specs/semantic/compilation-pipeline.md), and
  [glossary](specs/glossary.md).
- **Build an integration:** [public contract](specs/public-contract.md),
  [MCP transports](specs/interfaces/mcp-transports.md), and
  [runtime bootstrap](specs/operations/runtime-bootstrap.md).
- **Author semantic models:** [source authoring](specs/semantic/asset-authoring-lifecycle.md),
  [quality diagnostics](specs/semantic/model-quality-diagnostics.md), and
  [extensions](design/semantic/extensions.md).
- **Embed or extend Metis:** [runtime generations](specs/operations/semantic-runtime-activation.md),
  [data access policy](specs/operations/data-access-policy.md), and
  [Renderer/Driver extension authoring](specs/sql/extension-authoring.md).
- **Evaluate changes:** [testing architecture](specs/testing/architecture.md),
  [S2SBench CLI](specs/testing/s2sbench-cli.md), and
  [S2SBench design](design/tooling/s2sbench.md).

## Documentation roles

| Directory | Role |
| --- | --- |
| [specs](specs/README.md) | Current inputs, outputs, invariants, and testable behavior |
| [design](design/README.md) | How the implementation fits together |
| [decisions](decisions/README.md) | Decisions, alternatives, and consequences |
| [proposals](proposals/README.md) | RFC discussion and retained design history |
| [research](research/README.md) | Non-normative research notes |

Current specifications and public code contracts describe supported behavior.
An RFC's original **Implemented** status records that design's implementation
history; it does not make all historical examples current APIs. **Draft** and
**Superseded** records are explicitly non-normative. Update current design and
specifications when an accepted proposal changes behavior.

## Reading historical designs

These Core designs were curated for this repository on 2026-09-11. Original
RFC and ADR identifiers are retained so the reasoning remains traceable.
Records preserve historical alternatives and state their original status.
Platform-oriented proposals are excluded; current specifications describe the
embedding interfaces implemented by this repository.

Several names changed during development:

| Historical term | Current location or contract |
| --- | --- |
| `physical`, `renderer/sqlquery` | `renderer/sql`: SQL text, dialect, and separate parameters |
| `Catalog` | `manifest.SemanticManifest` and its derived `SemanticGraph` |
| `CompileTarget`, semantic `Engine`, `ExecutionBinding` | Explicit compile dialect; runtime Project → semantic model → applied DataSource → Backend |
| `AgentBench`, `tests/agentbench`, `tests/s2sbench` | `cmd/s2sbench`; black-box coverage under `tests/benchmarks` |
| SQL parameter materialization | Preserve SQL and parameters separately; database drivers bind values |

Use the current CLI help and specifications for commands. Historical pseudo-code,
benchmarks, migration checklists, and comparison tables explain decisions rather
than promise current capabilities. New documentation should link to repository
code and public sources, and must not depend on unavailable internal references.
