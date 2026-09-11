package builder

import (
	"sort"
	"strings"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// ResolveSharedGrain proves that independently planned metric candidates can be
// aligned at one semantic output grain before any physical staging strategy is
// chosen. Relationship paths and fanout safety are already semantic planning
// evidence at this boundary; ambiguity or unsafe traversal fails closed.
func ResolveSharedGrain(candidates []semanticplan.SharedGrainCandidate) (semanticplan.SharedGrainResolution, error) {
	if len(candidates) == 0 {
		return semanticplan.SharedGrainResolution{}, nil
	}

	owned := append([]semanticplan.SharedGrainCandidate(nil), candidates...)
	sort.SliceStable(owned, func(i, j int) bool { return owned[i].Metric < owned[j].Metric })

	resolution := semanticplan.SharedGrainResolution{Metrics: make([]semanticplan.MetricSharedGrainEvidence, 0, len(owned))}
	for i, candidate := range owned {
		roots := candidateRootDatasets(candidate)
		if candidate.Metric == "" || len(roots) == 0 {
			return semanticplan.SharedGrainResolution{}, &semanticplan.SharedGrainError{Failure: semanticplan.SharedGrainInvalidCandidate, Metric: candidate.Metric}
		}
		if candidate.PathAlternatives > 1 {
			return semanticplan.SharedGrainResolution{}, &semanticplan.SharedGrainError{
				Failure: semanticplan.SharedGrainAmbiguousPath,
				Metric:  candidate.Metric,
				Details: map[string]any{"path_alternatives": candidate.PathAlternatives},
			}
		}
		paths := candidateRelationshipPaths(candidate)
		if !candidate.FanoutSafe {
			return semanticplan.SharedGrainResolution{}, &semanticplan.SharedGrainError{
				Failure: semanticplan.SharedGrainFanoutUnsafe,
				Metric:  candidate.Metric,
				Details: map[string]any{
					"root_datasets":      append([]string(nil), roots...),
					"relationship_paths": cloneStringPaths(paths),
				},
			}
		}

		grainKey := canonicalGrainKey(candidate.OutputGrain)
		if i == 0 {
			resolution.Grain = cloneGroups(candidate.OutputGrain)
			resolution.GrainKey = grainKey
		} else if grainKey != resolution.GrainKey {
			return semanticplan.SharedGrainResolution{}, &semanticplan.SharedGrainError{
				Failure: semanticplan.SharedGrainIncompatible,
				Metric:  candidate.Metric,
				Details: map[string]any{"shared_grain": resolution.GrainKey, "metric_grain": grainKey},
			}
		}

		primaryRoot := candidate.RootDataset
		if primaryRoot == "" {
			primaryRoot = roots[0]
		}
		primaryPath := append([]string(nil), candidate.RelationshipPath...)
		if len(primaryPath) == 0 && len(paths) == 1 {
			primaryPath = append([]string(nil), paths[0]...)
		}
		resolution.Metrics = append(resolution.Metrics, semanticplan.MetricSharedGrainEvidence{
			Metric:            candidate.Metric,
			RootDataset:       primaryRoot,
			RootDatasets:      append([]string(nil), roots...),
			GrainKey:          grainKey,
			RelationshipPath:  primaryPath,
			RelationshipPaths: cloneStringPaths(paths),
			FanoutSafe:        true,
		})
	}
	return resolution, nil
}

func candidateRootDatasets(candidate semanticplan.SharedGrainCandidate) []string {
	roots := append([]string(nil), candidate.RootDatasets...)
	if candidate.RootDataset != "" {
		roots = append(roots, candidate.RootDataset)
	}
	return canonicalStrings(roots)
}

func candidateRelationshipPaths(candidate semanticplan.SharedGrainCandidate) [][]string {
	paths := cloneStringPaths(candidate.RelationshipPaths)
	if len(candidate.RelationshipPath) > 0 {
		paths = append(paths, append([]string(nil), candidate.RelationshipPath...))
	}
	seen := map[string]struct{}{}
	out := make([][]string, 0, len(paths))
	for _, path := range paths {
		key := stringPathKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, append([]string(nil), path...))
	}
	sort.Slice(out, func(i, j int) bool { return stringPathKey(out[i]) < stringPathKey(out[j]) })
	return out
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

func stringPathKey(path []string) string {
	return strings.Join(path, "\x1f")
}

func cloneStringPaths(paths [][]string) [][]string {
	out := make([][]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, append([]string(nil), path...))
	}
	return out
}

func canonicalGrainKey(groups []semanticplan.GroupBy) string {
	if len(groups) == 0 {
		return "scalar"
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		grain := ""
		if group.Grain != nil {
			grain = string(*group.Grain)
		}
		custom := ""
		if group.CustomCalendar != nil {
			custom = string(group.CustomCalendar.Grain)
		}
		parts = append(parts, strings.Join([]string{group.Dataset, group.Name, grain, custom}, "|"))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func cloneGroups(groups []semanticplan.GroupBy) []semanticplan.GroupBy {
	return append([]semanticplan.GroupBy(nil), groups...)
}
