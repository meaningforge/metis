# Agent-facing semantic extension errors

RFC-0021 defines `unsupported-critical` as a fail-closed semantic result. This document describes how that internal result crosses the shared service boundary and reaches REST and MCP clients.

## Boundary

The extension runtime keeps target-capability evidence in `extension.UnsupportedError`. The shared compile service maps that internal error to the existing transport-neutral `serrors.Error` contract before it leaves `Compile`, `Validate`, or `Explain`.

The mapping preserves both typed views in the Go error chain:

```text
extension.UnsupportedError
        |
        | service boundary mapping
        v
serrors.Error
  code = UNSUPPORTED_SEMANTIC_EXTENSION
        |
        +---- REST -> serrors.Payload
        |
        +---- MCP  -> the same serrors.Payload JSON
```

This keeps the `extension` package independent of transport concerns while allowing internal callers to inspect the original capability resolution with `errors.As`.

## Stable payload

Agent-facing errors use code `UNSUPPORTED_SEMANTIC_EXTENSION` and expose only bounded diagnostic fields:

| Field | Meaning |
| --- | --- |
| `extension_identity` | normalized extension identity |
| `capability` | required semantic capability |
| `version` | required/observed extension version |
| `engine` | selected semantic engine |
| `dialect` | selected SQL dialect |
| `reason` | stable unsupported reason such as `capability_missing` or `version_incompatible` |

Raw `CustomExtension.data` is deliberately excluded. Agents should branch on `code` and structured `details`; they must not parse human-readable error strings.

## Transport behavior

REST and MCP intentionally share `serrors.PayloadFrom` semantics. The service layer owns the extension-to-semantic-error conversion; transport adapters do not duplicate capability policy.

REST treats `UNSUPPORTED_SEMANTIC_EXTENSION` as a client-visible semantic request failure and returns the shared payload. MCP encodes the same payload as the tool error content with `isError=true`.

Therefore an unsupported critical extension has one semantic contract regardless of whether an Agent calls REST or MCP.

## Non-goals

This mapping does not expose extension registries, registrations, raw vendor payloads, database credentials, or execution internals. It also does not move extension interpretation into REST/MCP; capability resolution remains in the shared semantic preparation path.

See [`extensions.md`](extensions.md) for the `CustomExtension -> Interpreter -> Requirement -> Registry/Registration -> Resolution` runtime model.
