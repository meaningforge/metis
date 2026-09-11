package builder

import "github.com/meaningforge/metis/planner/semanticplan"

type (
	CustomCalendarGrouping       = semanticplan.CustomCalendarGrouping
	CustomCalendarLevel          = semanticplan.CustomCalendarLevel
	CustomCalendarOffsetPlan     = semanticplan.CustomCalendarOffsetPlan
	CustomCalendarCumulativePlan = semanticplan.CustomCalendarCumulativePlan
	CumulativeWindowNode         = semanticplan.CumulativeWindowNode
	DatasetRef                   = semanticplan.DatasetRef
	GroupBy                      = semanticplan.GroupBy
	OffsetToGrainNode            = semanticplan.OffsetToGrainNode
	OffsetToGrainPlan            = semanticplan.OffsetToGrainPlan
	SemanticPlanNode             = semanticplan.SemanticPlanNode
	SemanticPlanNodeBase         = semanticplan.SemanticPlanNodeBase
	SourceAggregateNode          = semanticplan.SourceAggregateNode
	TimeOffsetNode               = semanticplan.TimeOffsetNode
)
