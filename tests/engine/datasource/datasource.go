// Package datasource resolves test-only real-engine DataSources.
// It centralizes environment-variable names without becoming a production
// DataSourceRegistry, Backend registry, or execution-placement authority.
package datasource

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	runtimedatasource "github.com/meaningforge/metis/execution/datasource"
	"go.yaml.in/yaml/v3"
)

//go:embed datasources.yaml
var dataSourcesYAML []byte

type dataSourceDefinition struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type document map[string]dataSourceDefinition

var environmentReferencePattern = regexp.MustCompile(`^\$\{([A-Z_][A-Z0-9_]*)\}$`)

// DataSource is one resolved test connection. Values remain private so callers
// cannot accidentally format the whole DataSource, including credentials.
type DataSource struct {
	name    string
	typeID  string
	config  map[string]string
	values  map[string]string
	secrets map[string]string
}

// MissingEnvironmentError identifies unavailable connection inputs without
// exposing any resolved values.
type MissingEnvironmentError struct {
	DataSource string
	Variables  []string
}

func (e *MissingEnvironmentError) Error() string {
	return fmt.Sprintf("test DataSource %q requires environment variables %s", e.DataSource, strings.Join(e.Variables, ", "))
}

// Require resolves a checked-in DataSource and skips the test when its external
// environment is unavailable. Invalid DataSource configuration always fails.
func Require(t testing.TB, name string) DataSource {
	t.Helper()
	dataSource, err := Resolve(name, os.LookupEnv)
	if err == nil {
		return dataSource
	}
	var missing *MissingEnvironmentError
	if errors.As(err, &missing) {
		t.Skip(missing.Error())
	}
	t.Fatal(err)
	return DataSource{}
}

// Resolve loads one DataSource using the supplied environment lookup. Keeping
// lookup explicit makes resolution deterministic and unit-testable.
func Resolve(name string, lookupEnv func(string) (string, bool)) (DataSource, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(dataSourcesYAML)))
	decoder.KnownFields(true)
	var definitions document
	if err := decoder.Decode(&definitions); err != nil {
		return DataSource{}, fmt.Errorf("parse test DataSources: %w", err)
	}
	return resolve(definitions, name, lookupEnv)
}

func resolve(definitions document, name string, lookupEnv func(string) (string, bool)) (DataSource, error) {
	definition, ok := definitions[name]
	if !ok {
		return DataSource{}, fmt.Errorf("test DataSource %q is not configured", name)
	}
	typeID := strings.TrimSpace(definition.Type)
	if typeID == "" || typeID != definition.Type {
		return DataSource{}, fmt.Errorf("test DataSource %q has an invalid type", name)
	}
	if len(definition.Config) == 0 {
		return DataSource{}, fmt.Errorf("test DataSource %q has no config", name)
	}
	if lookupEnv == nil {
		return DataSource{}, fmt.Errorf("test DataSource environment lookup is required")
	}
	resolved := DataSource{
		name:    name,
		typeID:  typeID,
		config:  make(map[string]string, len(definition.Config)),
		values:  make(map[string]string, len(definition.Config)),
		secrets: make(map[string]string),
	}
	var missing []string
	for key, configured := range definition.Config {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key {
			return DataSource{}, fmt.Errorf("test DataSource %q has an invalid config name", name)
		}
		if configured == "" || strings.TrimSpace(configured) != configured {
			return DataSource{}, fmt.Errorf("test DataSource %q config %q must be a non-empty trimmed string", name, key)
		}
		resolved.config[key] = configured
		match := environmentReferencePattern.FindStringSubmatch(configured)
		if len(match) == 0 {
			if strings.Contains(configured, "${") {
				return DataSource{}, fmt.Errorf("test DataSource %q config %q has an invalid environment reference", name, key)
			}
			if isSensitiveConfigKey(key) {
				return DataSource{}, fmt.Errorf("test DataSource %q config %q must be an environment reference", name, key)
			}
			resolved.values[key] = configured
			continue
		}
		environment := match[1]
		value, exists := lookupEnv(environment)
		if !exists || value == "" {
			missing = append(missing, environment)
			continue
		}
		if isSensitiveConfigKey(key) {
			resolved.secrets[key] = value
		} else {
			resolved.values[key] = value
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return DataSource{}, &MissingEnvironmentError{DataSource: name, Variables: missing}
	}
	return resolved, nil
}

// Config returns one raw configured string, preserving environment references.
func (p DataSource) Config(name string) (string, bool) {
	value, ok := p.config[name]
	return value, ok
}

// Value returns one literal or resolved non-sensitive connection value without
// changing the reference token stored in Config.
func (p DataSource) Value(name string) (string, bool) {
	value, ok := p.values[name]
	return value, ok
}

// Secret returns one resolved sensitive value without copying it into Config.
func (p DataSource) Secret(name string) (string, bool) {
	value, ok := p.secrets[name]
	return value, ok
}

// Type returns the configured database-family type.
func (p DataSource) Type() string { return p.typeID }

// RuntimeDataSource returns the secret-reference preserving production
// DataSource shape used by real-engine execution tests. Resolved connection
// values remain private; execution/runner resolves the original references
// through its configured SecretResolver.
func (p DataSource) RuntimeDataSource(policy runtimedatasource.DataSourcePolicy) runtimedatasource.DataSource {
	config := make(map[string]string, len(p.config))
	for name, value := range p.config {
		config[name] = value
	}
	return runtimedatasource.DataSource{
		Type:   runtimedatasource.Type(p.typeID),
		Config: config,
		Policy: policy,
	}
}

// RequireValue returns one literal or resolved non-sensitive connection value.
func (p DataSource) RequireValue(t testing.TB, name string) string {
	t.Helper()
	value, ok := p.Value(name)
	if !ok {
		t.Fatalf("test DataSource %q of type %q has no connection value %q", p.name, p.typeID, name)
	}
	return value
}

// RequireSecret returns one resolved sensitive value without exposing it in
// failure output or the DataSource Config map.
func (p DataSource) RequireSecret(t testing.TB, name string) string {
	t.Helper()
	value, ok := p.Secret(name)
	if !ok {
		t.Fatalf("test DataSource %q of type %q has no resolved secret %q", p.name, p.typeID, name)
	}
	return value
}

func isSensitiveConfigKey(key string) bool {
	normalized := strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "password", "passwd", "pass", "secret", "token", "api_key", "apikey", "private_key", "access_key", "client_secret":
		return true
	default:
		return false
	}
}
