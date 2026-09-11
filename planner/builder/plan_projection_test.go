package builder

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

// The property the previous implementation could not satisfy, and the reason
// this projection exists.
//
// Fingerprinting used to reflect over everything reachable from a plan, so
// adding a description, an analysis result, or any derived evidence to a
// reachable struct churned every fingerprint. That is what pushed
// aggregation-algebra evidence onto manifest.AnalyzedExpression in #376: a
// measurement was deciding where code could live.
//
// Each case below changes state that is genuinely reachable from the plan and
// genuinely not part of its structure. The fingerprint must not move.
func TestNonProjectedStateDoesNotChangeTheFingerprint(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(plan *semanticplan.SemanticPlan)
	}{
		{
			name: "metric documentation",
			mutate: func(plan *semanticplan.SemanticPlan) {
				plan.Projections[0].Metric.Description = "revenue, net of refunds"
				plan.Projections[0].Metric.AIContext = map[string]any{"note": "agent hint"}
			},
		},
		{
			name: "field documentation",
			mutate: func(plan *semanticplan.SemanticPlan) {
				plan.Groups[0].Field.Label = "Order date"
				plan.Groups[0].Field.Description = "when the order was placed"
			},
		},
		{
			name: "raw custom extensions already represented in typed form",
			mutate: func(plan *semanticplan.SemanticPlan) {
				plan.Projections[0].Metric.CustomExtensions = []ossie.CustomExtension{
					{VendorName: "METIS", Data: `{"kind":"cumulative"}`},
				}
			},
		},
		{
			name: "the model's multi-dialect declaration behind a resolved expression",
			mutate: func(plan *semanticplan.SemanticPlan) {
				plan.Projections[0].Metric.Expression = ossie.Expression{Dialects: []ossie.DialectExpression{
					{Dialect: ossie.DialectANSISQL, Expression: "SUM(orders.amount)"},
					{Dialect: ossie.DialectClickHouse, Expression: "sum(orders.amount)"},
				}}
			},
		},
		{
			name: "expression analysis derived from the projected source text",
			mutate: func(plan *semanticplan.SemanticPlan) {
				plan.Projections[0].Expression = plan.Projections[0].Expression.WithAnalysis(expression.BoundExpression{}, expression.TypedExpression{})
			},
		},
		{
			name: "operator-facing prose explaining a rollup refusal",
			mutate: func(plan *semanticplan.SemanticPlan) {
				node := plan.Nodes[0].(semanticplan.SourceAggregateNode)
				node.Rollup.Reason = "distinct count retains no partial state"
				plan.Nodes[0] = node
			},
		},
		{
			name: "stage extension capability evidence",
			mutate: func(plan *semanticplan.SemanticPlan) {
				node := plan.Nodes[0].(semanticplan.SourceAggregateNode)
				node.Base.ExtensionEvidence = []extension.Evidence{projectionTestEvidence{
					identity: extension.Identity{Namespace: "acme", Kind: "scaled_metric", Scope: "metric"},
					note:     "capability resolution provenance",
				}}
				plan.Nodes[0] = node
			},
		},
		{
			name: "optimizer trace",
			mutate: func(plan *semanticplan.SemanticPlan) {
				plan.OptimizationTrace = append(plan.OptimizationTrace, semanticplan.OptimizationStep{Rule: "some-rule", Changed: true})
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := populatedFixturePlan()
			before, err := semanticplan.Fingerprint(base)
			if err != nil {
				t.Fatal(err)
			}
			mutated := populatedFixturePlan()
			tt.mutate(mutated)
			after, err := semanticplan.Fingerprint(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatalf("non-projected state changed the fingerprint\nbefore=%s\nafter= %s", before, after)
			}
		})
	}
}

