package harness

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/service/policy"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/sqlplan"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

type fixtureDataPolicy struct{ calls int }

func (p *fixtureDataPolicy) Evaluate(_ context.Context, req policy.Request) (policy.Decision, error) {
	p.calls++
	out := policy.Decision{Effect: policy.Constrained}
	for _, source := range req.Sources {
		out.Sources = append(out.Sources, policy.SourceConstraint{Dataset: source.Dataset, RowPredicates: []policy.Predicate{{Field: policy.FieldRef{Dataset: source.Dataset, Field: "amount"}, Operator: query.FilterGT, Values: []any{int64(10)}}}})
	}
	return out, nil
}

// Each production engine executes the same service-enforced predicate, including
// driver parameter binding. A renderer-only SQL assertion is not enough here.
func runDataPolicyContract(t *testing.T, backend Backend) {
	t.Run("data_policy", func(t *testing.T) {
		backend.Prepare(t, enginefixture.MetricScale)
		execution := backend.OpenExecution(t)
		doc := &ossie.Document{Version: ossie.SupportedSpecVersion, SemanticModel: []ossie.SemanticModel{{Name: "secured", Datasets: []ossie.Dataset{{Name: "orders", Source: "analytics.metric_scale_orders", Fields: []ossie.Field{{Name: "amount", Datatype: ossie.DataTypeDecimal, Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "orders.amount"}}}}}}}, Metrics: []ossie.Metric{{Name: "revenue", Datatype: ossie.DataTypeDecimal, Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{Dialect: ossie.DialectANSISQL, Expression: "SUM(orders.amount)"}}}}}}}}
		m, err := manifest.BuildProjectManifest("secured", doc)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := renderer.NewRegistry(execution.route.Backend.Renderer)
		if err != nil {
			t.Fatal(err)
		}
		adapter := &fixtureDataPolicy{}
		s := service.NewCompileService(resolver.New(manifest.NewStore(m)), planner.New(), compiler.NewCompiler(registry)).WithDataAccessPolicy(adapter)
		ctx := auth.WithPrincipal(context.Background(), &auth.Principal{TenantID: "tenant", SubjectID: "subject", Scopes: []string{auth.ScopeAll}})
		compiled, err := s.Compile(ctx, service.CompileRequest{Dialect: execution.route.Backend.SQLDialect(), Query: query.SemanticQuery{Project: "secured", Model: "secured", Metrics: []query.MetricRef{{Name: "revenue"}}}})
		if err != nil {
			t.Fatal(err)
		}
		if adapter.calls != 1 {
			t.Fatalf("policy evaluations = %d", adapter.calls)
		}
		actual := execution.RunCompiled(t, "data_policy", compiled)
		value, err := scenarios.ParseResultValue(scenarios.ResultNumber, "20")
		if err != nil {
			t.Fatal(err)
		}
		expected := &scenarios.ResultExpectation{ResultSet: scenarios.ResultSet{Columns: []scenarios.ResultColumn{{Name: "revenue", ValueKind: scenarios.ResultNumber}}, Rows: []scenarios.ResultRow{{value}}}, Comparison: scenarios.ResultUnordered}
		if err := compareResult(expected, actual); err != nil {
			t.Fatal(err)
		}
		// An absent nullable-side row must not remove its preserved-side row.
		// This catches the unsafe outer-WHERE placement on a real engine.
		plan := &sqlplan.Plan{Root: "root", Blocks: []sqlplan.QueryBlock{{ID: "root",
			From:        sqlplan.RelationRef{Alias: "left_rows", Source: &sqlplan.TableSource{Name: "analytics.metric_scale_orders"}},
			Projections: []sqlplan.Projection{{Expr: sqlplan.ColumnRef{Table: "left_rows", Name: "id"}, Alias: "id"}, {Expr: sqlplan.ColumnRef{Table: "right_rows", Name: "id"}, Alias: "matched"}},
			Joins:       []sqlplan.Join{{Kind: sqlplan.JoinFullOuter, Relation: sqlplan.RelationRef{Alias: "right_rows", FilteredSource: &sqlplan.FilteredTableSource{Name: "analytics.metric_scale_orders", Predicates: []sqlplan.Predicate{{Left: sqlplan.ColumnRef{Name: "amount"}, Operator: query.FilterGT, Values: []any{int64(10)}}}}}, On: sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: "left_rows", Name: "id"}, Operator: "=", Right: sqlplan.ColumnRef{Table: "right_rows", Name: "id"}}}},
		}}}
		physical, err := execution.route.Backend.Renderer.Render(plan)
		if err != nil {
			t.Fatal(err)
		}
		joined := execution.RunRawQuery(t, "data_policy_outer_join", physical, artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "id", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString}, {Name: "matched", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString}}})
		first, _ := scenarios.ParseResultValue(scenarios.ResultString, "o1")
		second, _ := scenarios.ParseResultValue(scenarios.ResultString, "o2")
		joinedExpected := &scenarios.ResultExpectation{ResultSet: scenarios.ResultSet{Columns: []scenarios.ResultColumn{{Name: "id", ValueKind: scenarios.ResultString}, {Name: "matched", ValueKind: scenarios.ResultString}}, Rows: []scenarios.ResultRow{{first, scenarios.NullResultValue(scenarios.ResultString)}, {second, second}}}, Comparison: scenarios.ResultUnordered}
		if err := compareResult(joinedExpected, joined); err != nil {
			t.Fatal(err)
		}
	})
}
