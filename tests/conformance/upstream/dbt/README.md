# Native dbt Ossie interoperability acceptance

This bounded fixture checks native artifacts, not a dbt converter. It does not
publish models, grant access, add a release gate, or claim real-model accuracy.

## Reproduce

Use Python 3.13.7 and the pinned requirements in an isolated environment. dbt
downloads its platform-native experimental parser (observed version 2.0.5);
the capture does not depend on a platform-specific wheel URL. Build
the actual CLI at the recorded Metis commit, then run:

```sh
go build -o /tmp/metis-native-dbt ./cmd/metis
python capture.py --metis /tmp/metis-native-dbt --output /tmp/new-private-dbt-evidence
```

Run the script from this directory with the environment's `dbt` on PATH. The
output must not already exist. Only private local DuckDB fixtures are used.
Each case retains source YAML, native `osi_document.json`, native
`semantic_manifest.json`, and diagnostics from the same first successful parse,
before repeated generation. Nonzero generation status is authoritative: never
load a file merely because it still exists. Keep the raw capture directory when
investigating losses; the checked-in evidence omits redundant stack traces.

The capture asserts valid generation, stable repeated Ossie bytes, expected
loss diagnostics, fresh-invalid absence, and the failed-after-success stale-file
counterexample. Default Go tests consume the captured bytes without network,
dbt installation, conversion, or answer regeneration.

## Observed compatibility (Metis 1866998401d8174719d7cddc49c5b295531a5e63)

Pinned dbt-core 1.12.0, dbt-duckdb 1.11.0, DuckDB 1.5.5, MetricFlow 0.213.0.
All three valid native artifacts declare Ossie **0.1.1**; Metis supports
**0.2.0.dev0**. The actual CLI exits 1 on root `dialects`, before version or
semantic validation. Native loading is therefore **not directly compatible**;
Metis compile/result acceptance is **NOT_EXECUTED**, not passed.

| Capability | Native evidence | Classification / boundary |
| --- | --- | --- |
| Basic metric identity and SQL | `revenue`, `SUM(orders.amount)`, sources and order/customer relationship survive | Preserved upstream, but whole document needs explicit Metis version/schema adaptation before reuse |
| Field types / measure references / keys | Source YAML has types; output omits datatypes and `amount` field, while SQL references it; physical key names differ from entity field names | Needs upstream representation repair or an explicitly designed adapter with source metadata; no type guessing or version-string rewrite |
| Cumulative window / grain-to-date | Both metrics remain as plain SUM expressions; two `CUMULATIVE_SEMANTICS_LOSS` I078 events | Temporarily unsupported for lossless ingestion; cannot treat imported SUM as equivalent |
| Native cumulative parser | Default parse rejects flat cumulative parameters produced by the new YAML path; explicit `require_nested_cumulative_type_params: false` permits generation and emits deprecation | Upstream compatibility issue; both attempts retained, not hidden |
| Private metrics / natural entities | `PRIVATE_METRIC_DROPPED` and `NATURAL_ENTITY_DROPPED`; source semantic manifest retains the originals | Loss must be determined from source plus diagnostics; importing surviving metrics never grants runtime access |
| Invalid fresh / invalid after success | Fresh parse exits 2 without Ossie; after success, exit 2 leaves old Ossie bytes unchanged | Integration must check success and invocation/freshness, use isolated targets, and refuse stale files |
| Repeated generation | Each successful sample produces byte-identical Ossie on repeat | Stable for these inputs only; retain invocation diagnostics separately from source semantic manifests |

The basic physical fixture is independently executable: APAC 50+70=120 and
EMEA=80. This is a dbt/DuckDB fixture sanity check, **not** evidence that Metis
compiled or executed the native artifact. No native end-to-end pass is claimed.
Conversion-metric dropping is documented upstream but not exercised here.

The private/natural sample is a valid slowly-changing dimension: its natural
entity uses a separate customer-code column, explicit primary_entity context,
and validity start/end dimensions. It does not combine prohibited primary/unique
entities with validity windows.

Next product work requires an explicit supported-version/semantic-loss contract;
this acceptance adds neither a generic converter nor a new RFC. Cloud ingestion
must preserve generation diagnostics/source provenance and current authorization.

References: [dbt 1.12 upgrade](https://docs.getdbt.com/docs/dbt-versions/dbt-upgrade/upgrading-to-v1.12),
[native Ossie generation and losses](https://docs.getdbt.com/docs/build/ossie-semantic-models),
[semantic manifest](https://docs.getdbt.com/reference/artifacts/sl-manifest).