// The other half: everything the projection does declare as structure must move
// the fingerprint. An inclusion list that quietly omitted a field would be a
// fingerprint that reports less than it claims, which is worse than one that
// reports too much.
func TestProjectedStructureChangesTheFingerprint(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(plan *semanticplan.SemanticPlan)
	}{
		{name: "model", mutate: func(p *semanticplan.SemanticPlan) { p.Model.Name = "other" }},
		{name: "root dataset", mutate: func(p *semanticplan.SemanticPlan) { p.Root.Source = "analytics.other" }},
		{name: "projection name", mutate: func(p *semanticplan.SemanticPlan) { p.Projections[0].Name = "other" }},
		{name: "projection kind", mutate: func(p *semanticplan.SemanticPlan) { p.Projections[0].Kind = semanticplan.ProjectionDimension }},
		{name: "metric identity", mutate: func(p *semanticplan.SemanticPlan) { p.Projections[0].Metric.Name = "other" }},
		{name: "metric datatype", mutate: func(p *semanticplan.SemanticPlan) { p.Projections[0].Metric.Datatype = ossie.DataTypeInteger }},
		{name: "resolved dialect", mutate: func(p *semanticplan.SemanticPlan) { p.Projections[0].Expression.SourceDialect = "CLICKHOUSE" }},
		{name: "resolved source text", mutate: func(p *semanticplan.SemanticPlan) { p.Projections[0].Expression.Source = "SUM(orders.net)" }},
		{name: "group grain", mutate: func(p *semanticplan.SemanticPlan) { grain := query.TimeGrainDay; p.Groups[0].Grain = &grain }},
		{name: "field identity", mutate: func(p *semanticplan.SemanticPlan) { p.Groups[0].Field.Name = "other" }},
		{name: "predicate operator", mutate: func(p *semanticplan.SemanticPlan) { p.Predicates[0].Filter.Operator = query.FilterNEQ }},
		{name: "predicate value", mutate: func(p *semanticplan.SemanticPlan) { p.Predicates[0].Filter.Value = "refunded" }},
		{name: "limit", mutate: func(p *semanticplan.SemanticPlan) { limit := 10; p.Limit = &limit }},
		{name: "node id", mutate: func(p *semanticplan.SemanticPlan) {
			node := p.Nodes[0].(semanticplan.SourceAggregateNode)
			node.Base.ID = "other"
			p.Nodes[0] = node
		}},
		{name: "node kind", mutate: func(p *semanticplan.SemanticPlan) {
			node := p.Nodes[0].(semanticplan.SourceAggregateNode)
			p.Nodes[0] = semanticplan.PostAggregateNode{Base: node.Base, Source: node.Source, MetricState: node.MetricState, Metric: node.Metric, Expression: node.Expression}
		}},
		{name: "node share group", mutate: func(p *semanticplan.SemanticPlan) {
			node := p.Nodes[0].(semanticplan.SourceAggregateNode)
			node.MetricState.ShareGroup = "other"
			p.Nodes[0] = node
		}},
		{name: "rollup mergeability", mutate: func(p *semanticplan.SemanticPlan) {
			node := p.Nodes[0].(semanticplan.SourceAggregateNode)
			node.Rollup.Mergeable = false
			p.Nodes[0] = node
		}},
		{name: "rollup merge operator", mutate: func(p *semanticplan.SemanticPlan) {
			node := p.Nodes[0].(semanticplan.SourceAggregateNode)
			node.Rollup.Merge = "MAX"
			p.Nodes[0] = node
		}},
		{name: "requested membership", mutate: func(p *semanticplan.SemanticPlan) { p.Requested = append(p.Requested, "extra") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := populatedFixturePlan()
			before, err := semanticplan.Fingerprint(base)
			if err != nil {
				t.Fatal(err)
			}
			mutated := populatedFixturePlan()
			tt.mutate(mutated)
			after, err := semanticplan.Fingerprint(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if before == after {
				t.Fatalf("projected structure did not change the fingerprint; the projection is missing this field")
			}
		})
	}
}

// Shared-grain evidence is projected both from the plan-level resolution and
// from each semantic node. Keep a direct graph-level regression for the
// multi-path form: a plan fixture that also carries SharedGrain could otherwise
// hide an omission in the stage projection.
func TestSemanticPlanFingerprintIncludesSharedGrainRelationshipPaths(t *testing.T) {
	left := *semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{{
		ID:   "revenue",
		Kind: semanticplan.SemanticPlanNodeSourceAggregate,
		Node: semanticplan.SourceAggregateNode{},
		SharedGrainEvidence: &semanticplan.MetricSharedGrainEvidence{
			Metric:            "revenue",
			RootDataset:       "orders",
			RootDatasets:      []string{"orders", "customers"},
			GrainKey:          "customer_id",
			RelationshipPath:  []string{"orders_customer"},
			RelationshipPaths: [][]string{{"orders_customer"}},
			FanoutSafe:        true,
		},
	}})
	right := semanticplan.ClonePlan(&left)
	node := right.Nodes[0].(semanticplan.SourceAggregateNode)
	node.MetricState.SharedGrainEvidence.RelationshipPaths = [][]string{{"orders_region", "region_customer"}}
	right.Nodes[0] = node

	leftFingerprint, err := semanticplan.Fingerprint(&left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint == rightFingerprint {
		t.Fatal("shared-grain relationship paths did not change the semantic plan fingerprint")
	}
}

