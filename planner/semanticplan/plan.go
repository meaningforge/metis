package semanticplan

// SemanticPlan is the deterministic output of query planning. Resolver has
// already selected Renderer-compatible expressions; Planner owns query
// structure and does not perform semantic lookup or dialect selection.
type SemanticPlan struct {
	PolicyScope string
	Model       ModelRef
	Root        DatasetRef
	Joins       []Join
	Projections []Projection
	Predicates  []Predicate
	Groups      []GroupBy
	Sorts       []Sort
	Limit       *int

	Requested []string
	Nodes     []SemanticPlanNode
	Output    SemanticOutputContract

	SharedGrain         *SharedGrainResolution
	DenseCalendar       *DenseCalendarPlan
	CustomDenseCalendar *CustomDenseCalendarPlan
	OptimizationTrace   []OptimizationStep
}
