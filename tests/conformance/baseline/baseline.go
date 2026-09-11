// Package baseline computes and archives the fingerprint evidence
// used to review changes to SQL generation and semantic plans.
//
// The planner uses SemanticPlan as the universal semantic DAG. Changes to its
// representation need two separate pieces of evidence, held to two contracts:
//
//   - compiled SQL must not move. Ownership moving inside the IR is not a
//     licence to render different SQL, so the compiled-SQL fingerprints are a
//     byte-stability gate: a diff is a SQL behavior change and needs its own
//     approved correctness fix.
//   - the semantic-plan fingerprints describe IR structure, and they are
//     expected to move exactly when ownership moves. They are archived so that
//     movement is reviewable per field rather than discovered afterwards, not
//     so that it is forbidden.
//
// Keeping both in one package keeps them the same corpus, the same targets, and
// the same production planning path, which is what makes the second file
// interpretable as a diagnostic for the first.
package baseline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Kind names one archived fingerprint contract.
type Kind string

const (
	// KindCompiledSQL hashes rendered SQL and its bound parameters. Byte stable
	// across planner refactoring unless an independently approved correctness fix records
	// the change.
	KindCompiledSQL Kind = "compiled-sql"
	// KindCompiledParameters records the exact parameter wire contract rather
	// than hashing it together with SQL. The SQL fingerprint still observes
	// parameter movement; this archive makes the count, order, names, Go value
	// types, and values reviewable during renderer changes.
	KindCompiledParameters Kind = "compiled-parameters"
	// KindSemanticPlan hashes the canonical semantic-plan projection. Expected
	// to move when IR ownership moves; every movement needs a disposition.
	KindSemanticPlan Kind = "semantic-plan"
)

// Kinds returns every archived contract in a stable order.
func Kinds() []Kind {
	return []Kind{KindCompiledSQL, KindCompiledParameters, KindSemanticPlan}
}

// Path returns the repository-relative archive for one kind.
func Path(kind Kind) string {
	return "tests/conformance/baseline/testdata/" + string(kind) + ".fingerprints"
}

// RegenerateCommand returns the exact command that rewrites one archive, so a
// failing gate can tell a reader how to record a movement it has decided to
// accept rather than leaving them to reconstruct it.
func RegenerateCommand(kind Kind) string {
	return fmt.Sprintf("go run ./tests/conformance/cmd/fingerprint -kind=%s > %s", kind, Path(kind))
}

// ParseKind resolves a command-line kind name.
func ParseKind(name string) (Kind, error) {
	for _, kind := range Kinds() {
		if string(kind) == name {
			return kind, nil
		}
	}
	names := make([]string, 0, len(Kinds()))
	for _, kind := range Kinds() {
		names = append(names, string(kind))
	}
	return "", fmt.Errorf("unknown fingerprint kind %q, want one of %s", name, strings.Join(names, ","))
}

// Fingerprints returns one sorted line per (target, scenario) pair for the
// requested kind.
//
// Every registered compiler target is covered rather than assumed: the
// compiler-evidence table is hand maintained and the dialect registry is what
// actually renders SQL, so a renderer added to one and missed in the other would
// otherwise produce a smaller archive that still diffs clean.
func Fingerprints(kind Kind) ([]string, error) {
	targets := evidence.CompilerTargets()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		return nil, fmt.Errorf("create Renderer registry: %w", err)
	}
	registered := registry.Dialects()
	registeredNames := make([]string, len(registered))
	for i, dialect := range registered {
		registeredNames[i] = string(dialect)
	}
	if err := requireEvidenceCoversEveryRenderer(targets, registeredNames); err != nil {
		return nil, err
	}

	digest, err := digester(kind)
	if err != nil {
		return nil, err
	}

	lines := make([]string, 0, len(targets)*len(scenarios.Core))
	for _, target := range targets {
		for _, scenario := range scenarios.Core {
			// A scenario the target cannot express is reported rather than
			// skipped silently: a capability that disappears during a refactor
			// would otherwise look like an unchanged fingerprint set.
			if missing := target.Capabilities.Missing(scenario.Requires); len(missing) != 0 {
				names := make([]string, len(missing))
				for i, capability := range missing {
					names[i] = string(capability)
				}
				lines = append(lines, fmt.Sprintf("%s/%s unsupported=%s", target.Dialect, scenario.Name, strings.Join(names, ",")))
				continue
			}
			value, err := digest(scenario, target.Dialect)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", target.Dialect, scenario.Name, err)
			}
			lines = append(lines, fmt.Sprintf("%s/%s %s=%s", target.Dialect, scenario.Name, recordLabel(kind), value))
		}
	}
	sort.Strings(lines)
	return lines, nil
}