func TestSemanticPlanFingerprintProjectionUsesNodeVocabulary(t *testing.T) {
	var raw strings.Builder
	if err := semanticplan.ProjectPlan(&raw, populatedFixturePlan()); err != nil {
		t.Fatal(err)
	}
	got := raw.String()
	for _, want := range []string{"nodes[", "node{", "node_id=", "node_predicate{", "owner_node_id="} {
		if !strings.Contains(got, want) {
			t.Fatalf("projection does not contain %q", want)
		}
	}
	for _, stale := range []string{"stages[", "stage{", "stage_id=", "stage_predicate{", "owner_stage_id=", ":stage_semantics;"} {
		if strings.Contains(got, stale) {
			t.Fatalf("projection contains retired vocabulary %q", stale)
		}
	}
	if got := string(semanticplan.SemanticPredicateProofNodeSemantics); got != "node_semantics" {
		t.Fatalf("node semantics proof = %q", got)
	}
}

// An inclusion list is only safe if omission is loud. A structural field added
// to the plan and forgotten here would silently narrow the fingerprint, and
// nothing else in the suite would notice.
//
// Every field of every plan-shaping type must therefore be accounted for: named
// as projected, or named as deliberately excluded with the reason. The check is
// the field set, so adding a field to any of these types fails until someone
// decides which it is.
func TestEveryPlanFieldIsProjectedOrDeliberatelyExcluded(t *testing.T) {
	for _, disposition := range planFieldDispositions() {
		t.Run(disposition.typ.String(), func(t *testing.T) {
			declared := map[string]bool{}
			for _, name := range disposition.projected {
				declared[name] = true
			}
			for name := range disposition.excluded {
				if declared[name] {
					t.Errorf("field %q is declared both projected and excluded", name)
				}
				declared[name] = true
			}

			var undeclared []string
			for i := 0; i < disposition.typ.NumField(); i++ {
				name := disposition.typ.Field(i).Name
				if !declared[name] {
					undeclared = append(undeclared, name)
					continue
				}
				delete(declared, name)
			}
			if len(undeclared) != 0 {
				sort.Strings(undeclared)
				t.Errorf("%s has fields with no declared fingerprint disposition: %s\n"+
					"Decide whether each is canonical plan structure or derived evidence, "+
					"then project it in plan_projection.go or exclude it here with the reason.",
					disposition.typ, strings.Join(undeclared, ", "))
			}
			if len(declared) != 0 {
				stale := make([]string, 0, len(declared))
				for name := range declared {
					stale = append(stale, name)
				}
				sort.Strings(stale)
				t.Errorf("%s declares dispositions for fields it no longer has: %s", disposition.typ, strings.Join(stale, ", "))
			}
		})
	}
}

type planFieldDisposition struct {
	typ       reflect.Type
	projected []string
	excluded  map[string]string
}

