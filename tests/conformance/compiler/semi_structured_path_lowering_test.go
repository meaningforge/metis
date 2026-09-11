package compiler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

const semiStructuredPathModelYAML = `
version: "0.2.0.dev0"
semantic_model:
  - name: semi_structured
    datasets:
      - name: events
        source: analytics.events
        fields:
          - name: payload
            datatype: Opaque
            expression: {dialects: [{dialect: ANSI_SQL, expression: events.payload}]}
    metrics:
      - name: customer_region
        datatype: String
        expression:
          dialects:
            - dialect: SNOWFLAKE
              expression: "MAX(events.payload:customer.region::STRING)"
`

func TestSemiStructuredPathCastSurvivesSemanticPlanningBeforePhysicalCapabilityCheck(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(semiStructuredPathModelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}

	model := snapshot.Projects[projectName].Models["semi_structured"]
	analysis := model.MetricAnalyses["customer_region"]
	if len(analysis.Expressions) != 1 {
		t.Fatalf("analyzed expressions = %d, want 1", len(analysis.Expressions))
	}
	typed := analysis.Expressions[0].Typed
	if len(typed.PathResolutions) != 1 || len(typed.CastResolutions) != 1 {
		t.Fatalf("typed evidence path=%d cast=%d, want 1/1", len(typed.PathResolutions), len(typed.CastResolutions))
	}

	renderer := mustRenderer(t, "SNOWFLAKE")
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query.SemanticQuery{
		Project: projectName,
		Model:   "semi_structured",
		Metrics: []query.MetricRef{{Name: "customer_region"}},
	}, renderer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Projections) != 1 || plan.Projections[0].Expression.Analysis == nil {
		t.Fatalf("projection analysis = %#v, want retained typed analysis", plan.Projections)
	}
	plannedTyped := plan.Projections[0].Expression.Analysis.Typed
	if len(plannedTyped.PathResolutions) != 1 || len(plannedTyped.CastResolutions) != 1 {
		t.Fatalf("planned evidence path=%d cast=%d, want 1/1", len(plannedTyped.PathResolutions), len(plannedTyped.CastResolutions))
	}

	_, err = compilePlan(context.Background(), plan, renderer)
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedDialect {
		t.Fatalf("compile error = %#v, want %s", err, serrors.ErrUnsupportedDialect)
	}
}

func TestSemiStructuredForeignOnlyPathExpressionFailsBeforeRegisteredTargetLowering(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(semiStructuredPathModelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query.SemanticQuery{
		Project: projectName,
		Model:   "semi_structured",
		Metrics: []query.MetricRef{{Name: "customer_region"}},
	}, mustRenderer(t, "CLICKHOUSE"))
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedExpression {
		t.Fatalf("resolve error = %#v, want %s", err, serrors.ErrUnsupportedExpression)
	}
}
