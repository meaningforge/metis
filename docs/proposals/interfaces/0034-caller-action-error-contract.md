# RFC-0034: Caller-action Error Contract

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-21
- **Last updated:** 2026-08-21
- **Scope:** `pkg/serrors`, the public error payload, REST and MCP adapters
- **Supersedes:** [`0032-transport-neutral-error-classes.md`](0032-transport-neutral-error-classes.md)

## Summary

Replace `ErrorClass` with `CallerAction`: a domain-owned statement of the next
machine-actionable remediation for a failure. HTTP status becomes a code-to-status
projection owned by `app/rest`, where transport knowledge belongs. `pkg/serrors`
stops carrying a vocabulary borrowed from HTTP's response taxonomy.

The inventory below is derived from `main` at `c9df065`, after RFC-0032's
follow-up work landed: 41 codes, 6 classes. See **As implemented** for what
shipped and where it differed.

## The model

Four rules. Everything else in this document follows from them.

1. **`ErrorCode` identifies the precise failed condition.**
2. **`CallerAction` identifies the deterministic remediation category for that
   condition.**
3. **Each `ErrorCode` MUST map to exactly one `CallerAction`. If different call
   sites of the same code require different remediation, the code is too broad and
   MUST be split.**
4. **Transport behavior is a projection of `ErrorCode` owned by the transport
   adapter. `CallerAction` is not a transport abstraction.**

```text
                  ErrorCode
                     |
          +----------+----------+
          v                     v
    CallerAction        transport projection
    domain-owned            adapter-owned
          |                     |
          |                     +-- REST -> HTTP status
          |                     |
          |                     `-- MCP  -> isError + Metis payload
          v
   Agent remediation