func planFieldDispositions() []planFieldDisposition {
	return []planFieldDisposition{
		{
			typ: reflect.TypeOf(semanticplan.SemanticPlan{}),
			projected: []string{
				"Model", "Root", "Joins", "Projections", "Predicates", "Groups", "Sorts",
				"Limit", "SharedGrain", "DenseCalendar", "CustomDenseCalendar",
				"Requested", "Nodes", "Output", "PolicyScope",
			},
			excluded: map[string]string{
				"OptimizationTrace": "how the plan was reached, not what it is",
			},
		},
		{
			typ:       reflect.TypeOf(semanticplan.SemanticOutputContract{}),
			projected: []string{"Projections", "Grain", "Predicates", "OrderBy", "Limit"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.Projection{}),
			projected: []string{"Name", "Kind", "Metric", "Field", "Dataset", "Datasets", "Grain", "Expression", "CustomCalendar"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.Predicate{}),
			projected: []string{"Filter", "Dataset", "Field", "Expression"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.GroupBy{}),
			projected: []string{"Name", "Dataset", "Field", "Grain", "Expression", "CustomCalendar"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.Sort{}),
			projected: []string{"Name", "Kind", "Direction", "Metric", "Field", "Dataset", "Expression"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.Join{}),
			projected: []string{"Relationship", "Temporal", "FromDataset", "FromSource", "ToDataset", "ToSource", "Policy"},
			excluded:  map[string]string{},
		},
		{
			typ: reflect.TypeOf(semanticplan.MetricSharedGrainEvidence{}),
			projected: []string{
				"Metric", "RootDataset", "RootDatasets", "GrainKey", "RelationshipPath",
				"RelationshipPaths", "FanoutSafe",
			},
			excluded: map[string]string{},
		},
		{
			typ: reflect.TypeOf(semanticplan.OffsetToGrainPlan{}),
			projected: []string{
				"TimeDimension", "QueryGrain", "BoundaryGrain", "CustomCalendar", "Dataset",
				"BoundaryBucket", "BoundaryExpression", "QueryOrdinal", "OrdinalExpression",
			},
			excluded: map[string]string{},
		},
		{
			typ: reflect.TypeOf(semanticplan.ConversionPlan{}),
			projected: []string{
				"BaseMetric", "ConversionMetric", "BaseRoot", "ConversionRoot", "BaseEventKey",
				"ConversionEventKey", "BaseTime", "ConversionTime", "Entity", "ConstantProperties",
				"Calculation", "Window", "Assignment", "CandidateMatch",
			},
			excluded: map[string]string{},
		},
		{
			typ: reflect.TypeOf(semanticplan.ConversionCandidateMatchPlan{}),
			projected: []string{
				"PartitionBy", "Equality", "BaseTime", "ConversionTime", "Window", "OrderBy",
				"KeepRank", "AggregateBaseIndependently",
			},
			excluded: map[string]string{},
		},
		{
			typ: reflect.TypeOf(semanticplan.ConversionPhysicalInputPlan{}),
			projected: []string{
				"BaseDataset", "ConversionDataset", "BaseEventKey", "ConversionEventKey",
				"BaseTime", "ConversionTime", "Entity", "ConstantProperties", "BaseValue", "ConversionValue",
			},
			excluded: map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.ConversionFieldRef{}),
			projected: []string{"Dataset", "Name"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.ConversionPhysicalFieldRef{}),
			projected: []string{"Dataset", "Name", "Expression"},
			excluded:  map[string]string{},
		},
		{
			typ:       reflect.TypeOf(semanticplan.ConversionEventValuePlan{}),
			projected: []string{"Metric", "Dataset", "Kind", "Field"},
			excluded:  map[string]string{},
		},
		{
			// The boundary that motivated the projection. Analysis is derived
			// from Source, so fingerprinting it makes every addition to
			// expression analysis look like a plan change.
			typ:       reflect.TypeOf(expression.ResolvedExpression{}),
			projected: []string{"SourceDialect", "Source"},
			excluded: map[string]string{
				"Analysis":          "derived from the projected source text",
				"ExtensionEvidence": "derived evidence about the expression, not plan structure",
			},
		},
		{
			typ:       reflect.TypeOf(ossie.Metric{}),
			projected: []string{"Name", "Datatype"},
			excluded: map[string]string{
				"AIContext":        "model documentation",
				"Description":      "model documentation",
				"Expression":       "the selected dialect already reaches the plan as a ResolvedExpression",
				"CustomExtensions": "semantic-critical extensions already reach the plan as typed evaluation payloads",
			},
		},
		{
			typ:       reflect.TypeOf(ossie.Field{}),
			projected: []string{"Name", "Datatype"},
			excluded: map[string]string{
				"AIContext":        "model documentation",
				"Description":      "model documentation",
				"Label":            "model documentation",
				"Dimension":        "a dimension declaration that changes planning changes the plan structure this projects",
				"Expression":       "the selected dialect already reaches the plan as a ResolvedExpression",
				"CustomExtensions": "semantic-critical extensions already reach the plan as typed evaluation payloads",
			},
		},
	}
}

// projectionTestEvidence stands in for a registered capability's runtime
// evidence. It exists to prove that evidence does not reach the fingerprint.
type projectionTestEvidence struct {
	identity extension.Identity
	note     string
}

func (e projectionTestEvidence) ExtensionIdentity() extension.Identity { return e.identity }

