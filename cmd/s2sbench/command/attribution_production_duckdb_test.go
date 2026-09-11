//go:build duckdb

package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	attributionanalytics "github.com/meaningforge/metis/analytics/attribution"
	appmcp "github.com/meaningforge/metis/app/mcp"
	service "github.com/meaningforge/metis/app/service/semantic"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProductionAttributionScenarioSelection(t *testing.T) {
	selected, err := selectProductionAttributionScenarios(productionAttributionSmoke, "additive_population_union")
	if err != nil || len(selected) != 1 {
		t.Fatalf("smoke scenarios = %#v, %v", selected, err)
	}
	selected, err = selectProductionAttributionScenarios(productionAttributionDiag, "")
	if err != nil || len(selected) != 4 {
		t.Fatalf("diagnostic scenarios = %#v, %v", selected, err)
	}
	if _, err := selectProductionAttributionScenarios(productionAttributionDiag, "additive_population_union"); err == nil {
		t.Fatal("diagnostic accepted a scenario override")
	}
}

func TestProductionAttributionRuntimeProducesEveryOracle(t *testing.T) {
	for _, scenario := range productionAttributionScenarios() {
		t.Run(scenario.Name, func(t *testing.T) {
			runtime, err := newProductionAttributionRuntime(t.TempDir(), scenario)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { runtime.Close(context.Background()) })
			oracle, err := runtime.Oracle(context.Background(), scenario)
			if err != nil {
				t.Fatal(err)
			}
			if oracle.Metric != scenario.Metric || len(oracle.Dimensions) != len(scenario.Dimensions) {
				t.Fatalf("oracle = %#v", oracle)
			}
			if scenario.Metric == "metric:attribution.revenue" && oracle.Strategy != attributionanalytics.AttributionStrategyAdditive {
				t.Fatalf("strategy = %q", oracle.Strategy)
			}
			if scenario.Metric == "metric:attribution.conversion_rate" && oracle.Strategy != attributionanalytics.AttributionStrategyRatio {
				t.Fatalf("strategy = %q", oracle.Strategy)
			}
			encoded, _ := json.Marshal(oracle)
			if _, err := decodeProductionAttributionContract(string(encoded)); err != nil {
				t.Fatalf("production oracle violates answer contract: %v", err)
			}
			semanticallyChanged := strings.Replace(string(encoded), `"metric":"`+scenario.Metric+`"`, `"metric":"metric:changed"`, 1)
			if _, err := decodeProductionAttributionContract(semanticallyChanged); err != nil {
				t.Fatalf("contract grading depends on oracle values: %v", err)
			}
			missingMetric := strings.Replace(string(encoded), `"metric":"`+scenario.Metric+`",`, "", 1)
			if _, err := decodeProductionAttributionContract(missingMetric); err == nil {
				t.Fatal("contract accepted missing metric")
			}
			canonical, err := canonicalProductionAttribution("evidence:\n```json\n" + string(encoded) + "\n```")
			if err != nil || len(canonical) == 0 {
				t.Fatalf("canonical oracle = %s, %v", canonical, err)
			}
		})
	}
}

