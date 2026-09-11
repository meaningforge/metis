package builder

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

func ratioAttributionBundlePlanForTest(t *testing.T, request attribution.ResolvedMetricAttributionRequest, dimension string) *semanticplan.SemanticPlan {
	t.Helper()
	field := &ossie.Field{Name: dimension, Datatype: ossie.DataTypeString}
	group := semanticplan.GroupBy{
		Name:       dimension,
		Dataset:    "events",
		Field:      field,
		Expression: expression.ResolvedExpression{SourceDialect: sqlplan.ANSISQLExpressionDialect, Source: dimension},
	}
	timeDimension := semanticplan.GroupBy{
		Name:       request.TimeDimensionRef,
		Dataset:    "events",
		Expression: expression.ResolvedExpression{SourceDialect: sqlplan.ANSISQLExpressionDialect, Source: "event_time"},
	}
	attribution, err := attribution.BuildRatioAttributionNode(
		request.MetricRef+"__ratio_attribution__"+dimension,
		request,
		ratioAttributionPlanForTest(),
		semanticplan.SemanticPlanNodeInput{NodeID: "converted", Grain: []semanticplan.GroupBy{group}},
		semanticplan.SemanticPlanNodeInput{NodeID: "sessions", Grain: []semanticplan.GroupBy{group}},
		group,
		timeDimension,
	)
	if err != nil {
		t.Fatal(err)
	}
	metric := func(name string) *ossie.Metric {
		return &ossie.Metric{
			Name:     name,
			Datatype: ossie.DataTypeDecimal,
			CustomExtensions: []ossie.CustomExtension{{
				VendorName: ossie.MetisExtensionVendor,
				Data:       `{"kind":"fill","policy":"zero"}`,
			}},
		}
	}
	ownedPredicates := func(owner string) []semanticplan.SemanticPlanNodePredicate {
		out := make([]semanticplan.SemanticPlanNodePredicate, 0, len(request.Filters))
		for i := range request.Filters {
			predicate := request.Filters[i]
			out = append(out, semanticplan.SemanticPlanNodePredicate{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: owner, Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &predicate})
		}
		return out
	}
	producer := func(name, source string) semanticplan.SourceAggregateNode {
		return semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:                        name,
				Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
				Dimensions:                []string{dimension},
				OutputGrain:               []semanticplan.GroupBy{group},
				Predicates:                ownedPredicates(name),
			},
			Source: semanticplan.SemanticSourceState{
				SourceRoots:      []string{"events"},
				RequiredDatasets: []string{"events"},
				Root:             semanticplan.DatasetRef{Name: "events", Source: "events"},
			},
			MetricState: semanticplan.SemanticMetricState{Metrics: []string{name}},
			Metric:      metric(name),
			Expression:  expression.ResolvedExpression{SourceDialect: sqlplan.ANSISQLExpressionDialect, Source: source},
		}
	}
	return &semanticplan.SemanticPlan{
		Model:      semanticplan.ModelRef{Project: request.ProjectID, Name: "conversion"},
		Root:       semanticplan.DatasetRef{Name: "events", Source: "events"},
		Predicates: append([]semanticplan.Predicate(nil), request.Filters...),
		Groups:     []semanticplan.GroupBy{group},
		Requested:  []string{request.MetricRef},
		Nodes: []semanticplan.SemanticPlanNode{
			producer("converted", "SUM(events.converted)"),
			producer("sessions", "SUM(events.sessions)"),
			attribution,
		},
		Output: semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{group}},
	}
}

func ratioAttributionSQLPlanForTest(t *testing.T) (*semanticplan.SemanticPlan, *sqlplan.Plan) {
	t.Helper()
	request := ratioAttributionRequestForTest()
	plan := ratioAttributionBundlePlanForTest(t, request, "country")
	physical, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	return plan, physical
}

func queryBlockByIDForTest(t *testing.T, plan *sqlplan.Plan, id sqlplan.QueryBlockID) sqlplan.QueryBlock {
	t.Helper()
	for _, block := range plan.Blocks {
		if block.ID == id {
			return block
		}
	}
	t.Fatalf("query block %q is missing", id)
	return sqlplan.QueryBlock{}
}

func TestLoweringStrategySelectsRatioAttribution(t *testing.T) {
	plan := ratioAttributionBundlePlanForTest(t, ratioAttributionRequestForTest(), "country")
	if got := conversion.LoweringStrategyForPlan(plan); got != conversion.SemanticLoweringRatioAttribution {
		t.Fatalf("lowering strategy = %q, want %q", got, conversion.SemanticLoweringRatioAttribution)
	}
}

