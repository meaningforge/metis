package ossie

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MetricExtensionAgentDiscovery MetricExtensionKind = "agent_discovery"
	maxAgentDiscoveryAliases                          = 20
	maxAgentDiscoveryAliasRunes                       = 128
)

// AgentDiscoveryMetricSpec is incubating Agent-facing catalog metadata carried
// through Ossie's extension mechanism until Ossie defines an equivalent native
// field. It affects deterministic discovery only, never metric evaluation.
type AgentDiscoveryMetricSpec struct {
	Kind    MetricExtensionKind `json:"kind"`
	Aliases []string            `json:"aliases"`
}

func AgentDiscoverySpec(metric *Metric) (AgentDiscoveryMetricSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionAgentDiscovery)
	if err != nil || !ok {
		return AgentDiscoveryMetricSpec{}, ok, err
	}
	var spec AgentDiscoveryMetricSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return AgentDiscoveryMetricSpec{}, false, fmt.Errorf("decode agent-discovery metric extension: %w", err)
	}
	if err := ValidateAgentDiscoverySpec(spec); err != nil {
		return AgentDiscoveryMetricSpec{}, false, err
	}
	aliases := append([]string(nil), spec.Aliases...)
	for i := range aliases {
		aliases[i] = strings.TrimSpace(aliases[i])
	}
	sort.Slice(aliases, func(i, j int) bool { return strings.ToLower(aliases[i]) < strings.ToLower(aliases[j]) })
	spec.Aliases = aliases
	return spec, true, nil
}

func ValidateAgentDiscoverySpec(spec AgentDiscoveryMetricSpec) error {
	if spec.Kind != MetricExtensionAgentDiscovery {
		return fmt.Errorf("agent-discovery metric extension kind must be %q", MetricExtensionAgentDiscovery)
	}
	if len(spec.Aliases) == 0 || len(spec.Aliases) > maxAgentDiscoveryAliases {
		return fmt.Errorf("agent-discovery aliases must contain between 1 and %d values", maxAgentDiscoveryAliases)
	}
	seen := make(map[string]struct{}, len(spec.Aliases))
	for _, raw := range spec.Aliases {
		alias := strings.TrimSpace(raw)
		if alias == "" {
			return fmt.Errorf("agent-discovery aliases must not contain an empty value")
		}
		if utf8.RuneCountInString(alias) > maxAgentDiscoveryAliasRunes {
			return fmt.Errorf("agent-discovery alias exceeds %d characters", maxAgentDiscoveryAliasRunes)
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(alias), " "))
		if _, ok := seen[normalized]; ok {
			return fmt.Errorf("agent-discovery aliases contain duplicate value %q", alias)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}
