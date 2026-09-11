package semantic

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestDiagnosticFromSemanticErrorPreservesStableEvidence(t *testing.T) {
	err := &serrors.Error{
		Code:        serrors.ErrMetricNotFound,
		Message:     "metric not found",
		Details:     map[string]any{"metric": "revenu", "model": "sales"},
		Suggestions: []string{"revenue"},
	}

	got := diagnosticFromSemanticError(err)
	if got.Code != serrors.ErrMetricNotFound || got.Severity != DiagnosticSeverityError {
		t.Fatalf("diagnostic identity = %#v", got)
	}
	wantSubject := &DiagnosticSubject{Kind: "metric", Name: "revenu"}
	if !reflect.DeepEqual(got.Subject, wantSubject) {
		t.Fatalf("subject = %#v, want %#v", got.Subject, wantSubject)
	}
	wantSuggestions := []SemanticSuggestion{{Kind: "hint", Value: "revenue"}}
	if !reflect.DeepEqual(got.Suggestions, wantSuggestions) {
		t.Fatalf("suggestions = %#v, want %#v", got.Suggestions, wantSuggestions)
	}
	if got.Details["model"] != "sales" {
		t.Fatalf("details = %#v", got.Details)
	}
}

func TestDiagnosticFromSemanticErrorLeavesUnknownSubjectUnclaimed(t *testing.T) {
	err := &serrors.Error{
		Code:    serrors.ErrUnsupportedDialect,
		Message: "unsupported dialect",
		Details: map[string]any{"dialect": "unknown"},
	}
	got := diagnosticFromSemanticError(err)
	if got.Subject != nil {
		t.Fatalf("subject = %#v", got.Subject)
	}
}

func TestEnrichDiagnosticAddsBoundedMetricCandidates(t *testing.T) {
	svc := &CompileService{discovery: newService(t)}
	diagnostic := SemanticDiagnostic{
		Code:     serrors.ErrMetricNotFound,
		Severity: DiagnosticSeverityError,
		Subject:  &DiagnosticSubject{Kind: "metric", Name: "revenue"},
	}

	svc.enrichDiagnostic(context.Background(), CompileRequest{
		Query: query.SemanticQuery{Project: "finance", Model: "sales"},
	}, &diagnostic)

	if len(diagnostic.Suggestions) == 0 {
		t.Fatal("expected metric repair candidates")
	}
	if len(diagnostic.Suggestions) > diagnosticCandidateLimit {
		t.Fatalf("candidate count = %d, max %d", len(diagnostic.Suggestions), diagnosticCandidateLimit)
	}
	first := diagnostic.Suggestions[0]
	if first.Kind != "metric_candidate" || first.Value != "sales.total_revenue" {
		t.Fatalf("first candidate = %#v", first)
	}
	if !reflect.DeepEqual(first.MatchReasons, []MatchReason{MatchReasonNameContains}) {
		t.Fatalf("candidate match reasons = %#v", first.MatchReasons)
	}
	assertMetricRepair(t, diagnostic.Repair, "finance", "revenue")
}

func TestEnrichDiagnosticDoesNotGuessForUnrelatedFailures(t *testing.T) {
	svc := &CompileService{discovery: newService(t)}
	diagnostic := SemanticDiagnostic{
		Code:     serrors.ErrUnsupportedDialect,
		Severity: DiagnosticSeverityError,
		Subject:  &DiagnosticSubject{Kind: "dialect", Name: "unknown"},
	}

	svc.enrichDiagnostic(context.Background(), CompileRequest{
		Query: query.SemanticQuery{Project: "finance", Model: "sales"},
	}, &diagnostic)

	if len(diagnostic.Suggestions) != 0 || diagnostic.Repair != nil {
		t.Fatalf("unexpected diagnostic repair evidence = %#v", diagnostic)
	}
}

func TestCompileErrorCarriesDimensionRepairThroughCompatibleMetricTool(t *testing.T) {
	svc := &CompileService{}
	err := svc.enrichCompileError(context.Background(), CompileRequest{
		Query: query.SemanticQuery{
			Project: "finance",
			Model:   "sales",
			Metrics: []query.MetricRef{{Name: "total_revenue"}},
		},
	}, &serrors.Error{Code: serrors.ErrDimensionNotFound, Message: "dimension not found", Details: map[string]any{"dimension": "region"}})

	semanticErr, ok := err.(*serrors.Error)
	if !ok {
		t.Fatalf("error = %T", err)
	}
	action, ok := semanticErr.Details["repair"].(*SemanticRepairAction)
	if !ok {
		t.Fatalf("repair = %#v", semanticErr.Details["repair"])
	}
	if action.Tool != semanticGetDimensionsRepairTool {
		t.Fatalf("repair tool = %q", action.Tool)
	}
	arguments, ok := action.Arguments.(GetDimensionsRequest)
	if !ok {
		t.Fatalf("repair arguments = %T", action.Arguments)
	}
	if arguments.ProjectID != "finance" || !reflect.DeepEqual(arguments.Search, []string{"region"}) || !reflect.DeepEqual(arguments.Metrics, []string{"metric:sales.total_revenue"}) {
		t.Fatalf("repair arguments = %#v", arguments)
	}
}

func TestMetricFreeDimensionRepairUsesModelScopedDiscovery(t *testing.T) {
	action := repairActionForDiagnostic(CompileRequest{
		Query: query.SemanticQuery{Project: "finance", Model: "sales"},
	}, &SemanticDiagnostic{
		Code:    serrors.ErrDimensionNotFound,
		Subject: &DiagnosticSubject{Kind: "dimension", Name: "customer_tier"},
	})
	if action == nil || action.Tool != semanticGetDimensionsRepairTool {
		t.Fatalf("repair action = %#v", action)
	}
	arguments, ok := action.Arguments.(GetDimensionsRequest)
	if !ok {
		t.Fatalf("repair arguments = %T", action.Arguments)
	}
	if arguments.ProjectID != "finance" || arguments.Model != "model:sales" || len(arguments.Metrics) != 0 || !reflect.DeepEqual(arguments.Search, []string{"customer_tier"}) {
		t.Fatalf("repair arguments = %#v", arguments)
	}
}

func TestCompileRepairOmitsInheritedProjectContext(t *testing.T) {
	action := repairActionForDiagnostic(CompileRequest{
		Query:                   query.SemanticQuery{Project: "finance", Model: "sales"},
		ProjectContextInherited: true,
	}, &SemanticDiagnostic{Code: serrors.ErrMetricNotFound, Subject: &DiagnosticSubject{Kind: "metric", Name: "revenue"}})
	arguments, ok := action.Arguments.(ListMetricsRequest)
	if !ok || arguments.ProjectID != "" || !reflect.DeepEqual(arguments.Search, []string{"revenue"}) {
		t.Fatalf("repair arguments = %#v", action.Arguments)
	}
}

func assertMetricRepair(t *testing.T, action *SemanticRepairAction, project, search string) {
	t.Helper()
	if action == nil || action.Tool != semanticListMetricsRepairTool {
		t.Fatalf("repair action = %#v", action)
	}
	arguments, ok := action.Arguments.(ListMetricsRequest)
	if !ok {
		t.Fatalf("repair arguments = %T", action.Arguments)
	}
	if arguments.ProjectID != project || !reflect.DeepEqual(arguments.Search, []string{search}) {
		t.Fatalf("repair arguments = %#v", arguments)
	}
}
