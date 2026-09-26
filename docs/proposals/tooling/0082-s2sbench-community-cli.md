# RFC-0082: Promote S2SBench to a Community CLI

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-04
- **Last updated:** 2026-09-07
- **Scope:** `cmd/s2sbench`, `tests/conformance`, `tests/benchmarks`, build, CI, and documentation
- **Supersedes:** None

## Summary

This RFC proposes promoting S2SBench from repository-local test machinery into a first-class community CLI alongside `metis`.

The target ownership model is:

```text
cmd/
├── metis/
└── s2sbench/
    ├── main.go
    ├── command/
    └── bench/
        ├── runner/
        ├── artifact/
        ├── agent/
        ├── lifecycle/
        └── report/

tests/
├── conformance/
│   └── ...
└── benchmarks/
    ├── s2sbench/
    │   ├── scenarios/
    │   ├── fixtures/
    │   └── golden/
    ├── agent-comparison/
    └── agent-attribution/
```

The dependency model is:

```text
tests/conformance/** --imports as needed--> stable production packages

tests/benchmarks/** --executes-----------> s2sbench CLI

tests/** ------------X--------------------> cmd/s2sbench/command/**
tests/** ------------X--------------------> cmd/s2sbench/bench/**
cmd/s2sbench/** ------X--------------------> tests/**
production code -----X--------------------> tests/**
```

`cmd/s2sbench` owns executable behavior. `tests/benchmarks` owns benchmark definitions and evidence. `tests/conformance` owns Metis correctness fixtures and MAY directly import stable production packages when direct contract validation is appropriate. No production code may depend on `tests/**`, and S2SBench benchmark/CLI integration tests must consume S2SBench through its executable contract rather than importing its implementation packages.

The migration also replaces the current shell lifecycle wrappers with native CLI behavior:

```text
s2sbench run ...
s2sbench run --detach ...
s2sbench stop <output-path-or-pid-file>
s2sbench stop --force <output-path-or-pid-file>
```

After this RFC is implemented, the built binary MUST be runnable outside a Metis repository checkout and MUST NOT import, read, infer, or otherwise depend on runtime paths under `tests/**`.

## Motivation

S2SBench has evolved beyond a test helper. It now provides benchmark orchestration, local validation, artifact generation, result analysis, Agent integration, detached execution, resume behavior, stop behavior, and benchmark reporting.

Keeping this implementation under `tests/s2sbench` creates several problems:

1. The repository layout presents a reusable executable as internal test machinery.
2. Runtime behavior and benchmark data are mixed together.
3. Shell wrappers contain application behavior such as detached execution, PID handling, resume, logging, and safe termination.
4. Hard-coded repository paths prevent independent distribution.
5. The current implementation directly uses test-owned Go types, which prevents a clean executable boundary.
6. Continuing to add runtime capabilities under `tests/` increases migration cost and encourages new reverse dependencies.

Promoting S2SBench to `cmd/s2sbench` establishes an explicit executable boundary without declaring its Go packages to be a stable public SDK.

## Product Positioning

Metis exposes two community-oriented CLIs with distinct responsibilities:

| CLI | Responsibility |
| --- | --- |
| `metis` | Offline Ossie-to-SQL compilation and inspection, plus runtime services |
| `s2sbench` | Benchmark execution, local validation, result analysis, and deeper end-to-end evaluation |

S2SBench is a benchmark and validation tool. Its output MUST NOT be presented as an official TPC-DS certification result.

`cmd/s2sbench/...` packages are implementation packages. This RFC does not establish a compatibility guarantee for importing them from other modules.

## Goals

1. Make `s2sbench` independently buildable from `cmd/s2sbench`.
2. Make the built executable runnable outside a Metis checkout.
3. Separate executable behavior from benchmark definitions, fixtures, and golden evidence.
4. Remove Go dependencies from `cmd/s2sbench/**` into `tests/**`.
5. Prevent tests from importing S2SBench implementation packages while preserving direct conformance testing of stable production packages.
6. Move benchmark runtime behavior into `cmd/s2sbench/bench` and subpackages.
7. Keep CLI parsing, validation, configuration, and user-facing output under `cmd/s2sbench/command`.
8. Replace run, background, and stop shell wrappers with native commands.
9. Define explicit and safe foreground, detached, stop, forced-stop, and resume semantics.
10. Make Cobra help authoritative for commands, flags, environment variables, defaults, and precedence.
11. Preserve benchmark scenarios and fixtures as external inputs rather than compile-time knowledge of repository paths.
12. Add CI checks that continuously enforce dependency directions and runtime-path boundaries.

