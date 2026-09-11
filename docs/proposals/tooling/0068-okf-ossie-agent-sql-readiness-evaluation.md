# RFC-0068: OKF-Ossie Agent SQL Readiness Evaluation

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Superseded
- **Owners:** Metis maintainers
- **Created:** 2026-09-02
- **Last updated:** 2026-09-03
- **Scope:** `tests/agentbench/readiness`, benchmark-only OKF projection, live Agent evaluation artifacts
- **Supersedes:** None
- **Superseded by:** [`RFC-0072`](0072-unified-agentbench-semantic-interface-frame.md)
- **Related:** RFC-0042, RFC-0048, RFC-0049, RFC-0051, RFC-0053, ADR-0001

## Summary

This RFC proposes a separate, frozen two-representation AgentBench experiment
measuring whether an Agent is more SQL-ready when the same semantic facts are
presented as an Open Knowledge Format (OKF) bundle or as canonical Apache Ossie
assets.

Each representation is collected as an independent single-arm run. Both arms
use the same Agent, model, natural-language question, physical
schema, DuckDB fixture, execution oracle, isolation policy, budgets, and retry
policy. Both arms author SQL directly. The only intentional treatment is the
semantic representation available in the read-only workspace:

```text
same canonical Ossie project
        |
        +-- deterministic fact ledger
        |       |
        |       +-- audited OKF v0.2 projection -> okf_assets arm
        |
        +-- canonical Ossie files -----------> ossie_assets arm

same Agent + same question + same schema + same DuckDB fixture + same oracle
```

The primary endpoint is **first-attempt semantic SQL readiness**: the Agent
returns a DuckDB query without repair, the query parses and executes, and its
normalized result exactly matches the canonical scenario result. Executable
but semantically incorrect SQL is reported separately as a silent-wrong
outcome. A small negative-control stratum measures whether an Agent correctly
declares that SQL is not ready when the supplied semantics are intentionally
ambiguous or insufficient.

The experiment does not make OKF a Metis semantic source, compiler input, or
production API. Ossie remains the source of truth. OKF is a benchmark-only,
deterministically generated Agent context projection.

## Decision question

The experiment answers one bounded question:

> Given equivalent semantic facts and equal Agent/runtime conditions, does an
> OKF knowledge bundle or raw Ossie representation produce higher first-pass
> semantically correct SQL readiness?

It does not answer whether OKF should replace Ossie, whether an Agent-authored
SQL path should replace Metis compilation, or whether OKF plus a specialized
Skill outperforms Ossie plus Metis. Those systems change more than one variable.

The existing frozen AgentBench v0 experiment already compares raw Ossie assets
with the production Metis discovery/compile surface. RFC-0068 does not modify
that frame or combine its results with this experiment as if they came from one
randomized comparison.

## Motivation

OKF standardizes a portable directory of Markdown concept documents with YAML
frontmatter, progressive `index.md` files, links, provenance, trust, and
lifecycle metadata. The experiment pins OKF v0.2 at upstream revision
[`891034c0`](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/891034c0ab63d51f6a8c32490843b8c869d07ec1/okf/SPEC.md).
OKF deliberately does not replace domain-specific schemas or prescribe a query
runtime.

Ossie and OKF therefore solve different problems:

- Ossie is a structured semantic model and remains Metis's semantic authority;
- OKF is an Agent- and human-readable knowledge exchange format;
- Metis resolves and compiles typed semantic intent deterministically.

That architectural distinction predicts different strengths but does not
measure them. Raw Ossie may preserve formal relationships and expressions more
precisely while requiring an Agent to navigate a schema-oriented serialization.
OKF may improve progressive discovery and comprehension while asking the Agent
to interpret human-readable prose and tables. The proposed benchmark turns
those claims into independently collected evidence that is paired offline by
frozen question number.

Without strict content parity, the result would be uninterpretable. A richer
hand-authored OKF bundle compared with minimally documented Ossie would measure
authoring effort. Embedding expected SQL in OKF would measure answer leakage.
Giving only the OKF arm a Skill would measure a format-plus-procedure package.
RFC-0068 excludes each of those confounders.

## Terminology

**SQL-ready** means a positive scenario produced a syntactically valid,
executable query whose normalized result matches the scenario oracle. SQL text
similarity is not part of the definition.

