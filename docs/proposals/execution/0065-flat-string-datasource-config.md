# RFC-0065: Flat String DataSource Config and External References

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-01
- **Last updated:** 2026-09-01
- **Implemented:** 2026-09-01
- **Scope:** `execution/datasource`, `execution/driver`, Runner secret resolution, Doris connection configuration, real-engine test DataSources
- **Supersedes:** The former nested arbitrary-value Config and `secret_ref` envelope design recorded in ADR-0011
- **Related:** RFC-0059, RFC-0060, ADR-0010, ADR-0011, ADR-0014

## Summary

DataSource and Driver configuration use one flat `map[string]string`.
`${ENV_NAME}` represents an environment reference, while
`secret://<provider>/<key>` represents a provider-neutral vault locator. Core
preserves that token and never stores resolved plaintext in Config. Runner resolves
references only while lazily opening a DataSource. Every Config copy retains
its token; resolved values are exposed solely through exact `driver.Secrets`
lookup and may move only into a Driver's private connection object.

This borrows the useful connection-configuration idea seen in systems such as
MetricFlow without copying their profile hierarchy, templating language, target
selection, or authentication schema. Metis configuration remains the `config`
section of `datasources.yaml`, and each Backend owns its closed field set.

## Motivation

The previous `map[string]any` and nested `secret_ref: {provider, key}` envelope
were broader than Metis's actual DataSources. They admitted nested shape and
runtime types that no built-in Driver needed, forced copying and validation of
an arbitrary value graph, and made test configuration diverge from production
configuration.

A flat string map has a smaller and more predictable extension contract. It is
also sufficient for endpoints, ports, database/catalog/schema names, usernames,
file paths, options, and references to externally supplied credentials. Future
Backends may add their own flat keys and validation without making Semantic Core
own database authentication models.

## Design

The public structures are:

```go
type DataSource struct {
    Type   datasource.Type
    Config map[string]string
    Policy datasource.DataSourcePolicy
}

type OpenRequest struct {
    Config  map[string]string
    Secrets driver.Secrets
}
```

The reference grammar is deliberately closed:

```text
${ENV_NAME}
secret://<provider>/<key>
```

`ENV_NAME` starts with an uppercase ASCII letter or underscore and continues
with uppercase ASCII letters, digits, or underscores. A provider name starts
with a lowercase ASCII letter and continues with lowercase letters, digits,
underscores, or hyphens; its key is non-empty and whitespace-free. Partial
interpolation, filters, defaults, lowercase environment names, nested objects,
and general template evaluation are rejected. Literal non-sensitive strings
remain valid.

Registration copies Config and preserves tokens without reading the
environment. Sensitive fields such as password, token, API key, private key,
and client secret must be references; plaintext fails registration or
Backend-specific validation. DriverFactory also validates its closed config
field set, so future authentication fields remain a Backend responsibility.

Runner enumerates references deterministically, resolves each through the
process-owned SecretResolver under the execution timeout, and creates an owned
per-open Driver Config that still contains the original tokens. Drivers obtain
individual resolved values from `driver.Secrets.Value(SecretRef)` (or the
non-mutating `driver.ConfigValue` helper) while constructing private connection
parameters. Neither the registered DataSource nor Driver Config contains any
resolved provider value.

The official process supplies only the `env` resolver. `SecretResolver` remains
provider-neutral, so a deployment can inject AWS Secrets Manager or another key
vault without changing the Config, Runner, or Driver contracts. A provider that
is not composed fails closed and has no plaintext fallback.

Runner holds the resolved map only for the lazy-open attempt. It does not attach
that map to DataSourceRegistry, `dataSourceEntry`, or the opened Runtime after
DriverFactory returns. A database client may keep its own private authentication
state, but Metis neither serializes nor re-exposes it.

The Doris Backend now owns `host`, `port`, optional `database`, optional
`username`, and optional referenced `password`. Splitting `host` and `port`
keeps network coordinates independently configurable and validates the port as
an integer in the range 1 through 65535 before opening a connection.

## Alternatives

**Keep `map[string]any`.** Rejected because current and foreseeable connection
fields do not require an arbitrary nested value graph, while the wider domain
increases SPI, copying, validation, and test complexity.

**Add a parallel `secret_refs` map.** Rejected because it duplicates field
identity across two maps and can create ambiguous precedence. A reference is a
value form inside the Backend-owned config field.

**Adopt MetricFlow/dbt profiles and Jinja evaluation.** Rejected because target
profiles, template filters, and database authentication schemas would create a
second configuration product and authority. Only the connection-field and
external-reference concepts are relevant.

**Resolve every reference into Driver Config.** Rejected because plaintext
would then enter Config and become easier to format, log, or retain.

## Rollout and migration

This is a clean-cut pre-release SPI change. All in-repository DataSources,
DriverFactories, Backends, bootstrap tests, and real-engine fixtures migrate in
one change. Old nested Config and `secret_ref` envelopes are rejected rather
than retained through aliases or compatibility decoding.

Existing Doris deployment configuration migrates from `address: host:port` to
separate `host` and `port` keys. Environment values use quoted `${ENV_NAME}`
tokens. Rollback requires restoring the previous code and configuration
together; mixed formats are intentionally unsupported.

## Test and acceptance criteria

- strict YAML decoding accepts only flat string Config values;
- malformed references and plaintext sensitive values fail closed;
- registry snapshots preserve `${ENV_NAME}` and cannot be mutated by callers;
- Runner preserves reference tokens in its Driver-owned Config copy;
- resolved environment and vault values remain absent from stored and Driver Config;
- missing environment values fail before Driver open with redacted errors;
- Doris validates `host`, `port`, its closed keys, and referenced password;
- shared workflow fixtures continue to run through DuckDB and the production
  Doris Backend, with real Doris and ClickHouse suites remaining environment
  gated.

## Documentation updates

- `docs/design/operations/runtime-bootstrap.md`
- `docs/specs/operations/runtime-bootstrap.md`
- `docs/specs/sql/extension-authoring.md`
- `docs/specs/testing/architecture.md`
- ADR-0011 and ADR-0014