// populatedFixturePlan is a plan in which every projected type is reachable and
// settable. Its completeness is enforced rather than assumed: a projected field
// this fixture cannot reach fails TestEveryProjectedFieldMovesTheFingerprint by
// name, so the fixture cannot quietly fall behind the projection.
func populatedFixturePlan() *semanticplan.SemanticPlan {
	grain := query.TimeGrainMonth
	limit := 100
	metric := &ossie.Metric{Name: "revenue", Datatype: ossie.DataTypeDecimal}
	field := &ossie.Field{Name: "order_date", Datatype: ossie.DataTypeDate}
	revenueExpression := expression.NewResolvedExpression("DUCKDB", "SUM(orders.amount)")
	dateExpression := expression.NewResolvedExpression("DUCKDB", "orders.order_date")
	dataset := semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"}

	calendarGrouping := &semanticplan.CustomCalendarGrouping{
		Spec: ossie.CustomCalendarSpec{
			Kind: "custom_calendar", Dataset: "calendar", BaseTime: "day",
			Grains: []ossie.CustomCalendarGrain{{Name: "fiscal_week", BucketDimension: "week_start", OrdinalDimension: "week_ordinal"}},
		},
		Grain: grain, Dataset: "calendar", DatasetSource: "analytics.calendar",
		BaseTimeField: field, BaseTimeExpression: "calendar.day",
		BucketField: field, BucketExpression: "calendar.week_start",
		OrdinalField: field, OrdinalExpression: "calendar.week_ordinal",
		Levels: map[string]semanticplan.CustomCalendarLevel{"fiscal_week": {
			Grain:       ossie.CustomCalendarGrain{Name: "fiscal_week"},
			BucketField: field, BucketExpression: "calendar.week_start",
			OrdinalField: field, OrdinalExpression: "calendar.week_ordinal",
		}},
	}
	offsetPlan := semanticplan.CustomCalendarOffsetPlan{
		Grain: grain, Count: -1, Dataset: dataset,
		BucketField: field, BucketExpression: "calendar.week_start",
		OrdinalField: field, OrdinalExpression: "calendar.week_ordinal",
	}
	cumulativePlan := semanticplan.CustomCalendarCumulativePlan{
		Grain: grain, Count: 3, Dataset: dataset,
		BucketField: field, BucketExpression: "calendar.week_start",
		OrdinalField: field, OrdinalExpression: "calendar.week_ordinal",
	}
	grainToDatePlan := semanticplan.CustomCalendarGrainToDatePlan{
		QueryGrain: grain, ResetGrain: query.TimeGrainYear, Dataset: dataset,
		ResetBucketField: field, ResetBucketExpression: "calendar.year_start",
		QueryOrdinalField: field, QueryOrdinalExpression: "calendar.week_ordinal",
	}
	densePlan := &semanticplan.DenseCalendarPlan{
		Dataset: dataset, TimeField: field, TimeExpression: "calendar.day",
		QueryTimeDimension: "order_date", Grain: grain,
		OutputPredicates: []semanticplan.Predicate{{Filter: query.Filter{Field: "day", Operator: query.FilterGTE, Value: "2026-01-01"}, Dataset: "calendar", Field: field, Expression: dateExpression}},
		ReadPredicates:   []semanticplan.Predicate{{Filter: query.Filter{Field: "day", Operator: query.FilterLTE, Value: "2026-12-31"}, Dataset: "calendar", Field: field, Expression: dateExpression}},
	}

	conversionFieldRef := semanticplan.ConversionFieldRef{Dataset: "orders", Name: "customer_id"}
	physicalFieldRef := semanticplan.ConversionPhysicalFieldRef{Dataset: "orders", Name: "customer_id", Expression: "orders.customer_id"}
	conversionPlan := &semanticplan.ConversionPlan{
		BaseMetric: "visits", ConversionMetric: "purchases",
		BaseRoot: "visits", ConversionRoot: "orders",
		BaseEventKey:       []semanticplan.ConversionFieldRef{conversionFieldRef},
		ConversionEventKey: []semanticplan.ConversionFieldRef{conversionFieldRef},
		BaseTime:           conversionFieldRef, ConversionTime: conversionFieldRef,
		Entity:             semanticplan.ConversionPropertyPlan{Base: conversionFieldRef, Conversion: conversionFieldRef},
		ConstantProperties: []semanticplan.ConversionPropertyPlan{{Base: conversionFieldRef, Conversion: conversionFieldRef}},
		Calculation:        "conversion_rate",
		Window:             &ossie.ConversionWindow{Count: 7, Unit: "day"},
		Assignment:         "first",
		CandidateMatch: semanticplan.ConversionCandidateMatchPlan{
			PartitionBy: []semanticplan.ConversionFieldRef{conversionFieldRef},
			Equality:    []semanticplan.ConversionPropertyPlan{{Base: conversionFieldRef, Conversion: conversionFieldRef}},
			BaseTime:    conversionFieldRef, ConversionTime: conversionFieldRef,
			Window:                     &ossie.ConversionWindow{Count: 7, Unit: "day"},
			OrderBy:                    []semanticplan.ConversionCandidateOrder{{Field: conversionFieldRef, Direction: "asc"}},
			KeepRank:                   1,
			AggregateBaseIndependently: true,
		},
	}
	conversionPhysical := &semanticplan.ConversionPhysicalInputPlan{
		BaseDataset: dataset, ConversionDataset: dataset,
		BaseEventKey:       []semanticplan.ConversionPhysicalFieldRef{physicalFieldRef},
		ConversionEventKey: []semanticplan.ConversionPhysicalFieldRef{physicalFieldRef},
		BaseTime:           physicalFieldRef, ConversionTime: physicalFieldRef,
		Entity:             semanticplan.ConversionPhysicalPropertyPlan{Base: physicalFieldRef, Conversion: physicalFieldRef},
		ConstantProperties: []semanticplan.ConversionPhysicalPropertyPlan{{Base: physicalFieldRef, Conversion: physicalFieldRef}},
		BaseValue:          semanticplan.ConversionEventValuePlan{Metric: "visits", Dataset: "visits", Field: &physicalFieldRef},
		ConversionValue:    semanticplan.ConversionEventValuePlan{Metric: "purchases", Dataset: "orders", Field: &physicalFieldRef},
	}

	sourceGroup := semanticNodeFixture{
		ID: "stage_1", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Inputs:  []semanticNodeInputFixture{{NodeID: "stage_0", Grain: []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: field, Grain: &grain, Expression: dateExpression}}}},
		Metrics: []string{"revenue"}, Dimensions: []string{"order_date"},
		OutputGrain:      []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: field, Grain: &grain, Expression: dateExpression}},
		SourceRoots:      []string{"orders"},
		RequiredDatasets: []string{"orders"},
		Root:             dataset,
		Joins:            []semanticplan.Join{{Relationship: &ossie.Relationship{Name: "orders_customer", From: "orders", FromColumns: []string{"customer_id"}, To: "customer", ToColumns: []string{"customer_id"}}, Temporal: &ossie.TemporalRelationshipSpec{Kind: "as_of", FromTimeDimension: "order_date", ToValidFrom: "valid_from", ToValidTo: "valid_to", Cardinality: "one"}, FromDataset: "orders", FromSource: "analytics.orders", ToDataset: "customer", ToSource: "analytics.customer"}},
		Predicates: []semanticNodePredicateFixture{{
			Scope: "pre_aggregation", OwnerNodeID: "stage_1",
			Predicate: &semanticplan.Predicate{Filter: query.Filter{Field: "status", Operator: query.FilterEQ, Value: "paid"}, Dataset: "orders", Field: field, Expression: dateExpression},
			Post:      &semanticplan.PostEvaluationPredicate{Name: "revenue", Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 0}},
		}},
		ShareGroup:          "share_1",
		SharedGrainEvidence: &semanticplan.MetricSharedGrainEvidence{Metric: "revenue", RootDataset: "orders", RootDatasets: []string{"orders"}, GrainKey: "order_date:month", RelationshipPath: []string{"orders_customer"}, RelationshipPaths: [][]string{{"orders_customer"}}, FanoutSafe: true},
		ExtensionEvidence:   []extension.Evidence{extension.MetricScaleEvidence{Identity: extension.Identity{Namespace: "METIS", Kind: "metric_scale", Scope: "metric"}, Metric: "revenue", Version: "1", Factor: 2}},
		Node: semanticplan.SourceAggregateNode{
			Metric: metric, Expression: revenueExpression,
			Rollup: semanticplan.RollupContract{Function: "SUM", Algebra: "DISTRIBUTIVE", Merge: "SUM", Mergeable: true},
		},
	}
	cumulativeStage := semanticNodeFixture{
		ID: "stage_2", Kind: semanticplan.SemanticPlanNodeCumulativeWindow,
		Node: semanticplan.CumulativeWindowNode{
			Metric: metric, Expression: revenueExpression,
			Spec:                      ossie.CumulativeMetricSpec{Kind: "cumulative", BaseMetric: "revenue", TimeDimension: "order_date", Window: ossie.CumulativeWindow{Type: "bounded", Count: 3, Unit: "month"}},
			CustomCalendarRolling:     &cumulativePlan,
			CustomCalendarGrainToDate: &grainToDatePlan,
		},
	}
	timeOffsetStage := semanticNodeFixture{
		ID: "stage_3", Kind: semanticplan.SemanticPlanNodeTimeOffset,
		Node: semanticplan.TimeOffsetNode{
			Metric: metric, Expression: revenueExpression,
			Spec:           ossie.TimeOffsetMetricSpec{Kind: "time_offset", BaseMetric: "revenue", TimeDimension: "order_date", Offset: ossie.TimeOffset{Count: -1, Unit: "month"}},
			CustomCalendar: &offsetPlan,
		},
	}
	offsetToGrainStage := semanticNodeFixture{
		ID: "stage_4", Kind: semanticplan.SemanticPlanNodeOffsetToGrain,
		Node: semanticplan.OffsetToGrainNode{
			Metric: metric, Expression: revenueExpression,
			Spec: ossie.OffsetToGrainMetricSpec{Kind: "offset_to_grain", BaseMetric: "revenue", TimeDimension: "order_date", Grain: "year"},
			OffsetPlan: &semanticplan.OffsetToGrainPlan{
				TimeDimension: "order_date", QueryGrain: grain, BoundaryGrain: query.TimeGrainYear,
				CustomCalendar: true, Dataset: dataset,
				BoundaryBucket: field, BoundaryExpression: "calendar.year_start",
				QueryOrdinal: field, OrdinalExpression: "calendar.week_ordinal",
			},
		},
	}
	conversionStage := semanticNodeFixture{
		ID: "stage_5", Kind: semanticplan.SemanticPlanNodeConversion,
		Node: semanticplan.ConversionNode{
			Metric: metric, Expression: revenueExpression,
			Spec: ossie.ConversionMetricSpec{
				Kind: "conversion", BaseMetric: "visits", ConversionMetric: "purchases",
				Entity:      ossie.ConversionPropertyPair{BaseProperty: "customer_id", ConversionProperty: "customer_id"},
				Calculation: "conversion_rate", Window: &ossie.ConversionWindow{Count: 7, Unit: "day"},
				ConstantProperties: []ossie.ConversionPropertyPair{{BaseProperty: "campaign", ConversionProperty: "campaign"}},
			},
			Conversion: conversionPlan, PhysicalInputs: conversionPhysical,
		},
	}
	semiAdditiveStage := semanticNodeFixture{
		ID: "stage_6", Kind: semanticplan.SemanticPlanNodeSemiAdditiveLast,
		Node: semanticplan.SemiAdditiveNode{
			Metric: metric, Expression: revenueExpression,
			Spec: ossie.SemiAdditiveMetricSpec{Kind: "semi_additive", BaseMetric: "quantity", NonAdditiveDimension: "snapshot_date", Aggregation: "last", TieBreakDimension: "warehouse", NullPolicy: "skip", WindowGroupings: []string{"warehouse"}, RollupAggregation: "sum"},
		},
	}
	derivedStage := semanticNodeFixture{
		ID: "stage_7", Kind: semanticplan.SemanticPlanNodePostAggregate,
		Node: semanticplan.PostAggregateNode{Metric: metric, Expression: revenueExpression},
	}

	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Model: semanticplan.ModelRef{Project: "commerce", Name: "sales"},
		Root:  dataset,
		Joins: append([]semanticplan.Join(nil), sourceGroup.Joins...),
		Projections: []semanticplan.Projection{{
			Name: "revenue", Kind: semanticplan.ProjectionMetric, Metric: metric, Field: field,
			Dataset: "orders", Datasets: []string{"orders"}, Grain: &grain,
			Expression: revenueExpression, CustomCalendar: calendarGrouping,
		}},
		Predicates: []semanticplan.Predicate{{
			Filter:  query.Filter{Field: "status", Operator: query.FilterEQ, Value: "paid"},
			Dataset: "orders", Field: field, Expression: dateExpression,
		}},
		Groups: []semanticplan.GroupBy{{
			Name: "order_date", Dataset: "orders", Field: field, Grain: &grain,
			Expression: dateExpression, CustomCalendar: calendarGrouping,
		}},
		Sorts: []semanticplan.Sort{{
			Name: "revenue", Kind: semanticplan.SortMetric, Direction: query.SortDesc,
			Metric: metric, Field: field, Dataset: "orders", Expression: revenueExpression,
		}},
		Limit:     &limit,
		Requested: []string{"revenue"},
		Output: semanticplan.SemanticOutputContract{
			Grain:      []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: field, Grain: &grain, Expression: dateExpression}},
			Predicates: []semanticplan.PostEvaluationPredicate{{Name: "revenue", Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 0}}},
		},
		SharedGrain: &semanticplan.SharedGrainResolution{
			Grain:    []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: field, Grain: &grain, Expression: dateExpression}},
			GrainKey: "order_date:month",
			Metrics:  []semanticplan.MetricSharedGrainEvidence{{Metric: "revenue", RootDataset: "orders", RootDatasets: []string{"orders"}, GrainKey: "order_date:month", RelationshipPath: []string{"orders_customer"}, RelationshipPaths: [][]string{{"orders_customer"}}, FanoutSafe: true}},
		},
		DenseCalendar:       densePlan,
		CustomDenseCalendar: &semanticplan.CustomDenseCalendarPlan{Dataset: dataset, QueryTimeDimension: "order_date", Grain: grain, BucketField: field, BucketExpression: "calendar.week_start", OrdinalField: field, OrdinalExpression: "calendar.week_ordinal"},
		OptimizationTrace:   []semanticplan.OptimizationStep{{Rule: "baseline", Changed: false}},
	}, []semanticNodeFixture{sourceGroup, cumulativeStage, timeOffsetStage, offsetToGrainStage, conversionStage, semiAdditiveStage, derivedStage})
	// The plan owns its stages and its output contract outright, as a
	// planner-built plan does. There is no wrapper to copy them from.
	return plan
}

