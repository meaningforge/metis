# RFC-0071: Governed Agent Metric Aliases and Candidate Differences

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-02
- **Last updated:** 2026-09-03
- **Scope:** Agent-facing metric discovery, explicit ambiguity evidence, governed authored aliases
- **Supersedes:** None

## Decision

Metric discovery is retrieval, not semantic selection. `list_metrics` marks
multi-candidate and truncated results with `selection_required`, reports exact
cross-model name collisions through `ambiguous_names`, and reports the closed
set of authored facts that differ through `selection_differences`. Stable result
ordering never authorizes selecting the first candidate.

Metis does not maintain a built-in business synonym dictionary. Until Apache
Ossie defines a native metric-alias field, an author may declare strong aliases
through a typed Ossie extension:

```json
{"kind":"agent_discovery","aliases":["GMV","merchandise sales"]}
```

The extension uses `vendor_name: METIS`, is validated fail closed, and affects
discovery only. It does not change metric evaluation or physical compilation.
Free-form description and AI context remain weaker authored search evidence and
are not interpreted as a hidden alias schema.

## Rationale

Name similarity and result rank cannot establish business identity. The same
term may refer to a balance, distinct count, period-end value, cumulative value,
or additive total. Returning explicit candidate differences lets an Agent use a
canonical ref or ask for clarification without moving natural-language intent
resolution into Metis.

## Compatibility and migration

The response additions are additive. Agent-facing metric selection now requires
canonical `metric:` refs, matching the existing public schema descriptions.
If Ossie adds an equivalent native alias field, Metis will consume that field as
the source of truth and retire this incubating extension through a separate
migration; the extension must not become a parallel permanent schema.
