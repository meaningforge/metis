package compiler_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	plannerattribution "github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/sqlplan"
)

type fakeRenderer struct {
	dimensions []string
	parameters []sql.QueryParameter
}

func newRegistry(t *testing.T, renderers ...renderer.Renderer) *renderer.Registry {
	t.Helper()
	registry, err := renderer.NewRegistry(renderers...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func (*fakeRenderer) SQLDialect() sql.SQLDialect { return "DORIS" }
func (*fakeRenderer) ExpressionDialect() string  { return "DORIS" }
func (*fakeRenderer) Capabilities() renderer.Capabilities {
	return renderer.Capabilities{}
}
func (r *fakeRenderer) Render(plan *sqlplan.Plan) (sql.SqlRenderResult, error) {
	for _, block := range plan.Blocks {
		if block.ID == plan.Root && len(block.Projections) > 0 {
			r.dimensions = append(r.dimensions, block.Projections[0].Alias)
		}
	}
	return sql.SqlRenderResult{Dialect: r.SQLDialect(), SQL: "SELECT 1", Parameters: r.parameters}, nil
}

func TestCompilerReturnsPhysicalQueryWithoutEmbeddingRoutingState(t *testing.T) {
	renderer := &fakeRenderer{}
	physicalCompiler := compiler.NewCompiler(newRegistry(t, renderer))
	request := attributionRequestForPipelineTest()
	request.ProjectID = "finance"
	request.Dimensions = []string{"region"}
	plan := attributionPlanForPipelineTest(t, request, "region")

	got, err := physicalCompiler.Compile(context.Background(), plan, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	query := got.SqlRenderResult
	if query.SQL != "SELECT 1" || query.Dialect != "DORIS" {
		t.Fatalf("unexpected physical query: %#v", query)
	}
	wantSchema := artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: "region", Kind: artifact.OutputDimension, Datatype: ossie.DataTypeString},
		{Name: "baseline_value", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "current_value", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "delta", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "total_delta", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
		{Name: "contribution_pct", Kind: artifact.OutputMetric, Datatype: ossie.DataTypeDecimal},
	}}
	if !reflect.DeepEqual(got.OutputSchema, wantSchema) {
		t.Fatalf("output schema = %#v, want %#v", got.OutputSchema, wantSchema)
	}
}

func TestCompilerRejectsRendererParameterOutsideArtifactDomain(t *testing.T) {
	renderer := &fakeRenderer{parameters: []sql.QueryParameter{{Value: &struct{ Value string }{Value: "mutable"}}}}
	physicalCompiler := compiler.NewCompiler(newRegistry(t, renderer))
	request := attributionRequestForPipelineTest()
	request.Dimensions = []string{"region"}
	plan := attributionPlanForPipelineTest(t, request, "region")

	if _, err := physicalCompiler.Compile(context.Background(), plan, "DORIS"); err == nil {
		t.Fatal("expected unsupported Renderer parameter value to fail")
	}
}

func TestCompilerTakesOwnershipOfRendererParameterContainers(t *testing.T) {
	buffer := []byte("a")
	renderer := &fakeRenderer{parameters: []sql.QueryParameter{{Name: "value", Value: buffer}}}
	physicalCompiler := compiler.NewCompiler(newRegistry(t, renderer))
	request := attributionRequestForPipelineTest()
	request.Dimensions = []string{"region"}
	plan := attributionPlanForPipelineTest(t, request, "region")

	compiled, err := physicalCompiler.Compile(context.Background(), plan, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	buffer[0] = 'b'
	renderer.parameters[0].Name = "mutated"
	query := compiled.SqlRenderResult
	if query.Parameters[0].Name != "value" || string(query.Parameters[0].Value.([]byte)) != "a" {
		t.Fatalf("compiled artifact retained Renderer aliases: %#v", query.Parameters)
	}
}

func attributionRequestForPipelineTest() plannerattribution.ResolvedMetricAttributionRequest {
	return plannerattribution.ResolvedMetricAttributionRequest{
		ProjectID:        "analytics",
		MetricRef:        "revenue",
		TimeDimensionRef: "order_time",
		Baseline: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		Current: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

func attributionPlanForPipelineTest(t *testing.T, request plannerattribution.ResolvedMetricAttributionRequest, dimension string) *semanticplan.SemanticPlan {
	t.Helper()
	field := &ossie.Field{Name: dimension, Datatype: ossie.DataTypeString}
	metric := &ossie.Metric{
		Name: request.MetricRef, Datatype: ossie.DataTypeDecimal,
		CustomExtensions: []ossie.CustomExtension{{VendorName: ossie.MetisExtensionVendor, Data: `{"kind":"fill","policy":"zero"}`}},
	}
	group := semanticplan.GroupBy{
		Name:       dimension,
		Dataset:    "orders",
		Field:      field,
		Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "orders." + dimension},
	}
	timeDimension := semanticplan.GroupBy{
		Name:       request.TimeDimensionRef,
		Dataset:    "orders",
		Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "orders.order_time"},
	}
	attributionPlan := plannerattribution.MetricAttributionPlan{
		Metric:         request.MetricRef,
		Exactness:      plannerattribution.MetricAttributionExact,
		Strategy:       semanticplan.MetricAttributionAdditiveContribution,
		Components:     []plannerattribution.MetricAttributionComponent{{Metric: request.MetricRef, Role: plannerattribution.MetricAttributionValue}},
		Reconciliation: semanticplan.MetricAttributionReconcileSegmentDelta,
	}
	attributionNode, err := plannerattribution.BuildAdditiveAttributionNode(
		request.MetricRef+"__attribution__"+dimension,
		request,
		attributionPlan,
		semanticplan.SemanticPlanNodeInput{NodeID: request.MetricRef, Grain: []semanticplan.GroupBy{group}},
		group,
		timeDimension,
	)
	if err != nil {
		t.Fatal(err)
	}
	producer := semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:          request.MetricRef,
			Boundary:    semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			Dimensions:  []string{dimension},
			OutputGrain: []semanticplan.GroupBy{group},
			PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
				Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
				Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
			},
		},
		Source: semanticplan.SemanticSourceState{
			SourceRoots:      []string{"orders"},
			RequiredDatasets: []string{"orders"},
			Root:             semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{request.MetricRef}},
		Metric:      metric,
		Expression:  expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "SUM(orders.revenue)"},
	}
	return &semanticplan.SemanticPlan{
		Model:     semanticplan.ModelRef{Project: request.ProjectID, Name: "sales"},
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Groups:    []semanticplan.GroupBy{group},
		Requested: []string{request.MetricRef},
		Nodes:     []semanticplan.SemanticPlanNode{producer, attributionNode},
		Output:    semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{group}},
	}
}
