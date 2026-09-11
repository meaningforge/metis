package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// ProductionExecution owns the production execution route used by one
// real-engine conformance backend. Fixture setup remains outside this type.
type ProductionExecution struct {
	runtime *runner.Runner
	route   runner.ResolvedDataSource
}

const fixtureDataSourceName = "real-engine-fixture"

var rawProjectionIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ProductionExecutionPolicy is the bounded policy shared by real-engine
// semantic conformance runs. It is intentionally generous enough for the
// canonical corpus while still exercising Runner admission and accounting.
func ProductionExecutionPolicy() datasource.DataSourcePolicy {
	maxRows := int64(10_000)
	maxBytes := int64(64 * 1024 * 1024)
	maxConcurrency := 1
	return datasource.DataSourcePolicy{
		QueryTimeout:   "60s",
		MaxRows:        &maxRows,
		MaxBytes:       &maxBytes,
		MaxConcurrency: &maxConcurrency,
	}
}

// NewProductionExecution composes one test DataSource with its production
// Backend and Runner. The route is resolved once and its exact Renderer is
// reused for compilation and execution.
func NewProductionExecution(t *testing.T, source datasource.DataSource, binding backend.Backend, secrets runner.SecretResolver) *ProductionExecution {
	t.Helper()
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{fixtureDataSourceName: source})
	if err != nil {
		t.Fatalf("create production conformance DataSource: %v", err)
	}
	backends, err := backend.NewBackendRegistry(binding)
	if err != nil {
		t.Fatalf("create production conformance Backend registry: %v", err)
	}
	if err := backends.ValidateDataSources(sources); err != nil {
		t.Fatalf("validate production conformance execution: %v", err)
	}
	runtime := runner.New(sources, backends, secrets, nil)
	route, err := runtime.ResolveDataSource(fixtureDataSourceName)
	if err != nil {
		t.Fatalf("resolve production conformance DataSource: %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Errorf("close production conformance execution: %v", err)
		}
	})
	return &ProductionExecution{runtime: runtime, route: route}
}

// Close releases the fixture-scoped production Runner before a test mutates
// the same physical tables. NewProductionExecution also registers this cleanup
// as a fail-safe, so Close is safe to call explicitly.
func (e *ProductionExecution) Close(t *testing.T) {
	t.Helper()
	if e == nil || e.runtime == nil {
		return
	}
	if err := e.runtime.Close(context.Background()); err != nil {
		t.Fatalf("close production conformance execution: %v", err)
	}
}

// CompileScenario compiles one scenario with the exact Renderer already
// selected by this execution route. Differential tests use optimize=false
// without introducing a dialect-name lookup or a second Renderer authority.
func (e *ProductionExecution) CompileScenario(t *testing.T, scenario scenarios.Scenario, optimize bool) *artifact.CompiledQuery {
	t.Helper()
	if e == nil || e.runtime == nil || e.route.Backend.Renderer == nil {
		t.Fatal("production conformance execution is not configured")
	}
	return compileScenario(t, scenario, e.route.Backend.Renderer, optimize)
}

func (e *ProductionExecution) execute(t *testing.T, scenario scenarios.Scenario) scenarios.ResultSet {
	t.Helper()
	if e == nil || e.runtime == nil || e.route.Backend.Renderer == nil {
		t.Fatal("production conformance execution is not configured")
	}
	compiled := e.CompileScenario(t, scenario, true)
	return e.executeCompiled(t, scenario.Name, compiled)
}

// RunCompiled executes a complete compiler artifact through the fixture's
// production Runner. It is exported only from the test harness for internal
// optimizer and attribution evidence contracts.
func (e *ProductionExecution) RunCompiled(t *testing.T, name string, compiled *artifact.CompiledQuery) scenarios.ResultSet {
	t.Helper()
	return e.executeCompiled(t, name, compiled)
}

// RunRawQuery is the narrow test-only escape hatch for a physical assertion
// that cannot originate as one complete semantic compilation, such as a
// projection over attribution evidence. Callers must provide an explicit
// OutputSchema; execution, normalization, limits, and cleanup still belong to
// production Runner and Driver implementations.
func (e *ProductionExecution) RunRawQuery(t *testing.T, name string, query sql.SQLQuery, schema artifact.OutputSchema) scenarios.ResultSet {
	t.Helper()
	if e == nil || e.runtime == nil || e.route.Backend.Renderer == nil {
		t.Fatal("production conformance execution is not configured")
	}
	if strings.TrimSpace(query.SQL) == "" {
		t.Fatal("raw test query SQL is required")
	}
	if query.Dialect == "" {
		query.Dialect = e.route.Backend.SQLDialect()
	}
	compiled, err := artifact.NewCompiledQuery(query, schema)
	if err != nil {
		t.Fatalf("construct raw test query contract: %v", err)
	}
	return e.executeCompiled(t, name, compiled)
}

// SelectOutputSchema derives an explicit projection contract for a raw test
// query from an existing compiler-owned schema. Requested names determine the
// physical output order and unknown or duplicate names fail closed.
func SelectOutputSchema(t *testing.T, schema artifact.OutputSchema, names ...string) artifact.OutputSchema {
	t.Helper()
	available := make(map[string]artifact.OutputColumn, len(schema.Columns))
	for _, column := range schema.Columns {
		if _, exists := available[column.Name]; exists {
			t.Fatalf("source output schema contains duplicate column %q", column.Name)
		}
		available[column.Name] = column
	}
	selected := make([]artifact.OutputColumn, len(names))
	seen := make(map[string]struct{}, len(names))
	for index, name := range names {
		if _, exists := seen[name]; exists {
			t.Fatalf("raw query output schema requests duplicate column %q", name)
		}
		column, exists := available[name]
		if !exists {
			t.Fatalf("raw query output schema references unknown column %q", name)
		}
		seen[name] = struct{}{}
		selected[index] = column
	}
	return artifact.OutputSchema{Columns: selected}
}

