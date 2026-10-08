# A2SBench architecture

A2SBench (Agent-to-SQL Benchmark) is the engine-neutral agent-evaluation CLI shipped from
`cmd/a2sbench`. It is separate from the offline `metis` commands: `metis`
compiles and inspects Ossie models, while A2SBench coordinates benchmark workloads, installed
Agents, evidence, and reports. It does not create a second semantic authoring,
compilation, query-serving or project-regression implementation.

The benchmark owns frozen business questions, agent/model identity, budgets,
attempts, independently reviewed expected results, scoring and reproducible
evidence. Interface adapters own engine-specific tool names, authentication,
request/response decoding and runtime assembly. Physical database fixtures are
execution targets, a separate dimension from the semantic interface under test.
Adding another semantic engine must add an explicit adapter, not a Metis-shaped
requirement to the benchmark protocol or a new scoring rule favoring that engine.

The current executable implements OKF assets and Metis MCP adapters. Some frozen
suites, scenario queries and evidence decoders are Metis-specific and remain
identified as such; renaming does not establish arbitrary-engine interoperability.
Agent-driver extension points are not semantic-engine adapters. The existing
result oracle judges independently reviewed results rather than matching Metis SQL.

CLI/build/package names use `a2sbench` only; no `s2sbench` command alias is shipped.
Existing versioned artifact/protocol identifiers and the `.s2sbench` lifecycle
directory are deliberately retained so a branding change does not silently
rewrite evidence, break wrappers or allow two differently named control leases
to own one output. Executable identity checks still apply: do not resume/stop a
live old executable with a different binary. Complete or stop existing runs
with the executable that started them before upgrading.

The executable is assembled in `cmd/a2sbench/command`. Runtime types and
behavior live below `cmd/a2sbench/bench`; these packages are implementation
details rather than a supported Go SDK. Immutable Agent adapters and skill
resources are embedded and materialized into invocation-owned temporary
directories only when an external process needs a filesystem path.

Benchmark definitions and committed evidence remain under `tests/benchmarks`.
Those tests build and execute the CLI as a black box. Production packages never
import test packages, and benchmark tests never import A2SBench command or
runtime implementation packages.

Each logical output has three paths:

```text
<output>.s2sbench/  stable lifecycle manifest, PID, lease, and console log
<output>.partial/   crash-recoverable incremental evidence
<output>/           immutable published result
```

Fresh runs and resumes acquire an atomic single-writer lease before inspecting
or mutating run state. Publication transitions durably from `running` through
`publishing` to `completed`, allowing resume to reconcile a crash after the
directory rename but before the final manifest update.

Detached execution re-executes the current binary in a new Unix session. The
launching process redirects all standard streams before the child initializes
and waits on a dedicated bounded readiness pipe. Until the child reports that
validation, preflight, lease acquisition, control metadata, and runner setup
are complete, the launching process owns and cleans up the child process group.

`a2sbench stop` accepts only a logical output path or its stable `runner.pid`.
Before signaling a process group it checks the manifest schema, output
association, PID, PGID, executable identity, operating-system process-start
identity, and separation from the stop command's own process group. Ambiguous
identity fails closed.