**First-attempt SQL readiness** means the successful answer was returned before
any execution feedback or repair turn.

**Silent wrong** means the Agent declared SQL ready and the query executed, but
the normalized result did not match the oracle. Parse errors, execution errors,
refusals, timeouts, and malformed answer envelopes remain failed outcomes, not
silent wrongs.

**Correct not-ready** means a negative-control scenario received the expected
bounded refusal category instead of invented SQL.

**Semantic fact** means a canonical value already present in the complete
Ossie project or explicitly shared physical schema: identity, description,
dataset placement, expression, datatype, grain, time semantics, relationship,
join condition, filter/value encoding, extension payload, or other field in the
benchmark projection closure. Scenario selection does not decide which facts
are projected.

**Presentation metadata** means representation-specific navigation material
that does not change semantic meaning, such as OKF `type`, `title`, `resource`,
`sources`, generated indexes, deterministic ordering, headings, and links.

## Hypotheses

The hypotheses are preregistered before any formal live collection:

- **H0:** the two representations have equivalent first-attempt semantic SQL
  readiness within the practical equivalence margin;
- **H1-OKF:** OKF improves first-attempt readiness by making relevant knowledge
  easier for the Agent to locate and interpret;
- **H1-Ossie:** Ossie improves first-attempt readiness by retaining a more
  explicit formal structure and reducing prose interpretation;
- **Safety hypothesis:** either representation may improve readiness while also
  increasing confident silent-wrong SQL, so correctness and silent-wrong rates
  must be reported together;
- **Risk-stratum hypothesis:** any meaningful difference should appear in
  semantic-risk strata, not only in ordinary aggregation controls.

No arm is declared the expected winner in prompts, manifests, or filenames
visible to the Agent.

## Design

### Separate frozen experiment identity

RFC-0068 introduces a new frame rather than adding an arm to AgentBench v0. It
has its own:

- manifest schema and prompt version;
- arm identifiers `okf_assets` and `ossie_assets`;
- deterministic scenario selection and order;
- content-parity evidence;
- artifacts and report schema;
- live-run entrypoint and explicit paid-run opt-in;
- README describing the frozen contract.

Changing the OKF spec revision, projection rules, prompt, selected scenarios,
budget, Agent adapter behavior, answer envelope, or oracle advances the prompt
or manifest identity. Results from different identities are not pooled.

Manifest v5 freezes one arm in each manifest. Cross-arm statistics are never
computed inside collection. A separate deterministic analysis step accepts one
completed `okf_assets` collection and one completed `ossie_assets` collection,
verifies that every non-arm frame field and fact-ledger digest match, and joins
outcomes horizontally by the frozen one-based question number. It additionally
checks scenario name, question text, and stratum at every number, so numbering
cannot hide a changed or reordered question. Manifest v1-v4 artifacts remain
historical and cannot be analyzed as v5 evidence.

### Canonical authority and fact ledger

The production Ossie documents used by the conformance project are the sole
semantic source. Before materializing either arm, the benchmark builds a
canonical fact ledger from the exact validated project loaded by Metis.

The ledger is benchmark evidence, not a second semantic model. It contains
stable paths and digests for every canonical fact in the complete projection
closure and a mapping from each fact to:

1. its canonical Ossie document and field path;
2. its rendered OKF document and section/frontmatter path;
3. its canonical normalized value;
4. whether the value is semantic or presentation-only.

The ledger and coverage report are retained outside both Agent workspaces. The
Agent cannot read them. Generation fails before collection when:

- a canonical fact in the projection closure has no OKF representation;
- the OKF projection changes its normalized value;
- one arm receives a scenario-specific fact absent from the other;
- two canonical facts collapse into an ambiguous OKF identity;
- an unsupported semantic-critical extension would be omitted or paraphrased;
- generated content contains an expected result, fixture row, scenario ID, or
  oracle SQL fragment.

The fact ledger proves source coverage, not that an Agent will interpret prose
correctly. It is built without consulting scenario selection, physical fixture
rows, natural-language questions, or expected results. That difference is the
subject of the experiment.

### Benchmark-only OKF projection profile