## Non-goals

This RFC does not:

- make `cmd/s2sbench/bench` a supported public Go SDK;
- define or claim official TPC-DS compliance or certification;
- add new benchmark methodologies or expand benchmark coverage;
- change Semantic Core planning or SQL compilation semantics;
- merge conformance tests and benchmark tests into one concept;
- require `tests/**` to avoid stable production packages when direct contract validation is appropriate;
- add a repository-wide `internal/s2sbench` SDK as part of this migration;
- retain permanent shell compatibility wrappers;
- require bundling third-party Agent executables;
- guarantee Windows process-group lifecycle support in the first implementation.

This RFC does not revive the deferred oracle-authority expansion proposed by RFC-0073.

## Ownership Model

### `cmd/s2sbench/command`

`cmd/s2sbench/command` owns the public CLI surface and process assembly:

- Cobra commands;
- flags and arguments;
- environment-variable mapping;
- configuration precedence;
- preflight validation;
- user-facing errors and output;
- wiring runtime services into commands.

It MUST NOT contain benchmark scoring logic, Agent protocol logic, artifact persistence internals, or process-lifecycle implementation beyond command assembly.

### `cmd/s2sbench/bench`

`cmd/s2sbench/bench` owns benchmark runtime behavior:

```text
runner/      benchmark protocol and orchestration
artifact/    run state, journals, manifests, publication
agent/       Agent abstractions, sessions, drivers, adapters
lifecycle/   foreground/detached lifecycle, stop, resume, identity, locks
report/      aggregation, analysis, report models
```

Exact package names MAY evolve, but the ownership split MUST remain intact.

### `tests/conformance`

`tests/conformance` owns correctness fixtures and test-only structures used to verify Metis semantic and compilation behavior.

Conformance tests MAY directly import stable production packages when the purpose is contract, semantic, parser, planner, compiler, or other direct correctness validation. Requiring every conformance check to go through CLI, HTTP, or MCP would unnecessarily weaken test precision and increase test cost.

Test-only helpers and reusable fixtures SHOULD remain under `tests/**` when they are not production responsibilities. They SHOULD NOT be promoted into production packages merely to make test reuse convenient.

S2SBench production code MUST NOT depend on `tests/conformance` packages or types.

### `tests/benchmarks`

`tests/benchmarks` owns:

- benchmark scenarios;
- benchmark fixtures;
- committed golden evidence;
- experiment definitions;
- black-box CLI integration tests;
- benchmark-specific documentation.

These resources are external inputs to the CLI, not Go dependencies of the CLI implementation.

Benchmark tests that validate S2SBench itself MUST execute the built `s2sbench` binary and MUST NOT import `cmd/s2sbench/command`, `cmd/s2sbench/bench`, or their subpackages.

Test-only benchmark helpers MAY live under `tests/**` and MAY be shared by other test packages where useful, provided they do not create a production dependency on `tests/**`.

## Package and Dependency Boundaries

The following rules are normative:

1. Production code MUST NOT import packages under `tests/**`.
2. `cmd/s2sbench/**` MUST NOT import any package whose Metis module path contains `/tests`.
3. `cmd/s2sbench/**` MUST NOT contain a fixed or inferred runtime path under `tests/**`.
4. `cmd/s2sbench/**` MUST NOT use test-owned structs such as `tests/conformance/scenarios.Scenario` or `tests/conformance/scenarios.ResultSet` as runtime domain types.
5. S2SBench MUST own neutral runtime/workload types or consume production/public types.
6. Benchmark files under `tests/benchmarks/**` MUST be loaded through explicit paths, configuration, or documented defaults outside the repository tree.
7. Unit tests under `cmd/s2sbench/**` MAY directly test their corresponding implementation packages.
8. Tests under `tests/**` MAY import stable production packages when direct contract validation is appropriate.
9. Tests under `tests/**` MUST NOT import `cmd/s2sbench/command`, `cmd/s2sbench/bench`, or their subpackages as programming interfaces.
10. S2SBench benchmark and CLI integration tests under `tests/benchmarks/**` MUST execute a built `s2sbench` binary as a black box.
11. S2SBench MUST consume Metis through production packages or public CLI/API/MCP contracts, not test-only helpers.
12. Test-only helpers SHOULD remain inside `tests/**` unless they represent a genuine production responsibility.