// The disposition test proves every field was decided about. It does not prove
// the projection actually emits the ones it calls projected, and those are
// different failures: a field declared projected but never written produces a
// fingerprint that reports less than it claims, silently.
//
// That gap is not hypothetical. It was found by deleting four projected fields
// from the conversion and offset-to-grain subtrees and watching the entire
// suite stay green, because nothing asserted the fingerprint depended on them.
//
// So: for every field declared projected, mutate it inside a fully populated
// plan and require the fingerprint to move. A field the fixture cannot reach is
// reported rather than skipped -- an unexercised field is exactly the one that
// would rot.
func TestEveryProjectedFieldMovesTheFingerprint(t *testing.T) {
	for _, disposition := range planFieldDispositions() {
		t.Run(disposition.typ.String(), func(t *testing.T) {
			for _, fieldName := range disposition.projected {
				t.Run(fieldName, func(t *testing.T) {
					base := populatedFixturePlan()
					before, err := semanticplan.Fingerprint(base)
					if err != nil {
						t.Fatal(err)
					}

					mutated := populatedFixturePlan()
					target := findAddressableValue(reflect.ValueOf(mutated), disposition.typ)
					if !target.IsValid() {
						t.Fatalf("the fixture plan contains no %s, so this field is never exercised", disposition.typ)
					}
					field := target.FieldByName(fieldName)
					if !field.IsValid() || !field.CanSet() {
						t.Fatalf("field %s.%s is not settable in the fixture", disposition.typ, fieldName)
					}
					if !mutateValue(field) {
						t.Fatalf("field %s.%s could not be given a distinguishable value; extend the fixture or the mutator",
							disposition.typ, fieldName)
					}

					after, err := semanticplan.Fingerprint(mutated)
					if err != nil {
						t.Fatal(err)
					}
					if before == after {
						t.Fatalf("%s.%s is declared projected but changing it did not move the fingerprint",
							disposition.typ, fieldName)
					}
				})
			}
		})
	}
}

