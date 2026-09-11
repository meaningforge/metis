package semanticplan

import "sort"

// CanonicalizeFingerprintSets normalizes set-valued semantic-plan fields in
// place for deterministic structural observation. It intentionally leaves
// sequence-valued fields untouched.
func CanonicalizeFingerprintSets(plan *SemanticPlan) {
	if plan == nil {
		return
	}
	for i := range plan.Projections {
		plan.Projections[i].Datasets = normalizeStringSet(plan.Projections[i].Datasets)
	}
	canonicalizeNodeSets(plan.Nodes)
}

func canonicalizeNodeSets(nodes []SemanticPlanNode) {
	for i, node := range nodes {
		if node == nil {
			continue
		}
		base := node.NodeBase()
		base.Dimensions = canonicalStrings(base.Dimensions)
		updated, err := WithNodeBase(node, base)
		if err != nil {
			continue
		}
		if source, ok := NodeSourceState(updated); ok {
			source.SourceRoots = canonicalStrings(source.SourceRoots)
			source.RequiredDatasets = canonicalStrings(source.RequiredDatasets)
			updated, err = WithNodeSourceState(updated, source)
			if err != nil {
				continue
			}
		}
		if metric, ok := NodeMetricState(updated); ok {
			metric.Metrics = canonicalStrings(metric.Metrics)
			updated, err = WithNodeMetricState(updated, metric)
			if err != nil {
				continue
			}
		}
		nodes[i] = updated
	}
}

func normalizeStringSet(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func canonicalStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
