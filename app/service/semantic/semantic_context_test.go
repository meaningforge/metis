package semantic

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

func newSemanticContextService(t *testing.T) *SemanticContextService {
	t.Helper()
	snapshot, err := manifest.BuildProjectManifest("finance", loadDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	return NewSemanticContextService(manifest.NewStore(snapshot)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
}

func TestSemanticContextReturnsFocusedMetricAndDimensionEvidence(t *testing.T) {
	result, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{
		Project:    "finance",
		Model:      "sales",
		Metrics:    []string{"gross_margin"},
		Dimensions: []string{"region", "category"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metrics) != 1 || result.Metrics[0].Name != "gross_margin" {
		t.Fatalf("metrics = %#v", result.Metrics)
	}
	metric := result.Metrics[0]
	if got, want := metric.DirectDependencies, []string{"total_cost", "total_revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("direct dependencies = %v, want %v", got, want)
	}
	if got, want := metric.Dependencies, []string{"total_cost", "total_revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %v, want %v", got, want)
	}
	if got, want := metric.SourceDatasets, []string{"costs", "orders"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source datasets = %v, want %v", got, want)
	}
	if len(result.Dimensions) != 2 {
		t.Fatalf("dimensions = %#v", result.Dimensions)
	}

	region := result.Dimensions[0]
	if region.QualifiedName != "customer.region" || len(region.Compatibility) != 1 {
		t.Fatalf("region = %#v", region)
	}
	if region.Compatibility[0].Status != CompatibilityCompatible || len(region.Compatibility[0].Paths) != 2 {
		t.Fatalf("region compatibility = %#v", region.Compatibility[0])
	}
	category := result.Dimensions[1]
	if category.QualifiedName != "inventory.category" || len(category.Compatibility) != 1 {
		t.Fatalf("category = %#v", category)
	}
	if category.Compatibility[0].Status != CompatibilityUnreachable || len(category.Compatibility[0].Issues) != 2 {
		t.Fatalf("category compatibility = %#v", category.Compatibility[0])
	}
	if got, want := relationshipNames(result.Relationships), []string{"costs_to_customer", "orders_to_customer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("relationships = %v, want %v", got, want)
	}
}

func TestSemanticContextCompatibleDimensionsPaginationIsDeterministic(t *testing.T) {
	doc := loadDoc(t)
	model := &doc.SemanticModel[0]
	for i := range model.Datasets {
		if model.Datasets[i].Name != "customer" {
			continue
		}
		model.Datasets[i].Fields = append(model.Datasets[i].Fields, ossie.Field{
			Name:      "segment",
			Datatype:  ossie.DataTypeString,
			Dimension: &ossie.Dimension{},
			Expression: ossie.Expression{Dialects: []ossie.DialectExpression{{
				Dialect: ossie.DialectANSISQL, Expression: "customer.segment",
			}}},
		})
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewSemanticContextService(manifest.NewStore(snapshot)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})

	first, err := svc.Get(context.Background(), SemanticContextRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Limit: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.CompatibleDimensionsPage == nil || len(first.CompatibleDimensionsPage.Items) != 1 {
		t.Fatalf("first page = %#v", first.CompatibleDimensionsPage)
	}
	if got := first.CompatibleDimensionsPage.Items[0].QualifiedName; got != "customer.region" {
		t.Fatalf("first dimension = %q", got)
	}
	if first.CompatibleDimensionsPage.NextCursor == "" {
		t.Fatal("expected next cursor")
	}

	second, err := svc.Get(context.Background(), SemanticContextRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Limit:  1,
			Cursor: first.CompatibleDimensionsPage.NextCursor,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.CompatibleDimensionsPage == nil || len(second.CompatibleDimensionsPage.Items) != 1 {
		t.Fatalf("second page = %#v", second.CompatibleDimensionsPage)
	}
	if got := second.CompatibleDimensionsPage.Items[0].QualifiedName; got != "customer.segment" {
		t.Fatalf("second dimension = %q", got)
	}
	if second.CompatibleDimensionsPage.NextCursor != "" {
		t.Fatalf("unexpected trailing cursor %q", second.CompatibleDimensionsPage.NextCursor)
	}
}

func TestSemanticContextRequiresExplicitPageMetricForMultipleFocusedMetrics(t *testing.T) {
	_, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue", "total_cost"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Limit: 10,
		},
	})
	assertSemanticContextErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestSemanticContextRejectsInvalidCursor(t *testing.T) {
	_, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue"},
		CompatibleDimensionsPage: &CompatibleDimensionsPageRequest{
			Cursor: "not-a-context-cursor",
		},
	})
	assertSemanticContextErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestSemanticContextRequiresFocusedAssets(t *testing.T) {
	_, err := newSemanticContextService(t).Get(context.Background(), SemanticContextRequest{Project: "finance", Model: "sales"})
	assertSemanticContextErrorCode(t, err, serrors.ErrInvalidQuery)
}

func TestMetricSemanticConstraintsExposeTypedCurrentSemantics(t *testing.T) {
	metric := &ossie.Metric{
		Name:     "snapshot_revenue",
		Datatype: ossie.DataTypeDecimal,
		CustomExtensions: []ossie.CustomExtension{
			{VendorName: ossie.MetisExtensionVendor, Data: "{\"kind\":\"fill\",\"policy\":\"zero\"}"},
			{VendorName: ossie.MetisExtensionVendor, Data: "{\"kind\":\"time_offset\",\"base_metric\":\"revenue\",\"time_dimension\":\"snapshot_date\",\"offset\":{\"count\":-1,\"unit\":\"month\"}}"},
			{VendorName: ossie.MetisExtensionVendor, Data: "{\"kind\":\"semi_additive\",\"base_metric\":\"revenue\",\"non_additive_dimension\":\"snapshot_date\",\"aggregation\":\"last\",\"tie_break_dimension\":\"sequence\",\"null_policy\":\"skip\",\"window_groupings\":[\"account_id\"],\"rollup_aggregation\":\"max\"}"},
		},
	}
	constraints, err := metricSemanticConstraints(metric)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := constraintKinds(constraints), []MetricSemanticConstraintKind{MetricConstraintFill, MetricConstraintSemiAdditive, MetricConstraintTimeOffset}; !reflect.DeepEqual(got, want) {
		t.Fatalf("constraint kinds = %v, want %v", got, want)
	}
	if constraints[0].Fill == nil || constraints[0].Fill.Policy != ossie.MetricFillZero {
		t.Fatalf("fill constraint = %#v", constraints[0])
	}
	if constraints[1].SemiAdditive == nil || constraints[1].SemiAdditive.Selector != "last" || constraints[1].SemiAdditive.RollupAggregation != "max" {
		t.Fatalf("semi-additive constraint = %#v", constraints[1])
	}
	if constraints[2].TimeOffset == nil || constraints[2].TimeOffset.Count != -1 || constraints[2].TimeOffset.Unit != "month" {
		t.Fatalf("time-offset constraint = %#v", constraints[2])
	}
}

func relationshipNames(relationships []RelationshipContext) []string {
	out := make([]string, 0, len(relationships))
	for _, relationship := range relationships {
		out = append(out, relationship.Name)
	}
	return out
}

func constraintKinds(constraints []MetricSemanticConstraint) []MetricSemanticConstraintKind {
	out := make([]MetricSemanticConstraintKind, 0, len(constraints))
	for _, constraint := range constraints {
		out = append(out, constraint.Kind)
	}
	return out
}

func assertSemanticContextErrorCode(t *testing.T, err error, want serrors.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", want)
	}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != want {
		t.Fatalf("error = %#v, want %s", err, want)
	}
}
