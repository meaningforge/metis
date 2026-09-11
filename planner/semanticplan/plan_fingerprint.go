package semanticplan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Fingerprint returns a deterministic regression fingerprint for typed
// semantic-plan structure. It is an internal engineering contract, not a
// semantic identity, cache key, authorization token, or Agent-facing value.
func Fingerprint(plan *SemanticPlan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("semantic plan is required")
	}

	canonical := ClonePlan(plan)
	CanonicalizeFingerprintSets(canonical)

	hash := sha256.New()
	projection := &projector{w: hash}
	projectSemanticPlan(projection, canonical)
	// Keep the established projection byte-for-byte stable for ordinary plans.
	// Attribution identity is appended only when the typed DAG actually contains
	// an attribution operator.
	projectMetricAttributionNodeIdentity(projection, canonical.Nodes)
	if projection.err != nil {
		return "", fmt.Errorf("fingerprint semantic plan: %w", projection.err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
