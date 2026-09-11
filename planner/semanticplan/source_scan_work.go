package semanticplan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrUnsharableSourceScan marks a node whose scan work a shared CTE could not
// reproduce. It is a refusal to describe, not a failure: fusion leaves such a
// node on its own and planning continues, while a node that has somehow been
// given a share group anyway is an invalid graph and says so.
var ErrUnsharableSourceScan = errors.New("source node scan work cannot be shared")

// SourceScanWork describes the rows a source-aggregate node reads. It is the
// semantic-plan owner's only definition of that, and making it the only one is
// the point of this file.
//
// Three places need to agree on what "the same scan work" means. Fusion decides
// which nodes may share a scan. Canonical graph validation checks that every
// node in a share group agrees. Lowering builds one CTE from the group's first
// node and hands it to all of them. Until now each had its own answer:
//
//   - fusion compared SourceRoots and RequiredDatasets in order, and compared
//     predicate Scope;
//   - identity compared them as sets, and dropped Scope;
//   - lowering read neither of the root lists, and dropped Scope.
//
// The three agreed on every graph the planner produces, which is exactly why the
// disagreement was invisible. It is unobservable for a reason worth writing
// down: every node-owned predicate the planner constructs is pre_aggregation
// with a non-nil Predicate, so the inputs that would separate the definitions do
// not currently exist. That is a property of today's construction sites, not of
// the contract -- node predicates accept four scopes, and the day a second one
// is constructed the three answers diverge, silently, in
// the direction of a shared CTE applying a predicate at the wrong point.
//
// So this refuses to describe a node it cannot describe faithfully rather than
// picking one of the three readings. A predicate the shared CTE would not place
// correctly fails closed here, where fusion, validation, and lowering all see it.
type SourceScanWork struct {
	Root             DatasetRef
	SourceRoots      []string
	RequiredDatasets []string
	Joins            []Join
	OutputGrain      []GroupBy
	Predicates       []Predicate
}

// SourceScanWorkOfNode derives the scan work a source aggregate can share.
func SourceScanWorkOfNode(node SemanticPlanNode) (SourceScanWork, error) {
	if node.Kind() != SemanticPlanNodeSourceAggregate {
		return SourceScanWork{}, fmt.Errorf("source scan work requires %q node, got %q", SemanticPlanNodeSourceAggregate, node.Kind())
	}
	base := node.NodeBase()
	source, ok := NodeSourceState(node)
	if !ok {
		return SourceScanWork{}, fmt.Errorf("source scan work requires source state on node %q", base.ID)
	}
	predicates := make([]Predicate, 0, len(base.Predicates))
	for _, predicate := range base.Predicates {
		if predicate.Scope != SemanticPredicatePreAggregation {
			return SourceScanWork{}, fmt.Errorf("%w: node %q owns a %q predicate and a shared scan places every predicate at the scan", ErrUnsharableSourceScan, base.ID, predicate.Scope)
		}
		if predicate.Predicate == nil {
			return SourceScanWork{}, fmt.Errorf("%w: node %q owns a pre_aggregation predicate with no scan representation", ErrUnsharableSourceScan, base.ID)
		}
		predicates = append(predicates, *predicate.Predicate)
	}
	sourceRoots := normalizeStringSet(append([]string(nil), source.SourceRoots...))
	requiredDatasets := normalizeStringSet(append([]string(nil), source.RequiredDatasets...))
	return SourceScanWork{
		Root:             source.Root,
		SourceRoots:      sourceRoots,
		RequiredDatasets: requiredDatasets,
		Joins:            source.Joins,
		OutputGrain:      base.OutputGrain,
		Predicates:       predicates,
	}, nil
}

// Identity returns the deterministic structural identity of source scan work.
func (work SourceScanWork) Identity() (string, error) {
	hash := sha256.New()
	p := &projector{w: hash}
	projectSourceScanWork(p, work)
	if p.err != nil {
		return "", fmt.Errorf("fingerprint source scan work: %w", p.err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// sharableSourceScanIdentity returns the identity fusion should group a node
// under, and whether the node can be grouped at all.
//
// Two nodes read the same rows exactly when this returns the same identity for
// both, which is the unification: the rule fusion applies and the rule canonical
// validation enforces are now the same encoding of the same description, so
// neither can begin accepting what the other rejects.
//
// A node whose scan work cannot be shared is not equivalent to anything,
// including an identical node. That is the case #402's owner-label fix did not
// reach: two nodes that both carry the same post-aggregation predicate
// compared equal, fused, and were lowered through a CTE that places every
// predicate at the scan -- so the filter moved to the wrong side of the
// aggregate. Refusing to group them is the fix; refusing to plan them is not,
// because an ungrouped node lowers correctly on its own.
// SharableSourceScanIdentityNode returns the identity fusion should group a
// node under and whether the node can be grouped at all.
func SharableSourceScanIdentityNode(node SemanticPlanNode) (string, bool, error) {
	work, err := SourceScanWorkOfNode(node)
	if err == nil {
		identity, identityErr := work.Identity()
		return identity, identityErr == nil, identityErr
	}
	if errors.Is(err, ErrUnsharableSourceScan) {
		return "", false, nil
	}
	return "", false, err
}
