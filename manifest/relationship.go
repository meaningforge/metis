package manifest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

var (
	relationshipPathSeparator     = string(rune(29))
	relationshipColumnSeparator   = string(rune(30))
	relationshipIdentitySeparator = string(rune(31))
)

type RelationshipGraph struct {
	adjacency map[string][]*ossie.Relationship
}

type DatasetPath struct {
	Dataset       string
	Relationships []*ossie.Relationship
}

type DatasetPathPlan struct {
	Root             string
	RequiredDatasets []string
	Paths            []DatasetPath
	Relationships    []*ossie.Relationship
}

func NewRelationshipGraph(relationships []*ossie.Relationship) *RelationshipGraph {
	g := &RelationshipGraph{adjacency: map[string][]*ossie.Relationship{}}
	seen := map[string]struct{}{}
	for _, rel := range relationships {
		if rel == nil {
			continue
		}
		key := relationshipIdentity(rel)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		g.adjacency[rel.From] = append(g.adjacency[rel.From], rel)
		g.adjacency[rel.To] = append(g.adjacency[rel.To], rel)
	}
	for dataset := range g.adjacency {
		sort.SliceStable(g.adjacency[dataset], func(i, j int) bool {
			return relationshipIdentity(g.adjacency[dataset][i]) < relationshipIdentity(g.adjacency[dataset][j])
		})
	}
	return g
}

func (g *RelationshipGraph) ShortestPath(from, to string) ([]*ossie.Relationship, error) {
	if from == to {
		return nil, nil
	}
	if g == nil {
		return nil, serrors.Internal("relationship graph is nil", map[string]any{"from": from, "to": to})
	}
	type state struct {
		node string
		path []*ossie.Relationship
	}
	queue := []state{{node: from}}
	bestDepth := -1
	var matches [][]*ossie.Relationship
	visitedDepth := map[string]int{from: 0}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		depth := len(cur.path)
		if bestDepth >= 0 && depth >= bestDepth {
			continue
		}
		for _, rel := range g.adjacency[cur.node] {
			next := rel.To
			if rel.To == cur.node {
				next = rel.From
			}
			nextPath := append(append([]*ossie.Relationship(nil), cur.path...), rel)
			if next == to {
				if bestDepth < 0 {
					bestDepth = len(nextPath)
				}
				if len(nextPath) == bestDepth {
					matches = append(matches, nextPath)
				}
				continue
			}
			if d, ok := visitedDepth[next]; ok && d < len(nextPath) {
				continue
			}
			visitedDepth[next] = len(nextPath)
			queue = append(queue, state{node: next, path: nextPath})
		}
	}
	if len(matches) == 0 {
		return nil, relationshipError(serrors.ErrRelationshipNotFound, "no relationship path between datasets", from, to)
	}
	if len(matches) > 1 {
		return nil, ambiguousRelationshipError(from, to, matches)
	}
	return matches[0], nil
}

func (g *RelationshipGraph) CanReachDirected(from, to string) bool {
	if from == to {
		return true
	}
	if g == nil {
		return false
	}
	visited := map[string]struct{}{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, relationship := range g.adjacency[current] {
			if relationship.From != current {
				continue
			}
			next := relationship.To
			if next == to {
				return true
			}
			if _, ok := visited[next]; ok {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return false
}

func (m *ModelIndex) ResolveDatasetPaths(root string, requiredDatasets []string) (*DatasetPathPlan, error) {
	if m == nil || m.Graph == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "relationship graph is not available"}
	}
	requiredSet := map[string]struct{}{root: {}}
	for _, dataset := range requiredDatasets {
		if dataset != "" {
			requiredSet[dataset] = struct{}{}
		}
	}
	required := make([]string, 0, len(requiredSet))
	for dataset := range requiredSet {
		required = append(required, dataset)
	}
	sort.Strings(required)
	plan := &DatasetPathPlan{Root: root, RequiredDatasets: required}
	relationships := map[string]*ossie.Relationship{}
	for _, dataset := range required {
		path, err := m.Graph.ShortestPath(root, dataset)
		if err != nil {
			return nil, err
		}
		plan.Paths = append(plan.Paths, DatasetPath{Dataset: dataset, Relationships: append([]*ossie.Relationship(nil), path...)})
		for _, relationship := range path {
			relationships[relationshipIdentity(relationship)] = relationship
		}
	}
	identities := make([]string, 0, len(relationships))
	for identity := range relationships {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		plan.Relationships = append(plan.Relationships, relationships[identity])
	}
	return plan, nil
}

func ambiguousRelationshipError(from, to string, matches [][]*ossie.Relationship) error {
	sort.SliceStable(matches, func(i, j int) bool {
		return relationshipPathIdentity(matches[i]) < relationshipPathIdentity(matches[j])
	})
	relationshipPaths := make([][]string, 0, len(matches))
	datasetPaths := make([][]string, 0, len(matches))
	for _, path := range matches {
		relationshipNames := make([]string, 0, len(path))
		datasets := []string{from}
		current := from
		for _, relationship := range path {
			relationshipNames = append(relationshipNames, relationshipLabel(relationship))
			next := relationship.To
			if relationship.To == current {
				next = relationship.From
			}
			datasets = append(datasets, next)
			current = next
		}
		relationshipPaths = append(relationshipPaths, relationshipNames)
		datasetPaths = append(datasetPaths, datasets)
	}
	return &serrors.Error{Code: serrors.ErrAmbiguousRelationshipPath, Message: "multiple shortest relationship paths between datasets", Details: map[string]any{"from": from, "to": to, "relationship_paths": relationshipPaths, "dataset_paths": datasetPaths}}
}

func relationshipIdentity(rel *ossie.Relationship) string {
	if rel == nil {
		return ""
	}
	return strings.Join([]string{rel.Name, rel.From, rel.To, strings.Join(rel.FromColumns, relationshipColumnSeparator), strings.Join(rel.ToColumns, relationshipColumnSeparator)}, relationshipIdentitySeparator)
}

func relationshipPathIdentity(path []*ossie.Relationship) string {
	parts := make([]string, 0, len(path))
	for _, relationship := range path {
		parts = append(parts, relationshipIdentity(relationship))
	}
	return strings.Join(parts, relationshipPathSeparator)
}

func relationshipLabel(rel *ossie.Relationship) string {
	if rel.Name != "" {
		return rel.Name
	}
	return rel.From + "->" + rel.To
}

func relationshipError(code serrors.ErrorCode, message, from, to string) error {
	return &serrors.Error{Code: code, Message: message, Details: map[string]any{"from": from, "to": to}}
}

func (g *RelationshipGraph) String() string {
	return fmt.Sprintf("RelationshipGraph{%d nodes}", len(g.adjacency))
}