func recordLabel(kind Kind) string {
	if kind == KindCompiledParameters {
		return "parameters"
	}
	return "sha256"
}

func digester(kind Kind) (func(scenarios.Scenario, string) (string, error), error) {
	switch kind {
	case KindCompiledSQL:
		return compiledFingerprint, nil
	case KindCompiledParameters:
		return compiledParameterEvidence, nil
	case KindSemanticPlan:
		return semanticPlanFingerprint, nil
	default:
		return nil, fmt.Errorf("unknown fingerprint kind %q", kind)
	}
}

// requireEvidenceCoversEveryRenderer fails closed unless the compiler-evidence
// dialects and the default physical dialect registry are the same set.
//
// Both directions matter. A renderer missing from the evidence table is a
// renderer this gate does not cover; an evidence dialect with no registered
// renderer is a row claiming conformance for something that cannot render.
func requireEvidenceCoversEveryRenderer(targets []evidence.Target, registered []string) error {
	evidenced := make(map[string]bool, len(targets))
	for _, target := range targets {
		evidenced[strings.ToUpper(target.Dialect)] = true
	}
	inRegistry := make(map[string]bool, len(registered))
	for _, name := range registered {
		inRegistry[strings.ToUpper(name)] = true
	}

	var unevidenced, unregistered []string
	for _, name := range registered {
		if !evidenced[strings.ToUpper(name)] {
			unevidenced = append(unevidenced, name)
		}
	}
	for dialect := range evidenced {
		if !inRegistry[dialect] {
			unregistered = append(unregistered, dialect)
		}
	}
	sort.Strings(unevidenced)
	sort.Strings(unregistered)

	switch {
	case len(unevidenced) != 0 && len(unregistered) != 0:
		return fmt.Errorf("registered renderers %s have no compiler-evidence target, and evidence targets %s have no registered renderer",
			strings.Join(unevidenced, ","), strings.Join(unregistered, ","))
	case len(unevidenced) != 0:
		return fmt.Errorf("registered renderers %s have no compiler-evidence target, so this gate would not cover them",
			strings.Join(unevidenced, ","))
	case len(unregistered) != 0:
		return fmt.Errorf("compiler-evidence targets %s have no registered renderer",
			strings.Join(unregistered, ","))
	}
	return nil
}

// PlanScenario runs the production resolve/plan path for one scenario.
//
// The archives share it deliberately. A semantic-plan fingerprint taken from a
// differently constructed plan than the one that produced the SQL could not be
// read as evidence about that SQL. It is exported for the same reason: a
// migration gate that reached the planner by its own slightly different route
// would be evidence about that route.
func PlanScenario(scenario scenarios.Scenario, dialect string) (*semanticplan.SemanticPlan, renderer.Renderer, error) {
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		return nil, nil, fmt.Errorf("create Renderer registry: %w", err)
	}
	renderer, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		return nil, nil, fmt.Errorf("resolve Renderer: %w", err)
	}

	definition, ok := fixtures.Lookup(scenario.Fixture)
	if !ok {
		return nil, renderer, fmt.Errorf("unknown conformance fixture %q", scenario.Fixture)
	}
	doc, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		return nil, renderer, fmt.Errorf("load fixture: %w", err)
	}
	snapshot, err := manifest.BuildProjectManifest(definition.Project, doc)
	if err != nil {
		return nil, renderer, fmt.Errorf("build snapshot: %w", err)
	}

	semanticQuery := scenario.Query
	semanticQuery.Project = definition.Project
	semanticQuery.Model = definition.Model

	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, renderer)
	if err != nil {
		return nil, renderer, fmt.Errorf("resolve: %w", err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		return nil, renderer, fmt.Errorf("plan: %w", err)
	}
	return plan, renderer, nil
}

