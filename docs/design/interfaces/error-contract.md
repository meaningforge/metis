# Shared Error Contract

Metis services return `serrors.Error` values with a precise stable code. Two
things are derived centrally from that code and nothing else: what the recipient
should do next, and how each transport expresses the failure.

```text
                        ErrorCode
                            |
                +-----------+-----------+
                v                       v
          CallerAction          transport projection
          domain-owned              adapter-owned
                |                       |
                |                       +-- REST -> HTTP status
                |                       |
                |                       `-- MCP  -> isError + payload
                v
         Agent remediation
```

Both are pure functions of the code. Neither consults runtime context, and
neither parses a code name.

## Caller action

`CallerAction` is the next machine-actionable remediation category Metis can
recommend for a deterministic failure.

| Value | Meaning |
| --- | --- |
| `CHANGE_REQUEST` | the submitted query or request parameters must change |
| `CHANGE_MODEL` | the semantic model must change; the caller cannot act |
| `CHANGE_TARGET` | the SQL dialect or deployment DataSource/Backend configuration must change |
| `AUTHENTICATE` | credentials must be supplied or refreshed |
| `REPORT_DEFECT` | nothing outside Metis can fix this |

The five values do not sit on one axis: three name an artifact to edit, two name
an action with no artifact. That asymmetry is kept deliberately. Making the
taxonomy uniform would cost the property that matters — every value names
something the recipient can actually do next — and `AUTHENTICATE` is not "who
changes what" while remaining an unambiguous remediation.

### It is derived from the code, never from the error

```go
func CallerActionOf(code ErrorCode) CallerAction   // the contract
func CallerActionFrom(err error) CallerAction      // prohibited
```

Taking a code keeps `code -> caller_action` a pure function, so a client can
reason about it before ever seeing a failure. Taking an error would make the
contract `code + runtime context -> caller_action`: the same code would reach an
Agent carrying different actions on different calls, and the published contract
would diverge from the moment it shipped. A test parses this package and fails if
any exported function takes an `error` and returns a `CallerAction`.

`PayloadFrom` still recovers the code through `errors.As`. That is not the
prohibited dependence — the code is what was reported, and nothing but the code
decides the action.

Where a code appears to need a runtime-dependent action, the rule below applies:
split the code.

## Transport projection

A transport's behavior is a projection of the code, owned by that transport.
`serrors` has no opinion about HTTP, and `app/rest` has no opinion about
remediation.

`app/httperr` owns an exhaustive `map[serrors.ErrorCode]int`. Every status is
written out rather than inferred.

It is a package rather than a function inside `app/rest` because more than one
HTTP entry point produces Metis errors: the REST handlers, and the bearer
middleware in `app/auth` that rejects a request before any handler runs. Those
two packages do not import each other and should not — a transport-generic auth
package depending on REST handlers would be backwards — so the projection lives
where both reach it and neither owns it. A test in `app/httperr` fails if any
non-test file outside it names a 4xx or 5xx status, because a completeness test
over the table cannot see a literal written somewhere else. Adapters must never derive behavior by parsing
a code name: names are identifiers, not a type hierarchy, and codes such as
`EXPRESSION_PARSE_FAILED` or `METRIC_SOURCE_UNREACHABLE` fit no prefix rule, so
any such scheme would need a list of exceptions longer than the table it
replaced. An unregistered code fails closed to 500.

| Status | Used for |
| --- | --- |
| 400 | malformed or unsatisfiable input, from the request or the model |
| 401 | no valid credential |
| 404 | a named asset, binding, engine, or join path does not exist |
| 409 | Metis can see several candidates and will not guess |
| 422 | understood and well-formed, but the target cannot express it |
| 500 | a Metis defect |

422 rather than 400 for the unsupported cases because the request *was*
understood; reporting it as malformed tells the caller something untrue and
gives them nothing actionable. Only `REPORT_DEFECT` codes may be 5xx, and a test
enforces that against the table itself.

### The two projections are independent

Neither table is derivable from the other, which is why they are two tables:

```text
METRIC_NOT_FOUND        404  ->  CHANGE_REQUEST
RELATIONSHIP_NOT_FOUND  404  ->  CHANGE_MODEL      one status, two remediations
CHANGE_TARGET           ->  404, 409, 422          one remediation, three statuses
```

A test asserts both directions. If some status ever covered exactly one
remediation and every remediation exactly one status, one of the two tables
would be redundant and should be deleted rather than maintained.

Every HTTP error response goes through `serrors.PayloadFrom`. A handler must not
assemble a `Payload` literal: the code would not have to be a declared one, and
`caller_action` would silently serialize empty.

MCP does not project at all. It returns the same JSON payload as tool-error
content with `isError: true`, which is what the MCP specification prescribes so
the model can see the failure and self-correct. A domain failure is never
reported as a JSON-RPC protocol error: a failed tool call is a *successful*
protocol call.

Authentication failures use this same payload. An Agent parsing errors sees one
response shape across every failure, including the ones raised at the HTTP
boundary before any service is reached.

## Codes name a condition, never a stage

An operator reading `METRIC_SOURCE_UNREACHABLE` knows what is wrong. One reading
`PLANNING_FAILED` only knows where in the pipeline the code happened to sit,
which is the least useful fact available. Codes therefore name the condition,
and follow four shapes:

```text
<SUBJECT>_NOT_FOUND      a named asset does not exist
<SUBJECT>_REQUIRED       a required input was absent
<ADJECTIVE>_<SUBJECT>    INVALID_ UNSUPPORTED_ AMBIGUOUS_ INCOMPATIBLE_
                         INCOMPLETE_ describe the state that is wrong