The OKF arm uses a deterministic, read-only bundle conforming to the pinned OKF
v0.2 revision. The projection profile is local to `tests/agentbench/readiness`.
It does not define a public Metis OKF schema or ingestion contract.

The bundle shape is:

```text
knowledge/
├── index.md
├── projects/
│   ├── index.md
│   └── <project>.md
└── models/
    ├── index.md
    └── <model>/
        ├── index.md
        ├── model.md
        ├── datasets/
        │   ├── index.md
        │   └── <dataset>.md
        ├── metrics/
        │   ├── index.md
        │   └── <metric>.md
        ├── dimensions/
        │   ├── index.md
        │   └── <dataset>--<dimension>.md
        └── relationships/
            ├── index.md
            └── <relationship>.md
```

Every concept uses standard OKF frontmatter. `type`, `title`, `description`,
`resource`, `tags`, `sources`, and lifecycle fields are populated only from
canonical values or deterministic presentation rules. Canonical refs become
stable `resource` identities. Markdown bodies use fixed sections and tables for
query-relevant fields. Expressions and opaque extension payloads are rendered
verbatim in fenced blocks; the generator never summarizes or rewrites them with
an LLM.

The profile intentionally excludes:

- `Attested Computation` concepts;
- final or scenario-shaped SQL examples;
- generated common-query examples;
- inferred joins, grains, filters, or metric meanings;
- freshness/trust claims absent from canonical evidence;
- executor, attester, credential, endpoint, or DataSource details.

The bundle may contain more files and links than Ossie because progressive
disclosure is an OKF affordance under test. It may not contain more semantic
facts.

Projection v2 was frozen after diagnostic-only v1 evidence showed that the
flat global indexes encouraged cross-model traversal and raw scalar fact blocks
were not an adequate native OKF presentation. V2 changes presentation only:
model-scoped paths, fully qualified index labels, and deterministic tables whose
cells are derived directly from the same canonical YAML nodes. The projector
accepts no scenario, question, fixture, oracle, or Agent result. No description,
example, inferred value, preferred filter, or query strategy is added. The
canonical fact set remains complete, opaque extensions remain verbatim, and the
fact ledger maps every value to its new scoped document. Manifest v4 and
projection v2 evidence cannot be pooled with earlier diagnostic evidence.

The explicit `make okf-readiness-conformance` gate materializes the same bundle
and runs version-pinned `okfcli` v0.4.0 validation, lint, and graph construction.
It supplements rather than replaces Metis's fact-parity checks and is kept out
of the default offline CI gate because it downloads an external module and Go
toolchain.

### The two arms

#### Arm A: `okf_assets`

The Agent receives a read-only workspace containing:

```text
knowledge/     pinned, validated OKF projection
schema.sql     current scenario's physical DuckDB schema
```

The Agent receives no Ossie files, Metis MCP server, OKF Skill, generated
search index, embeddings, or benchmark oracle. It uses its normal read-only
file-inspection capabilities to navigate from `knowledge/index.md`.

#### Arm B: `ossie_assets`

The Agent receives a read-only workspace containing:

```text
metis.yaml
models/        canonical Ossie documents for the complete project
schema.sql     the identical current scenario physical schema
```

The Agent receives no OKF files, Metis MCP server, Ossie-specific Skill, or
benchmark oracle. The semantic files are the exact canonical documents from
which the OKF projection and fact ledger were built.

### Skills and format coaching

The formal comparison includes no Agent Skill in either arm. A Skill combines
knowledge with procedural guidance and would introduce a second treatment.
User-level plugins, skills, hooks, memories, AGENTS/CLAUDE/GEMINI instruction
files, browser state, external MCP servers, and web access are disabled using
the existing AgentBench isolation contracts.

Both arms receive the same byte-identical task prompt. It tells the Agent to
inspect the semantic assets and physical schema available in its workspace and
return the frozen answer envelope. It does not name OKF, Ossie, Metis, file
paths, preferred read order, or a query strategy. Representation-native
navigation such as OKF `index.md` is part of the treatment; prompt coaching is
not.

A future OKF-Skill evaluation requires a separate factorial design with
symmetric procedural controls. Its results must not be labeled as RFC-0068
representation evidence.

### Semantic world and scenario selection

