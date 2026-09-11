package harness

import (
	"context"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// RatioAttributionEvidenceQuery is one independently compiled dimension in the
// canonical real-engine attribution bundle.
type RatioAttributionEvidenceQuery struct {
	Dimension string
	*artifact.CompiledQuery
}

// CompileRatioAttributionBundleEvidence builds and compiles the internal
// independent-query bundle used by ratio-attribution real-engine conformance. When
// filtered is true, the same governed cohort predicate must survive in both
// periods of every dimension query.
func CompileRatioAttributionBundleEvidence(t *testing.T, dialect string, dimensions []string, filtered bool) []RatioAttributionEvidenceQuery {
	t.Helper()
	return compileRatioAttributionBundleEvidence(t, mustRenderer(t, dialect), dimensions, filtered)
}

func compileRatioAttributionBundleEvidence(t *testing.T, selected renderer.Renderer, dimensions []string, filtered bool) []RatioAttributionEvidenceQuery {
	t.Helper()
	timeDimension := semanticplan.GroupBy{
		Name:       "event_time",
		Dataset:    "ratio_events",
		Field:      &ossie.Field{Name: "event_time", Datatype: ossie.DataTypeDateTime},
		Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "event_time"},
	}
	request := attribution.ResolvedMetricAttributionRequest{
		ProjectID:        "ratio_conformance",
		MetricRef:        "conversion_rate",
		TimeDimensionRef: "event_time",
		Baseline: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		Current: semanticplan.MetricAttributionTimeRange{
			Start: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		Dimensions: append([]string(nil), dimensions...),
	}
	if filtered {
		request.Filters = []semanticplan.Predicate{{
			Filter:     query.Filter{Field: "cohort", Operator: query.FilterEQ, Value: "included"},
			Dataset:    "ratio_events",
			Field:      &ossie.Field{Name: "cohort", Datatype: ossie.DataTypeString},
			Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "cohort"},
		}}
	}
	attributionPlan := attribution.MetricAttributionPlan{
		Metric:    "conversion_rate",
		Exactness: attribution.MetricAttributionExact,
		Strategy:  semanticplan.MetricAttributionRatioMixRate,
		Components: []attribution.MetricAttributionComponent{
			{Metric: "converted", Role: attribution.MetricAttributionNumerator},
			{Metric: "sessions", Role: attribution.MetricAttributionDenominator},
		},
		Reconciliation: semanticplan.MetricAttributionReconcileMixRate,
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
	producer := func(name, source string, group semanticplan.GroupBy) semanticplan.SourceAggregateNode {
		predicates := make([]semanticplan.SemanticPlanNodePredicate, 0, len(request.Filters))
		for i := range request.Filters {
			predicate := request.Filters[i]
			predicates = append(predicates, semanticplan.SemanticPlanNodePredicate{
				Scope:       semanticplan.SemanticPredicatePreAggregation,
				OwnerNodeID: name,
				Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
				Predicate:   &predicate,
			})
		}
		return semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:                        name,
				Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof, Proof: semanticplan.SemanticPredicateProofDatasetReachability},
				Dimensions:                []string{group.Name},
				OutputGrain:               []semanticplan.GroupBy{group},
				Predicates:                predicates,
			},
			Source: semanticplan.SemanticSourceState{
				SourceRoots:      []string{"ratio_events"},
				RequiredDatasets: []string{"ratio_events"},
				Root:             semanticplan.DatasetRef{Name: "ratio_events", Source: "analytics.ratio_attribution_events"},
			},
			MetricState: semanticplan.SemanticMetricState{Metrics: []string{name}},
			Metric:      metric(name),
			Expression:  expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: source},
		}
	}
	plans := make([]attribution.MetricAttributionDimensionPlan, 0, len(dimensions))
	for _, dimension := range dimensions {
		group := semanticplan.GroupBy{
			Name:       dimension,
			Dataset:    "ratio_events",
			Field:      &ossie.Field{Name: dimension, Datatype: ossie.DataTypeString},
			Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: dimension},
		}
		attributionNode, err := attribution.BuildRatioAttributionNode(
			"conversion_rate__ratio_attribution__"+dimension,
			request,
			attributionPlan,
			semanticplan.SemanticPlanNodeInput{NodeID: "converted", Grain: []semanticplan.GroupBy{group}},
			semanticplan.SemanticPlanNodeInput{NodeID: "sessions", Grain: []semanticplan.GroupBy{group}},
			group,
			timeDimension,
		)
		if err != nil {
			t.Fatal(err)
		}
		plan := &semanticplan.SemanticPlan{
			Model:      semanticplan.ModelRef{Project: request.ProjectID, Name: "ratio_conformance"},
			Root:       semanticplan.DatasetRef{Name: "ratio_events", Source: "analytics.ratio_attribution_events"},
			Predicates: append([]semanticplan.Predicate(nil), request.Filters...),
			Groups:     []semanticplan.GroupBy{group},
			Requested:  []string{request.MetricRef},
			Nodes: []semanticplan.SemanticPlanNode{
				producer("converted", "SUM(ratio_events.converted)", group),
				producer("sessions", "SUM(ratio_events.sessions)", group),
				attributionNode,
			},
			Output: semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{group}},
		}
		plans = append(plans, attribution.MetricAttributionDimensionPlan{Dimension: dimension, Plan: plan})
	}
	bundle, err := attribution.BuildMetricAttributionBundle(request, plans)
	if err != nil {
		t.Fatal(err)
	}
	queries := make([]RatioAttributionEvidenceQuery, 0, len(bundle.Queries))
	for _, bundled := range bundle.Queries {
		compiled, compileErr := compilePlan(context.Background(), bundled.Plan, selected)
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		queries = append(queries, RatioAttributionEvidenceQuery{Dimension: bundled.Dimension, CompiledQuery: compiled})
	}
	return queries
}