func TestProductionAttributionMCPArmsExposeAndExecuteProductionTools(t *testing.T) {
	scenario := productionAttributionScenarios()[0]
	runtime, err := newProductionAttributionRuntime(t.TempDir(), scenario)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close(context.Background()) })
	if _, err := runtime.Oracle(context.Background(), scenario); err != nil {
		t.Fatal(err)
	}
	for _, arm := range []productionAttributionArm{productionAttributionQuery, productionAttributionTool} {
		endpoint := runtime.endpoints[arm]
		client := mcp.NewClient(&mcp.Implementation{Name: "attribution-production-test", Version: "1"}, nil)
		transport := &mcp.StreamableClientTransport{
			Endpoint: endpoint.mcp.URL,
			HTTPClient: &http.Client{Transport: productionAttributionRoundTripper{
				base: http.DefaultTransport, token: endpoint.environment[s2sbenchMCPTokenEnv], project: productionAttributionProject,
			}},
			DisableStandaloneSSE: true,
		}
		session, err := client.Connect(context.Background(), transport, nil)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := session.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		hasAttribute := false
		for _, tool := range tools.Tools {
			hasAttribute = hasAttribute || tool.Name == appmcp.ToolAttributeMetric
		}
		if hasAttribute != (arm == productionAttributionTool) {
			t.Fatalf("arm=%s attribute_metric visible=%t", arm, hasAttribute)
		}
		if arm == productionAttributionTool {
			argumentsJSON, _ := json.Marshal(service.MetricAttributionQuery{
				Metric: scenario.Metric, TimeDimension: "dimension:attribution.attribution_events.event_time",
				Baseline:   attributionanalytics.AttributionPeriod{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
				Current:    attributionanalytics.AttributionPeriod{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
				Dimensions: scenario.Dimensions,
			})
			var arguments map[string]any
			_ = json.Unmarshal(argumentsJSON, &arguments)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: appmcp.ToolAttributeMetric, Arguments: arguments})
			if err != nil {
				t.Fatalf("attribute_metric MCP call: %v", err)
			}
			if result.IsError {
				encoded, _ := json.Marshal(result)
				t.Fatalf("attribute_metric MCP result: %s", encoded)
			}
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProductionAttributionTraceRequiresOnlyProductionWorkflow(t *testing.T) {
	scenario := productionAttributionScenarios()[0]
	success := func(name string) s2sbench.ToolCallEvidence {
		return s2sbench.ToolCallEvidence{Name: name, Status: "success"}
	}
	queryTrace := []s2sbench.ToolCallEvidence{success(appmcp.ToolQueryMetrics), success(appmcp.ToolQueryMetrics), success(appmcp.ToolQueryMetrics), success(appmcp.ToolQueryMetrics)}
	if err := validateProductionAttributionTrace(productionAttributionQuery, scenario, queryTrace); err != nil {
		t.Fatal(err)
	}
	if err := validateProductionAttributionTrace(productionAttributionTool, scenario, []s2sbench.ToolCallEvidence{success(appmcp.ToolAttributeMetric)}); err != nil {
		t.Fatal(err)
	}
	failed := s2sbench.ToolCallEvidence{Name: appmcp.ToolQueryMetrics, Status: "error"}
	if err := validateProductionAttributionTrace(productionAttributionQuery, scenario, append([]s2sbench.ToolCallEvidence{failed}, queryTrace...)); err != nil {
		t.Fatalf("query retry changed workflow verdict: %v", err)
	}
	if err := validateProductionAttributionTrace(productionAttributionTool, scenario, append(queryTrace, success(appmcp.ToolAttributeMetric))); err == nil {
		t.Fatal("attribute arm accepted query_metrics reconstruction")
	}
}

func TestProductionAttributionSummaryUsesSemanticVerdict(t *testing.T) {
	attempts := []productionAttributionAttempt{
		{Arm: productionAttributionQuery, SemanticVerdict: s2sbench.VerdictCorrect, ContractVerdict: s2sbench.VerdictWrong, WorkflowVerdict: s2sbench.VerdictCorrect},
		{Arm: productionAttributionQuery, SemanticVerdict: s2sbench.VerdictWrong, ContractVerdict: s2sbench.VerdictCorrect, WorkflowVerdict: s2sbench.VerdictWrong},
	}
	summary := summarizeProductionAttribution(productionAttributionQuery, attempts)
	if !reflect.DeepEqual([]any{summary.Total, summary.SemanticCorrect, summary.SemanticWrong, summary.ContractCorrect, summary.WorkflowCorrect, summary.SemanticAccuracyPercent}, []any{2, 1, 1, 1, 1, float64(50)}) {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestProductionAttributionCanonicalizationIgnoresPresentationAndInputEchoes(t *testing.T) {
	base := `{"analysis_id":"ignored","metric":"metric:m","time_dimension":"dimension:t","baseline":{"start":"2026-07-01T00:00:00Z","end":"2026-08-01T00:00:00Z"},"current":{"start":"2026-08-01T00:00:00Z","end":"2026-09-01T00:00:00Z"},"strategy":"additive_contribution","dimensions":[{"dimension":"dimension:b","member_type":"string","additive":{"summary":{"baseline_total":"1.0"},"segments":[]}},{"dimension":"dimension:a","member_type":"string","additive":{"summary":{"baseline_total":"2"},"segments":[]}}]}`
	presentationEquivalent := `{"metric":"metric:m","time_dimension":"dimension:t","baseline":{"start":"2026-07-01T00:00:00Z","end":"2026-08-01T00:00:00Z"},"current":{"start":"2026-08-01T00:00:00Z","end":"2026-09-01T00:00:00Z"},"strategy":"additive_contribution","dimensions":[{"dimension":"dimension:a","member_type":"string","additive":{"segments":[],"summary":{"baseline_total":2.0}}},{"dimension":"dimension:b","member_type":"string","additive":{"segments":[],"summary":{"baseline_total":"1"}}}]}`
	want, err := canonicalProductionAttribution("population union = {south, north, east}\n" + base)
	if err != nil {
		t.Fatal(err)
	}
	got, err := canonicalProductionAttribution(presentationEquivalent)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("presentation-only differences changed semantic evidence\nleft=%s\nright=%s", want, got)
	}
	dimensionsDetail := strings.Replace(base, `"dimensions":[`, `"dimensions":["dimension:a","dimension:b"],"dimensions_detail":[`, 1)
	detailCanonical, err := canonicalProductionAttribution(dimensionsDetail)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(detailCanonical, want) {
		t.Fatalf("dimension inventory presentation changed semantic evidence\nwant=%s\ngot=%s", want, detailCanonical)
	}
	missingClosingBrace := strings.TrimSuffix(base, "}")
	repaired, err := canonicalProductionAttribution(missingClosingBrace)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repaired, want) {
		t.Fatalf("unambiguous delimiter repair changed semantic evidence\nwant=%s\ngot=%s", want, repaired)
	}
	rounded, err := canonicalProductionAttribution(strings.Replace(base, `"baseline_total":"1.0"`, `"baseline_total":"1.00000000000000003"`, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !productionAttributionSemanticallyEqual(rounded, want) {
		t.Fatal("semantically negligible decimal rounding was rejected")
	}
	materiallyDifferent, err := canonicalProductionAttribution(strings.Replace(base, `"baseline_total":"1.0"`, `"baseline_total":"1.01"`, 1))
	if err != nil {
		t.Fatal(err)
	}
	if productionAttributionSemanticallyEqual(materiallyDifferent, want) {
		t.Fatal("material decimal difference was ignored")
	}
	for name, changed := range map[string]string{
		"time dimension": strings.Replace(base, `"time_dimension":"dimension:t"`, `"time_dimension":"dimension:other"`, 1),
		"period":         strings.Replace(base, "2026-07-01T00:00:00Z", "2026-06-01T00:00:00Z", 1),
		"member type":    strings.Replace(base, `"member_type":"string"`, `"member_type":"integer"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := canonicalProductionAttribution(changed)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(canonical, want) {
				t.Fatalf("%s input echo/presentation changed semantic evidence: %s", name, canonical)
			}
		})
	}
	if _, err := canonicalProductionAttribution(`{"dimensions":[{"additive":{"segments":[1]}}]}`); err == nil {
		t.Fatal("malformed segment did not fail canonicalization")
	}
}

type productionAttributionRoundTripper struct {
	base    http.RoundTripper
	token   string
	project string
}

func (transport productionAttributionRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.base == nil {
		return nil, fmt.Errorf("production attribution HTTP transport is unavailable")
	}
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+transport.token)
	request.Header.Set(appmcp.ProjectHeaderKey, transport.project)
	return transport.base.RoundTrip(request)
}
