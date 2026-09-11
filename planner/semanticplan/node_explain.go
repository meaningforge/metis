package semanticplan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/meaningforge/metis/extension"
)

// SemanticPlanExplanation is the bounded, deterministic Agent-facing
// projection of the canonical semantic planning IR. It contains semantic and
// logical evidence only; backend runtime, physical-plan metadata, and
// optimizer-only annotations are outside this contract.
type SemanticPlanExplanation struct {
	Requested   []string                      `json:"requested,omitempty"`
	OutputGrain []SemanticGrainEvidence       `json:"output_grain,omitempty"`
	Nodes       []SemanticPlanNodeExplanation `json:"nodes"`
	Lineage     []SemanticOutputLineage       `json:"lineage,omitempty"`
	Predicates  []SemanticPredicateEvidence   `json:"final_output_predicates,omitempty"`
}

type SemanticPlanNodeExplanation struct {
	ID                        string                              `json:"id"`
	Kind                      SemanticPlanNodeKind                `json:"kind"`
	Boundary                  SemanticPlanNodeBoundary            `json:"boundary"`
	Inputs                    []string                            `json:"inputs,omitempty"`
	Metrics                   []string                            `json:"metrics,omitempty"`
	Dimensions                []string                            `json:"dimensions,omitempty"`
	OutputGrain               []SemanticGrainEvidence             `json:"output_grain,omitempty"`
	SourceDatasets            []string                            `json:"source_datasets,omitempty"`
	RelationshipPaths         [][]string                          `json:"relationship_paths,omitempty"`
	FanoutSafe                *bool                               `json:"fanout_safe,omitempty"`
	PredicateBoundaryEvidence SemanticPredicateBoundaryEvidence   `json:"predicate_boundary"`
	Predicates                []SemanticPredicateEvidence         `json:"predicates,omitempty"`
	ExtensionEvidence         []extension.Evidence                `json:"extension_evidence,omitempty"`
	PopulationPreservation    []PopulationPreservationExplanation `json:"population_preservation,omitempty"`
	Attribution               *SemanticAttributionExplanation     `json:"attribution,omitempty"`
}

type SemanticAttributionExplanation struct {
	MetricRef            string                               `json:"metric_ref"`
	NumeratorRef         string                               `json:"numerator_ref,omitempty"`
	DenominatorRef       string                               `json:"denominator_ref,omitempty"`
	TimeDimensionRef     string                               `json:"time_dimension_ref"`
	DimensionRef         string                               `json:"dimension_ref"`
	Baseline             MetricAttributionRangeEvidence       `json:"baseline"`
	Current              MetricAttributionRangeEvidence       `json:"current"`
	Decomposition        string                               `json:"decomposition_kind"`
	Strategy             MetricAttributionStrategy            `json:"strategy"`
	Reconciliation       MetricAttributionReconciliation      `json:"reconciliation"`
	UndefinedRatioPolicy RatioAttributionUndefinedRatioPolicy `json:"undefined_ratio_policy,omitempty"`
	PopulationAlignment  RatioAttributionPopulationAlignment  `json:"population_alignment,omitempty"`
}

type MetricAttributionRangeEvidence struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type PopulationPreservationExplanation struct {
	Relationship string                                     `json:"relationship"`
	FromDataset  string                                     `json:"from_dataset"`
	ToDataset    string                                     `json:"to_dataset"`
	Multiplicity RelationshipMultiplicity                   `json:"multiplicity"`
	Preserved    bool                                       `json:"preserved"`
	Obligations  []PopulationPreservationObligationEvidence `json:"obligations"`
	Admission    *FanoutAdmissionEvidence                   `json:"fanout_admission,omitempty"`
}

type SemanticGrainEvidence struct {
	Name                string `json:"name"`
	Dataset             string `json:"dataset,omitempty"`
	Grain               string `json:"grain,omitempty"`
	CustomCalendarGrain string `json:"custom_calendar_grain,omitempty"`
}

type SemanticOutputKind string

const (
	SemanticOutputMetric    SemanticOutputKind = "metric"
	SemanticOutputDimension SemanticOutputKind = "dimension"
)

type SemanticOutputLineage struct {
	Kind           SemanticOutputKind `json:"kind"`
	Name           string             `json:"name"`
	NodeIDs        []string           `json:"node_ids"`
	SourceDatasets []string           `json:"source_datasets,omitempty"`
}

type SemanticPredicateEvidence struct {
	Scope       SemanticPredicateScope          `json:"scope"`
	OwnerNodeID string                          `json:"owner_node_id,omitempty"`
	Proof       SemanticPredicatePlacementProof `json:"proof"`
	Subject     string                          `json:"subject,omitempty"`
}