// compiledFingerprint hashes the rendered SQL and its parameters.
//
// Hashing the parameter-inlined form alone would be blind to a change that
// turned a literal into a bound parameter or the reverse: both inline to the
// same text, while the wire contract differs.
//
// The dialect the compiler reports is checked rather than hashed. Hashing it
// would make an identity migration -- the ansi-to-duckdb rename, or any future
// one -- move every fingerprint for the renamed target, burying whatever real
// SQL change the gate exists to catch. But leaving it out entirely, as the
// rename originally did, silently narrowed this instrument for good: the gate
// could no longer see a compiler that returned SQL for one dialect labeled as
// another, and the rename that needed the exemption is long finished. Asserting
// it keeps both properties.
func compiledFingerprint(scenario scenarios.Scenario, dialect string) (string, error) {
	sqlQuery, err := compileScenario(scenario, dialect)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		SQL        string               `json:"sql"`
		Parameters []sql.QueryParameter `json:"parameters"`
	}{SQL: sqlQuery.SQL, Parameters: sqlQuery.Parameters})
	if err != nil {
		return "", fmt.Errorf("serialize compiled query: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload)), nil
}

type parameterEvidence struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	GoType string `json:"go_type"`
	Value  any    `json:"value"`
}

func compiledParameterEvidence(scenario scenarios.Scenario, dialect string) (string, error) {
	sqlQuery, err := compileScenario(scenario, dialect)
	if err != nil {
		return "", err
	}
	evidence := make([]parameterEvidence, len(sqlQuery.Parameters))
	for i, parameter := range sqlQuery.Parameters {
		goType := "<nil>"
		if parameter.Value != nil {
			goType = fmt.Sprintf("%T", parameter.Value)
		}
		evidence[i] = parameterEvidence{
			Index:  i,
			Name:   parameter.Name,
			GoType: goType,
			Value:  parameter.Value,
		}
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return "", fmt.Errorf("serialize compiled parameters: %w", err)
	}
	return string(payload), nil
}

func compileScenario(scenario scenarios.Scenario, dialect string) (sql.SQLQuery, error) {
	plan, renderer, err := PlanScenario(scenario, dialect)
	if err != nil {
		return sql.SQLQuery{}, err
	}
	compiled, err := compiler.CompileWithRenderer(context.Background(), plan, renderer)
	if err != nil {
		return sql.SQLQuery{}, fmt.Errorf("compile: %w", err)
	}
	sqlQuery := compiled.PhysicalQuery
	if !strings.EqualFold(string(sqlQuery.Dialect), dialect) {
		return sql.SQLQuery{}, fmt.Errorf("compiled dialect = %q, want %q: the SQL below is labeled for a target that did not render it", sqlQuery.Dialect, dialect)
	}
	return sqlQuery, nil
}

// semanticPlanFingerprint hashes the canonical semantic-plan projection.
//
// FingerprintSemanticPlan already covers a flat plan as fully as a staged one,
// which is what makes it usable as pre-migration evidence: the direct corpus is
// recorded here in the same form it will be recorded in after it owns stages,
// so the cutover diff shows which scenarios changed structure and which did not.
func semanticPlanFingerprint(scenario scenarios.Scenario, dialect string) (string, error) {
	plan, _, err := PlanScenario(scenario, dialect)
	if err != nil {
		return "", err
	}
	fingerprint, err := semanticplan.Fingerprint(plan)
	if err != nil {
		return "", fmt.Errorf("fingerprint semantic plan: %w", err)
	}
	return fingerprint, nil
}
