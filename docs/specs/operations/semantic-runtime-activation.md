# Semantic Runtime Generations

Metis provides process-local semantic replacement through
[`Manager.Replace`](../../../app/service/runtime/manager.go). It accepts a
validated, single-Project manifest, its content digest, and an expected generation.
It is an embedding API. There is no runtime administration HTTP endpoint or
persistent generation history in this repository.

## Project generations

Each registered Project owns an independent immutable `Generation`: a
`SemanticManifest`, `SemanticGraph`, discovery index, and semantic services.
A Runtime can host multiple Projects. A Project may reference one DataSource;
semantic generations and database connections have separate lifetimes.

The Manager builds a complete replacement before exposing it. Replacement
requires authorization for the `activate` action and an exact expected
Project generation. Invalid input, construction failure, cancellation, or a
stale expected generation leaves the current generation unchanged. A successful
swap advances only that Project's generation.

Execution resources remain process-owned: Runner, DataSource runtimes,
connection pools, secret resolution, and admission state are reused across
semantic replacements. Replacing semantics does not reconfigure connections.
The caller owns validation and retention of source material passed to `Replace`.

## Request consistency

`Manager.Pin(ctx)` installs a lazy request-local cache. The first access to a
Project captures its generation; subsequent operations on that Project in the
same scope use that generation even if replacement occurs concurrently.
A request can capture different Projects independently. There is no
cross-Project transactional snapshot or deployment-wide generation counter.

Re-pinning with the same Manager preserves the existing scope. Pinning a context
with a different Manager creates a new scope owned by that Manager and leaves
the parent context unchanged. This prevents one embedded Runtime from resolving
another Runtime's semantic state.

`Current(projectID)` returns the current Project generation. Embedders should
capture it once for a logical operation, or use `Pin` and `FromContext`.
The legacy service fields on `bootstrap.Runtime` describe the cold-bootstrap
generation; reload-aware integrations use `Generations`.

## Verification

[`manager_test.go`](../../../app/service/runtime/manager_test.go) covers
replacement preconditions, generation isolation, failed builds, and request
pinning, including contexts crossing Manager instances. Source loading is
described in [semantic source authoring](../semantic/asset-authoring-lifecycle.md).
