package datasource_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/renderer/doris"
)

type validatingDriverFactory struct {
	dataSourceType datasource.Type
}

type identityDriverFactory struct {
	dataSourceType datasource.Type
	validated      bool
}

func intPointer(value int) *int { return &value }

func (f *identityDriverFactory) DataSourceType() datasource.Type {
	return f.dataSourceType
}

func (f *identityDriverFactory) ValidateConfig(map[string]string) error {
	f.validated = true
	return nil
}

func (*identityDriverFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return nil, nil
}

func (f *validatingDriverFactory) DataSourceType() datasource.Type {
	return f.dataSourceType
}

func (*validatingDriverFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return nil, nil
}

func (f *validatingDriverFactory) ValidateConfig(config map[string]string) error {
	host := config["host"]
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

func TestLoadDataSourceRegistryValidatesSecretReferencesAndCopiesConfig(t *testing.T) {
	registry, err := datasource.LoadDataSourceRegistry([]byte(`
doris-prod:
  type: DORIS
  config:
    host: doris.internal
    port: "9030"
    password: "${METIS_DORIS_PASSWORD}"
  policy:
    query_timeout: 30s
    max_rows: 100000
    max_bytes: 67108864
    max_concurrency: 8
`))
	if err != nil {
		t.Fatalf("LoadDataSourceRegistry() error = %v", err)
	}
	source, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if source.Type != "doris" || source.Policy.QueryTimeout != "30s" {
		t.Fatalf("source = %#v", source)
	}
	source.Config["host"] = "mutated"
	fresh, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	if fresh.Config["host"] != "doris.internal" {
		t.Fatalf("registry config was mutated: %#v", fresh.Config)
	}
}

func TestDataSourceRegistryRejectsInvalidShapeAndPlaintextSecrets(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown source field",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  endpoint: unexpected\n",
			want: "field endpoint not found",
		},
		{
			name: "duplicate source identity",
			yaml: "doris-prod:\n  type: doris\n  config: {}\ndoris-prod:\n  type: doris\n  config: {}\n",
			want: "mapping key \"doris-prod\" already defined",
		},
		{
			name: "nested config value",
			yaml: "doris-prod:\n  type: doris\n  config:\n    tls:\n      mode: verify_identity\n",
			want: "cannot unmarshal",
		},
		{
			name: "plaintext password",
			yaml: "doris-prod:\n  type: doris\n  config:\n    password: plaintext\n  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1024\n    max_concurrency: 8\n",
			want: "sensitive value must be an external value reference",
		},
		{
			name: "malformed secret reference",
			yaml: "doris-prod:\n  type: doris\n  config:\n    password: '${lowercase-is-invalid}'\n  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1024\n    max_concurrency: 8\n",
			want: "invalid environment reference",
		},
		{
			name: "malformed provider reference",
			yaml: "doris-prod:\n  type: doris\n  config:\n    password: 'secret://aws-secrets-manager/'\n  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1024\n    max_concurrency: 8\n",
			want: "invalid secret provider reference",
		},
		{
			name: "invalid timeout",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  policy:\n    query_timeout: never\n",
			want: "query_timeout must be a positive duration",
		},
		{
			name: "zero row limit",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  policy:\n    query_timeout: 30s\n    max_rows: 0\n    max_bytes: 1024\n    max_concurrency: 8\n",
			want: "max_rows must be positive",
		},
		{
			name: "missing timeout",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  policy:\n    max_rows: 100\n    max_bytes: 1024\n    max_concurrency: 8\n",
			want: "query_timeout is required",
		},
		{
			name: "missing row limit",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  policy:\n    query_timeout: 30s\n    max_bytes: 1024\n    max_concurrency: 8\n",
			want: "max_rows is required",
		},
		{
			name: "missing byte limit",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  policy:\n    query_timeout: 30s\n    max_rows: 100\n",
			want: "max_bytes is required",
		},
		{
			name: "multiple yaml documents",
			yaml: "doris-prod:\n  type: doris\n  config: {}\n  policy:\n    query_timeout: 30s\n    max_rows: 100\n    max_bytes: 1024\n---\nsecond:\n  type: doris\n  config: {}\n",
			want: "must contain exactly one YAML document",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := datasource.LoadDataSourceRegistry([]byte(test.yaml))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadDataSourceRegistry() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDataSourceRegistryDefaultsMaxConcurrency(t *testing.T) {
	registry, err := datasource.LoadDataSourceRegistry([]byte(`
doris-prod:
  type: doris
  config: {}
  policy:
    query_timeout: 30s
    max_rows: 100
    max_bytes: 1024
`))
	if err != nil {
		t.Fatal(err)
	}
	source, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	if source.Policy.MaxConcurrency == nil || *source.Policy.MaxConcurrency != datasource.DefaultMaxConcurrency {
		t.Fatalf("max_concurrency = %#v, want %d", source.Policy.MaxConcurrency, datasource.DefaultMaxConcurrency)
	}
}

func TestDataSourceRegistryValidatesEverySourceAgainstBackend(t *testing.T) {
	registry, err := datasource.LoadDataSourceRegistry([]byte(`
doris-prod:
  type: doris
  config:
    host: doris.internal
    port: "9030"
  policy:
    query_timeout: 30s
    max_rows: 100
    max_bytes: 1024
    max_concurrency: 8
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := (*backend.BackendRegistry)(nil).ValidateDataSources(registry); err == nil || !strings.Contains(err.Error(), "Backend registry is required") {
		t.Fatalf("ValidateDataSources(nil) error = %v", err)
	}
	backends, err := backend.NewBackendRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := backends.ValidateDataSources(registry); err == nil || !strings.Contains(err.Error(), "unavailable Backend") {
		t.Fatalf("ValidateDataSources() missing Backend error = %v", err)
	}
	if err := backends.Register(backend.Backend{
		Type:          "doris",
		Renderer:      doris.New(),
		DriverFactory: &validatingDriverFactory{dataSourceType: "doris"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := backends.ValidateDataSources(registry); err != nil {
		t.Fatalf("ValidateDataSources() error = %v", err)
	}
}

func TestDataSourceRegistryRejectsDriverOwnedInvalidConfig(t *testing.T) {
	registry, err := datasource.LoadDataSourceRegistry([]byte(`
doris-prod:
  type: doris
  config: {}
  policy:
    query_timeout: 30s
    max_rows: 100
    max_bytes: 1024
    max_concurrency: 8
`))
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{
		Type:          "doris",
		Renderer:      doris.New(),
		DriverFactory: &validatingDriverFactory{dataSourceType: "doris"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backends.ValidateDataSources(registry); err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("ValidateDataSources() error = %v", err)
	}
}

func TestBackendRegistryInvokesMandatoryDriverConfigurationValidation(t *testing.T) {
	registry, err := datasource.LoadDataSourceRegistry([]byte(`
doris-prod:
  type: doris
  config:
    host: doris.internal
    port: "9030"
  policy:
    query_timeout: 30s
    max_rows: 100
    max_bytes: 1024
    max_concurrency: 8
`))
	if err != nil {
		t.Fatal(err)
	}
	factory := &identityDriverFactory{dataSourceType: "doris"}
	backends, err := backend.NewBackendRegistry(backend.Backend{
		Type:          "doris",
		Renderer:      doris.New(),
		DriverFactory: factory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backends.ValidateDataSources(registry); err != nil {
		t.Fatalf("ValidateDataSources() error = %v", err)
	}
	if !factory.validated {
		t.Fatal("ValidateDataSources() did not invoke DriverFactory.ValidateConfig")
	}
}

func TestDataSourceRegistryCopiesExecutionPolicy(t *testing.T) {
	maxRows := int64(100)
	maxBytes := int64(1024)
	registry, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"doris-prod": {
			Type:   "doris",
			Config: map[string]string{},
			Policy: datasource.DataSourcePolicy{QueryTimeout: "30s", MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: intPointer(8)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	maxRows = 200
	maxBytes = 2048
	resolved, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	*resolved.Policy.MaxRows = 300
	*resolved.Policy.MaxBytes = 3072
	fresh, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	if *fresh.Policy.MaxRows != 100 || *fresh.Policy.MaxBytes != 1024 {
		t.Fatalf("stored policy was mutated: %#v", fresh.Policy)
	}
}

func TestDataSourceRegistryOwnsCanonicalConfigSnapshots(t *testing.T) {
	maxRows := int64(100)
	maxBytes := int64(1024)
	config := map[string]string{
		"host":     "doris.internal",
		"port":     "9030",
		"tls_mode": "verify_identity",
	}
	registry, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"doris-prod": {
			Type:   "doris",
			Config: config,
			Policy: datasource.DataSourcePolicy{QueryTimeout: "30s", MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: intPointer(8)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	config["host"] = "mutated"
	source, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	if source.Config["host"] != "doris.internal" || source.Config["tls_mode"] != "verify_identity" {
		t.Fatalf("registry Config retained caller aliases: %#v", source.Config)
	}

	snapshot := source.ConfigSnapshot()
	snapshot["tls_mode"] = "disabled"
	if source.Config["tls_mode"] != "verify_identity" {
		t.Fatalf("ConfigSnapshot mutation reached source: %#v", source.Config)
	}
	fresh, err := registry.Resolve("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Config["tls_mode"] != "verify_identity" {
		t.Fatalf("ConfigSnapshot mutation reached registry: %#v", fresh.Config)
	}
}

func TestSecretReferencesAreParsedFromEveryReferencedConfigField(t *testing.T) {
	references, err := datasource.SecretReferences(map[string]string{
		"host":        "${METIS_DORIS_HOST}",
		"password":    "${METIS_DORIS_PASSWORD}",
		"token":       "${METIS_DORIS_PASSWORD}",
		"private_key": "secret://aws-secrets-manager/prod/metis/private-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 3 ||
		references[0] != (datasource.SecretRef{Provider: "aws-secrets-manager", Key: "prod/metis/private-key"}) ||
		references[1] != (datasource.SecretRef{Provider: "env", Key: "METIS_DORIS_HOST"}) ||
		references[2] != (datasource.SecretRef{Provider: "env", Key: "METIS_DORIS_PASSWORD"}) {
		t.Fatalf("SecretReferences() = %#v", references)
	}
}