func TestRatioAttributionSQLPlanOwnsPeriodUnionAndOrderedOperands(t *testing.T) {
	_, physical := ratioAttributionSQLPlanForTest(t)
	if len(physical.Blocks) != 7 {
		t.Fatalf("ratio attribution blocks = %d, want 7", len(physical.Blocks))
	}
	for _, id := range []sqlplan.QueryBlockID{ratioAttributionBaselineBlockID, ratioAttributionCurrentBlockID} {
		block := queryBlockByIDForTest(t, physical, id)
		if len(block.Predicates) != 2 || block.Predicates[0].Operator != query.FilterGTE || block.Predicates[1].Operator != query.FilterLT {
			t.Fatalf("period block %q predicates = %#v", id, block.Predicates)
		}
		if id == ratioAttributionBaselineBlockID && block.Predicates[0].Values[0] != "2026-07-01 00:00:00" {
			t.Fatalf("baseline physical instant = %#v", block.Predicates[0].Values)
		}
		if len(block.Projections) != 4 || block.Projections[1].Alias != "converted" || block.Projections[2].Alias != "sessions" {
			t.Fatalf("period block %q does not preserve ordered operands: %#v", id, block.Projections)
		}
	}
	aligned := queryBlockByIDForTest(t, physical, ratioAttributionAlignedBlockID)
	if len(aligned.Joins) != 1 || aligned.Joins[0].Kind != sqlplan.JoinFullOuter {
		t.Fatalf("ratio population alignment = %#v", aligned.Joins)
	}
	if len(aligned.Projections) != 7 || aligned.Projections[1].Alias != ratioAttributionBaselinePresentAlias || aligned.Projections[2].Alias != ratioAttributionCurrentPresentAlias {
		t.Fatalf("ratio aligned evidence = %#v", aligned.Projections)
	}
}

func TestRatioAttributionSQLPlanOwnsSymmetricEffectsAndUndefinedEvidence(t *testing.T) {
	_, physical := ratioAttributionSQLPlanForTest(t)
	effects := queryBlockByIDForTest(t, physical, ratioAttributionEffectsBlockID)
	aliases := make(map[string]sqlplan.Expr, len(effects.Projections))
	for _, projection := range effects.Projections {
		aliases[projection.Alias] = projection.Expr
	}
	for _, alias := range []string{ratioAttributionRateEffectAlias, ratioAttributionMixEffectAlias, ratioAttributionEntryEffectAlias, ratioAttributionExitEffectAlias} {
		if _, ok := aliases[alias].(sqlplan.CaseExpr); !ok {
			t.Fatalf("%s is not an explicit searched CASE: %#v", alias, aliases[alias])
		}
	}
	baselineRate, ok := aliases[ratioAttributionBaselineRateAlias].(sqlplan.CastExpr)
	if !ok || baselineRate.Type != sqlplan.CastDecimal20Scale12 {
		t.Fatalf("baseline rate lacks stable intermediate decimal precision: %#v", aliases[ratioAttributionBaselineRateAlias])
	}
	if _, ok := baselineRate.Expr.(sqlplan.ParenthesizedExpr); !ok {
		t.Fatalf("baseline rate lacks guarded grouped division: %#v", aliases[ratioAttributionBaselineRateAlias])
	}
	if _, ok := aliases[ratioAttributionSegmentDefinedAlias].(sqlplan.ParenthesizedExpr); !ok {
		t.Fatalf("segment defined evidence is not typed logic: %#v", aliases[ratioAttributionSegmentDefinedAlias])
	}
}