// Explain returns the bounded, deterministic Agent-facing projection of a
// validated plan-owned typed-node DAG.
func Explain(plan *SemanticPlan) (SemanticPlanExplanation, error) {
	if plan == nil {
		return SemanticPlanExplanation{}, fmt.Errorf("semantic plan is required")
	}
	if len(plan.Nodes) == 0 {
		return SemanticPlanExplanation{}, fmt.Errorf("semantic plan owns no nodes to explain")
	}
	if err := ValidateDAGContract(plan); err != nil {
		return SemanticPlanExplanation{}, fmt.Errorf("explain semantic graph: %w", err)
	}
	explanation := SemanticPlanExplanation{
		Requested:   append([]string(nil), plan.Requested...),
		OutputGrain: explainSemanticGrain(plan.Output.Grain),
	}
	byID := NodesByID(plan.Nodes)
	nodeOrder := make(map[string]int, len(plan.Nodes))
	for i, node := range plan.Nodes {
		id := node.NodeBase().ID
		nodeOrder[id] = i
		explanation.Nodes = append(explanation.Nodes, explainSemanticPlanNode(node))
	}
	for _, predicate := range plan.Output.Predicates {
		explanation.Predicates = append(explanation.Predicates, explainFinalOutputPredicate(predicate))
	}

	metrics := map[string][]string{}
	dimensions := map[string][]string{}
	for _, node := range plan.Nodes {
		base := node.NodeBase()
		metricState, _ := NodeMetricState(node)
		for _, metric := range metricState.Metrics {
			metrics[metric] = append(metrics[metric], base.ID)
		}
		for _, dimension := range base.Dimensions {
			dimensions[dimension] = append(dimensions[dimension], base.ID)
		}
	}
	explanation.Lineage = append(explanation.Lineage, buildSemanticOutputLineage(SemanticOutputMetric, metrics, byID, nodeOrder)...)
	explanation.Lineage = append(explanation.Lineage, buildSemanticOutputLineage(SemanticOutputDimension, dimensions, byID, nodeOrder)...)
	return explanation, nil
}

func explainSemanticPlanNode(node SemanticPlanNode) SemanticPlanNodeExplanation {
	base := node.NodeBase()
	source, _ := NodeSourceState(node)
	metric, _ := NodeMetricState(node)
	explanation := SemanticPlanNodeExplanation{
		ID:                        base.ID,
		Kind:                      node.Kind(),
		Boundary:                  base.Boundary,
		Metrics:                   append([]string(nil), metric.Metrics...),
		Dimensions:                append([]string(nil), base.Dimensions...),
		OutputGrain:               explainSemanticGrain(base.OutputGrain),
		SourceDatasets:            append([]string(nil), source.RequiredDatasets...),
		PredicateBoundaryEvidence: base.PredicateBoundaryEvidence,
		ExtensionEvidence:         append([]extension.Evidence(nil), base.ExtensionEvidence...),
	}
	if attribution, ok := node.(AdditiveAttributionNode); ok {
		explanation.Attribution = explainAdditiveAttributionNode(attribution)
	}
	if attribution, ok := node.(RatioAttributionNode); ok {
		explanation.Attribution = explainRatioAttributionNode(attribution)
	}
	for _, evidence := range source.PopulationPreservationEvidence {
		explanation.PopulationPreservation = append(explanation.PopulationPreservation, PopulationPreservationExplanation{
			Relationship: evidence.Relationship,
			FromDataset:  evidence.FromDataset,
			ToDataset:    evidence.ToDataset,
			Multiplicity: evidence.Multiplicity,
			Preserved:    evidence.Preserved(),
			Obligations:  append([]PopulationPreservationObligationEvidence(nil), evidence.Obligations...),
		})
		if evidence.Admission != nil {
			admission := *evidence.Admission
			explanation.PopulationPreservation[len(explanation.PopulationPreservation)-1].Admission = &admission
		}
	}
	for _, input := range base.Inputs {
		explanation.Inputs = append(explanation.Inputs, input.NodeID)
	}
	paths := make([][]string, 0)
	joinPath := make([]string, 0, len(source.Joins))
	for _, join := range source.Joins {
		if join.Relationship != nil && join.Relationship.Name != "" {
			joinPath = append(joinPath, join.Relationship.Name)
		}
	}
	if len(joinPath) > 0 {
		paths = append(paths, joinPath)
	}
	if metric.SharedGrainEvidence != nil {
		paths = append(paths, metric.SharedGrainEvidence.RelationshipPaths...)
		if len(metric.SharedGrainEvidence.RelationshipPath) > 0 {
			paths = append(paths, metric.SharedGrainEvidence.RelationshipPath)
		}
		proof := metric.SharedGrainEvidence.FanoutSafe
		explanation.FanoutSafe = &proof
	}
	explanation.RelationshipPaths = canonicalRelationshipPaths(paths)
	for _, predicate := range base.Predicates {
		explanation.Predicates = append(explanation.Predicates, explainSemanticPlanNodePredicate(predicate))
	}
	return explanation
}

