# Online project validation

`metis project validate` defaults to offline loading, semantic checks and quality
diagnostics. `--offline` makes that choice explicit. `--online` checks a caller's
query inventory against Doris or ClickHouse using production routing and EXPLAIN.

```sh
metis project validate --offline --project sales --config ./sales/project.yaml
metis project validate --online --project sales --config ./metis.yaml \
  --queries ./queries.json --output ./validation.json
```

Offline configuration is the semantic project manifest; online configuration is
the deployment root. Flags are mutually exclusive. Online requires strict version
1 JSON queries and a fresh private output path. See the
[runnable examples](../../../examples/authoring/live/README.md).

Reports record `mode`, candidate/inventory digests, and separate offline, compile,
catalog and engine coverage. Failed/unsupported checks cannot pass. Exit codes:
0 all checks passed; 1 failed/incomplete; 2 command/input/report I/O failure.

Online preparation checks every case before opening databases. Project author and
compile authorization precedes configuration/source/registry reads. Asset visibility
and data-policy preflight precede probes. Embedders provide an authenticated
Principal and their policies; the CLI explicitly supplies a trusted-local author.
Secrets and pools are lazy; Runner owns limits, cancellation and cleanup.

Catalog checks required physical column existence and direct declared type families;
precision, timezone, nullability and expression output metadata are not certified.
Unsupported/unresolved dependencies produce incomplete reports. Exact qualified
relations are described without sampling. Native errors remain unavailable rather
than being guessed from text.

ClickHouse supports parameterized queries with server-side named values. Execution
and validation use one transport adapter that changes only placeholder spelling,
preserving the compiler artifact and ordered values. It handles strings, integers,
finite floats, booleans, binary strings, NULL and supported exact JSON numbers.
Date/time strings retain their original text; the engine validates contextual
conversion. IN/BETWEEN lists expand to ordered scalar parameters. Policy values
follow the same path and remain absent from reports. Unsupported parameter forms
and ambiguous SQL quoting/counts fail pure preflight before credential resolution.

Doris 3.0.8 parameterized validation remains unsupported. Ordinary PREPARE accepts
nonexistent relations, so it cannot replace a full planning check. There is no
interpolation, PREPARE-only success or SELECT fallback. The native suite verifies command
behavior on the engine versions pinned in the executable conformance registry;
other versions receive evidence from their own observed runs.

Reports exclude SQL, values, source excerpts, physical identities, endpoints,
credentials and raw database errors. EXPLAIN acceptance concerns planning at the
observed time. Use `metis project test` for independently authored result expectations;
later SELECT permissions and database state can still change.

See [RFC-0088](../../proposals/tooling/0088-online-semantic-validation.md) for
version 1 report fields, limits, private categories and extension responsibilities.