// CompileRatioAttributionBundleForExecution uses the exact Renderer already
// selected by a production execution route. Real-engine tests must use this
// entrypoint instead of resolving a Renderer again by dialect name.
func CompileRatioAttributionBundleForExecution(t *testing.T, execution *ProductionExecution, dimensions []string, filtered bool) []RatioAttributionEvidenceQuery {
	t.Helper()
	if execution == nil || execution.route.Backend.Renderer == nil {
		t.Fatal("production conformance execution is not configured")
	}
	return compileRatioAttributionBundleEvidence(t, execution.route.Backend.Renderer, dimensions, filtered)
}

// RunDefinedRatioAttributionBundleEvidence executes the complete compiled
// bundle through production Runner and checks the shared ground truth.
func RunDefinedRatioAttributionBundleEvidence(t *testing.T, execution *ProductionExecution, queries []RatioAttributionEvidenceQuery) {
	t.Helper()
	for _, compiled := range queries {
		effects := map[string]float64{"store": 0.0466666666666667, "web": 0.0466666666666667}
		if compiled.Dimension == "segment" {
			effects = map[string]float64{"A": 0.0466666666666667, "B": -0.0333333333333333, "C": 0.08}
		}
		result := execution.RunCompiled(t, "ratio_attribution/"+compiled.Dimension, compiled.CompiledQuery)
		AssertDefinedRatioAttributionDimensionEvidence(t, result, compiled.Dimension, effects)
	}
}

// RunUndefinedRatioSegmentQuery executes the shared test-only evidence
// projection through Runner with a schema selected from the compiler artifact.
func RunUndefinedRatioSegmentQuery(t *testing.T, execution *ProductionExecution, segment *artifact.CompiledQuery) {
	t.Helper()
	result := execution.RunProjection(t, "ratio_attribution/undefined_segment", segment,
		[]string{"segment_defined", "segment_effect", "decomposed_delta", "reconciliation_residual", "attribution_defined"},
		"segment = 'D'")
	AssertUndefinedRatioSegmentEvidence(t, result)
}

