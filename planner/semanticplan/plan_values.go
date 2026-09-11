package semanticplan

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

type DenseCalendarPlan struct {
	Dataset            DatasetRef
	TimeField          *ossie.Field
	TimeExpression     string
	QueryTimeDimension string
	Grain              query.TimeGrain
	OutputPredicates   []Predicate
	ReadPredicates     []Predicate
}

// CustomDenseCalendarPlan describes the model-defined logical-period domain
// used by custom time-relative evaluation. Physical densification must enumerate
// the declared bucket/ordinal relation rather than infer periods from fact rows
// or built-in Gregorian grain functions.
type CustomDenseCalendarPlan struct {
	Dataset            DatasetRef
	QueryTimeDimension string
	Grain              query.TimeGrain
	BucketField        *ossie.Field
	BucketExpression   string
	OrdinalField       *ossie.Field
	OrdinalExpression  string
}

type CustomCalendarLevel struct {
	Grain             ossie.CustomCalendarGrain
	BucketField       *ossie.Field
	BucketExpression  string
	OrdinalField      *ossie.Field
	OrdinalExpression string
}

type CustomCalendarGrouping struct {
	Spec               ossie.CustomCalendarSpec
	Grain              query.TimeGrain
	Dataset            string
	DatasetSource      string
	BaseTimeField      *ossie.Field
	BaseTimeExpression string
	BucketField        *ossie.Field
	BucketExpression   string
	OrdinalField       *ossie.Field
	OrdinalExpression  string
	Levels             map[string]CustomCalendarLevel
}

// CustomCalendarOffsetPlan is the engine-neutral mapping required to move a
// custom-calendar time offset by logical periods. Physical lowering must move
// OrdinalExpression by Count and recover BucketExpression from Dataset; it must
// not infer adjacency from fact-row presence or a dialect date function.
type CustomCalendarOffsetPlan struct {
	Grain             query.TimeGrain
	Count             int
	Dataset           DatasetRef
	BucketField       *ossie.Field
	BucketExpression  string
	OrdinalField      *ossie.Field
	OrdinalExpression string
}

// CustomCalendarCumulativePlan describes a bounded rolling cumulative window
// over consecutive custom-calendar ordinals. Count includes the current period;
// physical lowering must evaluate [ordinal-(Count-1), ordinal] over the declared
// dense logical-period domain rather than Gregorian date arithmetic.
type CustomCalendarCumulativePlan struct {
	Grain             query.TimeGrain
	Count             int
	Dataset           DatasetRef
	BucketField       *ossie.Field
	BucketExpression  string
	OrdinalField      *ossie.Field
	OrdinalExpression string
}

// CustomCalendarGrainToDatePlan describes a cumulative reset boundary proven by
// the model-declared custom-calendar hierarchy. Physical lowering partitions by
// ResetBucketExpression and orders within that partition using the dense query
// grain ordinal; it must not derive reset membership from Gregorian arithmetic.
type CustomCalendarGrainToDatePlan struct {
	QueryGrain             query.TimeGrain
	ResetGrain             query.TimeGrain
	Dataset                DatasetRef
	ResetBucketField       *ossie.Field
	ResetBucketExpression  string
	QueryOrdinalField      *ossie.Field
	QueryOrdinalExpression string
}

type ModelRef struct {
	Project string
	Name    string
}
type Join struct {
	Policy       *RelationPolicy
	Relationship *ossie.Relationship
	Temporal     *ossie.TemporalRelationshipSpec
	FromDataset  string
	FromSource   string
	ToDataset    string
	ToSource     string
}
type ProjectionKind string

const (
	ProjectionMetric    ProjectionKind = "metric"
	ProjectionDimension ProjectionKind = "dimension"
)

type Projection struct {
	Name           string
	Kind           ProjectionKind
	Metric         *ossie.Metric
	Field          *ossie.Field
	Dataset        string
	Datasets       []string
	Grain          *query.TimeGrain
	Expression     expression.ResolvedExpression
	CustomCalendar *CustomCalendarGrouping
}
type Predicate struct {
	Filter     query.Filter
	Dataset    string
	Field      *ossie.Field
	Expression expression.ResolvedExpression
}
type GroupBy struct {
	Name           string
	Dataset        string
	Field          *ossie.Field
	Grain          *query.TimeGrain
	Expression     expression.ResolvedExpression
	CustomCalendar *CustomCalendarGrouping
}
type SortTargetKind string

const (
	SortMetric    SortTargetKind = "metric"
	SortDimension SortTargetKind = "dimension"
)

type Sort struct {
	Name       string
	Kind       SortTargetKind
	Direction  query.SortDirection
	Metric     *ossie.Metric
	Field      *ossie.Field
	Dataset    string
	Expression expression.ResolvedExpression
}