// findAddressableValue returns the first settable value of the wanted type
// reachable from root, in a deterministic walk order.
func findAddressableValue(root reflect.Value, want reflect.Type) reflect.Value {
	var found reflect.Value
	var walk func(value reflect.Value)
	walk = func(value reflect.Value) {
		if found.IsValid() || !value.IsValid() {
			return
		}
		if value.Type() == want && value.CanSet() {
			found = value
			return
		}
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !value.IsNil() {
				walk(value.Elem())
			}
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				walk(value.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				walk(value.Index(i))
			}
		case reflect.Map:
			// Map values are not addressable; copy, walk, and store back is
			// not needed because no projected type is only reachable through
			// a map value in this fixture.
		}
	}
	walk(root)
	return found
}

// mutateValue gives a settable value a distinguishable one, reporting whether
// it could.
//
// Composite values change by presence or length rather than by mutating a leaf
// inside them. That matters: recursing into a pointer and changing its first
// settable leaf reaches a field that may not itself be projected -- the first
// draft did exactly that and reported ossie.Metric as unprojected because it had
// mutated CustomExtensions, which the projection deliberately ignores. Toggling
// presence is observable for any field the projection emits at all, because
// absent and present encode differently and sequences carry their length.
func mutateValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.String:
		value.SetString(value.String() + "__mutated")
		return true
	case reflect.Bool:
		value.SetBool(!value.Bool())
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(value.Int() + 7)
		return true
	case reflect.Float32, reflect.Float64:
		value.SetFloat(value.Float() + 7)
		return true
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			if value.Kind() == reflect.Interface {
				return false
			}
			value.Set(reflect.New(value.Type().Elem()))
			return true
		}
		value.Set(reflect.Zero(value.Type()))
		return true
	case reflect.Slice:
		if value.Len() == 0 {
			value.Set(reflect.Append(value, reflect.New(value.Type().Elem()).Elem()))
			return true
		}
		value.Set(value.Slice(0, value.Len()-1))
		return true
	case reflect.Map:
		if value.Len() == 0 {
			if value.IsNil() {
				value.Set(reflect.MakeMap(value.Type()))
			}
			key := reflect.New(value.Type().Key()).Elem()
			if key.Kind() != reflect.String {
				return false
			}
			key.SetString("__mutated")
			value.SetMapIndex(key, reflect.New(value.Type().Elem()).Elem())
			return true
		}
		value.SetMapIndex(value.MapKeys()[0], reflect.Value{})
		return true
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if value.Field(i).CanSet() && mutateValue(value.Field(i)) {
				return true
			}
		}
		return false
	}
	return false
}
