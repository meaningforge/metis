# Semantic Source Architecture

The source package is the shared boundary between authored Ossie documents and
an assembled Project. Bootstrap and offline authoring use the same loader:

```text
Project manifest + source documents
    -> app/service/source
       -> Ossie parsing and deterministic document merge
       -> SemanticManifest and semantic validation
       -> quality diagnostics and source content digest
    -> ProjectSource
       -> bootstrap or explicit runtime replacement
       -> inspection and semantic comparison
```

`ProjectSource` and `ImmutableSemanticBundle` retain the information needed to
inspect the exact input and compute reproducible identities. They are data
contracts, not a persistent repository. Canonical model identity comes from
Ossie and the Project namespace, not filenames or import order.

Quality diagnostics report authoring problems separately from query meaning.
The quality threshold changes the advisory eligibility result, not metric
resolution or generated SQL. Ownership and lifecycle evidence remain typed
Ossie extension data; the query services consume the validated manifest.

The [source specification](../../specs/semantic/asset-authoring-lifecycle.md)
describes the public functions and offline commands. The
[runtime generation contract](../../specs/operations/semantic-runtime-activation.md)
describes how an embedder replaces validated semantic state.
