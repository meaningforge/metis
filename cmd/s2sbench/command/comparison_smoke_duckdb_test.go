//go:build duckdb

package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/auth"
	appmcp "github.com/meaningforge/metis/app/mcp"
	service "github.com/meaningforge/metis/app/service/semantic"
	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestComparisonLiveRequiresExplicitPaidInvocationAcknowledgement(t *testing.T) {
	err := runComparisonLive([]string{
		"--agent", "generic", "--model", "test", "--provider", "test",
		"--model-version", "test", "--output", t.TempDir() + "/result",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--execute-live") {
		t.Fatalf("error = %v", err)
	}
}

func TestComparisonRunModeSelection(t *testing.T) {
	selected, err := selectComparisonScenarios(comparisonModeSmoke, "")
	if err != nil || len(selected) != 1 || selected[0].Name != "population_union_multi_metric" {
		t.Fatalf("default smoke scenarios = %#v, error = %v", selected, err)
	}
	selected, err = selectComparisonScenarios(comparisonModeDiag, "")
	if err != nil || len(selected) != 4 {
		t.Fatalf("diagnostic scenarios = %#v, error = %v", selected, err)
	}
	if _, err := selectComparisonScenarios(comparisonModeDiag, "zero_baseline_region"); err == nil {
		t.Fatal("diagnostic accepted a scenario override")
	}
	if _, err := selectComparisonScenarios("benchmark", ""); err == nil {
		t.Fatal("comparison experiment accepted unsupported formal mode")
	}
}

func TestComparisonSummaryUsesSemanticVerdictAsHeadline(t *testing.T) {
	attempts := []comparisonSmokeAttempt{
		{Arm: comparisonArmQuery, SemanticVerdict: s2sbench.VerdictCorrect, ContractVerdict: s2sbench.VerdictWrong, WorkflowVerdict: s2sbench.VerdictCorrect, DurationMS: 10},
		{Arm: comparisonArmQuery, SemanticVerdict: s2sbench.VerdictWrong, ContractVerdict: s2sbench.VerdictCorrect, WorkflowVerdict: s2sbench.VerdictWrong, DurationMS: 20},
	}
	summary := summarizeComparisonArm(comparisonArmQuery, attempts)
	if summary.SemanticCorrect != 1 || summary.SemanticWrong != 1 || summary.SemanticAccuracyRate != 50 || summary.ContractCorrect != 1 || summary.WorkflowCorrect != 1 || summary.DurationMS != 30 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestComparisonSmokeEvidenceRoundTripsStrictly(t *testing.T) {
	want := expectedComparisonSmokeEvidence()
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeComparisonSmokeEvidence(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
	if _, err := decodeComparisonSmokeEvidence(string(body[:len(body)-1]) + `,"unknown":true}`); err == nil {
		t.Fatal("unknown comparison evidence field was accepted")
	}
}

func TestComparisonSemanticVerdictIgnoresMemberShapeAndOrdering(t *testing.T) {
	want := expectedComparisonSmokeEvidence()
	baseline, _ := json.Marshal(want.Baseline.Start)
	current, _ := json.Marshal(want.Current.Start)
	timeDimension := json.RawMessage(`{"dimension":"dimension:comparison.events.event_time","grain":"month"}`)
	flexible := comparisonSmokeFlexibleEvidence{
		TimeDimension: timeDimension, Baseline: baseline, Current: current,
		Metrics: append([]string(nil), want.Metrics...), Dimensions: append([]string(nil), want.Dimensions...),
	}
	for index := len(want.Rows) - 1; index >= 0; index-- {
		row := want.Rows[index]
		members := make(map[string]json.RawMessage, len(row.Members))
		for _, member := range row.Members {
			members[member.Dimension] = member.Value
		}
		encodedMembers, err := json.Marshal(members)
		if err != nil {
			t.Fatal(err)
		}
		values := append([]comparison.MetricValue(nil), row.Values...)
		for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
			values[left], values[right] = values[right], values[left]
		}
		encodedValues, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		flexible.Rows = append(flexible.Rows, comparisonSmokeFlexibleRow{
			Members: encodedMembers, BaselinePresent: row.BaselinePresent, CurrentPresent: row.CurrentPresent, Values: encodedValues,
		})
	}
	for left, right := 0, len(flexible.Metrics)-1; left < right; left, right = left+1, right-1 {
		flexible.Metrics[left], flexible.Metrics[right] = flexible.Metrics[right], flexible.Metrics[left]
	}
	body, err := json.Marshal(flexible)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeComparisonSmokeEvidence(string(body)); err == nil {
		t.Fatal("strict contract accepted object-shaped members")
	}
	got, err := decodeComparisonSemanticEvidence("Computed evidence follows.\n```json\n" + string(body) + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	normalizedWant, err := normalizeComparisonSemanticEvidence(want)
	if err != nil {
		t.Fatal(err)
	}
	if !comparisonSemanticallyEqual(got, normalizedWant) {
		t.Fatalf("semantic evidence = %#v, want %#v", got, normalizedWant)
	}
}

func TestComparisonSemanticValuesAcceptObjectAndNumericPresentation(t *testing.T) {
	raw := json.RawMessage(`{
		"metric:comparison.revenue": {
			"baseline_value": 100.0,
			"current_value": "120",
			"delta": 20,
			"percent_change": "20.00",
			"change_defined": true,
			"percent_defined": true
		}
	}`)
	values, err := normalizeComparisonValues(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Metric != "metric:comparison.revenue" || string(*values[0].BaselineValue) != "100" || string(*values[0].PercentChange) != "20" {
		t.Fatalf("values = %#v", values)
	}
}

func TestComparisonSemanticMembersAcceptAliasesAndPositionalValues(t *testing.T) {
	dimension := "dimension:comparison.events.region"
	for _, raw := range []string{
		`[{"name":"dimension:comparison.events.region","value":"east"}]`,
		`["east"]`,
	} {
		members, err := normalizeComparisonMembers(json.RawMessage(raw), []string{dimension})
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 1 || members[0].Dimension != dimension || string(members[0].Value) != `"east"` {
			t.Fatalf("members = %#v", members)
		}
	}
}

func TestComparisonSmokeToolTraceRequiresProductionWorkflow(t *testing.T) {
	success := func(name string) s2sbench.ToolCallEvidence {
		return s2sbench.ToolCallEvidence{Name: name, Status: "success"}
	}
	if err := validateComparisonToolTrace(comparisonArmQuery, []s2sbench.ToolCallEvidence{success(appmcp.ToolQueryMetrics), success(appmcp.ToolQueryMetrics)}); err != nil {
		t.Fatal(err)
	}
	if err := validateComparisonToolTrace(comparisonArmCompare, []s2sbench.ToolCallEvidence{success(appmcp.ToolCompareMetrics)}); err != nil {
		t.Fatal(err)
	}
	if err := validateComparisonToolTrace(comparisonArmQuery, []s2sbench.ToolCallEvidence{success(appmcp.ToolCompareMetrics)}); err == nil {
		t.Fatal("query arm accepted compare_metrics")
	}
	if err := validateComparisonToolTrace(comparisonArmCompare, []s2sbench.ToolCallEvidence{{Name: appmcp.ToolCompareMetrics, Status: "error"}, success(appmcp.ToolCompareMetrics)}); err == nil {
		t.Fatal("compare arm accepted a failed extra compare_metrics invocation")
	}
}

func TestComparisonSmokeRuntimeProducesFrozenOracleThroughProductionService(t *testing.T) {
	runtime, err := newComparisonSmokeRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close(context.Background()) })
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	for _, scenario := range frozenComparisonScenarios() {
		t.Run(scenario.Name, func(t *testing.T) {
			result, err := runtime.runtime.CompareMetrics.CompareMetrics(ctx, service.MetricComparisonQuery{
				ProjectID: comparisonProject, Metrics: scenario.Metrics,
				TimeDimension: "dimension:comparison.events.event_time",
				Baseline:      comparison.Period{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
				Current:       comparison.Period{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
				Dimensions:    scenario.Dimensions, Filters: scenario.Filters,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := comparisonSmokeEvidence{
				TimeDimension: result.TimeDimension, Baseline: result.Baseline, Current: result.Current,
				Metrics: result.Metrics, Dimensions: result.Dimensions, Rows: result.Rows,
			}
			if want := expectedComparisonEvidence(scenario.Name); !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Fatalf("production comparison evidence differs\ngot=%s\nwant=%s", gotJSON, wantJSON)
			}
		})
	}
}

func TestComparisonSmokeMCPArmsExposeAndExecuteProductionTools(t *testing.T) {
	runtime, err := newComparisonSmokeRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close(context.Background()) })
	for _, arm := range []comparisonSmokeArm{comparisonArmQuery, comparisonArmCompare} {
		endpoint := runtime.endpoints[arm]
		client := mcp.NewClient(&mcp.Implementation{Name: "comparison-test", Version: "1"}, nil)
		transport := &mcp.StreamableClientTransport{
			Endpoint: endpoint.mcp.URL,
			HTTPClient: &http.Client{Transport: comparisonSmokeRoundTripper{
				base: http.DefaultTransport, token: endpoint.environment[s2sbenchMCPTokenEnv], project: comparisonProject,
			}},
			DisableStandaloneSSE: true,
		}
		session, connectErr := client.Connect(context.Background(), transport, nil)
		if connectErr != nil {
			t.Fatalf("arm=%s connect: %v", arm, connectErr)
		}
		tools, listErr := session.ListTools(context.Background(), nil)
		if listErr != nil {
			t.Fatal(listErr)
		}
		hasCompare := false
		for _, tool := range tools.Tools {
			hasCompare = hasCompare || tool.Name == appmcp.ToolCompareMetrics
		}
		if hasCompare != (arm == comparisonArmCompare) {
			t.Fatalf("arm=%s compare_metrics visible=%t", arm, hasCompare)
		}
		if arm == comparisonArmCompare {
			argumentsJSON, _ := json.Marshal(service.MetricComparisonQuery{
				Metrics:       []string{"metric:comparison.revenue", "metric:comparison.sessions"},
				TimeDimension: "dimension:comparison.events.event_time",
				Baseline:      comparison.Period{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
				Current:       comparison.Period{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
				Dimensions:    []string{"dimension:comparison.events.region"},
			})
			var arguments map[string]any
			if err := json.Unmarshal(argumentsJSON, &arguments); err != nil {
				t.Fatal(err)
			}
			result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: appmcp.ToolCompareMetrics, Arguments: arguments})
			if callErr != nil {
				t.Fatalf("production compare_metrics MCP call: %v", callErr)
			}
			if result.IsError {
				encoded, _ := json.Marshal(result)
				t.Fatalf("production compare_metrics MCP result: %s", encoded)
			}
		}
		if closeErr := session.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

type comparisonSmokeRoundTripper struct {
	base    http.RoundTripper
	token   string
	project string
}

func (transport comparisonSmokeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.base == nil {
		return nil, fmt.Errorf("comparison smoke HTTP transport is unavailable")
	}
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+transport.token)
	request.Header.Set(appmcp.ProjectHeaderKey, transport.project)
	return transport.base.RoundTrip(request)
}