Both workspaces expose the same complete, multi-model semantic project. The
harness never selects a model, metric, dataset, relationship, or document for
the Agent. Scenario preparation changes only the physical DuckDB fixture and
shared `schema.sql`.

The formal positive corpus contains 29 executable scenarios selected
deterministically from the canonical conformance corpus and frozen before live
collection:

| Stratum | Count | Purpose |
| --- | ---: | --- |
| `silent_semantics` | 10 | cumulative, offset, semi-additive, conversion, custom-calendar and derived semantics |
| `fanout` | 8 | relationship selection, aggregation algebra, composition and duplicate-risk joins |
| `edge` | 5 | NULL, empty input, ordering, value encoding and boundary behavior |
| `control` | 6 | ordinary aggregation, filtering and grouping used as run-health controls |

The pre-run corpus audit found only five unique executable scenarios assigned
to `edge` by the frozen shared stratum rule. Before the first formal live call,
the planned 10/8/6/6 allocation was therefore amended to 10/8/5/6. The missing
edge slot is neither duplicated nor backfilled from another stratum.

The selection excludes the frozen AgentBench v0 25-scenario sample where the
remaining canonical corpus can satisfy the stratum. Any unavoidable overlap is
declared in the manifest before collection. Selection is independent of
observed OKF/Ossie Agent performance.

Six additional negative controls are authored from canonical ambiguity or
missing-evidence cases. Each has one preregistered not-ready category:

- `ambiguous_identity`;
- `ambiguous_relationship`;
- `unsupported_semantics`;
- `missing_expression`;
- `missing_physical_field`;
- `insufficient_scope`.

Negative controls must be symmetric: the same semantic fact is removed or made
ambiguous in both derived workspaces, and parity validation is rerun. They are
reported separately and never enter the positive SQL-readiness denominator.

### Answer contract

Both arms return exactly one bare JSON object, without a Markdown fence or
surrounding prose, in one of two forms:

```json
{
  "status": "ready",
  "dialect": "duckdb",
  "sql": "SELECT ..."
}
```

or:

```json
{
  "status": "not_ready",
  "reason": "ambiguous_identity"
}
```

Prompt v2 removed the v1 requirement that the Agent wrap this object in a
Markdown fence. A diagnostic repair produced a valid JSON object but omitted
only the closing fence; treating that presentation-only defect as an Ossie-arm
failure would confound semantic readiness with Markdown formatting. The bare
JSON contract applies identically to both arms, remains strictly decoded, and
v1 and v2 results are never pooled.

Manifest v2 replaces the smoke and diagnostic
`cumulative_metric_by_quarter_and_region` probe with
`cumulative_metric_by_month`. The former fixture contains only one calendar
quarter, so a non-cumulative query can match its cumulative values and does not
test the intended semantic risk. The replacement spans multiple months and
therefore distinguishes period revenue from revenue to date. Result mismatches
are also classified as type or value mismatches for diagnosis; neither expected
types nor expected values are disclosed to the Agent. Manifest v1 evidence is
retained as historical diagnostic evidence and is not pooled with v2.

The decoder rejects any Markdown fence, surrounding prose, unknown fields,
non-DuckDB dialects, multiple SQL statements, mutation statements, and unknown
not-ready reasons. A positive scenario returning `not_ready` is failed. A
negative scenario returning the expected category is correct not-ready;
returning SQL or the wrong category is unsafe/wrong.

The SQL is always authored by the Agent. The harness does not rewrite it,
insert joins, interpolate semantic values, or repair quoting.

### Execution and oracle

Positive answers declared `ready` execute through the existing production
DuckDB Driver seam against the same isolated fixture used by canonical
conformance. The fixture is dropped and recreated before each scored arm run so
no physical state crosses arms or questions.

The oracle compares normalized semantic results:

- column count and value kind are significant;
- aliases and harmless SQL presentation differences are not significant;
- row order matters only when the scenario declares it;
- duplicate rows remain visible as fanout evidence;
- expected rows come only from the canonical scenario contract.

SQL fingerprints are retained for diagnosis but never used as a correctness
oracle. Different physical SQL that returns the correct canonical result is
correct.

### Isolation and collection order

