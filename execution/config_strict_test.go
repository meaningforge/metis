package execution

import (
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestLoadProjectConfigRejectsUnknownTopLevelField(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sourcess:
  sales:
    path: ./sales.ossie.yaml
`))
	assertUnknownConfigField(t, err, "semantic_sourcess")
}

func TestLoadProjectConfigRejectsUnknownSemanticSourceField(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
    description: Sales model
`))
	assertUnknownConfigField(t, err, "description")
}

func TestLoadProjectConfigRejectsDeploymentIdentityAndBindingFields(t *testing.T) {
	for _, field := range []string{"project", "default_execution_binding", "execution_bindings"} {
		t.Run(field, func(t *testing.T) {
			_, err := LoadProjectConfig([]byte(field + `: value
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
`))
			assertUnknownConfigField(t, err, field)
		})
	}
}

func TestLoadProjectConfigAcceptsSemanticSources(t *testing.T) {
	cfg, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
  customers:
    path: ./customers/*.ossie.yaml
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SemanticSources) != 2 || cfg.SemanticSources["sales"].Path != "./sales.ossie.yaml" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadProjectConfigAcceptsQualityPolicy(t *testing.T) {
	cfg, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
quality:
  severities:
    MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION: error
  publication_threshold: warning
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Quality.PublicationThreshold != "warning" || cfg.Quality.Severities["MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION"] != "error" {
		t.Fatalf("quality policy = %#v", cfg.Quality)
	}
}

func TestLoadProjectConfigRejectsInvalidQualitySeverity(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
quality:
  severities:
    MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION: advisory
`))
	if err == nil || !strings.Contains(err.Error(), "quality severity") {
		t.Fatalf("invalid quality policy error = %v", err)
	}
}

func TestLoadProjectConfigReportsQualityPolicyErrorsDeterministically(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
quality:
  severities:
    MODEL_QUALITY_Z_RULE: advisory
    MODEL_QUALITY_A_RULE: fatal
`))
	var configErr *serrors.Error
	if !errors.As(err, &configErr) || configErr.Details["code"] != "MODEL_QUALITY_A_RULE" {
		t.Fatalf("deterministic quality policy error = %#v", err)
	}
}

func TestLoadProjectConfigRejectsInvalidQualityPublicationThreshold(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
quality:
  publication_threshold: advisory
`))
	if err == nil || !strings.Contains(err.Error(), "quality publication threshold") {
		t.Fatalf("invalid quality publication threshold error = %v", err)
	}
}

func TestLoadProjectConfigRejectsUnknownQualityField(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
quality:
  threshold: warning
`))
	assertUnknownConfigField(t, err, "threshold")
}

func TestLoadProjectConfigRejectsDuplicateSemanticSourcePaths(t *testing.T) {
	_, err := LoadProjectConfig([]byte(`
semantic_sources:
  first:
    path: ./models/../sales.ossie.yaml
  second:
    path: ./sales.ossie.yaml
`))
	if err == nil || !strings.Contains(err.Error(), "duplicate semantic source path") {
		t.Fatalf("expected duplicate source path error, got %v", err)
	}
}

func assertUnknownConfigField(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected unknown field %q to fail", field)
	}
	var configErr *serrors.Error
	if !errors.As(err, &configErr) || configErr.Code != serrors.ErrInvalidExecutionConfig {
		t.Fatalf("error=%#v want %s", err, serrors.ErrInvalidExecutionConfig)
	}
	cause, _ := configErr.Details["cause"].(string)
	if !strings.Contains(cause, field) || !strings.Contains(cause, "field") {
		t.Fatalf("cause=%q does not identify unknown field %q", cause, field)
	}
}