INTERNAL_*               a Metis defect, never the caller's to fix
```

A code ending in `_FAILED` is acceptable only when the verb *is* the condition,
as in `EXPRESSION_PARSE_FAILED`. Stage names are not conditions.

**When a failure cannot be named precisely, it uses `INTERNAL_ERROR` rather than
a vague new code.** A bucket that means several unrelated things tells an
operator nothing, and it cannot be narrowed later without a compatibility break —
whereas a generic code can always be replaced by a specific one as understanding
improves. Inventing `SOMETHING_FAILED` to avoid saying "we do not know" trades a
permanent contract for a temporary convenience.

Applying this retired three stage-named buckets. `PLANNING_FAILED`,
`METRIC_PLANNING_FAILED`, and `COMPILATION_FAILED` between them covered 102 call
sites and a dozen unrelated conditions; they were replaced by the specific codes
below plus `INTERNAL_INVARIANT_VIOLATION`, and no longer exist.

`UNSUPPORTED_METRIC_EVALUATION` was retired the same way and is the cleanest
demonstration of the rule. "Metric evaluation" is a pipeline stage, so the code
collected whatever failed inside it: sixty-odd guards on planner-built structures
(a stage without its evaluation payload, a dependency whose stage is absent, a
typed plan with missing inputs), seven cases where the *query* omitted a required
time dimension, one malformed filter value, and two `default:` branches over
Metis's own enums. Attributing each to its actual condition left the bucket
empty. A code that dissolves completely under attribution was never naming a
condition.

## One code, one remediation

> **A single `ErrorCode` MUST imply a single remediation. If different call sites
> of one code require the recipient to change different things, the code is too
> broad and MUST be split.**

This is the rule that makes `code -> caller_action` (RFC-0034) a pure function of
the code, and therefore a contract a client can rely on ahead of time. The
alternative — deciding the remediation from runtime context — would make the same
code arrive with different meanings on different calls, and the contract would
diverge from the moment it shipped.

Two consequences worth stating explicitly, because both are easy to get wrong:

- **A lookup failure names who supplied the name.** `METRIC_NOT_FOUND` means *the
  request* named a metric that does not resolve; the caller edits the request.
  When a *model* references a metric, dataset, or field it does not define, that
  is `UNRESOLVED_SEMANTIC_REFERENCE` and only the model's author can act. Before
  these were separated, one code covered both and could not say which.
- **A guard on a Metis-built structure is never a caller error**, even when it sits
  in the middle of caller-triggered work. See the section above.

## External cause versus internal defect

A failure is classified by **who can act on it**, and that is decided by one
question:

> Could any combination of user-supplied query and user-authored semantic model
> reach this line?

If one could, the failure is the caller's — it keeps a client-actionable code,
and the message and details are what lets them fix it. If none could, the
failure is a Metis defect and uses `serrors.Internal`, which carries
`INTERNAL_INVARIANT_VIOLATION`, whose remediation is `REPORT_DEFECT`.

Internal by this rule are nil guards, pipeline-ordering violations, a structure
Metis itself built turning out inconsistent, and fail-closed invariant checks
catching Metis's own defect — `ValidateSemanticPlan` failing is the clearest
example, since the plan under inspection is one the planner just produced.

Both directions of misclassification are expensive, which is why the rule is
stated rather than left to taste:

- reporting a caller-fixable failure as internal strips them of the diagnostic
  they need, and inflates server error rates with things that are not faults;
- reporting a Metis defect as a client error hides it from error-rate
  monitoring and tells the caller to fix something they cannot.

When a site is genuinely ambiguous, it stays client-actionable. A caller who
receives an unhelpful 400 still has the message and details; a caller who
receives a 500 has nothing.

Note that model-authored content is *external*. An invalid metric extension or
an incomplete calendar declaration is the model author's to fix and stays
client-actionable, even though no query caused it.

Both the remediation and the status are derived from the code centrally, never
attached at individual call sites, so a code cannot mean one thing in one place
and another elsewhere. Unknown errors and unregistered codes fail closed to
`REPORT_DEFECT` and 500; a new code cannot gain a client-facing status merely
because its name contains a familiar word.

Registration is a table rather than a switch, so the inventory is enumerable
through `serrors.Codes()`. Tests parse the declared `ErrorCode` constants and
assert each one appears in **both** tables, because a code that is declared but
never registered is silently indistinguishable from a server fault — the failure
would first appear as an unexplained 500 on an error that is not internal at
all.

## Code inventory

Rendered from `serrors.Codes()`. A test parses the declared constants and fails
if any is missing from either registration table, so this list cannot silently
drift from the code.

The two derived columns are worth reading against each other. Four of the six
statuses each cover **three** different remediations, and three of the five
remediations span more than one status. `RELATIONSHIP_NOT_FOUND` and
`METRIC_NOT_FOUND` share a status while one is the model author's to fix and the
other the caller's; that difference is what `caller_action` carries and a status
line cannot.

| Code | Caller action | HTTP | Operator reads it as |
| --- | --- | --- | --- |
| `PROJECT_NOT_FOUND` | CHANGE_REQUEST | 404 | the named project is not loaded |
| `MODEL_NOT_FOUND` | CHANGE_REQUEST | 404 | the named model is not in the project |
| `METRIC_NOT_FOUND` | CHANGE_REQUEST | 404 | the named metric is not in the model |
| `DIMENSION_NOT_FOUND` | CHANGE_REQUEST | 404 | the named dimension is not in the model |
| `FIELD_NOT_FOUND` | CHANGE_REQUEST | 404 | the named field is not in the dataset |
| `RELATIONSHIP_NOT_FOUND` | CHANGE_MODEL | 404 | no relationship path connects the datasets |
| `ENGINE_NOT_FOUND` | CHANGE_TARGET | 404 | the binding names an unregistered engine |
| `EXECUTION_CONFIG_NOT_FOUND` | CHANGE_TARGET | 404 | the project has no execution configuration |
| `PROJECT_REQUIRED` | CHANGE_REQUEST | 400 | the request omitted the project |
| `TARGET_REQUIRED` | CHANGE_TARGET | 400 | no compile target resolved and none was given |
| `INVALID_QUERY` | CHANGE_REQUEST | 400 | the semantic query is malformed |
| `INVALID_MODEL` | CHANGE_MODEL | 400 | the Ossie model is malformed |
| `INVALID_EXECUTION_CONFIG` | CHANGE_TARGET | 400 | the manifest is malformed |
| `INVALID_FILTER_VALUE` | CHANGE_REQUEST | 400 | a filter carries a value the operator cannot take |
| `INVALID_SORT` | CHANGE_REQUEST | 400 | the requested ordering cannot be planned |
| `INVALID_RELATIONSHIP` | CHANGE_MODEL | 400 | a relationship or its temporal extension is malformed |
| `INVALID_METRIC_EXTENSION` | CHANGE_MODEL | 400 | a metric's evaluation extension is malformed or duplicated |
| `INVALID_CONVERSION_METRIC` | CHANGE_MODEL | 400 | a conversion metric's dependencies do not match its declaration |
| `INVALID_METRIC_DEFINITION_FILTER` | CHANGE_MODEL | 400 | a metric-definition filter is malformed |
| `INCOMPLETE_CUSTOM_CALENDAR` | CHANGE_MODEL | 400 | the model's custom calendar is under-declared for this use |
| `INVALID_METRIC_ROLLUP` | CHANGE_MODEL | 400 | a metric rolls up a base whose aggregation cannot be merged |
| `INCOMPATIBLE_QUERY_GRAIN` | CHANGE_REQUEST | 400 | the query's grain conflicts with what the metric requires |
| `UNRESOLVED_SEMANTIC_REFERENCE` | CHANGE_MODEL | 400 | the model references a metric, dataset, or field it does not define |
| `UNRESOLVED_METRIC_SOURCE` | CHANGE_MODEL | 400 | a metric's own root does not resolve to exactly one dataset |
| `METRIC_SOURCE_UNREACHABLE` | CHANGE_MODEL | 400 | the metric's root is known but no relationship path reaches a required dataset |
| `METRIC_DEPENDENCY_CYCLE` | CHANGE_MODEL | 400 | metric definitions form a cycle |
| `EXPRESSION_PARSE_FAILED` | CHANGE_MODEL | 400 | an expression did not parse |
| `SEMANTIC_ANALYSIS_FAILED` | CHANGE_MODEL | 400 | an expression parsed but did not type-check |
| `INCONSISTENT_EXPRESSION_REFERENCES` | CHANGE_MODEL | 400 | dialect expressions of one metric reference different assets |
| `AMBIGUOUS_FIELD` | CHANGE_REQUEST | 409 | a field name matches more than one dataset |
| `AMBIGUOUS_EXPRESSION_REFERENCE` | CHANGE_REQUEST | 409 | an expression reference matches more than one symbol |
| `AMBIGUOUS_METRIC_SOURCE` | CHANGE_REQUEST | 409 | a metric has no unique source root |
| `AMBIGUOUS_RELATIONSHIP_PATH` | CHANGE_MODEL | 409 | more than one relationship path connects the datasets |
| `AMBIGUOUS_TARGET` | CHANGE_TARGET | 409 | more than one compile target resolves |
| `UNSUPPORTED_EXPRESSION` | CHANGE_MODEL | 422 | no expression is declared for the selected target |
| `UNSUPPORTED_RELATIONSHIP_FANOUT` | CHANGE_REQUEST | 422 | the requested relationship traversal is not proven safe for the selected metric population |
| `UNSUPPORTED_DIALECT` | CHANGE_TARGET | 422 | the selected dialect has no renderer |
| `UNSUPPORTED_SEMANTIC_EXTENSION` | CHANGE_MODEL | 422 | a semantic-critical extension is unsupported by the target |
| `UNSUPPORTED_TIME_FILTER` | CHANGE_REQUEST | 422 | this filter form is not supported on a time dimension |
| `UNSUPPORTED_QUERY_SHAPE` | CHANGE_REQUEST | 422 | this combination of semantics is not supported yet |
| `UNAUTHENTICATED` | AUTHENTICATE | 401 | no valid bearer API key |
| `INTERNAL_INVARIANT_VIOLATION` | REPORT_DEFECT | 500 | **a Metis defect** — unreachable from any query or model |
| `INTERNAL_ERROR` | REPORT_DEFECT | 500 | an unrecognised failure; treat as a Metis defect until shown otherwise |

The two internal codes are the operator's signal that no change to the query or
the model will help. Everything above them is the caller's to act on, and every
one of them carries structured `details` naming the offending asset.