func TestRatioAttributionSQLPlanReconcilesBeforeDeterministicOrdering(t *testing.T) {
	_, physical := ratioAttributionSQLPlanForTest(t)
	reconciliation := queryBlockByIDForTest(t, physical, ratioAttributionReconciliationBlockID)
	if len(reconciliation.Projections) != 2 || reconciliation.Projections[0].Alias != ratioAttributionDecomposedDeltaAlias || reconciliation.Projections[1].Alias != ratioAttributionUndefinedCountAlias {
		t.Fatalf("reconciliation projections = %#v", reconciliation.Projections)
	}
	root := queryBlockByIDForTest(t, physical, physical.Root)
	if len(root.Inputs) != 6 || root.Inputs[0].Block != ratioAttributionBaselineBlockID || root.Inputs[1].Block != ratioAttributionCurrentBlockID || root.Inputs[2].Block != ratioAttributionAlignedBlockID || root.Inputs[3].Block != ratioAttributionTotalsBlockID || root.Inputs[4].Block != ratioAttributionEffectsBlockID || root.Inputs[5].Block != ratioAttributionReconciliationBlockID {
		t.Fatalf("ratio root CTE topology = %#v", root.Inputs)
	}
	if len(root.Projections) != 25 || root.Projections[22].Alias != ratioAttributionDecomposedDeltaAlias || root.Projections[23].Alias != ratioAttributionResidualAlias || root.Projections[24].Alias != ratioAttributionDefinedAlias {
		t.Fatalf("ratio root projection contract = %#v", root.Projections)
	}
	if len(root.OrderBy) != 3 || root.OrderBy[0].Direction != query.SortAsc || root.OrderBy[1].Direction != query.SortDesc || root.OrderBy[2].Direction != query.SortAsc {
		t.Fatalf("ratio deterministic ordering = %#v", root.OrderBy)
	}
}

func TestRatioAttributionPhysicalInputsFailClosedOnDivergentSourcePopulation(t *testing.T) {
	plan := ratioAttributionBundlePlanForTest(t, ratioAttributionRequestForTest(), "country")
	denominator := plan.Nodes[1].(semanticplan.SourceAggregateNode)
	denominator.Source.Root = semanticplan.DatasetRef{Name: "other", Source: "other"}
	denominator.Source.SourceRoots = []string{"other"}
	denominator.Source.RequiredDatasets = []string{"other"}
	plan.Nodes[1] = denominator
	_, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB"))
	if err == nil || !strings.Contains(err.Error(), "one governed source population") {
		t.Fatalf("divergent ratio population error = %v", err)
	}
}

func TestSemanticPlanRejectsMultipleIndependentAttributionRoots(t *testing.T) {
	plan := ratioAttributionBundlePlanForTest(t, ratioAttributionRequestForTest(), "country")
	_, additive := buildAdditiveAttributionNodeForTest(t)
	plan.Nodes = append(plan.Nodes, additive)
	if err := semanticplan.ValidateSemanticPlan(plan); err == nil || !strings.Contains(err.Error(), "exactly one independent attribution node") {
		t.Fatalf("multiple attribution roots error = %v", err)
	}
}

func TestRatioAttributionOutputSchemaMatchesPhysicalProjection(t *testing.T) {
	plan, physical := ratioAttributionSQLPlanForTest(t)
	schema, err := conversion.BuildOutputSchema(plan)
	if err != nil {
		t.Fatal(err)
	}
	root := queryBlockByIDForTest(t, physical, physical.Root)
	if len(schema.Columns) != len(root.Projections) {
		t.Fatalf("schema columns = %d, physical projections = %d", len(schema.Columns), len(root.Projections))
	}
	for i := range schema.Columns {
		if schema.Columns[i].Name != root.Projections[i].Alias {
			t.Fatalf("schema column %d = %q, projection = %q", i, schema.Columns[i].Name, root.Projections[i].Alias)
		}
	}
	if schema.Columns[1].Datatype != ossie.DataTypeBoolean || schema.Columns[24].Datatype != ossie.DataTypeBoolean {
		t.Fatalf("ratio defined evidence datatypes = %q, %q", schema.Columns[1].Datatype, schema.Columns[24].Datatype)
	}
}

func TestRatioAttributionBundleLowersIndependentDimensions(t *testing.T) {
	request := ratioAttributionRequestForTest()
	request.Dimensions = []string{"country", "channel"}
	bundle, err := attribution.BuildMetricAttributionBundle(request, []attribution.MetricAttributionDimensionPlan{
		{Dimension: "country", Plan: ratioAttributionBundlePlanForTest(t, request, "country")},
		{Dimension: "channel", Plan: ratioAttributionBundlePlanForTest(t, request, "channel")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Queries[0].Dimension != "channel" || bundle.Queries[1].Dimension != "country" {
		t.Fatalf("ratio bundle order = %#v", bundle.Queries)
	}
	for _, query := range bundle.Queries {
		physical, err := conversion.BuildSQLPlan(query.Plan, mustRenderer(t, "DUCKDB"))
		if err != nil {
			t.Fatalf("lower ratio dimension %q: %v", query.Dimension, err)
		}
		root := queryBlockByIDForTest(t, physical, physical.Root)
		if root.Projections[0].Alias != query.Dimension {
			t.Fatalf("ratio bundle combined dimensions: %#v", root.Projections[0])
		}
	}
}
