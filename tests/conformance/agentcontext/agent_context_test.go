package agentcontext_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

const conformanceProject = "agent-context"

func TestCanonicalSemanticContextEvidence(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		model      string
		metric     string
		dimension  string
		constraint service.MetricSemanticConstraintKind
		status     service.CompatibilityStatus
		issueCode  string
		relation   string
	}{
		{
			name:       "time offset relationship context",
			fixture:    "commerce.ossie.yaml",
			model:      "commerce",
			metric:     "previous_month_revenue",
			dimension:  "region",
			constraint: service.MetricConstraintTimeOffset,
			status:     service.CompatibilityCompatible,
			relation:   "orders_to_customer",
		},
		{
			name:       "semi additive context",
			fixture:    "commerce.ossie.yaml",
			model:      "commerce",
			metric:     "inventory_balance",
			dimension:  "warehouse",
			constraint: service.MetricConstraintSemiAdditive,
			status:     service.CompatibilityCompatible,
		},
		{
			name:       "conversion context",
			fixture:    "conversion.ossie.yaml",
			model:      "conversion",
			metric:     "signup_to_purchase_rate",
			constraint: service.MetricConstraintConversion,
		},
		{
			name:       "definition filter context",
			fixture:    "definition_filters.ossie.yaml",
			model:      "definition_filters",
			metric:     "gold_revenue",
			constraint: service.MetricConstraintDefinitionFilter,
		},
		{
			name:      "ambiguous relationship evidence",
			fixture:   "ambiguous_paths.ossie.yaml",
			model:     "ambiguous_paths",
			metric:    "revenue",
			dimension: "country",
			status:    service.CompatibilityAmbiguous,
			issueCode: string(serrors.ErrAmbiguousRelationshipPath),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := runtimeForFixture(t, tt.fixture)
			contextService := service.NewSemanticContextService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
			req := service.SemanticContextRequest{
				Project: conformanceProject,
				Model:   tt.model,
				Metrics: []string{tt.metric},
			}
			if tt.dimension != "" {
				req.Dimensions = []string{tt.dimension}
			}

			got := getContext(t, contextService, req)
			gotAgain := getContext(t, contextService, req)
			assertStableJSON(t, got, gotAgain)

			if len(got.Metrics) != 1 || got.Metrics[0].Name != tt.metric {
				t.Fatalf("metric context = %#v", got.Metrics)
			}
			if tt.constraint != "" && !hasConstraint(got.Metrics[0].SemanticConstraints, tt.constraint) {
				t.Fatalf("metric %q constraints = %#v, want %q", tt.metric, got.Metrics[0].SemanticConstraints, tt.constraint)
			}
			if tt.dimension == "" {
				return
			}
			if len(got.Dimensions) != 1 || len(got.Dimensions[0].Compatibility) != 1 {
				t.Fatalf("dimension context = %#v", got.Dimensions)
			}
			compatibility := got.Dimensions[0].Compatibility[0]
			if compatibility.Status != tt.status {
				t.Fatalf("compatibility status = %q, want %q", compatibility.Status, tt.status)
			}
			if tt.issueCode != "" && !hasIssueCode(compatibility.Issues, tt.issueCode) {
				t.Fatalf("compatibility issues = %#v, want code %q", compatibility.Issues, tt.issueCode)
			}
			if tt.relation != "" && !hasRelationship(got.Relationships, tt.relation) {
				t.Fatalf("relationships = %#v, want %q", got.Relationships, tt.relation)
			}
		})
	}
}