```

Rule 3 is the load-bearing one, and it is not new to Metis. RFC-0032's follow-up
faced the same situation with `PLANNING_FAILED`, which mixed a caller problem with
an internal defect. The resolution was not to classify it dynamically; it was to
split the code. This RFC applies that precedent as a standing rule.

## Motivation

RFC-0032 introduced `ErrorClass` to stop REST from substring-matching error code
names, and it succeeded at that. This RFC does not propose returning to substring
matching. It questions the shape of the replacement.

### The class vocabulary is HTTP's taxonomy under different spelling

| Class | HTTP status |
| --- | --- |
| `INVALID_REQUEST` | 400 |
| `UNAUTHENTICATED` | 401 |
| `NOT_FOUND` | 404 |
| `CONFLICT` | 409 |
| `UNSUPPORTED` | 422 |
| `INTERNAL` | 500 |

Six classes, six statuses, one-to-one in both directions. Not one class exists
that two statuses share, and not one status exists that two classes share.
RFC-0032 states the intent directly: "Transports derive coarse behavior from the
class." The vocabulary was chosen by enumerating the HTTP statuses REST wanted to
emit and giving each one a protocol-neutral name.

Calling a value transport-neutral does not make it so. `ErrorClass` is neutral in
its *type* — no `net/http` import in `pkg/serrors` — but not in its *content*: its
value set is a bijection with a status set. What sits in the domain layer is a
transport handling abstraction with the transport identifier stripped off, fixed
before Metis had a second transport whose needs could contradict HTTP's. It still
does not have one.

### `INVALID_REQUEST` holds 19 of 41 codes and at least four different remedies

The largest class is not a handling strategy. It is the residue of "HTTP has one
status for *your side of the exchange is at fault*":

| Code | Remediation |
| --- | --- |
| `INVALID_QUERY`, `INVALID_FILTER_VALUE`, `INVALID_SORT`, `PROJECT_REQUIRED`, `INCOMPATIBLE_QUERY_GRAIN` | edit the query |
| `INVALID_MODEL`, `INVALID_RELATIONSHIP`, `INVALID_METRIC_EXTENSION`, `INVALID_CONVERSION_METRIC`, `INVALID_METRIC_DEFINITION_FILTER`, `INCOMPLETE_CUSTOM_CALENDAR`, `INCONSISTENT_EXPRESSION_REFERENCES`, `METRIC_DEPENDENCY_CYCLE`, `EXPRESSION_PARSE_FAILED`, `SEMANTIC_ANALYSIS_FAILED` | edit the semantic model |
| `TARGET_REQUIRED`, `EXECUTION_BINDING_REQUIRED`, `INVALID_EXECUTION_CONFIG` | change the target or its binding |
| `METRIC_SOURCE_UNREACHABLE` | ambiguous — the code is too broad, see rule 3 |

An Agent that receives `INCOMPATIBLE_QUERY_GRAIN` should retry with a different
query. One that receives `INVALID_METRIC_EXTENSION` should stop retrying and
escalate to a human, because the artifact at fault is one it cannot edit and
another attempt will fail identically. Both are `INVALID_REQUEST`, both are 400.

Metis knows which is which — the code says so precisely, and `Internal`'s own doc
comment on `main` already makes external-vs-internal attribution an explicit rule.
The class layer is where the finer attribution is discarded, because HTTP can say
*your side* but not *which part of your side*.

So a client that needs the distinction must parse the code after all — the exact
behavior RFC-0032 set out to prevent, displaced from the adapter into the client.

### Metis already ships the remediation, as prose

`resolver/target.go:213` returns `UNSUPPORTED_EXPRESSION` with
`Suggestions: ["add a <dialect> expression for <kind> <name>", "add an ANSI_SQL
fallback with equivalent semantic meaning"]`. Both suggestions say *change the
model*. The remediation is known at the call site and already serialized — as
English sentences, in a free-form array, that no client can branch on.

`CallerAction` is not new information. It is information Metis already computes
and currently emits in the one shape a machine cannot use.

### Class has one behavioral consumer

- `app/rest/discovery.go:106` (`httpStatusForErrorClass`) is the only place in the
  tree where a class value changes behavior.
- `app/mcp/errors.go` marshals `serrors.PayloadFrom(err)` and never reads `Class`.
- `serrors.ClassFrom` is exported and has no non-test caller.

`pkg/serrors` on `main` justifies the class this way: "without it, every adapter
would carry its own copy of the full code table and drift from it." That
prediction has not been borne out and cannot be while the second adapter has no
coarse-behavior decision to make: MCP does not carry a code table, because MCP
does not branch. The centralization argument needs a table with more than one
reader.

The premise the comment defends — that adapters must not derive handling by
parsing code names — is correct and this RFC preserves it under rule 4. What does
not follow is that the derived value must be shaped like an HTTP status.

### The JSON-RPC duplication charge, examined and rejected

A natural objection is that class re-invents JSON-RPC 2.0's error object, which
MCP already carries. On the evidence, it does not. `app/mcp/errors.go` returns
tool errors as `isError: true` with the payload as a text block; JSON-RPC error
codes are reserved for protocol-level failures and Metis never emits a domain
error through them. MCP therefore hands a client exactly one bit — "the tool
failed" — and any further structure must travel inside the payload.

So *something* in the payload must classify. The finding is narrower than "class
duplicates JSON-RPC": class duplicates **HTTP's** classification, and the
in-payload classifier MCP genuinely needs should be shaped by what Metis knows,
not by what a status line can express.

## Design

### `CallerAction`

> `CallerAction` is the next machine-actionable remediation category Metis can
> recommend for a deterministic failure.

| Value | Meaning |
| --- | --- |
| `CHANGE_REQUEST` | The submitted query or request parameters must change. |
| `CHANGE_MODEL` | The semantic model must change. |
| `CHANGE_TARGET` | The engine, dialect, or execution binding must change. |
| `AUTHENTICATE` | Credentials must be supplied or refreshed. |
| `REPORT_DEFECT` | Nothing outside Metis can fix this. |

The definition is deliberately *remediation category*, not *who must change what*.
The five values do not sit on one axis: three name an artifact to edit, while
`AUTHENTICATE` and `REPORT_DEFECT` name an action with no artifact. That is
accepted rather than repaired. Introducing a further abstraction to make the
taxonomy uniform would buy symmetry and cost the property that matters — that
every value maps to something an Agent can actually do next. `AUTHENTICATE` is not
"who changes what" and is nonetheless an unambiguous remediation.

### `CallerAction` is a pure function of the code

```go
func CallerActionOf(code ErrorCode) CallerAction
```

Not `CallerActionFrom(err *Error)`. The distinction is the contract:

- **`CallerActionOf(code)`** — the action is derivable from the code alone, so
  `code -> caller_action` is a stable published contract a client can reason about
  ahead of time.
- **`CallerActionFrom(err)`** — the action would depend on runtime context, so the
  contract becomes `code + runtime context -> caller_action`. The same code would
  then reach an Agent carrying different actions on different calls, and the
  contract diverges from the moment it ships.

The second form is prohibited. Where a code appears to need it, rule 3 applies:
split the code. `PayloadFrom` may still traverse a wrapped error to *recover the
code* via `errors.As`, exactly as it does today; what it must not do is consult
anything but that code to choose the action.

### The nine codes that must be split

Nine of the 41 codes cannot satisfy rule 3 as they stand. This is not an
unfinished table. It is the rule doing its job: a code that cannot name one
remediation is not yet naming one condition.

| Code | Conflicting remediations |
| --- | --- |
| `PROJECT_NOT_FOUND`, `MODEL_NOT_FOUND`, `METRIC_NOT_FOUND`, `DIMENSION_NOT_FOUND`, `FIELD_NOT_FOUND`, `RELATIONSHIP_NOT_FOUND` | the caller mistyped a name (`CHANGE_REQUEST`) vs. the model does not define it (`CHANGE_MODEL`) |
| `METRIC_SOURCE_UNREACHABLE` | the model author adds a relationship (`CHANGE_MODEL`) vs. the caller drops the dimension (`CHANGE_REQUEST`) |
| `UNSUPPORTED_EXPRESSION` | `resolver/target.go:213` — no dialect or ANSI expression in the model (`CHANGE_MODEL`) vs. `planner/conversion_target.go:62` — the selected dialect cannot express it (`CHANGE_TARGET`) |
| `UNSUPPORTED_METRIC_EVALUATION` | model-level and query-level limits, plus `planner/custom_calendar_offset_sql.go:134`, where Metis's own physical mapping is incomplete and nobody outside Metis can act |

`UNSUPPORTED_EXPRESSION` is the clearest case. Its three call sites do not report
one condition under three circumstances; they report three different conditions.
Asking which action it maps to is the wrong question — the answer is that it
should not remain one code.

A worked example of the shape a split takes, subject to naming review:

```text
METRIC_NOT_FOUND
    -> METRIC_REFERENCE_NOT_FOUND   the query names a metric that does not resolve
                                    CHANGE_REQUEST
    -> METRIC_DEFINITION_MISSING    the model is expected to define it and does not
                                    CHANGE_MODEL
```

Splitting is bounded by call-site semantics, not by a target count. The goal is
that each code names one condition, not that 41 becomes as large a number as
possible.

### Proposed mapping for the other 32

Derived from call sites, not from code names.

| `CallerAction` | Codes |
| --- | --- |
| `CHANGE_REQUEST` | `INVALID_QUERY`, `INVALID_FILTER_VALUE`, `INVALID_SORT`, `PROJECT_REQUIRED`, `INCOMPATIBLE_QUERY_GRAIN`, `UNSUPPORTED_TIME_FILTER`, `UNSUPPORTED_QUERY_SHAPE`, `AMBIGUOUS_FIELD`, `AMBIGUOUS_EXPRESSION_REFERENCE`, `AMBIGUOUS_METRIC_SOURCE` |
| `CHANGE_MODEL` | `INVALID_MODEL`, `INVALID_RELATIONSHIP`, `INVALID_METRIC_EXTENSION`, `INVALID_CONVERSION_METRIC`, `INVALID_METRIC_DEFINITION_FILTER`, `INCOMPLETE_CUSTOM_CALENDAR`, `INCONSISTENT_EXPRESSION_REFERENCES`, `METRIC_DEPENDENCY_CYCLE`, `EXPRESSION_PARSE_FAILED`, `SEMANTIC_ANALYSIS_FAILED`, `UNSUPPORTED_SEMANTIC_EXTENSION`, `AMBIGUOUS_RELATIONSHIP_PATH` |
| `CHANGE_TARGET` | `TARGET_REQUIRED`, `AMBIGUOUS_TARGET`, `EXECUTION_BINDING_REQUIRED`, `EXECUTION_BINDING_NOT_FOUND`, `INVALID_EXECUTION_CONFIG`, `ENGINE_NOT_FOUND`, `UNSUPPORTED_DIALECT` |
| `AUTHENTICATE` | `UNAUTHENTICATED` |
| `REPORT_DEFECT` | `INTERNAL_INVARIANT_VIOLATION`, `INTERNAL_ERROR` |

Two placements that are not obvious:

- `AMBIGUOUS_*` splits across three actions. The caller can qualify a field name
  or pick a target; nobody but the model author can remove a second join path
  between two datasets. `CONFLICT` grouped these because all three are 409.
- `UNSUPPORTED_SEMANTIC_EXTENSION` is `CHANGE_MODEL`, not `CHANGE_TARGET`: the
  model declares a semantic-critical extension Metis will not silently drop, and
  no choice of engine changes that.

### The error payload

```json
{
  "code": "INVALID_METRIC_EXTENSION",
  "caller_action": "CHANGE_MODEL",
  "message": "...",
  "details": {"metric": "revenue"}
}
```

Four fields, each one an Agent can branch on. `suggestions` stays optional and
additive; it is prose for a human reading the transcript, and after this RFC it is
no longer the only place the remediation appears.

Both transports serialize this same `Payload`, so neither adapter needs a change
to adopt it.

### MCP placement is already correct

MCP's fullest expression of structured tool output is `structuredContent`, and the
obvious reading of *follow the MCP spec* is to put the payload there rather than
JSON-inside-a-text-block. On the evidence, that reading is wrong for an *error*
payload, for two independent reasons.

1. **The spec's own contract forbids it.** `structuredContent` must conform to the
   tool's declared `outputSchema`, and `app/mcp/server.go` derives each tool's
   output schema from its success type (`*service.SearchResult`,
   `*execution.CompiledQuery`, and so on). An error payload in `structuredContent`
   would violate the schema the tool itself advertises. Making it conform would
   mean declaring every tool's output as a union of success-or-error, degrading
   the success schema — the thing `outputSchema` exists to make precise — in order
   to carry a failure it was never about.

2. **The SDK blocks it anyway.** In `go-sdk` v1.6.1, a handler returning a non-nil
   error causes `server.go:352` to construct a *fresh* `CallToolResult` and call
   `SetError`, which populates `Content` only; the `*CallToolResult` the handler
   returned is discarded. Returning a result with `StructuredContent` set and a
   nil error does not work either: `server.go:383` assigns
   `res.StructuredContent = outJSON` unconditionally.

The spec's guidance for tool errors is the `content` block with `isError: true`,
precisely so the model can see the failure and self-correct — which is what
`encodeToolError` already does. Metis's placement is therefore already conformant,
and this RFC changes only the fields.

**Out of scope:** splitting the payload across several content blocks so a
transcript carries a human-readable `message` separately from the JSON. That is a
presentation improvement, not an error-semantics one, and mixing it in would
muddy the change. It belongs in its own change once the error contract is stable.

### HTTP status moves to `app/rest`

`app/rest` replaces `httpStatusForErrorClass` with `httpStatusForErrorCode`, backed
by an explicit exhaustive `map[serrors.ErrorCode]int`. An unregistered code fails
closed to 500, preserving RFC-0032's rule.

This produces two tables:

```text
pkg/serrors     ErrorCode -> CallerAction     "what should the Agent do next?"
app/rest        ErrorCode -> HTTP status      "how does REST express this?"
```

These are two projections of one code, answering two genuinely different
questions — not two adapters each copying a domain classification, which is what
RFC-0032 was written to prevent. The `CallerAction` table stays singular and
domain-owned; nothing about it is duplicated in `app/rest`.

This does contradict a stated consequence of ADR-0002: "Transports must not infer
behavior from code-name patterns or maintain private code classification maps."
That clause is worth reading in two halves. Inferring behavior from name patterns
stays prohibited, unconditionally — that is the substring matching RFC-0032
removed and this RFC does not restore. The second half changes: an exhaustive,
explicit, test-verified projection is neither inference nor a private semantic
judgment, and it is exactly what lets the shared contract stop being shaped like
one transport. Implementing this RFC requires a new ADR superseding that clause.

The registration cost is real: adding an error code means registering it in two
tables. A completeness test over each table keeps that a CI failure rather than a
silent 500 in production.

### No `retryable` field

No code in the current inventory is retryable in the conventional sense: every
failure is deterministic for a fixed model, query, and target.

`CHANGE_REQUEST` is not a retry signal in disguise. It means *a different request
may succeed*; `retryable: true` conventionally promises *the same request may
succeed later*. Emitting a constant-`false` `retryable` alongside `CHANGE_REQUEST`
would invite exactly the confusion between those two, against a field no Metis
failure has ever set.

When Metis grows a genuinely transient failure — a catalog fetch timeout, an
unreachable remote Ossie source — the field arrives together with the first code
that sets it to `true`.

## Alternatives

- **Keep `class` as is.** Cheapest, and defensible while REST is the only consumer
  making decisions. Rejected because the cost is paid by Agents, who are the
  primary audience and who currently cannot distinguish "retry differently" from
  "escalate to a human" without parsing codes.
- **Keep `class` and add `caller_action` alongside permanently.** Rejected as a
  steady state: two derived classifications over one code set is the drift
  RFC-0032 was written to prevent. It is the correct *transitional* state — see
  rollout step 3.
- **Split `INVALID_REQUEST` into two classes instead.** The minimal fix for the
  largest information loss, without disturbing RFC-0032's structure. Rejected
  because it treats the symptom: the same collapsing recurs in `CONFLICT` and
  `UNSUPPORTED`, and each repair drags the class vocabulary further from HTTP
  until it is a caller-action vocabulary with HTTP names.
- **Abstract the five values onto one uniform axis.** Rejected: the asymmetry
  between "artifact to edit" and "action to take" is real, and removing it costs
  the property that every value names something an Agent can do next.
- **Derive the action from runtime context where a code is ambiguous.** Rejected
  under rule 3 — it converts a published contract into a per-call one. Split the
  code instead.
- **Put HTTP status directly in `pkg/serrors`.** Rejected for the same reason
  RFC-0032 rejected it: the core must not encode a transport.
- **Emit a JSON-RPC error code from MCP for domain failures.** Rejected on MCP
  semantics: a failed tool call is a successful protocol call, and reporting it as
  a JSON-RPC error misreports the protocol layer.

## Rollout and migration

Four separable changes. Each is independently reviewable and independently
revertible; none depends on a later one having landed.

**1. Fix the semantics (this RFC).** Documentation only. Establishes rules 1–4,
the `CallerAction` definition, the nine codes requiring a split, and the transport
boundary. No code changes.

**2. Split the overloaded codes.** Resolve the nine codes above by call-site
semantics. No `CallerAction` type yet. The acceptance criterion is mechanical:
after this change, every code's remediation is determined by the code alone, which
is what makes step 3's table expressible as `CallerActionOf(code)` rather than
`CallerActionFrom(err)`.

**3. Introduce `caller_action`, keep `class`.** Add the `CallerAction` type and
`callerActionByCode`, and emit both fields:

```json
{
  "code": "INVALID_METRIC_EXTENSION",
  "class": "INVALID_REQUEST",
  "caller_action": "CHANGE_MODEL",
  "message": "..."
}
```

Purely additive; no status changes; a completeness test asserts the declared code
set and the table's key set are equal, and unregistered codes fail closed to
`REPORT_DEFECT`.

**4. REST owns `ErrorCode -> HTTP status`; remove `class`.** Replace
`httpStatusForErrorClass` with `httpStatusForErrorCode`, then delete `class`,
`ErrorClass`, `ClassOf`, and `ClassFrom`, and update
`docs/specs/public-contract.md` accordingly. RFC-0032 moves from `Implemented` to
`Superseded` at this step and not before: its contract is current behavior until
`class` is actually gone.

Steps 1–3 are reversible by deleting an additive field. Step 4 is the payload
break and is gated on step 2 having landed — until every code names one condition,
removing `class` would ship a `caller_action` that cannot be trusted.

## As implemented

All four steps landed. The design held; three findings changed the detail, and
they are recorded here rather than folded silently into the text above.

**Two of the nine "ambiguous" codes were not ambiguous.** A call-site census
before step 2 found that `UNSUPPORTED_EXPRESSION`'s four producers all report one
condition — no expression is declared for the selected target — and that
`DIMENSION_NOT_FOUND` has a single producer. The RFC had inferred different
remedies from file names rather than from the messages. Neither was split. The
real semantics, not this document's prediction, decided.

**`UNSUPPORTED_METRIC_EVALUATION` had no producer left and was retired.** It was a
stage-shaped bucket holding roughly sixty guards on planner-built structures,
seven cases where the query omitted a required time dimension, one malformed
filter value, and two `default:` branches over Metis's own enums. Attributing each
site emptied it. A code that dissolves completely under attribution was never
naming a condition — the same result rule 3 produced for `PLANNING_FAILED`.

**Three codes were added rather than four remediations guessed:**
`UNRESOLVED_SEMANTIC_REFERENCE`, `UNRESOLVED_METRIC_SOURCE`, and
`EXECUTION_CONFIG_NOT_FOUND`. The first resolves open question 1: `METRIC_NOT_FOUND`
now means only that *the request* named something unresolvable, so the lookup
codes answer statically without consulting runtime context. Open question 3 was
answered the same way — by splitting, not by choosing an action.

**The information gain is larger than this document estimated.** It argued from
`INVALID_REQUEST` holding 19 codes and four remedies. Measured against the final
43-code inventory, four of the six former classes each spanned three different
remediations, and three of the five remediations span more than one HTTP status.
Neither table is derivable from the other; a test asserts both directions, so if
that ever ceases to hold one of the two tables should be deleted rather than
maintained.

**REST status behavior is unchanged.** All 43 codes project to exactly the status
the class-derived mapping produced.

Two open questions remain open by design. Question 2 — whether five values is the
right set — is deferred to the benchmark, as recorded under follow-up. Question 4
on `structuredContent` is unchanged: still blocked by `outputSchema` and by
`go-sdk` v1.6.1.

## Test and acceptance criteria

Step 2:

- no error code has call sites implying different remediations;
- the resulting codes each name one condition, per `docs/design/interfaces/error-contract.md`.

Step 3:

- the declared `ErrorCode` set and the `callerActionByCode` key set are equal,
  asserted by a source-parsing test in the style of
  `pkg/serrors/registration_test.go`;
- the action is derived by `CallerActionOf(code)`; no code path consults anything
  but the code to choose one;
- unregistered codes and unknown Go errors yield `REPORT_DEFECT`;
- `REPORT_DEFECT` is emitted only for codes in the `INTERNAL` class, mirroring
  `pkg/serrors/internal_boundary_test.go`;
- wrapped Metis errors recover their code, and therefore their action, through
  `errors.As`;
- REST and MCP payloads carry the same `caller_action` for the same error;
- `tests/e2e/smoke.sh` asserts `caller_action` on both transports.

Step 4:

- every declared code has an explicitly registered HTTP status in `app/rest`,
  asserted by a completeness test;
- REST status behavior is unchanged for every code that survives step 2 unsplit;
- an unregistered code yields 500;
- no reference to `class` remains in `pkg/serrors`, either adapter, the specs, or
  `tests/e2e/smoke.sh`.

## Follow-up, not gates

- **Validate the value set against the Agent benchmark (#375).** Five values is a
  design decision, not a measurement. Once #375 exists, an added condition holding
  the path fixed and varying only the error payload can test whether an Agent given
  `caller_action` recovers from a failed compile more often than one given `class`,
  and whether any recovery strategy it attempts has no value in the set to name it.
  A gap found there is a reason to extend the vocabulary, not to have waited.
- **Re-check `structuredContent` at the next SDK bump.** Both blockers could
  change: a future MCP revision could define an error schema distinct from
  `outputSchema`, and the SDK's discard of the handler's result on the error path
  is arguably a bug worth reporting upstream.
- **MCP content-block presentation.** Deliberately out of scope above.

## Documentation updates

- `docs/specs/public-contract.md`
- `docs/design/interfaces/error-contract.md`
- [`docs/decisions/interfaces/0002-error-codes-and-classes.md`](../../decisions/interfaces/0002-error-codes-and-classes.md)
  — superseded by a new ADR recording that transport-shaped vocabularies do not
  belong in `pkg/serrors`, and that a transport may own an explicit exhaustive
  code-to-status projection