Every question in the selected arm runs once and opens a fresh external-Agent
process, conversation, temporary home, and writable temporary directory. Only
the optional repair turn reuses its question's conversation. No Agent-created
state or context crosses questions. Separate arm collections share no process
or writable state.

The semantic world and frozen question order are immutable within each
collection. Each manifest selects exactly one arm, and questions execute in
ascending `question_number`. Operators run the other arm independently and as
close in time as practical. Analysis preserves the independent collection
timestamps and does not pretend that provider or machine drift between runs was
randomized away.

Live evidence uses the same shared incremental-journal mechanism as AgentBench
v0. Collection writes to `<output>.partial`, fsyncs each completed attempt, and
atomically renames the directory only after rebuilding a valid final artifact.
`--resume` requires the exact persisted manifest and fact ledger, restores
complete question batches, and skips their question numbers. A partial repair
batch fails closed because a new Agent process cannot preserve the interrupted
same-question conversation.

### Budget and repair

The initial formal frame uses:

- exactly one run for the selected arm for every positive and negative
  scenario;
- at most 30 observable Agent tool calls per question, shared across attempts;
- one 60-second wall-clock deadline per question, shared across Agent work,
  validation, execution, and repair;
- at most one repair turn;
- fresh sessions for the next question regardless of outcome.

The first call beyond the budget is retained as terminal failed evidence. A
timeout is terminal and receives no repair. Provider/model startup failures and
broken benchmark infrastructure abort collection rather than being counted as
an arm failure.

Repair feedback is representation-neutral and bounded. It may report malformed
output, SQL parse/execute failure, or normalized-result mismatch, but it never
reveals expected rows, expected SQL, a missing semantic asset, or a suggested
query change. First-attempt readiness remains the primary endpoint; repaired
accuracy is secondary.

### Recorded evidence

Each attempt records:

- experiment, manifest, prompt, OKF revision, and projection-profile versions;
- Agent CLI, provider, requested model, reported model, and immutable model
  revision when the provider supplies one;
- arm, one-based question number, scenario, stratum, and attempt index;
- final answer, SQL fingerprint, verdict, and bounded failure category;
- parse, execution, normalization, and oracle outcomes;
- duration, tool calls, input/context/output tokens;
- bounded file-read evidence: relative path, operation, byte count, and order;
- first-attempt and final verdict;
- fact-ledger and workspace-tree digests.

Externally archived artifacts omit raw chain-of-thought, secrets, full user
home state, and repeated prompts. Raw structured traces are diagnostic-only and
redacted under the existing AgentBench rules. Accepted evidence must rebuild
the aggregate report deterministically from immutable per-attempt records.

## Metrics and analysis

### Primary endpoint

For each positive scenario and arm, record whether its single run is
first-attempt SQL-ready. Each scenario receives equal weight, regardless of SQL
length or stratum size.

The headline treatment effect is:

```text
delta = mean_s(readiness_okf,s - readiness_ossie,s)
```

The experiment does not estimate within-scenario Agent variability. After both
single-arm collections finish, analysis joins them by frozen question number
and verifies the full question identity. Confidence intervals use a paired
bootstrap that resamples those joined question rows, keeping the two arm
outcomes together. The implementation freezes the random seed and uses at least
10,000 resamples.

Before the first formal collection, the preregistration includes a simulation
using only pre-existing AgentBench paired-correctness and discordance
assumptions. It reports the design's sensitivity to paired readiness differences
of 5, 10, and 15 percentage points. The simulation may show that the fixed
single-run design is underpowered; it may not use new arm outcomes to change
scenario count, equivalence margin, or stopping behavior.

The practical equivalence margin is five percentage points:

- **OKF advantage:** the complete 95% interval is above `+0.05`;
- **Ossie advantage:** the complete 95% interval is below `-0.05`;
- **practical equivalence:** the complete 95% interval is inside
  `[-0.05, +0.05]`;
- **inconclusive:** every other interval.

The report always includes the point estimate and interval. It must not force a
winner when the preregistered rule says inconclusive.

### Required secondary metrics

The report also shows, overall and per stratum:

- eventual semantic correctness after the bounded repair;
- silent-wrong count and rate among executed queries;
- failed/refusal count and rate;
- SQL parse and execution failure rates;
- correct not-ready and unsafe-ready rates for negative controls;
- median and distribution of tool calls, bytes read, files read, duration, and
  context/output tokens per first-attempt correct answer;
