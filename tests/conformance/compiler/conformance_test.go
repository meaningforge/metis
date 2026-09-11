package compiler_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

const (
	projectName = "metricflow-reference"
	modelName   = "commerce"
)

func TestSharedCompilerContract(t *testing.T) {
	for _, target := range compilerTargets {
		target := target
		t.Run(target.Name, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				scenario := scenario
				t.Run(scenario.Name, func(t *testing.T) {
					if missing := target.Capabilities.Missing(scenario.Requires); len(missing) != 0 {
						t.Fatalf("compiler target %s does not declare required capabilities for %q: %v", target.Name, scenario.Name, missing)
					}
					plan, sqlQuery, err := compileScenario(t, scenario, target.Dialect)
					if err != nil {
						t.Fatalf("%s compile: %v", target.Dialect, err)
					}
					for _, fragment := range scenario.ExpectedQuery.Fragments {
						if !strings.Contains(sqlQuery.SQL, fragment) {
							t.Fatalf("%s SQL missing %q:\n%s", target.Dialect, fragment, sqlQuery.SQL)
						}
					}
					if len(sqlQuery.Parameters) != scenario.ExpectedQuery.Parameters {
						t.Fatalf("%s parameters = %d, want %d", target.Dialect, len(sqlQuery.Parameters), scenario.ExpectedQuery.Parameters)
					}
					assertOutputSchema(t, target.Dialect, scenario, plan)
					if scenario.Name == "limit_without_order_by" {
						if plan.Limit == nil || *plan.Limit != 5 {
							t.Fatalf("%s plan limit = %v, want 5", target.Dialect, plan.Limit)
						}
						if len(plan.Sorts) != 0 {
							t.Fatalf("%s plan sorts = %v, want none", target.Dialect, plan.Sorts)
						}
					}
				})
			}
		})
	}
}

func assertOutputSchema(t *testing.T, dialect string, scenario scenarios.Scenario, plan *semanticplan.SemanticPlan) {
	t.Helper()
	schema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		t.Fatalf("%s output schema: %v", dialect, err)
	}
	if scenario.ExpectedResult == nil {
		t.Fatalf("%s scenario %q has no typed result schema", dialect, scenario.Name)
	}
	if len(schema.Columns) != len(scenario.ExpectedResult.Columns) {
		t.Fatalf("%s output columns = %d, want %d", dialect, len(schema.Columns), len(scenario.ExpectedResult.Columns))
	}
	for i, column := range schema.Columns {
		expected := scenario.ExpectedResult.Columns[i]
		if column.Name != expected.Name {
			t.Fatalf("%s output column %d name = %q, want %q", dialect, i, column.Name, expected.Name)
		}
		if got := conformanceResultKind(column.Datatype); got != expected.ValueKind {
			t.Fatalf("%s output column %q datatype = %q (%s), want %s", dialect, column.Name, column.Datatype, got, expected.ValueKind)
		}
		if i < len(scenario.Query.Dimensions) {
			if column.Kind != artifact.OutputDimension {
				t.Fatalf("%s output column %q kind = %q, want dimension", dialect, column.Name, column.Kind)
			}
			if !reflect.DeepEqual(column.Grain, scenario.Query.Dimensions[i].Grain) {
				t.Fatalf("%s output column %q grain = %v, want %v", dialect, column.Name, column.Grain, scenario.Query.Dimensions[i].Grain)
			}
		} else if column.Kind != artifact.OutputMetric {
			t.Fatalf("%s output column %q kind = %q, want metric", dialect, column.Name, column.Kind)
		}
	}
}

func conformanceResultKind(datatype ossie.DataType) scenarios.ResultValueKind {
	switch datatype {
	case ossie.DataTypeString:
		return scenarios.ResultString
	case ossie.DataTypeInteger:
		return scenarios.ResultInteger
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		return scenarios.ResultNumber
	case ossie.DataTypeBoolean:
		return scenarios.ResultBoolean
	case ossie.DataTypeDate:
		return scenarios.ResultDate
	case ossie.DataTypeTime:
		return scenarios.ResultTime
	case ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return scenarios.ResultDateTime
	default:
		return scenarios.ResultOpaque
	}
}

