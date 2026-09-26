# Semantic Source Authoring

[`app/service/source`](../../../app/service/source/) owns deterministic project
loading, validation, quality diagnostics, formatting, and semantic comparison.
The same loading path is used by bootstrap and the offline `metis` tools.

## Source authority

Apache Ossie documents are the authored semantic authority. A Project manifest
contains ordered source declarations; file matches are expanded deterministically
and each source declaration must resolve to input. `LoadProject` loads a project
manifest and its documents. `LoadProjectDocuments` accepts already acquired
source documents through the same assembly boundary.

A `ProjectSource` contains the merged document, project manifest, source bundle,
content digest, and quality report. The bundle records source identities and
exact bytes for deterministic inspection and hashing. These value types do not
provide persistent storage, publication, or source recovery services.

Unknown vendor extension payloads survive loading and formatting. Unsupported
semantic-critical extensions fail explicitly. Governance replacement references
are validated against the assembled Project, including cross-document references.

## Validation and quality

For business expectations beyond structural validation, authors can keep a
[project regression suite](../testing/project-compile-regression.md) beside their
models and run `metis project test` locally or in existing CI. Compile mode checks
the compilation contract without a database; runtime mode checks metric results
against an externally prepared fixture. Review expected values independently of
the model implementation. These checks do not test custom host authorization or
replace Metis's own conformance suite.

`ValidateProject` returns a structured `ValidationResult`. Structural and
semantic validity are separate from the quality report's `publishable` flag.
That flag means findings are below the configured quality threshold; it does
not represent a publication operation or approval.

Diagnostics have stable codes, severities, caller actions, and bounded asset or
location evidence. Advisory findings remain visible without changing query
semantics. See [model quality diagnostics](model-quality-diagnostics.md).

## Formatting and comparison

`FormatDocument` loads Ossie and emits deterministic YAML.
`Compare` reports added, removed, and modified canonical semantic assets.
Comparison is deterministic and does not infer renames from similarity.
Governance changes participate in affected asset digests.

The offline CLI provides `metis project validate`, `metis project inspect`,
`metis project diff`, and `metis model format`. See the [project README](../../../README.md)
for the supported commands. Source validation and comparison do not replace a
running generation; that is a separate [embedding operation](../operations/semantic-runtime-activation.md).
