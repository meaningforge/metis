// Package datasource defines deployment-scoped DataSource instances and their
// secret-reference preserving configuration boundary.
package datasource

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

// Type is the database-family identity of a deployment DataSource. It is not a
// SQL dialect, driver name, or connection protocol.
type Type string

// NormalizeType canonicalizes a deployment DataSource type.
func NormalizeType(name string) Type {
	return Type(strings.ToLower(strings.TrimSpace(name)))
}

// SecretRef is a provider-owned external-value locator. It never contains a
// resolved value. Execution and control-plane network adapters resolve it only
// at their credential-use boundary; semantic assets never contain credentials.
type SecretRef struct {
	Provider string `yaml:"provider" json:"provider"`
	Key      string `yaml:"key" json:"key"`
}

// DefaultMaxConcurrency is the bounded execution ceiling applied when a
// DataSource omits policy.max_concurrency.
const DefaultMaxConcurrency = 100

// DataSourcePolicy declares bounded execution and admission policy without
// opening a connection. MaxConcurrency defaults to DefaultMaxConcurrency;
// queueing is opt-in, and an absent MaxQueue fails fast at capacity.
type DataSourcePolicy struct {
	QueryTimeout   string `yaml:"query_timeout,omitempty" json:"query_timeout,omitempty"`
	MaxRows        *int64 `yaml:"max_rows,omitempty" json:"max_rows,omitempty"`
	MaxBytes       *int64 `yaml:"max_bytes,omitempty" json:"max_bytes,omitempty"`
	MaxConcurrency *int   `yaml:"max_concurrency,omitempty" json:"max_concurrency,omitempty"`
	MaxQueue       *int   `yaml:"max_queue,omitempty" json:"max_queue,omitempty"`
	QueueTimeout   string `yaml:"queue_timeout,omitempty" json:"queue_timeout,omitempty"`
}

var (
	environmentSecretRefPattern = regexp.MustCompile(`^\$\{([A-Z_][A-Z0-9_]*)\}$`)
	providerSecretRefPattern    = regexp.MustCompile(`^secret://([a-z][a-z0-9_-]*)/(\S+)$`)
)

// DataSource is one named deployment-scoped database or warehouse instance.
// Config is a flat string map. Sensitive fields contain external references
// such as ${METIS_DORIS_PASSWORD} or secret://provider/key; resolved plaintext
// never enters Config.
type DataSource struct {
	Type   Type              `yaml:"type" json:"type"`
	Config map[string]string `yaml:"config" json:"config"`
	Policy DataSourcePolicy  `yaml:"policy,omitempty" json:"policy,omitempty"`
}

// DataSourceRegistry owns concrete instance configuration by deployment-local
// identity. It does not own Backend implementations or semantic routing.
type DataSourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]DataSource
	frozen  bool
}