func TestPlanInvariants(t *testing.T) {
	t.Run("dimension-only root and join", func(t *testing.T) {
		scenario, _ := scenarios.ByName("dimensions_only")
		plan, _, err := compile(t, scenario.Query, "DORIS")
		if err != nil {
			t.Fatal(err)
		}
		if plan.Root.Name != "orders" {
			t.Fatalf("root = %s, want orders", plan.Root.Name)
		}
		if len(plan.Joins) != 1 || plan.Joins[0].Relationship.Name != "orders_to_customer" {
			t.Fatalf("joins = %#v, want orders_to_customer", plan.Joins)
		}
		if got := conversion.LoweringStrategyForPlan(plan); got != conversion.SemanticLoweringCompact {
			t.Fatalf("lowering strategy = %q, want a dimension-only query to lower as one select", got)
		}
	})
	t.Run("multi hop path", func(t *testing.T) {
		scenario, _ := scenarios.ByName("multi_hop_join")
		plan, _, err := compile(t, scenario.Query, "DORIS")
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, len(plan.Joins))
		for i := range plan.Joins {
			got[i] = plan.Joins[i].Relationship.Name
		}
		if want := []string{"orders_to_customer", "customer_to_geography"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("joins = %v, want %v", got, want)
		}
	})
	t.Run("multi hop intermediate filter preserves path", func(t *testing.T) {
		scenario, _ := scenarios.ByName("multi_hop_intermediate_filter")
		plan, _, err := compile(t, scenario.Query, "DORIS")
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, len(plan.Joins))
		for i := range plan.Joins {
			got[i] = plan.Joins[i].Relationship.Name
		}
		if want := []string{"orders_to_customer", "customer_to_geography"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("joins = %v, want %v", got, want)
		}
		if len(plan.Predicates) != 1 || plan.Predicates[0].Dataset != "customer" {
			t.Fatalf("predicates = %#v, want customer predicate", plan.Predicates)
		}
	})
	t.Run("dimensions-only composition keeps semantic graph nil", func(t *testing.T) {
		scenario, _ := scenarios.ByName("dimensions_only_filter_order_limit")
		plan, _, err := compile(t, scenario.Query, "DORIS")
		if err != nil {
			t.Fatal(err)
		}
		if got := conversion.LoweringStrategyForPlan(plan); got != conversion.SemanticLoweringCompact {
			t.Fatalf("lowering strategy = %q, want one select", got)
		}
		if plan.Limit == nil || *plan.Limit != 20 || len(plan.Sorts) != 1 || len(plan.Predicates) != 1 {
			t.Fatalf("plan composition mismatch: limit=%v sorts=%d predicates=%d", plan.Limit, len(plan.Sorts), len(plan.Predicates))
		}
	})
	t.Run("shared source fusion", func(t *testing.T) {
		scenario, _ := scenarios.ByName("derived_metric")
		plan, _, err := compile(t, scenario.Query, "DORIS")
		if err != nil {
			t.Fatal(err)
		}
		shareGroup := ""
		seen := map[string]bool{}
		for _, node := range plan.Nodes {
			base := node.NodeBase()
			if base.ID != "discounts" && base.ID != "revenue" {
				continue
			}
			source, ok := node.(semanticplan.SourceAggregateNode)
			if !ok {
				t.Fatalf("source node %q has type %T", base.ID, node)
			}
			seen[base.ID] = true
			if source.MetricState.ShareGroup == "" {
				t.Fatalf("source node %q has no canonical share group", base.ID)
			}
			if shareGroup == "" {
				shareGroup = source.MetricState.ShareGroup
			} else if source.MetricState.ShareGroup != shareGroup {
				t.Fatalf("source nodes have different share groups: %#v", plan.Nodes)
			}
		}
		if !seen["discounts"] || !seen["revenue"] {
			t.Fatalf("canonical graph missing fused source nodes: %#v", plan.Nodes)
		}
	})
	t.Run("period over period growth keeps offset dependency", func(t *testing.T) {
		scenario, _ := scenarios.ByName("time_offset_period_over_period_growth")
		plan, _, err := compile(t, scenario.Query, "DORIS")
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Nodes) != 3 {
			t.Fatalf("semantic nodes = %#v, want revenue + previous_month_revenue + revenue_growth_rate", plan.Nodes)
		}
		got := make([]string, len(plan.Nodes))
		for i, node := range plan.Nodes {
			got[i] = node.NodeBase().ID
		}
		if want := []string{"revenue", "previous_month_revenue", "revenue_growth_rate"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("semantic nodes = %v, want %v", got, want)
		}
		growth := plan.Nodes[2]
		base := growth.NodeBase()
		dependencies := make([]string, len(base.Inputs))
		for i := range base.Inputs {
			dependencies[i] = base.Inputs[i].NodeID
		}
		if growth.Kind() != semanticplan.SemanticPlanNodePostAggregate || !reflect.DeepEqual(dependencies, []string{"previous_month_revenue", "revenue"}) {
			t.Fatalf("growth node = %#v", growth)
		}
	})
}
func TestSemanticErrorsAreExplicit(t *testing.T) {
	cases := []struct {
		name string
		q    query.SemanticQuery
		code serrors.ErrorCode
	}{{name: "unknown metric", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "missing"}}}, code: serrors.ErrMetricNotFound}, {name: "unknown dimension", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "missing"}}}, code: serrors.ErrDimensionNotFound}, {name: "unreachable dimension", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}}, code: serrors.ErrRelationshipNotFound}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := compile(t, tc.q, "DORIS")
			var apiErr *serrors.Error
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code {
				t.Fatalf("error = %#v, want %s", err, tc.code)
			}
		})
	}
}
func TestCompilationIsDeterministicAcrossRegisteredTargets(t *testing.T) {
	q := scenarios.TimeGrain("time_month", query.TimeGrainMonth).Query
	for _, target := range compilerTargets {
		_, first, err := compile(t, q, target.Dialect)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			_, next, err := compile(t, q, target.Dialect)
			if err != nil {
				t.Fatal(err)
			}
			if first.SQL != next.SQL || !reflect.DeepEqual(first.Parameters, next.Parameters) {
				t.Fatalf("%s compilation is not deterministic", target.Dialect)
			}
		}
	}
}
func compile(t *testing.T, query query.SemanticQuery, dialect string) (*semanticplan.SemanticPlan, sql.SqlStatement, error) {
	t.Helper()
	return compileWithFixture(t, query, fixtures.Commerce, dialect)
}

func compileScenario(t *testing.T, scenario scenarios.Scenario, dialect string) (*semanticplan.SemanticPlan, sql.SqlStatement, error) {
	t.Helper()
	return compileWithFixture(t, scenario.Query, scenario.Fixture, dialect)
}

func compileWithFixture(t *testing.T, query query.SemanticQuery, fixture fixtures.ID, dialect string) (*semanticplan.SemanticPlan, sql.SqlStatement, error) {
	t.Helper()
	definition, ok := fixtures.Lookup(fixture)
	if !ok {
		t.Fatalf("unknown conformance fixture %q", fixture)
	}
	doc, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(definition.Project, doc)
	if err != nil {
		t.Fatal(err)
	}
	query.Project = definition.Project
	query.Model = definition.Model
	renderer := mustRenderer(t, dialect)
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query, renderer)
	if err != nil {
		return nil, sql.SqlStatement{}, err
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		return nil, sql.SqlStatement{}, err
	}
	sqlQuery, err := compilePlan(context.Background(), plan, renderer)
	if err != nil {
		return nil, sql.SqlStatement{}, err
	}
	return plan, sqlQuery, nil
}