The black-box rule is intentionally scoped to S2SBench benchmark/CLI integration testing. It does not prohibit conformance tests from importing stable production packages directly.

## Runtime Workload Model

The migration MUST establish a S2SBench-owned neutral workload boundary before the implementation is fully moved.

The exact type names are implementation details, but the runtime model should resemble:

```go
type WorkloadCase struct {
    ID       string
    Question string
    Inputs   Inputs
}
```

Expected results, fixtures, and benchmark metadata MAY be represented by additional neutral runtime types or external references.

The important rule is ownership: runtime code under `cmd/s2sbench` must not expose or depend on Go types owned by `tests/**`.

A loader or adapter MAY translate external benchmark definitions into the neutral runtime model.

This enables repository-independent usage such as:

```bash
s2sbench run --scenario ./my-benchmark.yaml
```

without requiring the Metis repository layout.

## CLI Surface

The public command set remains:

```text
s2sbench gen
s2sbench okfgen
s2sbench run
s2sbench analyze
s2sbench report
s2sbench attribution-run
s2sbench comparison-run
s2sbench stop
```

### Parameter rules

1. Cobra help is authoritative for supported commands and parameters.
2. `run` executes in the foreground by default.
3. `--detach` explicitly requests detached execution.
4. Required Agent, provider, and model selections MUST be explicit unless a neutral documented default exists.
5. Development-specific choices such as Claude, Codex, Pi, or DeepSeek MUST NOT be silently preferred.
6. Environment variables MAY supply configuration, but help MUST describe them and their precedence.
7. Unless otherwise documented, precedence is:

   ```text
   explicit CLI flag > environment variable > documented neutral default
   ```

8. Missing required configuration MUST produce an actionable error before benchmark execution starts.
9. Sensitive values MUST NOT be persisted in logs, manifests, artifacts, or PID files.
10. Sensitive values SHOULD be supplied through environment variables or controlled secret files rather than command-line arguments visible in the process table.

## Repository-independent Runtime Resources

The CLI MUST NOT read fixed paths such as:

```text
tests/s2sbench/...
tests/benchmarks/...
tests/conformance/...
```

Runtime resources are classified as follows:

| Resource type | Treatment |
| --- | --- |
| Small immutable runtime resource | Embed with `go:embed` |
| Resource consumed directly by Go | Read from the embedded filesystem |
| Resource passed to an external process | Materialize into an invocation-owned temporary directory |
| Benchmark scenario or fixture | Require an explicit external path or documented repository-independent default |
| Run output | Require or generate a runtime output path independent of the repository |

Temporary resource extraction MUST:

- use a directory owned by the current invocation;
- use restrictive permissions where supported;
- avoid predictable shared filenames;
- clean up after normal exit and handled signals;
- avoid including secrets in filenames or content;
- produce an actionable error if extraction fails.

External Agent executables MAY remain separately installed dependencies. They MUST be checked before starting a run and documented in command help.

## Output Directory Semantics

Foreground and detached execution MUST use the same run-directory and artifact format.

For a new run:

1. The CLI generates an output directory only when the user has not supplied one.
2. The default location MUST NOT be under `tests/**`.
3. The CLI MUST NOT overwrite an existing non-resumable directory.
4. Initialization MUST use atomic file creation or write-then-rename where partial metadata would be unsafe.
5. Generated paths MUST be printed after successful initialization.

Committed golden evidence and locally generated run results MUST remain clearly separated.

### Stable result and control paths

`--output <path>` names the logical final result directory even while the run is active.