// NewDataSourceRegistry constructs a registry from named concrete instances.
func NewDataSourceRegistry(sources map[string]DataSource) (*DataSourceRegistry, error) {
	registry := &DataSourceRegistry{sources: make(map[string]DataSource, len(sources))}
	for _, name := range sortedDataSourceNames(sources) {
		if err := registry.Register(name, sources[name]); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// LoadDataSourceRegistryFile loads one strict DataSource registry file.
func LoadDataSourceRegistryFile(path string) (*DataSourceRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadDataSourceRegistry(data)
}

// LoadDataSourceRegistry decodes one strict DataSource registry document.
func LoadDataSourceRegistry(data []byte) (*DataSourceRegistry, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var sources map[string]DataSource
	if err := decoder.Decode(&sources); err != nil {
		return nil, fmt.Errorf("parse DataSource registry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parse DataSource registry: %w", err)
		}
		return nil, fmt.Errorf("DataSource registry must contain exactly one YAML document")
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("DataSource registry must define at least one DataSource")
	}
	return NewDataSourceRegistry(sources)
}

// Register adds one concrete DataSource and validates only deployment-neutral
// shape. Driver-specific configuration is validated by BackendRegistry.
func (r *DataSourceRegistry) Register(name string, source DataSource) error {
	if r == nil {
		return fmt.Errorf("DataSource registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("DataSource registry is frozen")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("DataSource name is required")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("DataSource name %q must not have surrounding whitespace", name)
	}
	if NormalizeType(string(source.Type)) == "" {
		return fmt.Errorf("DataSource type is required for %q", name)
	}
	source.Policy = WithPolicyDefaults(source.Policy)
	if err := ValidatePolicy(source.Policy); err != nil {
		return fmt.Errorf("validate DataSource %q policy: %w", name, err)
	}
	config, err := snapshotStringMap(source.Config, "config")
	if err != nil {
		return fmt.Errorf("validate DataSource %q config: %w", name, err)
	}
	for field, value := range config {
		_, isReference, parseErr := ParseSecretRef(value)
		if parseErr != nil {
			return fmt.Errorf("validate DataSource %q config.%s: %w", name, field, parseErr)
		}
		if isSensitiveConfigKey(field) {
			if !isReference {
				return fmt.Errorf("validate DataSource %q config.%s: sensitive value must be an external value reference", name, field)
			}
		}
	}
	if r.sources == nil {
		r.sources = make(map[string]DataSource)
	}
	if _, exists := r.sources[name]; exists {
		return fmt.Errorf("DataSource %q is already registered", name)
	}
	source.Type = NormalizeType(string(source.Type))
	source.Config = config
	source.Policy = copyDataSourcePolicy(source.Policy)
	r.sources[name] = source
	return nil
}

// Resolve returns a copy of one named DataSource.
func (r *DataSourceRegistry) Resolve(name string) (DataSource, error) {
	if r == nil {
		return DataSource{}, fmt.Errorf("DataSource registry is nil")
	}
	if strings.TrimSpace(name) == "" {
		return DataSource{}, fmt.Errorf("DataSource name is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	source, exists := r.sources[name]
	if !exists {
		return DataSource{}, fmt.Errorf("DataSource %q is not registered", name)
	}
	source.Config = copyStringMap(source.Config)
	source.Policy = copyDataSourcePolicy(source.Policy)
	return source, nil
}

// Names returns registered DataSource names in deterministic order.
func (r *DataSourceRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sortedDataSourceNames(r.sources)
}

// Freeze makes the registry an immutable runtime read model. Deployments use
// replacement rather than mutating live DataSource configuration.
func (r *DataSourceRegistry) Freeze() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

// ValidatePolicy verifies that execution and admission policy is bounded.
func ValidatePolicy(policy DataSourcePolicy) error {
	if strings.TrimSpace(policy.QueryTimeout) == "" {
		return fmt.Errorf("query_timeout is required")
	}
	duration, err := time.ParseDuration(policy.QueryTimeout)
	if err != nil || duration <= 0 {
		return fmt.Errorf("query_timeout must be a positive duration")
	}
	if policy.MaxRows == nil {
		return fmt.Errorf("max_rows is required")
	}
	if *policy.MaxRows <= 0 {
		return fmt.Errorf("max_rows must be positive")
	}
	if policy.MaxBytes == nil {
		return fmt.Errorf("max_bytes is required")
	}
	if *policy.MaxBytes <= 0 {
		return fmt.Errorf("max_bytes must be positive")
	}
	if policy.MaxConcurrency == nil || *policy.MaxConcurrency <= 0 {
		return fmt.Errorf("max_concurrency must be positive")
	}
	if policy.MaxQueue != nil && *policy.MaxQueue < 0 {
		return fmt.Errorf("max_queue must not be negative")
	}
	if policy.MaxQueue == nil || *policy.MaxQueue == 0 {
		if strings.TrimSpace(policy.QueueTimeout) != "" {
			return fmt.Errorf("queue_timeout requires a positive max_queue")
		}
		return nil
	}
	if strings.TrimSpace(policy.QueueTimeout) == "" {
		return fmt.Errorf("queue_timeout is required when max_queue is positive")
	}
	queueTimeout, err := time.ParseDuration(policy.QueueTimeout)
	if err != nil || queueTimeout <= 0 {
		return fmt.Errorf("queue_timeout must be a positive duration")
	}
	return nil
}

// WithPolicyDefaults applies bounded defaults for omitted optional policy.
func WithPolicyDefaults(policy DataSourcePolicy) DataSourcePolicy {
	if policy.MaxConcurrency == nil {
		maxConcurrency := DefaultMaxConcurrency
		policy.MaxConcurrency = &maxConcurrency
	}
	return policy
}

// ParseSecretRef recognizes environment shorthand and provider-neutral secret
// locators. It never contacts a provider or returns resolved material.
func ParseSecretRef(value string) (SecretRef, bool, error) {
	match := environmentSecretRefPattern.FindStringSubmatch(value)
	if len(match) == 2 {
		return SecretRef{Provider: "env", Key: match[1]}, true, nil
	}
	match = providerSecretRefPattern.FindStringSubmatch(value)
	if len(match) == 3 {
		return SecretRef{Provider: match[1], Key: match[2]}, true, nil
	}
	if strings.Contains(value, "${") {
		return SecretRef{}, false, fmt.Errorf("invalid environment reference")
	}
	if strings.Contains(value, "secret://") {
		return SecretRef{}, false, fmt.Errorf("invalid secret provider reference")
	}
	return SecretRef{}, false, nil
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

func snapshotStringMap(config map[string]string, path string) (map[string]string, error) {
	if config == nil {
		return nil, nil
	}
	copy := make(map[string]string, len(config))
	for field, value := range config {
		if strings.TrimSpace(field) == "" || strings.TrimSpace(field) != field {
			return nil, fmt.Errorf("%s field names must be non-empty trimmed strings", path)
		}
		copy[field] = value
	}
	return copy, nil
}

func copyStringMap(config map[string]string) map[string]string {
	copy, _ := snapshotStringMap(config, "config")
	return copy
}

// ConfigSnapshot returns an owned copy. Environment reference strings remain
// unchanged and resolved plaintext is available only through driver.Secrets.
func (s DataSource) ConfigSnapshot() map[string]string {
	return copyStringMap(s.Config)
}

// SecretReferences returns every distinct external reference in Config, in
// deterministic order. Every Config snapshot retains the reference string;
// resolved values are exposed separately through driver.Secrets.
func SecretReferences(config map[string]string) ([]SecretRef, error) {
	set := make(map[SecretRef]struct{})
	for field, value := range config {
		reference, ok, err := ParseSecretRef(value)
		if err != nil {
			return nil, fmt.Errorf("config.%s: %w", field, err)
		}
		if !ok {
			continue
		}
		set[reference] = struct{}{}
	}
	references := make([]SecretRef, 0, len(set))
	for reference := range set {
		references = append(references, reference)
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].Provider == references[j].Provider {
			return references[i].Key < references[j].Key
		}
		return references[i].Provider < references[j].Provider
	})
	return references, nil
}

func copyDataSourcePolicy(policy DataSourcePolicy) DataSourcePolicy {
	copy := policy
	if policy.MaxRows != nil {
		maxRows := *policy.MaxRows
		copy.MaxRows = &maxRows
	}
	if policy.MaxBytes != nil {
		maxBytes := *policy.MaxBytes
		copy.MaxBytes = &maxBytes
	}
	if policy.MaxConcurrency != nil {
		maxConcurrency := *policy.MaxConcurrency
		copy.MaxConcurrency = &maxConcurrency
	}
	if policy.MaxQueue != nil {
		maxQueue := *policy.MaxQueue
		copy.MaxQueue = &maxQueue
	}
	return copy
}

func sortedDataSourceNames(sources map[string]DataSource) []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
