package bootstrap_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestValidateExplainCompileConsistency(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "sales.ossie.yaml"), salesModel)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
`)

	runtime, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	projectID := filepath.Base(dir)
	valid := service.CompileRequest{Query: query.SemanticQuery{
		Project:    projectID,
		Model:      "sales",
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "region"}},
	}, Dialect: "DORIS"}
	validation, err := runtime.Compile.Validate(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || len(validation.Diagnostics) != 0 {
		t.Fatalf("validation = %#v", validation)
	}
	if _, err := runtime.Compile.Explain(ctx, valid); err != nil {
		t.Fatalf("explain valid query: %v", err)
	}
	if _, err := runtime.Compile.Compile(ctx, valid); err != nil {
		t.Fatalf("compile valid query: %v", err)
	}

	invalid := service.CompileRequest{Query: query.SemanticQuery{
		Project: projectID,
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "missing_revenue"}},
	}, Dialect: "DORIS"}
	validation, err = runtime.Compile.Validate(ctx, invalid)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid || len(validation.Diagnostics) != 1 {
		t.Fatalf("invalid validation = %#v", validation)
	}
	got := validation.Diagnostics[0]
	if got.Code != serrors.ErrMetricNotFound {
		t.Fatalf("validation code = %s, want %s", got.Code, serrors.ErrMetricNotFound)
	}
	if got.Subject == nil || got.Subject.Kind != "metric" || got.Subject.Name != "missing_revenue" {
		t.Fatalf("validation subject = %#v", got.Subject)
	}

	_, explainErr := runtime.Compile.Explain(ctx, invalid)
	_, compileErr := runtime.Compile.Compile(ctx, invalid)
	if explainErr == nil || compileErr == nil {
		t.Fatalf("expected explain/compile failures: explain=%v compile=%v", explainErr, compileErr)
	}
	explainPayload := serrors.PayloadFrom(explainErr)
	compilePayload := serrors.PayloadFrom(compileErr)
	if explainPayload.Code != string(got.Code) || compilePayload.Code != string(got.Code) {
		t.Fatalf("codes: validate=%s explain=%s compile=%s", got.Code, explainPayload.Code, compilePayload.Code)
	}
	if explainPayload.Details["metric"] != got.Details["metric"] || compilePayload.Details["metric"] != got.Details["metric"] {
		t.Fatalf("metric evidence diverged: validate=%#v explain=%#v compile=%#v", got.Details, explainPayload.Details, compilePayload.Details)
	}
}
