package semanticplan

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

// RollupContract records whether a source metric's aggregated value can be
// re-aggregated at a coarser grain, and with which operator.
type RollupContract struct {
	Function  string
	Algebra   expression.RollupAlgebra
	Merge     string
	Mergeable bool
	Reason    string
}

// OffsetToGrainPlan is the engine-neutral boundary proof used by physical
// lowering. Custom calendars carry the declared bucket and ordinal relation.
type OffsetToGrainPlan struct {
	TimeDimension      string
	QueryGrain         query.TimeGrain
	BoundaryGrain      query.TimeGrain
	CustomCalendar     bool
	Dataset            DatasetRef
	BoundaryBucket     *ossie.Field
	BoundaryExpression string
	QueryOrdinal       *ossie.Field
	OrdinalExpression  string
}