```text
<output>.s2sbench/
  manifest.json
  runner.pid
  console.log
  run.lock
<output>.partial/
<output>/
```

The stable control directory is never renamed with result artifacts.

PID, process identity, current attempt, log location, lock ownership, and `run_state` are recorded there.

`stop <output>` MUST derive `<output>.s2sbench/manifest.json` without requiring `<output>` itself to exist.

Passing `<output>.s2sbench/runner.pid` MUST resolve the same manifest through the control directory. A raw numeric PID or unrelated PID file is not a valid control target.

## Single-writer Run Lease

Every fresh start and resume MUST acquire an exclusive single-writer lease before validating or mutating run state.

The lease MUST:

- be acquired atomically;
- cover the stable control directory and result artifacts for that logical output;
- be held for the complete run lifetime;
- prevent two concurrent fresh starts from using the same output;
- prevent two concurrent resumes from both passing an inactive-run check;
- fail closed when ownership cannot be established.

Stale-lock recovery MUST use the same process-identity model used by `stop`. A lock MUST NOT be considered stale solely because a timestamp is old.

A second start or resume targeting a currently leased run MUST fail with an actionable error before spawning any benchmark child process.

## Crash-consistent Result Publication

Publishing successful results MUST be crash-consistent.

The implementation MUST NOT rely on an uncoordinated two-step sequence where `<output>.partial` is renamed to `<output>` and the process may crash before durable run-state metadata reflects that publication.

The lifecycle MUST include an explicit durable publication protocol, for example:

```text
running -> publishing -> completed
```

or an equivalent completion marker/state transition.

The exact mechanism is an implementation detail, but recovery MUST be deterministic if the process crashes:

- before publication starts;
- after result rename but before final manifest update;
- after completion metadata is persisted.

On restart or resume, S2SBench MUST reconcile the stable control state and result directories without silently overwriting or duplicating completed artifacts.

## Detached Execution

`s2sbench run --detach` replaces the background shell wrapper.

It MUST provide:

- a new Unix session using `setsid` or Go's equivalent `SysProcAttr.Setsid`;
- a child that is the process-group leader;
- no controlling-terminal dependency after startup;
- stdin redirected from `/dev/null` before child initialization;
- stdout and stderr redirected to the console log before child initialization;
- the same validation and configuration behavior as foreground execution;
- output-directory collision protection and single-writer lease acquisition;
- atomic PID, PGID, manifest, and lease metadata persistence;
- `--resume` support according to this RFC;
- preflight checks for required Agent executables and resources;
- final output containing run directory, log path, PID metadata path, and stop command.

### Readiness handshake

The parent MUST NOT report detached startup success immediately after spawning the child.

It MUST wait for an explicit child startup handshake confirming:

1. configuration and inputs are valid;
2. output/control state is initialized;
3. the run lease is held;
4. PID/PGID and run metadata are durably written;
5. runtime resources and Agent executables passed preflight checks;
6. the runner reached its ready state.

The readiness handshake MUST use a dedicated pipe or equivalent control channel, not stdin, stdout, or stderr.

The parent creates and opens the console log before spawning the child and redirects standard streams before child validation.

### Ownership before readiness

Before readiness is acknowledged, the detached child remains owned by the launching parent.

The readiness wait MUST have a bounded documented timeout.

If the parent receives or observes any of the following before readiness:

- startup timeout;
- SIGINT or SIGTERM;
- control-channel EOF;
- malformed handshake;
- explicit child startup failure;
- any other unrecoverable parent-side error;

then the parent MUST terminate the child's verified process group, wait/reap the child, release the run lease if safe, and return non-zero.

The parent MUST NOT leave an unreported detached run behind before the user has received successful startup metadata.

After readiness succeeds, closing the launching terminal MUST NOT terminate the detached run or its descendants.

The implementation SHOULD re-execute the current binary using `os.Executable` and a hidden internal child mode rather than requiring a shell wrapper.

## Run Manifest and Process Identity

PID reuse makes a numeric PID insufficient for process control.

Each detached run MUST persist a versioned manifest with at least:

