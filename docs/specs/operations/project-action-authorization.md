# Project Action Authorization

This specification defines the shared application-layer authorization contract
for a `Principal`, resolved Project, and closed action. Authentication proves
the caller identity; routing selects a Project; neither grants access.

## Policy API

`app/service/semantic` owns these transport-neutral types:

```text
ProjectAuthorizationRequest {
  Principal *auth.Principal
  ProjectID string
  Action    ProjectAction
}

ProjectAuthorizationDecision {
  Effect allow | deny
  Reason bounded_reason
}

ProjectAuthorizer.AuthorizeProject(context, request) -> decision
```

An absent authorizer, an unknown action, an empty resolved Project, an invalid
effect, or an unregistered reason MUST fail closed. A policy engine's free-form
message, rule expression, remote response, or exception MUST NOT enter the
public error or operational label contract.

The action vocabulary is closed:

| Action | Operation |
| --- | --- |
| `discover` | Project and semantic metadata discovery |
| `compile` | query validation, explanation, and physical compilation |
| `execute` | `query_metrics`, `get_dimension_values`, `attribute_metric`, and `compare_metrics` |
| `author` | source authoring operations supplied by an embedding host |
| `publish` | reserved host publication action; no built-in publication service |
| `activate` | replace an in-process semantic generation |
| `admin` | host administration action; no built-in administration endpoint |

Actions imply no other actions. Candidate validation does not grant
publication; publication does not grant activation; activation does not grant
administration.

## Built-in adapters

Project compile authorization includes disclosure of the complete constrained
physical query and policy parameters. Deployments that cannot disclose these
must deny compile and expose governed execution instead. Row/column policy is
a subsequent independent boundary, not an expansion of Project identity or
actions; see [data access policy](data-access-policy.md).

The production server uses `ScopeProjectAuthorizer`. It maps actions exactly:

| Action | Scope |
| --- | --- |
| `discover` | `semantic:read` |
| `compile` | `semantic:compile` |
| `execute` | `semantic:execute` |
| `author` | `semantic:author` |
| `publish` | `semantic:publish` |
| `activate` | `semantic:activate` |
| `admin` | `semantic:admin` |

`*` is the only implication rule and grants every registered action. A scope
for one action never grants another. The environment-backed static API key
produces a wildcard Principal and therefore remains the default single-key
local server policy.

`AllAccessProjectAuthorizer` is the trusted offline/embedder adapter. Offline
lifecycle commands, focused direct-service tests, and repository conformance
runtimes opt into it explicitly. Service constructors and network runtime
assembly default to the fail-closed scope adapter and MUST NOT infer all-access
from missing configuration. An external policy store may replace either adapter
without changing service or transport DTOs and may decide differently for each
Principal and Project.

## Resolution and enforcement order

Every direct operation performs:

```text
authenticate Principal
  -> resolve explicit project > default_project > sole project
  -> authorize Principal × resolved Project × Action
  -> read semantic inventory or runtime configuration
  -> perform the operation
```

An invalid explicit Project returns `PROJECT_NOT_FOUND` and never falls back.
An existing but unauthorized explicit, default, or sole Project returns
`PROJECT_ACCESS_DENIED`. Its models, assets, DataSource, and Backend availability
MUST NOT be consulted first.

Canonical model, metric, dimension, and relationship references do
not bypass the Project decision. Internal compilation performed after an
`execute` decision does not require a second `compile` grant: `execute` is an
independent operation whose implementation includes controlled compilation.

List operations discard unauthorized Projects before result construction,
pagination, counts, and search results. `list_projects` itself requires
`discover`. Each advertised capability is the intersection of runtime wiring
and its action decision:

- `compile_sql` requires `compile`;
- `query_metrics` requires `execute` and an executable Project DataSource and
  Backend.

Capability evaluation does not open a Driver, resolve a secret, or probe a
warehouse.

## Errors and transport behavior

Every policy denial becomes the same transport-neutral error:

```json
{
  "code": "PROJECT_ACCESS_DENIED",
  "caller_action": "CHANGE_REQUEST",
  "message": "project action is not authorized",
  "details": {
    "project_id": "finance",
    "action": "discover"
  }
}
```

REST projects the code to HTTP `403`. MCP returns the identical JSON payload as
tool-error content with `isError: true`. Adapters MUST NOT repeat policy checks
or introduce transport-specific action names.

## Decision observation

The service decision boundary emits a fixed `ProjectAuthorizationAudit` with
tenant, subject, API-key identifier, Project, action, effect, and bounded
reason. It never contains the API key, bearer token, Principal scopes, semantic
request, canonical asset refs, SQL, credentials, policy-engine messages, or
request values. Observer failure is isolated and cannot change a decision.

The production JSON log records the fixed audit fields. Prometheus records only
the low-cardinality `action`, `effect`, and `reason` labels in
`metis_project_authorization_decisions_total`; Principal and Project identities
MUST NOT become metric labels.

## Layer boundary

This gate authorizes a Project operation only. Asset visibility, member-level
access, row filters, column masking, warehouse credentials, and physical-engine
permissions are separate policies. Asset governance runs after this gate and is
specified by
[`../semantic/asset-governance.md`](../semantic/asset-governance.md).

## Conformance

Tests MUST cover the complete action/scope matrix, wildcard behavior, invalid
decision fail-closed behavior, observer isolation, explicit/default/sole
resolution, list filtering, capability intersection, denial before semantic or
runtime access, author-versus-publish separation, and equivalent REST/MCP error
payloads.
