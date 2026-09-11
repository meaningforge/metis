package semanticplan

import "fmt"

type SharedGrainFailure string

const (
	SharedGrainIncompatible     SharedGrainFailure = "INCOMPATIBLE_GRAIN"
	SharedGrainAmbiguousPath    SharedGrainFailure = "AMBIGUOUS_RELATIONSHIP_PATH"
	SharedGrainFanoutUnsafe     SharedGrainFailure = "FANOUT_UNSAFE"
	SharedGrainInvalidCandidate SharedGrainFailure = "INVALID_CANDIDATE"
)

type SharedGrainError struct {
	Failure SharedGrainFailure
	Metric  string
	Details map[string]any
}

func (e *SharedGrainError) Error() string {
	if e == nil {
		return ""
	}
	if e.Metric == "" {
		return string(e.Failure)
	}
	return fmt.Sprintf("%s: metric %q", e.Failure, e.Metric)
}

type SharedGrainCandidate struct {
	Metric            string
	RootDataset       string
	RootDatasets      []string
	OutputGrain       []GroupBy
	RelationshipPath  []string
	RelationshipPaths [][]string
	PathAlternatives  int
	FanoutSafe        bool
}

type MetricSharedGrainEvidence struct {
	Metric            string
	RootDataset       string
	RootDatasets      []string
	GrainKey          string
	RelationshipPath  []string
	RelationshipPaths [][]string
	FanoutSafe        bool
}

type SharedGrainResolution struct {
	Grain    []GroupBy
	GrainKey string
	Metrics  []MetricSharedGrainEvidence
}

func CloneMetricSharedGrainEvidence(in MetricSharedGrainEvidence) MetricSharedGrainEvidence {
	out := in
	out.RootDatasets = append([]string(nil), in.RootDatasets...)
	out.RelationshipPath = append([]string(nil), in.RelationshipPath...)
	out.RelationshipPaths = make([][]string, 0, len(in.RelationshipPaths))
	for _, path := range in.RelationshipPaths {
		out.RelationshipPaths = append(out.RelationshipPaths, append([]string(nil), path...))
	}
	return out
}