```text
schema_version
run_id
pid
pgid
started_at
executable_identity
process_start_identity
output_directory
run_state
```

On supported lifecycle platforms, canonical executable identity and operating-system process-start identity MUST be persisted and verified.

A random run nonce MAY be used as an additional identity signal.

The manifest MUST NOT contain API keys, access tokens, secret environment variables, authorization headers, or an unredacted command line.

## Stop Semantics

`s2sbench stop` replaces the stop shell wrapper.

Accepted targets are:

```text
s2sbench stop <output-path>
s2sbench stop <pid-file>
s2sbench stop --force <output-path>
s2sbench stop --force <pid-file>
```

A PID-file target MUST resolve to the corresponding run manifest.

The CLI MUST NOT signal a process using only an unverified numeric PID.

Before signaling, the command MUST verify:

- PID syntax and range;
- process existence;
- process-start identity where supported;
- executable identity;
- PID-to-PGID relationship;
- association with the expected run directory and manifest;
- that the target group is not the stop command's own process group.

Default behavior:

1. Send `TERM` to the verified process group.
2. Wait for a bounded documented interval.
3. Return success after confirming termination.
4. If the timeout expires, return an actionable error suggesting `--force`.

`--force` behavior:

1. Send `KILL` to the verified process group.
2. Wait for and confirm termination.
3. Return an error if termination cannot be confirmed.

Stopping an already exited run is idempotent and returns success after stale state is safely identified.

Identity mismatch, PID reuse, malformed metadata, unsafe group identity, or ambiguous ownership MUST fail closed without signaling.

Initial process-group support MAY be limited to Linux and macOS. Unsupported platforms MUST fail explicitly or exclude unsupported lifecycle code with build constraints.

## Resume Semantics

`--resume` explicitly permits reuse of compatible incomplete run state. It does not mean “ignore output collisions.”

Resume MUST acquire the single-writer lease before making active/inactive or compatibility decisions.

Resume MUST verify:

- a valid and supported manifest schema;
- that no verified active run currently owns the lease;
- compatibility of benchmark identity;
- compatibility of configuration;
- compatibility of input fixtures;
- compatibility of artifact schema;
- compatibility of S2SBench version where required;
- deterministic reconciliation of publication state;
- completion state of each benchmark case.

Completed cases MAY be skipped. Failed or incomplete cases MAY be retried according to the benchmark protocol.

The selected retry/skip policy MUST be deterministic and recorded in run artifacts.

If configuration, fixture identity, or benchmark identity differs, resume MUST reject the run unless a future RFC defines an explicit safe override.

## Security and Privacy

S2SBench MUST apply a centralized redaction policy to console output, logs, manifests, and generated diagnostic metadata.

At minimum:

- complete process environments MUST NOT be persisted;
- secret-bearing CLI arguments MUST NOT be recorded verbatim;
- provider credentials and authorization headers MUST be redacted;
- PID, manifest, and lock files MUST use restrictive permissions where supported;
- materialized embedded resources MUST use invocation-owned temporary directories;
- path canonicalization and symlink handling MUST prevent accidental writes outside the selected run directory;
- stop and stale-lock recovery MUST fail closed when identity cannot be established.

## Shell Wrapper Removal

After native lifecycle behavior reaches parity, the following legacy entry points will be removed:

```text
tests/s2sbench/run.sh
tests/s2sbench/run-background.sh
tests/s2sbench/stop-background.sh
```

Their replacements are:

| Removed entry point | Replacement |
| --- | --- |
| `run.sh` | `s2sbench run ...` |
| `run-background.sh` | `s2sbench run --detach ...` |
| `stop-background.sh` | `s2sbench stop [--force] <target>` |

No permanent compatibility wrapper remains after cutover.

## Build Integration

The Makefile will provide an explicit build target, for example:

```make
s2sbench-build:
	CGO_ENABLED=1 go build -tags duckdb -o bin/s2sbench ./cmd/s2sbench
```

Supported development entry points include:

```bash
go run ./cmd/s2sbench --help
CGO_ENABLED=1 go run -tags duckdb ./cmd/s2sbench run ...
make s2sbench-build
bin/s2sbench --help
```

