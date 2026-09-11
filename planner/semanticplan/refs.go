package semanticplan

// DatasetRef identifies a semantic dataset and its engine-neutral source.
type DatasetRef struct {
	Name   string
	Source string
	Policy *RelationPolicy
}