func explainRatioAttributionNode(node RatioAttributionNode) *SemanticAttributionExplanation {
	return &SemanticAttributionExplanation{
		MetricRef:        node.MetricRef,
		NumeratorRef:     node.NumeratorRef,
		DenominatorRef:   node.DenominatorRef,
		TimeDimensionRef: node.TimeDimensionRef,
		DimensionRef:     node.DimensionRef,
		Baseline: MetricAttributionRangeEvidence{
			Start: canonicalAttributionInstant(node.Baseline.Start),
			End:   canonicalAttributionInstant(node.Baseline.End),
		},
		Current: MetricAttributionRangeEvidence{
			Start: canonicalAttributionInstant(node.Current.Start),
			End:   canonicalAttributionInstant(node.Current.End),
		},
		Decomposition:        "ratio",
		Strategy:             MetricAttributionRatioMixRate,
		Reconciliation:       MetricAttributionReconcileMixRate,
		UndefinedRatioPolicy: RatioAttributionUndefinedNullWithDefinedFlag,
		PopulationAlignment:  RatioAttributionFullUnionEntryExit,
	}
}

func explainAdditiveAttributionNode(node AdditiveAttributionNode) *SemanticAttributionExplanation {
	return &SemanticAttributionExplanation{
		MetricRef:        node.MetricRef,
		TimeDimensionRef: node.TimeDimensionRef,
		DimensionRef:     node.DimensionRef,
		Baseline: MetricAttributionRangeEvidence{
			Start: canonicalAttributionInstant(node.Baseline.Start),
			End:   canonicalAttributionInstant(node.Baseline.End),
		},
		Current: MetricAttributionRangeEvidence{
			Start: canonicalAttributionInstant(node.Current.Start),
			End:   canonicalAttributionInstant(node.Current.End),
		},
		Decomposition:  "additive",
		Strategy:       MetricAttributionAdditiveContribution,
		Reconciliation: MetricAttributionReconcileSegmentDelta,
	}
}

func explainSemanticGrain(groups []GroupBy) []SemanticGrainEvidence {
	out := make([]SemanticGrainEvidence, 0, len(groups))
	for _, group := range groups {
		evidence := SemanticGrainEvidence{Name: group.Name, Dataset: group.Dataset}
		if group.Grain != nil {
			evidence.Grain = string(*group.Grain)
		}
		if group.CustomCalendar != nil {
			evidence.CustomCalendarGrain = string(group.CustomCalendar.Grain)
		}
		out = append(out, evidence)
	}
	return out
}

// explainFinalOutputPredicate describes a predicate in the plan's output
// contract. Its scope and proof are fixed by that position, which is why the
// contract stores the predicate itself rather than a wrapper repeating them.
func explainFinalOutputPredicate(predicate PostEvaluationPredicate) SemanticPredicateEvidence {
	return SemanticPredicateEvidence{
		Scope:   SemanticPredicateFinalOutput,
		Proof:   SemanticPredicateProofOutputSemantics,
		Subject: predicate.Name,
	}
}

func explainSemanticPlanNodePredicate(predicate SemanticPlanNodePredicate) SemanticPredicateEvidence {
	evidence := SemanticPredicateEvidence{
		Scope:       predicate.Scope,
		OwnerNodeID: predicate.OwnerNodeID,
		Proof:       predicate.Proof,
	}
	if predicate.Predicate != nil {
		evidence.Subject = predicate.Predicate.Filter.Field
	}
	if predicate.Post != nil {
		evidence.Subject = predicate.Post.Name
	}
	return evidence
}

func buildSemanticOutputLineage(kind SemanticOutputKind, owners map[string][]string, byID map[string]SemanticPlanNode, nodeOrder map[string]int) []SemanticOutputLineage {
	names := make([]string, 0, len(owners))
	for name := range owners {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	out := make([]SemanticOutputLineage, 0, len(names))
	for _, name := range names {
		visited := map[string]struct{}{}
		for _, owner := range owners[name] {
			collectSemanticLineageNodes(owner, byID, visited)
		}
		nodeIDs := make([]string, 0, len(visited))
		datasets := []string{}
		for nodeID := range visited {
			nodeIDs = append(nodeIDs, nodeID)
			source, _ := NodeSourceState(byID[nodeID])
			datasets = append(datasets, source.RequiredDatasets...)
		}
		sort.Slice(nodeIDs, func(i, j int) bool { return nodeOrder[nodeIDs[i]] < nodeOrder[nodeIDs[j]] })
		out = append(out, SemanticOutputLineage{
			Kind:           kind,
			Name:           name,
			NodeIDs:        nodeIDs,
			SourceDatasets: canonicalStrings(datasets),
		})
	}
	return out
}

func canonicalAttributionInstant(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func canonicalRelationshipPaths(paths [][]string) [][]string {
	seen := map[string]struct{}{}
	out := make([][]string, 0, len(paths))
	for _, path := range paths {
		key := strings.Join(path, "\x1f")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, append([]string(nil), path...))
	}
	sort.Slice(out, func(i, j int) bool { return strings.Join(out[i], "\x1f") < strings.Join(out[j], "\x1f") })
	return out
}

func collectSemanticLineageNodes(nodeID string, byID map[string]SemanticPlanNode, visited map[string]struct{}) {
	if _, ok := visited[nodeID]; ok {
		return
	}
	node, ok := byID[nodeID]
	if !ok {
		return
	}
	visited[nodeID] = struct{}{}
	for _, input := range node.NodeBase().Inputs {
		collectSemanticLineageNodes(input.NodeID, byID, visited)
	}
}