func TestCanonicalExplainEvidence(t *testing.T) {
	month := query.TimeGrainMonth
	limit := 10
	tests := []struct {
		name      string
		fixture   string
		model     string
		query     query.SemanticQuery
		wantKinds []service.ExplanationStepKind
	}{
		{
			name:    "time offset and composition",
			fixture: "commerce.ossie.yaml",
			model:   "commerce",
			query: query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "revenue_growth_rate"}},
				Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &month}},
			},
			wantKinds: []service.ExplanationStepKind{
				service.ExplanationTimeAlignment,
				service.ExplanationTimeOffset,
				service.ExplanationComposition,
				service.ExplanationGrouping,
			},
		},
		{
			name:    "semi additive selection",
			fixture: "commerce.ossie.yaml",
			model:   "commerce",
			query: query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "inventory_balance"}},
				Dimensions: []query.DimensionRef{{Name: "warehouse"}},
			},
			wantKinds: []service.ExplanationStepKind{service.ExplanationSemiAdditiveSelection},
		},
		{
			name:    "conversion composition",
			fixture: "conversion.ossie.yaml",
			model:   "conversion",
			query: query.SemanticQuery{
				Metrics: []query.MetricRef{{Name: "signup_to_purchase_rate"}},
			},
			wantKinds: []service.ExplanationStepKind{service.ExplanationComposition},
		},
		{
			name:    "metric definition filter",
			fixture: "definition_filters.ossie.yaml",
			model:   "definition_filters",
			query: query.SemanticQuery{
				Metrics: []query.MetricRef{{Name: "gold_revenue"}},
			},
			wantKinds: []service.ExplanationStepKind{service.ExplanationDefinitionFilter},
		},
		{
			name:    "query filter grouping ordering and limit",
			fixture: "commerce.ossie.yaml",
			model:   "commerce",
			query: query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "revenue"}},
				Dimensions: []query.DimensionRef{{Name: "region"}},
				Filters:    []query.Filter{{Field: "status", Operator: query.FilterEQ, Value: "paid"}},
				OrderBy:    []query.OrderBy{{Field: "revenue", Direction: query.SortDesc}},
				Limit:      &limit,
			},
			wantKinds: []service.ExplanationStepKind{
				service.ExplanationRelationship,
				service.ExplanationQueryFilter,
				service.ExplanationGrouping,
				service.ExplanationOrdering,
				service.ExplanationLimit,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := runtimeForFixture(t, tt.fixture)
			tt.query.Project = conformanceProject
			tt.query.Model = tt.model
			req := service.CompileRequest{Query: tt.query, Dialect: "DORIS"}

			got, err := runtime.Compile.Explain(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			gotAgain, err := runtime.Compile.Explain(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			assertStableJSON(t, got, gotAgain)
			for _, want := range tt.wantKinds {
				if !hasStepKind(got.Steps, want) {
					t.Fatalf("explanation steps = %v, want kind %q", stepKinds(got.Steps), want)
				}
			}
		})
	}
}

func runtimeForFixture(t *testing.T, fixture string) *bootstrap.ProjectRuntime {
	t.Helper()
	fixturePath, err := filepath.Abs(filepath.Join("..", "fixtures", fixture))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	projectManifest := fmt.Sprintf("semantic_sources:\n  fixture:\n    path: %q\n", fixturePath)
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(projectManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("version: 1\nprojects:\n  %s:\n    path: ./project.yaml\n", conformanceProject)
	manifestPath := filepath.Join(dir, "metis.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := bootstrap.LoadRuntime(manifestPath, bootstrap.WithLocalAllAccessProjectAuthorization())
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func getContext(t *testing.T, contextService *service.SemanticContextService, req service.SemanticContextRequest) *service.SemanticContext {
	t.Helper()
	got, err := contextService.Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertStableJSON(t *testing.T, first, second any) {
	t.Helper()
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstJSON, secondJSON) {
		t.Fatalf("Agent-facing response is not deterministic:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func hasConstraint(constraints []service.MetricSemanticConstraint, want service.MetricSemanticConstraintKind) bool {
	for _, constraint := range constraints {
		if constraint.Kind == want {
			return true
		}
	}
	return false
}

func hasIssueCode(issues []service.DimensionPathIssue, want string) bool {
	for _, issue := range issues {
		if issue.Code == want {
			return true
		}
	}
	return false
}

func hasRelationship(relationships []service.RelationshipContext, want string) bool {
	for _, relationship := range relationships {
		if relationship.Name == want {
			return true
		}
	}
	return false
}

func hasStepKind(steps []service.ExplanationStep, want service.ExplanationStepKind) bool {
	for _, step := range steps {
		if step.Kind == want {
			return true
		}
	}
	return false
}

func stepKinds(steps []service.ExplanationStep) []service.ExplanationStepKind {
	out := make([]service.ExplanationStepKind, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.Kind)
	}
	return out
}