// RunProjection executes a portable test-only SELECT over a complete compiled
// artifact. It is deliberately limited to simple output identifiers plus an
// explicit predicate; it is not a general query builder or product surface.
func (e *ProductionExecution) RunProjection(t *testing.T, name string, source *artifact.CompiledQuery, names []string, predicate string) scenarios.ResultSet {
	t.Helper()
	if source == nil {
		t.Fatal("raw projection source is required")
	}
	if len(names) == 0 {
		t.Fatal("raw projection columns are required")
	}
	for _, identifier := range names {
		if !rawProjectionIdentifier.MatchString(identifier) {
			t.Fatalf("raw projection identifier %q is not portable", identifier)
		}
	}
	statement := "SELECT " + strings.Join(names, ", ") + " FROM (" +
		strings.TrimSuffix(strings.TrimSpace(source.PhysicalQuery.SQL), ";") + ") evidence"
	if strings.TrimSpace(predicate) != "" {
		statement += " WHERE " + predicate
	}
	query := source.PhysicalQuery
	query.SQL = statement
	return e.RunRawQuery(t, name, query, SelectOutputSchema(t, source.OutputSchema, names...))
}

func (e *ProductionExecution) executeCompiled(t *testing.T, name string, compiled *artifact.CompiledQuery) scenarios.ResultSet {
	t.Helper()
	result, err := e.runtime.ExecuteResolved(context.Background(), e.route, compiled, runner.ExecutionOptions{})
	if err != nil {
		var executionErr *runner.ExecutionError
		if errors.As(err, &executionErr) && executionErr.ResultContract != nil {
			failure := executionErr.ResultContract
			t.Fatalf("execute %s through production runtime: %v (column=%s expected=%s observed=%s)\nSQL:\n%s",
				name, err, failure.Column, failure.ExpectedDatatype, failure.ObservedFamily, compiled.PhysicalQuery.SQL)
		}
		t.Fatalf("execute %s through production runtime: %v\nSQL:\n%s", name, err, compiled.PhysicalQuery.SQL)
	}
	return conformanceResult(t, result)
}

// RunRegisteredMetricScale compiles the semantic-critical extension with the
// exact resolved Backend Renderer and executes the complete artifact through
// the same production route as the shared scenario corpus.
func (e *ProductionExecution) RunRegisteredMetricScale(t *testing.T) scenarios.ResultSet {
	t.Helper()
	if e == nil || e.runtime == nil || e.route.Backend.Renderer == nil {
		t.Fatal("production conformance execution is not configured")
	}
	compiled := compileRegisteredMetricScale(t, e.route.Backend.Renderer)
	return e.executeCompiled(t, "registered_metric_scale", compiled)
}

func conformanceResult(t *testing.T, result runner.ResultSet) scenarios.ResultSet {
	t.Helper()
	columns := make([]scenarios.ResultColumn, len(result.Schema.Columns))
	for index, column := range result.Schema.Columns {
		kind, err := resultKind(column.Datatype)
		if err != nil {
			t.Fatalf("output column %q: %v", column.Name, err)
		}
		columns[index] = scenarios.ResultColumn{Name: column.Name, ValueKind: kind}
	}
	rows := make([]scenarios.ResultRow, len(result.Rows))
	for rowIndex, values := range result.Rows {
		if len(values) != len(columns) {
			t.Fatalf("runtime row %d has %d values, want %d", rowIndex, len(values), len(columns))
		}
		row := make(scenarios.ResultRow, len(values))
		for columnIndex, value := range values {
			kind := columns[columnIndex].ValueKind
			if value == nil {
				row[columnIndex] = scenarios.NullResultValue(kind)
				continue
			}
			literal, err := resultLiteral(value)
			if err != nil {
				t.Fatalf("runtime row %d column %q: %v", rowIndex, columns[columnIndex].Name, err)
			}
			normalized, err := scenarios.ParseResultValue(kind, literal)
			if err != nil {
				t.Fatalf("runtime row %d column %q: %v", rowIndex, columns[columnIndex].Name, err)
			}
			row[columnIndex] = normalized
		}
		rows[rowIndex] = row
	}
	return scenarios.ResultSet{Columns: columns, Rows: rows}
}

func resultKind(datatype ossie.DataType) (scenarios.ResultValueKind, error) {
	switch datatype {
	case ossie.DataTypeString:
		return scenarios.ResultString, nil
	case ossie.DataTypeInteger:
		return scenarios.ResultInteger, nil
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		return scenarios.ResultNumber, nil
	case ossie.DataTypeBoolean:
		return scenarios.ResultBoolean, nil
	case ossie.DataTypeDate:
		return scenarios.ResultDate, nil
	case ossie.DataTypeTime:
		return scenarios.ResultTime, nil
	case ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return scenarios.ResultDateTime, nil
	case "", ossie.DataTypeOpaque:
		return scenarios.ResultOpaque, nil
	default:
		return "", fmt.Errorf("unsupported semantic datatype %q", datatype)
	}
}

func resultLiteral(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	case json.Number:
		return typed.String(), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported normalized runtime value %T", value)
	}
}
