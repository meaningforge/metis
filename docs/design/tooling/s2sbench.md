# S2SBench architecture

S2SBench is the community benchmark and validation CLI shipped from
`cmd/s2sbench`. It is separate from the offline `metis` commands: `metis`
compiles and inspects Ossie models, while S2SBench coordinates benchmark workloads, installed
Agents, evidence, and reports.

The executable is assembled in `cmd/s2sbench/command`. Runtime types and
behavior live below `cmd/s2sbench/bench`; these packages are implementation
details rather than a supported Go SDK. Immutable Agent adapters and skill
resources are embedded and materialized into invocation-owned temporary
directories only when an external process needs a filesystem path.

Benchmark definitions and committed evidence remain under `tests/benchmarks`.
Those tests build and execute the CLI as a black box. Production packages never
import test packages, and benchmark tests never import S2SBench command or
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

`s2sbench stop` accepts only a logical output path or its stable `runner.pid`.
Before signaling a process group it checks the manifest schema, output
association, PID, PGID, executable identity, operating-system process-start
identity, and separation from the stop command's own process group. Ambiguous
identity fails closed.
