# Principal-aware data access policy

This is the application-owned contract implemented by `app/service/policy` and
shared semantic services. Project-action authorization and asset visibility
remain independent, earlier checks. Data policy is not a SQL gateway or a
warehouse credential boundary.

## Assembly and authority

Embedder composition MAY install `bootstrap.WithDataAccessPolicy(adapter)`.
The same adapter is captured by each immutable semantic runtime generation.
Adapters receive a copied authenticated Principal, resolved Project, and the
canonical union of dataset/required-field identities for the complete operation.
They MUST NOT receive transport, action, dialect, physical placement or SQL.
Each configured adapter is evaluated exactly once per operation. The result is
copied and reused for every subquery; it is never reevaluated by the planner,
optimizer, renderer or execution runtime.

Bootstrap and standalone CompileService explicitly default to
`NoRestrictionDataAccessPolicy`. This constant compatibility mode bypasses the
policy workload/binder and does not require an HTTP Principal for trusted
compile-only callers. It preserves existing expression support and physical
fingerprints. This is the only evaluation shortcut: an arbitrary adapter that
returns unrestricted still undergoes canonical workload validation and one
evaluation. Nil, including typed nil, MUST NOT become unrestricted.
There is no YAML entitlement schema or Agent-facing policy input.

## Decisions and binding

The closed effects are unrestricted, constrained and denied. Constrained
decisions MUST cover each workload dataset exactly once, with no extras.
Unrestricted and denied decisions MUST carry no constraints. Every predicate
and denied field uses exact model/dataset/field identity, not physical names.
Denial of any required field rejects the entire operation, including indirect
metric dependencies, filters, join keys and temporal/analytical inputs.

Workload collection consumes the same resolved queries and single validated
MetricEvaluationPlan per metric-bearing query that planning subsequently uses.
The builder supplies source-aware dependencies; field closure expands selected
expressions. Unsupported or unprovable dependencies fail closed, never truncate.
V1 configured-policy workloads reject semi-structured path/index dependencies
whose canonical field closure cannot be established. Unrestricted compatibility
mode is unaffected. This is a conservative V1 limitation, not silent authorization.

The binder MUST use those same generation-owned model objects and already
selected Renderer expressions. Each policy field MUST resolve to a direct
physical column of its own source. Computed expressions, semantic indirection,
casts, functions, paths and cross-source references are invalid. Policy-only
fields cannot be denied and MUST NOT become semantic outputs or discovery
evidence. Required-field closure is rechecked during binding.

Predicates are conjunctive and admit only eq, neq, gt, gte, lt, lte, in, not_in,
between, is_null and is_not_null. Values are homogeneous, flat string, bool,
int64 or finite float64 scalars. Dates/times use type-checked ISO strings;
integers retain 64-bit precision. Decimal predicates currently accept int64 or
finite float64, not arbitrary precision decimal strings. Adapters MUST NOT
round exact entitlements into floating point; an unrepresentable entitlement
must be denied or reported unavailable. Boolean predicates admit equality,
membership and null tests only. Null operators have no values; between has two;
membership has at least one; other operators have one.

Bounds are operation-wide: 128 sources, 4,096 required fields, 4,096 denied
fields, 256 predicates, 4,096 values, and 65,536 scalar bytes. Identity components
are limited to 1,024 bytes and Principal scopes to 256 entries. Overflow fails.
Conjunctions, denied-field sets and source coverage are canonicalized before
scope hashing; typed values remain distinct. No snapshot or scope is telemetry.

## Enforcement and atomicity

`Planner.PlanPrepared` accepts the already validated metric plan and bound
relation constraints. Core packages never receive Principal or an adapter.
Constraints attach to every owned SemanticPlan relation input before optimizer
execution, including conversion and calendar inputs. Plan validation requires
complete scope coverage. Clones preserve immutable constraints; scan identity
includes scope, canonical fields, physical columns, operators, typed values and
relation-input placement. Fusion MUST NOT cross a policy difference.

SQLPlan lowers constrained inputs as `FilteredTableSource`, not outer WHERE
predicates. Renderers MUST filter before joins and preserve outer-join null and
preserved-row semantics. All values use physical parameters. A renderer that
cannot implement this relation form MUST reject it, never ignore constraints.
Runner consumes only the completed artifact and output schema.

Compile, validate and explain use this common preflight. query_metrics,
get_dimension_values, attribute_metric and compare_metrics use it before
execution. Multi-query operations evaluate the union once and prepare every
physical artifact before the first Runner call. Policy failures produce zero
execution calls. Later execution failures may follow successful earlier calls,
but MUST return no partial workflow result.

## Privacy and errors

`DATA_ACCESS_DENIED` is HTTP 403 / CHANGE_REQUEST.
`DATA_ACCESS_POLICY_UNAVAILABLE` and `INVALID_DATA_ACCESS_POLICY` are HTTP 422 /
CHANGE_TARGET: a caller cannot repair deployment policy by changing SQL.
REST and MCP share these service errors. Error text is fixed and contains no
adapter cause, Principal, field, predicate or value. Adapter panic, error,
cancellation, nil and malformed output fail closed. Adapters MUST honor context
cancellation; Metis does not abandon an uncooperative adapter in a background
goroutine.

Explain exposes only `data_constraints_applied: true` when constrained. It MUST
NOT disclose predicates, policy-only fields, scope hashes or values. Existing
bounded error-code observation is reused; no identity-bearing metric labels
are added. Authorized compile output intentionally includes physical policy
columns and parameter values: granting Project compile grants that disclosure.
Deployments that cannot disclose these must deny compile and offer execution.
Metis cannot enforce external use of a handed-off compiled query.

## Evidence

Tests in `app/service/policy`, `app/service/semantic`,
`planner/semanticplan`, `tests/conformance/compiler` and `renderer/sqlkit`
cover copying, failure paths, shared snapshots, closure and scan preservation.
The shared engine harness executes the same service-enforced row policy through
the production DuckDB, ClickHouse and Doris drivers.
