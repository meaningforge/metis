# S2SBench CLI contract

This specification defines the current executable and lifecycle contract for
the S2SBench community CLI.

## Commands and configuration

The public commands are `gen`, `okfgen`, `run`, `analyze`, `report`,
`attribution-run`, `comparison-run`, and `stop`. Cobra help is authoritative
for flags and defaults. Agent, provider, model, and model-version selections
are explicit. `run` executes in the foreground unless `--detach` is present.
When `--output` is omitted, S2SBench generates a path below
`./s2sbench-results`; caller-provided relative paths resolve from the caller's
working directory.

The binary must work outside a Metis checkout. It may consume benchmark inputs
only through explicit paths or documented repository-independent defaults. It
must not import test-owned Go packages or contain a repository-relative test
runtime path. Resources required by external Agents are extracted with private
permissions into an invocation-owned temporary directory and removed on normal
or handled-signal exit.

## Run lifecycle

A fresh run refuses existing result, partial-result, or prior control state. A
resume requires a supported lifecycle manifest and a compatible incomplete
artifact. Compatibility covers the workload identity and frozen run
configuration; incompatible resumes fail without changing benchmark evidence.

The stable control manifest records its schema, artifact schema, random run
identity, PID, PGID, start time, canonical executable identity,
operating-system process-start identity, canonical output path, run state,
compatibility digest, deterministic retry policy, and attempt number. Control,
PID, lock, and log files use owner-only permissions.

Only one process may own an output. A lease is acquired atomically and held for
the complete runtime. A stale lease is recovered only when the recorded
process is absent; a live process with mismatched or unverifiable identity is
ambiguous and fails closed.

Successful publication persists `publishing`, renames the partial directory to
the final output, then persists `completed`. Resume treats a publishing state
with a final directory and no partial directory as completed. Any other
ambiguous publication topology fails closed.

## Detached run and stop

Detached mode creates a new Unix session, makes the child its process-group
leader, and redirects stdin from the null device and stdout/stderr to the
stable console log before child initialization. A dedicated readiness channel
has a 30-second startup timeout. Failure, malformed or closed handshake,
timeout, or parent interruption before readiness terminates and reaps the
unready child.

Graceful stop sends TERM to a fully verified process group and waits five
seconds by default; timeout errors direct the caller to `--force`. Forced stop
sends KILL and still confirms termination. An already exited, safely identified
run is an idempotent success. A raw PID, unrelated PID file, reused PID,
identity mismatch, malformed manifest, unsafe process group, or ambiguous owner
is rejected without signaling.

Native lifecycle control is supported on Linux and macOS. Other platforms fail
explicitly.

## Dependency enforcement

`make s2sbench-boundary-check` is the canonical gate. It checks production Go
imports, command production and test dependencies, runtime-path source text,
and imports from test packages into S2SBench internals. Its matcher tests prove
the three forbidden directions and preserve the permitted conformance-test to
stable-production direction.

## SQL and parameter transport

Compiled responses retain SQL placeholders and an ordered `parameters` array.
S2SBench preserves both through Agent handoff, execution, retry evidence, and
persisted attempt records. Query fingerprints include parameter values as well
as SQL. The fixture execution interface passes bindings separately to the
underlying driver; it does not interpolate values into SQL text.

External tools replaying an attempt must bind `parameters` in order. SQL alone
is sufficient only when the parameter array is empty. JSON readers preserve
numeric values without passing large integers through floating-point decoding.
