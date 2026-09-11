package semanticplan

import "github.com/meaningforge/metis/ossie"

const ConversionAssignmentNearestPrecedingBase = "nearest_preceding_base"

type ConversionFieldRef struct{ Dataset, Name string }
type ConversionPropertyPlan struct{ Base, Conversion ConversionFieldRef }
type ConversionCandidateOrder struct {
	Field     ConversionFieldRef
	Direction string
}
type ConversionCandidateMatchPlan struct {
	PartitionBy                []ConversionFieldRef
	Equality                   []ConversionPropertyPlan
	BaseTime                   ConversionFieldRef
	ConversionTime             ConversionFieldRef
	Window                     *ossie.ConversionWindow
	OrderBy                    []ConversionCandidateOrder
	KeepRank                   int
	AggregateBaseIndependently bool
}
type ConversionPlan struct {
	BaseMetric, ConversionMetric, BaseRoot, ConversionRoot string
	BaseEventKey, ConversionEventKey                       []ConversionFieldRef
	BaseTime, ConversionTime                               ConversionFieldRef
	Entity                                                 ConversionPropertyPlan
	ConstantProperties                                     []ConversionPropertyPlan
	Calculation                                            string
	Window                                                 *ossie.ConversionWindow
	Assignment                                             string
	CandidateMatch                                         ConversionCandidateMatchPlan
}
type ConversionPhysicalFieldRef struct{ Dataset, Name, Expression string }
type ConversionPhysicalPropertyPlan struct{ Base, Conversion ConversionPhysicalFieldRef }
type ConversionEventValueKind string

const (
	ConversionEventValueSumField  ConversionEventValueKind = "sum_field"
	ConversionEventValueCountRows ConversionEventValueKind = "count_rows"
)

type ConversionEventValuePlan struct {
	Metric, Dataset string
	Kind            ConversionEventValueKind
	Field           *ConversionPhysicalFieldRef
}
type ConversionPhysicalInputPlan struct {
	BaseDataset, ConversionDataset   DatasetRef
	BaseEventKey, ConversionEventKey []ConversionPhysicalFieldRef
	BaseTime, ConversionTime         ConversionPhysicalFieldRef
	Entity                           ConversionPhysicalPropertyPlan
	ConstantProperties               []ConversionPhysicalPropertyPlan
	BaseValue, ConversionValue       ConversionEventValuePlan
}
