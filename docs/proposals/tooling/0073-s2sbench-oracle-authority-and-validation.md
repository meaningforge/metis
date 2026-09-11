# RFC-0073: S2SBench Oracle Authority and Validation Tiers

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Draft
- **Owners:** Metis maintainers
- **Created:** 2026-09-03
- **Last updated:** 2026-09-03
- **Scope:** `tests/s2sbench` workload generation, oracle artifacts, collection manifests, and analysis
- **Supersedes:** None; refines the oracle paragraph of RFC-0072

## Summary

S2SBench MUST make the authority of every result oracle explicit and auditable.
It MUST distinguish a provider-authored reference result on frozen data from an
independently validated semantic oracle and from an official TPC-DS
qualification result. A workload MUST NOT claim a stronger tier than the
evidence it carries.

The proposal adds three oracle profiles:

1. `provider-reference-v1`: reproducible internal regression oracle;
2. `test-suite-equivalence-v1`: provider reference SQL proven against multiple
   independent fixture variants and negative mutations;
3. `tpcds-qualification-v1`: a standard TPC-DS query/parameter instance
   validated against a matching authorized qualification answer set.

The existing `ossie-tpcds-example` workload begins at
`provider-reference-v1`. It is not an official TPC-DS qualification benchmark.

## Motivation

RFC-0072 correctly prevents an Agent or Metis from defining its own answer:
the provider writes inspectable `reference.sql`, executes it on generated data,
and freezes normalized rows as the oracle. Metis compilation must agree during
generation, but Metis is only a cross-check and never the oracle authority.

That is a useful regression contract, but it is insufficient evidence for a
public claim that an answer is a TPC-DS standard answer:

- the current questions are custom questions over metrics and relationships in
  Apache Ossie's five-table TPC-DS example, rather than the standard TPC-DS
  query templates;
- a provider-authored SQL expression can contain a shared misunderstanding;
- one fixture can allow a wrong candidate query to be accidentally equivalent;
- DuckDB's embedded `dsdgen` kit version is recorded, but it is not thereby a
  current TPC qualification kit or an audited run.

The smoke experiment also needs its oracle provenance to be visible to a reader
of a collection/report, rather than requiring them to infer it from a workload
implementation detail.

## Terminology

**Reference SQL** is provider-maintained, inspectable SQL that states the
intended result for one case. It is logic evidence, not an Agent answer.

**Fixture variant** is a separately generated and digest-verified physical
database instance for the same logical workload. A provider may produce
variants with an official generator seed, supported generator parameters, or
another explicitly documented independent generation mechanism. This RFC does
not assume every engine supports a seed flag.

**Independent evaluator** executes reference and candidate SQL without using
Metis compilation to determine expected rows. It may be a separately
implemented evaluator or an authoritative published answer artifact, depending
on the profile.

**Oracle profile** is the versioned statement of evidence required to score a
workload. It is immutable once a workload is sealed.

## Current baseline

The present generated workload contract is retained as
`provider-reference-v1`:

```text
provider reference.sql
        -> frozen DuckDB TPC-DS data
        -> normalized typed rows
        -> oracle

Metis compiled SQL on the same data
        -> must match the oracle during generation
```

It provides deterministic, reproducible internal regression scoring. Exact
numeric normalization, NULL representation, row-comparison policy, asset
SHA-256 values, generator version, reference SQL, and the frozen result remain
part of the evidence.

The profile MUST be reported as `provider-reference-v1`. Its reports MUST say
that it is not a qualification or published TPC benchmark result.

## Proposed oracle profiles

### `provider-reference-v1`

This is the minimum profile for a generated S2SBench workload.

A sealed workload MUST contain:

- provider identity and source revision;
- data-generator identity, version, parameters, and digests;
- one inspectable reference SQL asset per case;
- typed, normalized expected columns and rows;
- explicit row-order and numeric-comparison policy;
- the semantic model asset and its digest when the case is semantic-model
  derived;
- recorded Metis cross-validation status, labeled as a cross-check.

It MUST NOT call itself `standard`, `official`, `qualified`, `audited`, or
`TPC-DS compliant`.

### `test-suite-equivalence-v1`

This profile is for semantic-model questions that have no official published
answer set, including the current Apache Ossie example cases.

In addition to all baseline evidence, every case MUST have:

1. at least three named, independently digest-verified fixture variants;
2. the provider reference SQL result for each variant;
3. a candidate-equivalence procedure that compares a submitted candidate SQL
   against the reference on every variant using the declared comparison policy;
4. an independent evaluator record for every variant; and
5. a mutation suite containing wrong join, wrong metric/expression, wrong
   filter, and wrong grouping/order variants whenever that mutation is
   meaningful for the case.

Each required mutation MUST disagree with the reference in at least one
fixture variant. A surviving mutation is a validation failure: the case MUST
not be sealed at this profile until the fixture set or case definition makes
the difference observable.

Multiple fixtures reduce accidental equivalence; they do not prove SQL
equivalence in the formal sense. The profile therefore remains an
independently validated semantic benchmark, not an official TPC qualification
claim.

### `tpcds-qualification-v1`

