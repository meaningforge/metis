# ADR-0003: Remediation Is Domain-owned, Transport Behavior Is a Projection

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-21
**Last reviewed:** 2026-08-21
**Supersedes:** [`0002-error-codes-and-classes.md`](0002-error-codes-and-classes.md)

## Context

ADR-0002 put a transport-neutral `ErrorClass` in `serrors` so REST would stop
choosing HTTP status by substring-matching code names. That goal was right and is
preserved here. The vocabulary was not.

The six classes — `INVALID_REQUEST`, `UNAUTHENTICATED`, `NOT_FOUND`, `CONFLICT`,
`UNSUPPORTED`, `INTERNAL` — were the names of HTTP 400, 401, 404, 409, 422, and
500, and mapped one-to-one onto them in both directions. A value set that is a
bijection with a status set is HTTP's taxonomy with the transport identifier
removed, not a transport-neutral concept that happens to suit HTTP.

Two costs followed. Four of the six classes each spanned three different
remediations, so the class could not tell a caller who must act:
`METRIC_NOT_FOUND` and `RELATIONSHIP_NOT_FOUND` shared a class and a status while
one is the caller's to fix and the other only the model author's. And the
centralization argument required a second reader that never appeared — REST was
the only consumer that ever branched on a class, and MCP serialized the payload
without reading it.

## Decision

`serrors` owns `CallerAction`: the next machine-actionable remediation for a
failure, derived centrally from the code alone.

Transport behavior is a projection of the code, owned by the transport.
`app/httperr` holds an exhaustive `map[serrors.ErrorCode]int` — a package rather
than a function inside `app/rest`, because the bearer middleware in `app/auth`
also produces HTTP error responses and the two must not import each other.
`serrors` holds no HTTP vocabulary.

Two rules from ADR-0002 are kept unchanged, because they were never the part that
was wrong:

- a transport must not derive behavior by parsing a code name;
- unknown errors and unregistered codes fail closed — to `REPORT_DEFECT` and 500.

One rule is reversed. ADR-0002 said transports must not "maintain private code
classification maps". A transport may now own an explicit, exhaustive,
test-verified code-to-status table. That is not the prohibited thing: inference
from spelling stays banned unconditionally, and an enumerated projection is a
transport writing down its own view of a shared contract rather than duplicating
a semantic judgment. It is what allows the shared contract to stop being shaped
like one transport.

`CallerAction` is derived by `CallerActionOf(code)`, never from an error value. A
function taking an `error` would make the contract `code + runtime context ->
action`, so the same code could reach a client with different meanings on
different calls. Where a code appears to need that, the code is too broad and is
split instead.

## Consequences

- Error-code strings remain the precise compatibility contract.
- Clients branch on a five-value remediation vocabulary that says who must act.
- Adding a stable error code requires two explicit decisions — its remediation
  and its status — each enforced by a completeness test.
- `class`, `ErrorClass`, `ClassOf`, and `ClassFrom` are removed. The payload
  carries `code` and `caller_action`.
- Exactly one place in the tree chooses an HTTP status for a Metis error, enforced
  by a test that reads every other package.
- The two tables are independent in both directions: one status covers several
  remediations, and one remediation spans several statuses. A test asserts this,
  because if either became derivable from the other, one table should be deleted
  rather than maintained.