// RunUndefinedRatioTotalQuery executes the shared zero-total evidence
// projection through the same bounded raw-query utility.
func RunUndefinedRatioTotalQuery(t *testing.T, execution *ProductionExecution, segment *artifact.CompiledQuery) {
	t.Helper()
	result := execution.RunProjection(t, "ratio_attribution/undefined_total", segment,
		[]string{"current_ratio", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined"},
		"segment = 'D'")
	AssertUndefinedRatioTotalEvidence(t, result)
}

// AssertDefinedRatioAttributionEvidence is the shared real-engine result
// contract for the canonical continuing, exit, and entry population.
func AssertDefinedRatioAttributionEvidence(t *testing.T, result scenarios.ResultSet) {
	AssertDefinedRatioAttributionDimensionEvidence(t, result, "segment", map[string]float64{
		"A": 0.0466666666666667,
		"B": -0.0333333333333333,
		"C": 0.08,
	})
}

// AssertDefinedRatioAttributionDimensionEvidence verifies independently
// calculated ground truth for one dimension query in the bundle.
func AssertDefinedRatioAttributionDimensionEvidence(t *testing.T, result scenarios.ResultSet, dimension string, effects map[string]float64) {
	t.Helper()
	index := make(map[string]int, len(result.Columns))
	for i, column := range result.Columns {
		index[column.Name] = i
	}
	required := []string{dimension, "baseline_present", "current_present", "segment_effect", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined"}
	for _, name := range required {
		if _, ok := index[name]; !ok {
			t.Fatalf("ratio attribution result is missing column %q: %#v", name, result.Columns)
		}
	}
	if len(result.Rows) != len(effects) {
		t.Fatalf("ratio attribution rows = %#v", result.Rows)
	}
	rows := make(map[string]scenarios.ResultRow, len(result.Rows))
	for _, row := range result.Rows {
		rows[row[index[dimension]].Canonical] = row
		assertRatioApprox(t, row[index["ratio_delta"]], 0.0933333333333333)
		assertRatioApprox(t, row[index["decomposed_delta"]], 0.0933333333333333)
		assertRatioApprox(t, row[index["reconciliation_residual"]], 0)
		if !ratioTruth(row[index["attribution_defined"]]) {
			t.Fatalf("attribution_defined = %#v", row[index["attribution_defined"]])
		}
	}
	for value, effect := range effects {
		row, ok := rows[value]
		if !ok {
			t.Fatalf("ratio attribution %s %q is missing: %#v", dimension, value, rows)
		}
		assertRatioApprox(t, row[index["segment_effect"]], effect)
	}
	if dimension == "segment" && (!ratioTruth(rows["A"][index["baseline_present"]]) || !ratioTruth(rows["A"][index["current_present"]]) || ratioTruth(rows["B"][index["current_present"]]) || ratioTruth(rows["C"][index["baseline_present"]])) {
		t.Fatalf("ratio entry/exit presence evidence = %#v", rows)
	}
}

// AssertUndefinedRatioSegmentEvidence checks that a present zero-denominator
// segment does not become a defined zero effect.
func AssertUndefinedRatioSegmentEvidence(t *testing.T, result scenarios.ResultSet) {
	t.Helper()
	if len(result.Rows) != 1 || ratioTruth(result.Rows[0][0]) || !result.Rows[0][1].Null || !result.Rows[0][2].Null || !result.Rows[0][3].Null || ratioTruth(result.Rows[0][4]) {
		t.Fatalf("undefined ratio segment evidence = %#v", result.Rows)
	}
}

// AssertUndefinedRatioTotalEvidence checks that a zero total denominator makes
// the global ratio, decomposition, and reconciliation explicitly undefined.
func AssertUndefinedRatioTotalEvidence(t *testing.T, result scenarios.ResultSet) {
	t.Helper()
	if len(result.Rows) != 1 || !result.Rows[0][0].Null || !result.Rows[0][1].Null || !result.Rows[0][2].Null || !result.Rows[0][3].Null || ratioTruth(result.Rows[0][4]) {
		t.Fatalf("undefined ratio total evidence = %#v", result.Rows)
	}
}

func ratioTruth(value scenarios.ResultValue) bool {
	return !value.Null && (value.Canonical == "true" || value.Canonical == "1")
}

func assertRatioApprox(t *testing.T, value scenarios.ResultValue, want float64) {
	t.Helper()
	if value.Null {
		t.Fatalf("ratio attribution value is null, want %.12f", want)
	}
	rational, ok := new(big.Rat).SetString(value.Canonical)
	if !ok {
		t.Fatalf("ratio attribution value is not numeric: %q", value.Canonical)
	}
	got, _ := rational.Float64()
	if math.Abs(got-want) > 1e-8 {
		t.Fatalf("ratio attribution value = %q, want %.12f", value.Canonical, want)
	}
}