This profile applies only when a case maps one-to-one to a supported standard
TPC-DS query template and its parameterization. It is intentionally separate
from custom Ossie metrics and natural-language questions.

In addition to the baseline evidence, a sealed qualification workload MUST
record:

- exact TPC-DS specification, toolkit, query template, substitution parameters,
  scale factor, and generator settings;
- the matching authorized official answer artifact, or a separately identified
  corrected reproduction whose license and provenance permit its use;
- the official comparison behavior needed for the selected artifact, including
  NULL ordering, result ordering, numeric tolerance, and formatting rules;
- evidence that reference SQL was only a dialect adaptation of the standard
  query logic, not a semantic rewrite; and
- any required licensing, disclosure, and audit limitations.

Only a completed official process may make an "audited TPC-DS result" claim.
This profile makes a qualification-answer comparison reproducible; it does not
itself confer audit or publication approval.

## Manifest and report contract

The workload manifest SHOULD replace its free-form oracle authority string
with a versioned, typed `oracle` object conceptually equivalent to:

```json
{
  "profile": "test-suite-equivalence-v1",
  "reference_sql": { "path": "cases/example/reference.sql", "sha256": "..." },
  "comparison": { "rows": "unordered", "numeric": "exact-rational" },
  "fixtures": [
    { "id": "variant-a", "database": { "path": "...", "sha256": "..." } }
  ],
  "independent_evaluator": { "id": "...", "version": "..." },
  "metis_cross_validation": "match"
}
```

The exact Go type names are implementation work, but the following invariants
are normative for this RFC:

- oracle profile and evidence assets participate in the workload digest;
- collection manifests copy the profile and its digest;
- analysis rejects collections whose oracle-profile digest differs;
- reports display profile, fixture count, evaluator identity, and whether the
  result is qualification-scoped;
- Metis cross-validation MUST have a distinct field and MUST NOT satisfy an
  independent-evaluator requirement by itself;
- absent/malformed evidence fails closed at workload load or generation time.

## Case-authoring rules

Each case MUST carry a concise semantic review note that identifies the source
of its fields, metric definitions, relationships, filters, grouping, and
ordering. For the Ossie example, these references point to the generated,
pinned Ossie model and the physical source catalog; they do not elevate model
prose into an oracle.

Reference SQL review is required before a case can move beyond
`provider-reference-v1`. Reviewers MUST be able to reproduce each expected row
from the named data asset and compare it with the stored normalized result.

Agent-generated SQL, any single renderer output, and LLM judgment MUST NOT be
accepted as reference-answer authority.

## Rollout

1. Preserve the existing workload behavior as `provider-reference-v1` and
   expose that profile in manifests and reports.
2. Add typed oracle-profile contracts and migrate the existing free-form
   `oracle_authority` field without accepting an ambiguous compatibility
   synonym.
3. Add fixture-variant generation and mutation validation behind the
   `test-suite-equivalence-v1` provider workflow.
4. Promote custom Ossie semantic cases only after their review, variants, and
   mutation evidence are sealed.
5. Add a separate `tpcds-qualification` provider only after the matching
   licensed toolkit and answer artifacts are available. Do not relabel the
   DuckDB 2.10.0 bundle as that provider.

Collections produced under different profiles are intentionally not comparable.
Historical `provider-reference-v1` evidence remains readable but does not gain
a higher profile retroactively.

## Alternatives

### Treat Metis SQL as a second oracle

Rejected. It detects disagreements, but Metis and a provider reference may
share a semantic error. It also makes the evaluated system partly define the
answer.

### Use only official TPC-DS answers

Rejected. Official answers are valuable for standard templates, but cannot
validate custom Ossie metrics such as `customer_lifetime_value` unless the
question exactly maps to a standard query.

### Keep one frozen fixture indefinitely

Rejected for promoted semantic suites. It is inexpensive but permits accidental
equivalence and does not test data-sensitive semantics.

### Let an LLM review candidate results

Rejected. A probabilistic judgment is neither a reproducible oracle nor an
appropriate authority for benchmark scoring.

## Acceptance criteria

RFC-0073 may become `Implemented` only when:

1. workload, collection, and comparison artifacts expose and digest a typed
   oracle profile;
2. `provider-reference-v1` is the explicit profile of the existing workload,
   and its reports include the non-qualification disclosure;
3. the loader rejects incomplete/mismatched oracle evidence and cross-profile
   analysis inputs;
4. at least one semantic workload implements three fixture variants, an
   independent evaluator, and all applicable negative mutations;
5. tests prove an accidental-equivalence mutation is detected by a variant;
6. no custom semantic workload can be labeled as TPC qualification; and
7. any `tpcds-qualification-v1` provider has automated evidence that it uses
   matching versioned answer artifacts and declared comparison rules.

## Documentation updates

An implementation MUST update:

- `tests/benchmarks/s2sbench/README.md` with the current oracle-profile
  behavior and operator-facing disclosure;
- the S2SBench workload/collection package documentation with the concrete
  manifest schema;
- any current testing specification that becomes the normative home for the
  implemented profile contract; and
- RFC-0072's historical oracle paragraph only where a current-document link or
  supersession note is needed.
