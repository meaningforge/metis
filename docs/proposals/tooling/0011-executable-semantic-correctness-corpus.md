# RFC-0011: Executable Semantic Correctness Corpus

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-14
- **Last updated:** 2026-08-14
- **Scope:** `tests/conformance/`, `tests/engine/`, `tests/benchmarks/`, CI correctness gates
- **Supersedes:** None

## Summary

Define one canonical semantic-query corpus that carries its fixture, semantic
capabilities, compiler obligation, result-verification status, and engine-neutral
expected result. Compiler and real-engine gates must consume that registry, and
the human-readable coverage report must be generated from it.

## Motivation

Metis already has broad compiler tests and real ClickHouse execution tests, but
the evidence is split across shared scenarios, compiler-only expectation maps,
backend-local result maps, specialized engine tests, and hand-maintained
coverage documents. This makes “implemented”, “compiled”, and “proved by real
results” easy to confuse.

Metis's long-term contract is deterministic semantic correctness across
analytical engines. The test architecture therefore needs to prove the same
semantic intent at progressively stronger layers without moving query execution
into the production runtime.

## Design

Each shared scenario declares:

- a stable name and semantic category;
- required engine-neutral semantic capabilities;
- the canonical Ossie fixture;
- the semantic query;
- the common target-neutral physical-query shape expectation;
- whether compiler and result contracts are required or pending;
- an engine-neutral expected result when result verification is required;
- an explicit reason for every pending result contract.

Membership in the registry does not silently imply result-level correctness.
The compiler contract, renderer-specific tests, and real-engine result contract
remain separate gates over the same semantic definition.

The generated corpus report is descriptive evidence, not another source of
truth. CI fails when the checked-in report differs from the registry.

Production Metis remains compile-only. Database setup, connection, execution,
and result normalization exist only in test backends under `tests/engine/`.

## Alternatives

### Keep backend-local expectation maps

Rejected because each backend can drift into a separate semantic inventory and
new scenarios do not create a visible shared obligation.

### Infer support from passing compiler tests

Rejected because successful SQL generation does not prove real result
correctness, and implementation presence does not communicate evidence depth.

### Hand-maintain a coverage matrix

Rejected for internal corpus status because it inevitably drifts. External
benchmark mappings may remain curated, but must link to generated internal
evidence depth.

## Implementation status

The canonical scenario contract now carries fixture identity, compiler and
result verification status, target-neutral SQL-shape expectations, and expected
rows. The generated coverage report is checked in CI. All specialized
ClickHouse semantic cases have been migrated into the shared registry, and the
ClickHouse backend contains only physical setup, compilation/execution
plumbing, and result normalization.

Pending status is allowed during migration only when it carries an explicit
reason. It must not be interpreted as an unsupported compiler capability.

The result contract now carries logical column names, engine-neutral value
kinds, normalized typed values, and an explicit ordered/unordered comparison
mode. Backend adapters must read physical result metadata and normalize it into
that contract; they cannot reinterpret every value as a string. ClickHouse and
Doris both execute the same result-required corpus through the shared harness;
neither backend owns a semantic scenario or expectation inventory.

## Test and acceptance criteria

- Every canonical scenario declares a known fixture.
- Every canonical scenario has a required compiler contract.
- A required result contract always has a canonical expectation.
- A pending result contract always has an explicit reason and no expectation.
- Real-engine backends cannot own a separate shared expectation map.
- The generated report is deterministic and CI rejects stale output.
- Adding a result-required scenario automatically adds an obligation to every
  backend that claims its capabilities.

The implementation acceptance condition is met: specialized backend semantic
inventories are empty, the canonical corpus has no pending or unclassified
scenario, and both ClickHouse and Doris prove every result-required scenario.

## Documentation updates

- Update `docs/specs/testing/architecture.md` with the canonical contract and
  generated-report rule.
- Link the generated evidence report from benchmark matrices.
- Clarify that real-engine execution is required test infrastructure, not a
  production Metis responsibility.