Distribution documentation MUST state CGO requirements, DuckDB build tags, supported operating systems, and required external Agent executables.

## Dependency Boundary Enforcement

CI MUST enforce production-to-tests, command-to-tests, and tests-to-command-internals boundaries while still permitting tests to import stable production packages for direct correctness validation.

### Production-to-tests boundary

Production packages MUST NOT import `tests/**`. The canonical boundary checker MUST cover production package imports broadly enough that moving code outside `cmd/s2sbench` cannot create an unnoticed reverse dependency into tests.

### Command-to-tests boundary

Checks MUST cover at least:

```bash
go list -deps ./cmd/s2sbench/...
go list -deps -test ./cmd/s2sbench/...
rg -n 'tests/' cmd/s2sbench
```

The Go dependency output MUST NOT contain a Metis package under `/tests`.

The source scan MUST reject repository-relative `tests/` runtime paths anywhere under `cmd/s2sbench`.

### Tests-to-command-internals boundary

CI MUST reject imports from `tests/**` into S2SBench implementation packages, including:

```text
<module>/cmd/s2sbench/command
<module>/cmd/s2sbench/command/...
<module>/cmd/s2sbench/bench
<module>/cmd/s2sbench/bench/...
```

This restriction does not prohibit imports from `tests/**` into stable production packages outside S2SBench command internals. Conformance tests MAY continue to use direct production-package imports when that is the appropriate validation surface.

### Negative boundary tests

The boundary checks MUST have one canonical entry point such as:

```text
make s2sbench-boundary-check
```

Automated negative tests MUST prove that the gate rejects:

1. a forbidden production or `cmd/s2sbench -> tests/**` Go import;
2. a forbidden repository-relative `tests/**` runtime path under `cmd/s2sbench`;
3. a forbidden `tests/** -> cmd/s2sbench/command...` or `tests/** -> cmd/s2sbench/bench...` import.

The same gate MUST NOT reject an allowed `tests/conformance/** -> stable production package` import.

Violating fixtures MUST NOT live in the normal production source tree.

## Implementation Plan

The migration is delivered as independently reviewable changes.

### Phase 0: Establish the runtime boundary

Before the mechanical move, remove test-owned Go types from S2SBench runtime interfaces.

Work includes:

- define neutral S2SBench workload/result/runtime types;
- add loaders/adapters for existing benchmark definitions;
- remove direct dependencies on `tests/conformance/scenarios` and other `tests/**` packages from S2SBench runtime code;
- preserve legitimate conformance-test imports of stable production packages;
- preserve current benchmark behavior while changing ownership boundaries;
- add initial dependency gates.

Exit criteria:

- current S2SBench runtime code can compile without importing any `tests/**` package;
- neutral runtime types are owned outside `tests/**`;
- conformance tests remain free to validate stable production packages directly;
- benchmark behavior remains equivalent for existing scenarios.

### Phase 1: Mechanical package migration

- Create `cmd/s2sbench`, `command`, and `bench` package structure.
- Move runtime implementation and package-level unit tests according to ownership.
- Preserve command behavior.
- Keep benchmark fixtures/scenarios/golden data under `tests/benchmarks`.
- Temporarily update repository callers to invoke the new command path.
- Keep legacy background/stop wrappers operational until native lifecycle parity is complete.

Exit criteria:

- `go test ./cmd/s2sbench/...` passes;
- the new CLI builds and shows help;
- `cmd/s2sbench/**` has no dependency on `tests/**`;
- command code and benchmark runtime code are separated according to this RFC.

### Phase 2: Repository-independent runtime

- classify runtime resources;
- embed immutable runtime resources;
- materialize resources needed by external processes;
- replace repository defaults with explicit paths or repository-independent defaults;
- add preflight validation for external Agent executables;
- add an external-directory smoke test.

Exit criteria:

- source scans return no forbidden runtime paths;
- a real foreground smoke benchmark succeeds using a copied binary outside the repository;
- missing external dependencies produce actionable errors.

### Phase 3: Native lifecycle