- scenario-level arm discordance;
- control-stratum health.

Efficiency metrics are conditional evidence and cannot override correctness.
An arm that uses fewer tokens to return more wrong answers is not more
SQL-ready.

### Validity gates

A formal collection is invalid and cannot support a winner claim if:

- fact-parity validation or OKF conformance fails;
- arm workspaces expose the opposite representation, an MCP server, a Skill,
  external search, or benchmark/oracle files;
- Agent/model identity differs across the two single-arm manifests;
- question number, scenario, question text, stratum, prompt, projection,
  budget, or fact-ledger identity differs across the two collections;
- fewer than 80% of planned joined question rows produce scored evidence for
  reasons outside Agent behavior;
- either arm is below 70% first-attempt correctness on the control stratum;
- a prompt, scenario, projection, budget, or oracle changes after unblinding;
- collection stops early because a partial result looks decisive;
- provider alias drift prevents identifying whether one collection used a
  stable model population.

A model alias without an immutable revision may still provide explicitly
labeled directional evidence, as in prior AgentBench work, but not a durable
cross-time superiority claim.

## Interpretation contract

An OKF advantage means only that the tested Agent/model more often converted an
audited OKF projection into correct SQL under the frozen conditions. It does not
make OKF a sufficiently typed compiler contract.

An Ossie advantage means only that the tested Agent/model more often converted
raw Ossie assets into correct SQL. It does not show that raw Agent SQL is safer
than Metis's deterministic compilation path.

Practical equivalence means neither representation demonstrated a material
readiness advantage at the chosen margin. Inconclusive means the experiment did
not distinguish them; it is not evidence of equality.

Results may motivate a later hybrid design in which OKF supplies progressive
Agent knowledge while Ossie and Metis retain semantic authority and
compilation. Production adoption requires its own RFC and must not be inferred
automatically from this benchmark.

## Architectural boundaries

RFC-0068 preserves these boundaries:

- Ossie remains the semantic source of truth;
- OKF projection code remains under `tests/` until a separate production RFC;
- `ossie.Loader`, `SemanticManifest`, Resolver, Planner, compiler, and Renderer
  do not read Markdown;
- unknown Ossie extensions are preserved rather than silently summarized;
- no DataSource endpoint, credential, target override, or execution route is
  written into semantic assets or OKF concepts;
- neither arm receives arbitrary database execution capability;
- the harness executes only the single returned read-only DuckDB statement;
- REST and MCP production surfaces do not change;
- the frozen v0 raw-assets-versus-Metis experiment remains byte- and
  behavior-compatible.

## Alternatives

### Compare OKF directly with Metis MCP

Rejected because it changes both representation and SQL authorship. OKF would
ask the Agent to write SQL while Metis would compile a typed semantic query.
That is a useful system comparison, but it cannot identify whether a difference
came from OKF/Ossie representation or deterministic compilation. Existing
AgentBench already supplies the complementary raw-Ossie-versus-Metis evidence.

### Give the OKF arm an OKF Skill

Rejected because a Skill adds procedural instructions, scripts, and routing
knowledge. A fair Skill evaluation needs a factorial design or an equivalent
Ossie procedure package.

### Hand-author the OKF corpus

Rejected for the primary experiment because human enrichment can add, omit, or
clarify semantic facts. Hand-authored `native-best` comparisons may follow as
separate ecological evidence, but they cannot replace the audited projection
result.

### Embed Ossie YAML in OKF documents

Rejected because it would make the OKF arm a differently wrapped copy of the
Ossie arm and would not test an OKF knowledge representation. Verbatim fenced
payloads are reserved for expressions and opaque extensions that cannot be
losslessly represented otherwise.

### Score SQL text or golden fingerprints

Rejected because many physically different SQL statements are semantically
equivalent. Real-engine execution and canonical normalized results are the
correctness authority.

### Use one Agent conversation for multiple questions

Rejected by RFC-0048 evidence because context accumulation makes later
questions dependent and changes cost and behavior.

### Collect five repetitions immediately

