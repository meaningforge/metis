package semantic

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

type countingGovernanceProjects struct{ dataSourceCalls int }

type versionedVisibilityPolicy struct{ scope string }

func (*versionedVisibilityPolicy) EvaluateAssetVisibility(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
	return AssetVisibilityDecision{Effect: AssetVisibilityVisible, Reason: AssetVisibilityReasonPolicyVisible}
}

func (p *versionedVisibilityPolicy) AssetVisibilityScope(context.Context, AssetVisibilityScopeRequest) string {
	return p.scope
}

func (*countingGovernanceProjects) ResolveProject(string) (string, error) { return "finance", nil }
func (p *countingGovernanceProjects) DataSourceForProject(string) (string, bool) {
	p.dataSourceCalls++
	return "warehouse", true
}

const governedServiceModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: region}]
            dimension: {}
    metrics:
      - name: public_revenue
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
      - name: deprecated_revenue
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"asset_governance","lifecycle":"deprecated","replacement":"metric:sales.public_revenue"}'
      - name: secret_revenue
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"asset_governance","policy_tags":["internal"]}'
`

func governedDiscovery(t *testing.T, requests *[]AssetVisibilityRequest) *DiscoveryService {
	t.Helper()
	document, err := ossie.NewLoader().Load([]byte(governedServiceModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", document)
	if err != nil {
		t.Fatal(err)
	}
	policy := AssetVisibilityPolicyFunc(func(_ context.Context, req AssetVisibilityRequest) AssetVisibilityDecision {
		if requests != nil {
			*requests = append(*requests, req)
		}
		for _, tag := range req.PolicyTags {
			if tag == "internal" {
				return AssetVisibilityDecision{Effect: AssetVisibilityHidden, Reason: AssetVisibilityReasonPolicyHidden}
			}
		}
		return AssetVisibilityDecision{Effect: AssetVisibilityVisible, Reason: AssetVisibilityReasonPolicyVisible}
	})
	return NewDiscoveryService(manifest.NewStore(snapshot)).WithProjectAuthorizer(AllAccessProjectAuthorizer{}).WithAssetVisibilityPolicy(policy)
}

func TestAssetVisibilityFiltersBeforeSearchAndPagination(t *testing.T) {
	var requests []AssetVisibilityRequest
	discovery := governedDiscovery(t, &requests)
	search, err := discovery.SearchSemantics(context.Background(), SearchSemanticsRequest{Project: "finance", Query: "revenue", Kinds: []AssetKind{AssetMetric}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Matches) != 2 {
		t.Fatalf("visible search matches = %#v", search.Matches)
	}
	for _, match := range search.Matches {
		if match.Name == "secret_revenue" {
			t.Fatalf("hidden metric leaked: %#v", match)
		}
	}
	model, err := discovery.GetModel(context.Background(), GetModelRequest{Project: "finance", Model: "sales"})
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range model.Metrics {
		if metric.Name == "secret_revenue" {
			t.Fatalf("hidden metric leaked through model detail: %#v", model.Metrics)
		}
	}
	_, err = discovery.GetMetric(context.Background(), GetMetricRequest{Project: "finance", Model: "sales", Metric: "secret_revenue"})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrMetricNotFound {
		t.Fatalf("hidden direct metric error = %v", err)
	}
	enumerated, err := NewSemanticSearchService(discovery).Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(enumerated.Items) != 2 || enumerated.NextCursor != "" {
		t.Fatalf("filtered pagination = %#v", enumerated)
	}
	if len(requests) == 0 || requests[0].ProjectID != "finance" || requests[0].Action != ProjectActionDiscover {
		t.Fatalf("policy requests = %#v", requests)
	}
}

func TestVisibilityScopeChangeRequiresPaginationRestart(t *testing.T) {
	policy := &versionedVisibilityPolicy{scope: "policy-v1"}
	discovery := governedDiscovery(t, nil).WithAssetVisibilityPolicy(policy).WithGenerationIdentity("finance", 1)
	service := NewSemanticSearchService(discovery)
	first, err := service.Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	policy.scope = "policy-v2"
	_, err = service.Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 1, Cursor: first.NextCursor})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrSemanticPaginationRestart {
		t.Fatalf("err=%v, want %s", err, serrors.ErrSemanticPaginationRestart)
	}
}

func TestHiddenAssetsDoNotConsumePaginationPositions(t *testing.T) {
	discovery := governedDiscovery(t, nil).WithGenerationIdentity("finance", 1)
	service := NewSemanticSearchService(discovery)
	first, err := service.Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" || first.Items[0].Name == "secret_revenue" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := service.Search(context.Background(), SemanticSearchRequest{Project: "finance", Kinds: []AssetKind{AssetMetric}, Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Name == "secret_revenue" || second.Items[0].Ref == first.Items[0].Ref {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
}

func TestAssetVisibilityBlocksDirectExecutionReferenceAndDeprecatedWarns(t *testing.T) {
	discovery := governedDiscovery(t, nil)
	secret := query.SemanticQuery{Project: "finance", Model: "sales", Metrics: []query.MetricRef{{Name: "secret_revenue"}}}
	err := discovery.authorizeSemanticQueryAssets(context.Background(), secret, ProjectActionExecute)
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrMetricNotFound {
		t.Fatalf("hidden execution error = %v", err)
	}
	warnings, err := discovery.semanticQueryGovernanceWarnings(query.SemanticQuery{Project: "finance", Model: "sales", Metrics: []query.MetricRef{{Name: "deprecated_revenue"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].Code != AssetGovernanceWarningDeprecated || warnings[0].AssetRef != "metric:sales.deprecated_revenue" || warnings[0].Replacement != "metric:sales.public_revenue" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestHiddenExecutionAssetIsRejectedBeforeDataSourceResolution(t *testing.T) {
	discovery := governedDiscovery(t, nil)
	compile := &CompileService{discovery: discovery}
	projects := &countingGovernanceProjects{}
	service := NewQueryMetricsService(compile, projects, runner.New(nil, nil, nil, nil)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	_, err := service.QueryMetrics(context.Background(), QueryMetricsRequest{Query: query.SemanticQuery{
		Model: "sales", Metrics: []query.MetricRef{{Name: "secret_revenue"}},
	}})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrMetricNotFound {
		t.Fatalf("hidden execution error = %v", err)
	}
	if projects.dataSourceCalls != 0 {
		t.Fatalf("DataSource resolution calls = %d, want 0", projects.dataSourceCalls)
	}
}

func TestAssetVisibilityInvalidDecisionFailsClosed(t *testing.T) {
	discovery := governedDiscovery(t, nil).WithAssetVisibilityPolicy(AssetVisibilityPolicyFunc(func(context.Context, AssetVisibilityRequest) AssetVisibilityDecision {
		return AssetVisibilityDecision{Effect: AssetVisibilityVisible, Reason: AssetVisibilityReasonPolicyHidden}
	}))
	_, err := discovery.GetModel(context.Background(), GetModelRequest{Project: "finance", Model: "sales"})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrModelNotFound {
		t.Fatalf("invalid decision error = %v", err)
	}
}
