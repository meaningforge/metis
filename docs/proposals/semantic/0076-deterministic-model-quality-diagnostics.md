# RFC-0076: Deterministic Model Quality Diagnostics

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-03
- **Last updated:** 2026-09-08
- **Scope:** deterministic diagnostics for authored Ossie semantic model quality
- **Supersedes:** None

## Summary

Add structured, deterministic model-quality diagnostics to the shared model
validation path. Diagnostics help authors find duplicate definitions,
ambiguous discovery identities, missing structured governance evidence, and
risky graph shapes during source validation, while leaving legitimate business
choices reviewable.

## Implementation status

Implemented in the shared `app/service/source` candidate path with a stable
rule registry, deterministic project report, multi-document attribution,
typed `quality_claims` evidence, project severity overrides, and a publication
threshold reported to the caller as eligibility evidence. Core does not perform
publication or approval. Offline CLI output and the shared service use the same result.

Lifecycle/replacement-specific findings intentionally await the typed
governance fields owned by [Asset visibility contract](../../specs/semantic/asset-governance.md). RFC-0076 does not infer those fields from
prose or introduce an overlapping governance extension; its registry can be
extended when that typed authority lands.

## Motivation

Schema-valid models can still be difficult for people and Agents to use. Recent
evaluation exposed two metrics with definition-equivalent behavior but names
that implied different grouping requirements. Runtime correctly refused to
invent that distinction, but the author received no early warning.

This is a model quality problem, not a reason to modify Agent prompts or make
the resolver guess intent.

## Design

Diagnostics run after successful Ossie loading and project assembly. Each item
contains a stable code, severity (`error`, `warning`, or `info`), canonical
references, source locations when available, bounded evidence, and a caller
action. Results sort by severity, code, canonical reference, and location.

The initial rule set covers:

- exact definition-equivalent metrics under different canonical identities;
- duplicate or conflicting governed aliases;
- identity collisions that make discovery selection ambiguous;
- contradictions between typed semantic fields and explicitly structured
  claims in a governed Ossie field or typed extension;
- unreachable dimensions, unresolved relationships, and disconnected assets
  not already rejected by structural validation;
- deprecated references without an explicit replacement;
- inconsistent type, grain, aggregation, or expression evidence.

Exact-definition equivalence is a warning by default. Two names may represent
valid business identities even when their current formulas match. Metis reports
the evidence and never merges, renames, or selects one automatically.

Rules use typed Ossie fields, parsed expressions, canonical graph evidence, and
typed Metis extensions only. An explicitly structured claim such as an authored
`expected_grain` may be compared with a typed grain. Free-form descriptions and
unstructured AI context are display and retrieval evidence only: diagnostics
MUST NOT tokenize, pattern-match, or interpret their prose to infer a semantic
claim or contradiction. Rules do not query warehouse values or call an LLM.

Quality policy maps stable diagnostic codes to project-specific severity and
publication thresholds. The default policy blocks existing structural errors
but reports advisory quality findings as warnings. Policy changes do not change
the underlying diagnostic evidence.

The same result is available through the authoring lifecycle in [Semantic source contract](../../specs/semantic/asset-authoring-lifecycle.md) and
offline validation. Primary Agent MCP discovery may surface a bounded existing
warning on selected asset detail, but no new quality-scanning Agent tool is
introduced.

## Alternatives

Embedding more selection guidance in the Agent prompt is rejected because it
hides model defects and biases evaluations. Automatically rejecting all
equivalent definitions is rejected because equivalence does not prove duplicate
business identity. LLM review may be offered externally, but is not accepted as
the deterministic validation authority.

## Rollout and migration

1. Define the diagnostic result and registry of stable codes.
2. Implement equivalence, alias, identity, and lifecycle rules as warnings.
3. Add configurable publication thresholds without changing load behavior.
4. Add remaining graph and structured-claim consistency rules with fixtures.

No existing model becomes unloadable solely because a new advisory rule lands.
A rule may become blocking only through an explicit compatibility decision.

## Test and acceptance criteria

- Identical project content yields byte-stable ordered diagnostics.
- The known definition-equivalent metric fixture produces a focused warning.
- Legitimate equivalent metrics remain loadable by default.
- Alias and canonical-ref collisions include every conflicting reference.
- Free-form prose is never parsed for aggregation, grouping, grain,
  relationship, filter semantics, or contradiction diagnostics.
- Multi-document locations and opaque extensions remain attributable.
- CLI and shared service validation return equivalent structured results.

## Documentation updates

Implementation must add `docs/specs/semantic/model-quality-diagnostics.md` and
update the authoring lifecycle, Agent semantic discovery, and caller-action
error specifications.
