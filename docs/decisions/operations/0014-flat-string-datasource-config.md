# ADR-0014: DataSource Config Is a Flat String Map with External References

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted
**Date:** 2026-09-01
**Last reviewed:** 2026-09-16
**Supersedes:** ADR-0011 only where it stabilized arbitrary nested DataSource and Driver Config values and nested SecretRef envelopes

## Context

Metis DataSources need database-owned connection fields and references to
deployment-provided values. They do not currently need an arbitrary nested
configuration graph or a Core-owned hierarchy of database authentication
models. The original `map[string]any` SPI admitted substantially more values
than built-in Drivers used and made production and real-engine fixture
configuration unnecessarily different.

External systems demonstrate that environment- and vault-backed connection
fields are useful, but their complete profile grammar, templating language,
targets, and authentication schema are not Metis authorities. Metis already
has deterministic semantic-model-to-DataSource placement and exact Driver
secret lookup.

## Decision

`execution/datasource.DataSource.Config`, `driver.Factory.ValidateConfig`, and
`driver.OpenRequest.Config` use `map[string]string`.

`${ENV_NAME}` is the shorthand `env` reference form.
`secret://<provider>/<key>` is the provider-neutral vault form. Core does not
implement interpolation or general templates. DataSourceRegistry stores and
returns owned copies containing the reference tokens and never reads the
environment or contacts a provider.

Runner resolves references lazily under the execution lifecycle. Resolved
values are held only behind exact `driver.Secrets.Value(SecretRef)` lookup.
Every Driver Config field retains its token; Drivers may move an individual
value into a private connection object but never into Config. Resolved plaintext
therefore enters neither DataSource nor Driver Config.

The official process composes only the `env` resolver. A deployment may inject
a resolver for `aws-secrets-manager` or another provider. Unsupported providers
fail closed; Core and Drivers do not contain provider-specific branches.
Resolved values are initialization-attempt local in Runner and are not retained
in its DataSource registry or entry state after DriverFactory returns.

Each Backend owns and validates its closed flat keys, including which fields
are credentials. Core applies a conservative sensitive-name guard, while a
Driver remains responsible for rejecting plaintext in every additional
Backend-specific credential field. Adding future BigQuery or other
authentication modes extends that Backend's config contract rather than Core's
semantic or project schemas.

Doris uses separate required `host` and `port` fields rather than a combined
address. `port` remains a string at the common boundary and is validated by the
Doris Backend as a TCP port.

## Consequences

- DataSource and Driver configuration has one small, deterministic value type.
- Test DataSources can reuse the production `datasources.yaml` shape.
- Sensitive plaintext has no Config representation or compatibility path.
- Reference resolution stays downstream of registration and inside bounded
  execution lifecycle; persisted configuration contains locators only.
- Backends can evolve flat connection and authentication fields independently.
- Nested options must be represented as explicit Backend-owned string fields or
  proposed as a future reviewed SPI change.
- This is a deliberate pre-release compatibility break to ADR-0011's Config
  shape; its Renderer authority, Driver lifecycle, and remaining stable type
  closure stay accepted.

## Alternatives considered

**Retain nested `map[string]any`.** Rejected because unused expressiveness is
not free at a stable extension boundary.

**Create Metis profiles matching another project.** Rejected because it would
introduce target and authentication authorities outside DataSource/Backend.

**Store resolved secrets in Config.** Rejected because Config is readily
copied, inspected, and validated; credentials require the narrower Secrets
capability.

## References

- [RFC-0065](../../proposals/execution/0065-flat-string-datasource-config.md)
- [ADR-0011](../sql/0011-renderer-and-driver-spi-stabilization.md)
- [Runtime Bootstrap](../../specs/operations/runtime-bootstrap.md)
- [Renderer and Backend Extension Authoring](../../specs/sql/extension-authoring.md)
