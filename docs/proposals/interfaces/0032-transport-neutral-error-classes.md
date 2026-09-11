# RFC-0032: Transport-neutral Error Classes

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Superseded
- **Owners:** Metis maintainers
- **Created:** 2026-08-20
- **Last updated:** 2026-09-03
- **Superseded by:** [`RFC-0034`](0034-caller-action-error-contract.md) on
  2026-08-21; `ErrorClass` no longer exists
- **Scope:** `pkg/serrors`, REST, MCP, and the public error payload
- **Supersedes:** None

## Summary

Add a small stable error-class vocabulary alongside precise Metis error codes.
Every REST and MCP error payload carries both values. Transports derive coarse
behavior from the class and never parse the spelling of an error code.

## Motivation

REST previously inferred `404` and `409` with substring checks against error
codes. That made an unrelated future code containing `NOT_FOUND` or `AMBIGUOUS`
silently acquire protocol behavior, while REST and MCP had no shared coarse
classification contract.

## Design

`pkg/serrors` owns five protocol-neutral classes: `InvalidRequest`,
`NotFound`, `Conflict`, `Unsupported`, and `InternalError`. Stable codes are mapped
explicitly. Unknown Go errors and unregistered typed codes fail closed as
`InternalError`.

The serialized payload adds `class` without changing existing code strings,
messages, details, or suggestions. REST maps the class to HTTP status. MCP
serializes the same payload and does not invent a separate taxonomy.

## Alternatives

- Parsing code suffixes was rejected because names are identifiers, not a type
  hierarchy.
- Storing an independently supplied class on every error value was rejected
  because code and class could disagree at individual call sites.
- Putting HTTP status in `pkg/serrors` was rejected because the shared semantic
  contract must remain transport-neutral.

## Rollout and migration

The payload field is additive. Existing HTTP status behavior is preserved for
all registered codes. Future error codes must receive an explicit class before
they can intentionally expose non-500 REST behavior.

## Test and acceptance criteria

- every current stable code has an asserted class;
- wrapped Metis errors preserve their class;
- unknown errors and unregistered codes map to `InternalError` and HTTP 500;
- REST status mapping consumes only the class;
- MCP and REST expose the same class in the shared payload.

## Documentation updates

- `docs/specs/public-contract.md`
- `docs/design/interfaces/error-contract.md`
- ADR-0002