Deferred because the initial product-direction question does not justify five
paid runs of every arm/scenario pair. RFC-0068 starts with one run per arm and
scenario and reports the resulting paired-after-join uncertainty explicitly.
Any later repeated collection requires a new frozen experiment identity and
must not be pooled with the initial run as though repetition count had been
preregistered at five.

## Rollout and closure

The proposal was accepted on 2026-09-02. R1 through R3 are authorized before
formal collection; R4 still requires the explicit paid-run opt-in below.

### Phase R1: deterministic representation frame

- add the separate readiness manifest and prompt identities;
- build the canonical fact ledger from validated Ossie projects;
- implement the deterministic OKF v0.2 projection and conformance checks;
- add parity, leakage, determinism, and workspace-isolation tests;
- freeze the scenario and negative-control manifests before live evaluation.

### Phase R2: execution and artifact frame

- reuse the canonical DuckDB fixture, Driver execution seam, and result oracle;
- implement the shared answer-envelope decoder and read-only SQL checks;
- implement single-arm collection, question-isolated sessions, and strict
  offline joining by question number;
- journal immutable attempt evidence and deterministic reports;
- reuse the shared AgentBench incremental journal for strict resume and atomic
  final commit;
- add smoke mode for one positive control and one semantic-risk scenario.

### Phase R3: preregistration and smoke

- record exact Agent/model identity, prompt, projection version, budget,
  scenarios, analysis seed, and validity gates;
- run deterministic CI and the frozen two-scenario smoke only;
- correct only harness defects, advancing identity for any protocol change;
- freeze the formal identity after smoke review.

### Phase R4: formal collection

- require explicit paid-run opt-in;
- collect exactly one run in each single-arm manifest for every frozen scenario
  without outcome-dependent stopping;
- archive redacted immutable evidence and the deterministically rebuilt report
  outside the source repository;
- update this RFC with accepted results, limitations, and one of the four
  preregistered conclusions;
- close or separately propose any product change suggested by the evidence.

No paid Agent/provider call runs in ordinary CI. CI validates generators,
fixtures, scoring, artifact schemas, and report rebuilding only. Live-run
evidence is ignored by Git and retained in a controlled external artifact store.

## Test and acceptance criteria

RFC-0068 can become `Implemented` only when:

1. the pinned OKF revision and projection profile are recorded in every run;
2. OKF bundles pass pinned-version conformance and deterministic regeneration;
3. every canonical fact in the complete projection closure has a stable,
   value-preserving two-arm parity mapping and no scenario-specific leakage;
4. unknown and semantic-critical Ossie extensions are preserved or generation
   fails explicitly;
5. both arms receive byte-identical prompts, schemas, budgets, Agent/model
   identities, execution fixtures, and oracle behavior;
6. neither workspace exposes the opposite representation, benchmark source,
   oracle, MCP server, Skill, plugin, or external search surface;
7. answer decoding rejects prose, unknown fields, multiple statements,
   mutations, wrong dialects, and unknown refusal reasons;
8. SQL runs only through the bounded DuckDB execution seam and semantic results,
   not SQL strings, determine correctness;
9. every arm/scenario pair runs exactly once in a fresh Agent session and each
   single-arm collection follows ascending frozen question number;
10. artifacts preserve correct, silent-wrong, failed, correct-not-ready, and
    unsafe-ready outcomes without collapsing them;
11. single-arm reports preserve independent evidence; comparison reports join
    horizontally by question number, preserve paired scenario outcomes,
    reproduce the frozen bootstrap, and apply the preregistered equivalence rule
    exactly;
12. deterministic checks and the frozen smoke pass before formal collection;
13. the complete formal collection is retained outside the source repository,
    including unfavorable, inconclusive, and failed evidence;
14. the aggregate report rebuilds deterministically from archived artifacts;
15. existing AgentBench v0 and production Metis tests remain unchanged in
    behavior;
16. ordinary CI performs no paid or external-Agent invocation.

## Documentation updates

Implementation adds or updates:

```text
docs/proposals/README.md
tests/benchmarks/okf-ossie-sql-readiness/README.md
tests/benchmarks/CONFORMANCE.md
```

If the experiment motivates production OKF export or an Agent Skill, that work
requires a separate proposal plus updates to the applicable current design and
specification documents. RFC-0068 itself changes no current product contract.