- implement the single-writer run lease;
- implement `run --detach` with a new Unix session and detached standard streams;
- implement bounded parent-child readiness handshake and pre-readiness ownership cleanup;
- implement stable control state and versioned manifest;
- implement crash-consistent result publication;
- implement safe `stop` and `stop --force`;
- implement deterministic `--resume` validation and reconciliation;
- add PID-reuse, stale-lock, malformed-manifest, abnormal-child-exit, and publication fault tests.

Exit criteria:

- detached startup reports success only after readiness;
- startup timeout or parent interruption before readiness leaves no detached orphan;
- closing the launching terminal after readiness does not terminate the run;
- stop and force-stop safely control the intended process group;
- repeated stop is idempotent;
- concurrent start/resume is rejected by the single-writer lease;
- resume rejects incompatible or active runs;
- publication recovery is deterministic across injected crash points.

### Phase 4: Cutover and cleanup

- update Makefile, CI, README, `AGENTS.md`, and benchmark documentation;
- migrate all repository callers to the built CLI;
- move benchmark definitions/fixtures/golden evidence to `tests/benchmarks` ownership;
- delete legacy shell wrappers;
- delete the old `tests/s2sbench` implementation directory;
- enable final source and dependency gates as required CI checks.

Exit criteria:

- no old entry-point references remain;
- no permanent compatibility wrapper remains;
- S2SBench benchmark/CLI integration tests use the CLI as a black box;
- no test imports S2SBench implementation packages;
- conformance tests retain direct access to stable production packages where appropriate;
- the full verification matrix passes.

## Verification Plan

### Package verification

```bash
go test ./cmd/s2sbench/...
CGO_ENABLED=1 go test -tags duckdb ./cmd/s2sbench/...
make s2sbench-build
bin/s2sbench --help
bin/s2sbench run --help
bin/s2sbench stop --help
```

### Boundary verification

```bash
rg -n 'tests/' cmd/s2sbench
go list -deps ./cmd/s2sbench/...
go list -deps -test ./cmd/s2sbench/...
make s2sbench-boundary-check
```

The boundary checker MUST also verify that tests do not import S2SBench command internals and MUST include a positive fixture proving that conformance tests may import an allowed stable production package.

