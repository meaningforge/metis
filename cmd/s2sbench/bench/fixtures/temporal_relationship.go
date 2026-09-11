package fixtures

import _ "embed"

const (
	TemporalRelationship      ID = "temporal_relationship"
	TemporalRelationshipModel    = "temporal_relationship"
)

// TemporalRelationshipModelYAML isolates point-in-time SCD relationship
// correctness from the general commerce fixture.
//
//go:embed temporal_relationship.ossie.yaml
var TemporalRelationshipModelYAML []byte

func init() {
	definitions[TemporalRelationship] = Definition{
		ID:       TemporalRelationship,
		Project:  ConformanceProject,
		Model:    TemporalRelationshipModel,
		Document: TemporalRelationshipModelYAML,
	}
}