### Project verification

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go vet ./...
```

If migration intentionally changes module metadata, the tidy check MUST compare against the reviewed intended state.

### Required smoke scenarios

At least the following scenarios MUST pass before final cutover:

1. A real foreground benchmark from the repository.
2. The same benchmark using a copied binary from an otherwise empty temporary directory.
3. A detached run followed by graceful `stop`.
4. A detached run followed by `stop --force`.
5. Repeated stop after the task has exited.
6. Resume of an interrupted compatible run.
7. Rejection of resume for an active or incompatible run.
8. Rejection of a stale/reused PID or mismatched process identity.
9. Output paths containing spaces and canonicalized absolute paths.
10. Collision handling for existing empty and non-empty output directories.
11. A detached run remains alive after the launching terminal exits.
12. Startup readiness timeout terminates and reaps the pre-ready child.
13. Parent SIGINT/SIGTERM before readiness terminates and reaps the pre-ready child.
14. Two concurrent fresh starts targeting the same output do not both start.
15. Two concurrent resumes targeting the same output do not both start.
16. Stop resolves the same stable manifest before and after result publication.
17. Crash after result rename but before final completion state is deterministically reconciled.
18. The boundary gate rejects a forbidden production/command-to-tests import.
19. The boundary gate rejects a forbidden runtime `tests/**` path.
20. The boundary gate rejects a forbidden tests-to-command-internals import.
21. The boundary gate accepts a conformance test importing an allowed stable production package.

## Compatibility and Rollout

This is an intentional breaking change for repository-local development entry points.

The project will update in-repository callers before deleting the old directory. It will not maintain two source trees or permanent shell compatibility wrappers.

Artifact and manifest formats introduced by the native CLI MUST be versioned.

Compatibility across versions MAY be rejected with an actionable error if safe migration is unavailable.

The implementation sequence provides rollback points before final cutover. After Phase 4, rollback should revert the cutover commit rather than recreate compatibility wrappers manually.

## Alternatives Considered

### Keep S2SBench under `tests/`

Rejected because S2SBench is now an executable tool with runtime lifecycle and distribution concerns, not merely a test helper.

### Move all of `tests/s2sbench` mechanically into `cmd/s2sbench`

Rejected because the current implementation directly depends on test-owned types and would preserve the wrong dependency boundary under a new path.

### Keep shell wrappers around a Go runner

Rejected because detached lifecycle, process safety, resume, locking, and diagnostics are executable behavior and require one native implementation.

### Move everything into `internal/s2sbench`

Deferred. The current goal is a cohesive executable, not a repository-wide internal SDK. If multiple production consumers later need the runtime, a separate RFC can revisit that boundary.

### Allow S2SBench tests to import `cmd/s2sbench/bench`

Rejected because this would create an implicit Go SDK and allow benchmark/CLI tests to bypass the actual executable contract.

### Require all tests to use only black-box interfaces

Rejected. Conformance tests need precise and efficient validation of stable production contracts, and direct production-package imports are appropriate for that purpose. The black-box requirement applies specifically to S2SBench benchmark/CLI integration tests.

## Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| Test-owned types survive inside command runtime | Add Phase 0 and explicit neutral runtime types |
| Behavior changes hidden inside migration | Separate runtime-boundary work, mechanical move, resource work, lifecycle work, and cutover |
| S2SBench tests become coupled to command internals | Enforce reverse dependency boundary in CI |
| Over-broad boundary rules weaken conformance testing | Explicitly allow tests to import stable production packages where direct validation is appropriate |
| PID reuse signals an unrelated process | Verify manifest, executable identity, process-start identity, PID, and PGID |
| Detached parent exits before child readiness | Bounded handshake plus mandatory pre-readiness child cleanup |
| Concurrent resume corrupts one run | Atomic single-writer lease held for run lifetime |
| Crash during final publication leaves inconsistent state | Durable publication state/marker and deterministic reconciliation |
| Embedded resources only work in repository | Required external-directory smoke test |
| Secrets leak into metadata or logs | Central redaction and prohibition on persisted environments/unredacted commands |
| CGO or process groups reduce portability | Document supported platforms and fail explicitly where unsupported |

## Completion Criteria

This RFC is complete when all of the following are true:

- `s2sbench` builds independently from `cmd/s2sbench`;
- the built binary runs outside a Metis checkout;
- production code does not import `tests/**`;
- `cmd/s2sbench/**` neither imports nor reads nor infers any `tests/**` path;
- S2SBench runtime interfaces no longer use test-owned Go types;
- command assembly and benchmark runtime have clear package ownership;
- benchmark definitions, fixtures, and golden evidence are owned by `tests/benchmarks`;
- conformance fixtures remain owned by `tests/conformance`;
- conformance tests may directly validate stable production packages where appropriate;
- S2SBench benchmark/CLI integration tests execute the built CLI as a black box;
- tests do not import S2SBench command/runtime implementation packages;
- foreground, detached, stop, force-stop, and resume behaviors are implemented and verified;
- detached runs use a new session and survive launching-terminal closure after readiness;
- pre-readiness parent failure cannot leave an unreported detached run;
- single-writer locking prevents concurrent ownership of one run;
- result publication is crash-consistent and recoverable;
- manifest, PID, lock, and log discovery remain stable across the run lifecycle;
- detached process control fails closed when process identity is uncertain;
- sensitive information is absent from logs, manifests, artifacts, and PID metadata;
- all former shell wrappers are deleted;
- the former `tests/s2sbench` implementation directory is removed;
- package, boundary, full-project, and smoke verification all pass.

## Decision

If accepted, S2SBench becomes a first-class community CLI owned by `cmd/s2sbench`.

`cmd/s2sbench/command` owns CLI behavior. `cmd/s2sbench/bench` owns benchmark runtime behavior. `tests/benchmarks` owns benchmark definitions and evidence. `tests/conformance` owns Metis correctness fixtures and may directly import stable production packages for appropriate contract validation.

Production code never depends on `tests/**`. Tests do not import S2SBench implementation packages. S2SBench benchmark/CLI integration tests consume S2SBench through its executable contract, while conformance tests retain direct access to stable production packages where that is the correct validation surface.
