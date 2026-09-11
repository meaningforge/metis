package conversion

import (
	"fmt"
	"strings"
	"time"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

const (
	additiveAttributionBaselineBlockID sqlplan.QueryBlockID = "attribution_baseline"
	additiveAttributionCurrentBlockID  sqlplan.QueryBlockID = "attribution_current"
	additiveAttributionAlignedBlockID  sqlplan.QueryBlockID = "attribution_aligned"
	additiveAttributionTotalBlockID    sqlplan.QueryBlockID = "attribution_total"
	additiveAttributionRootBlockID     sqlplan.QueryBlockID = "root"

	additiveAttributionBaselineAlias = "__metis_attribution_baseline"
	additiveAttributionCurrentAlias  = "__metis_attribution_current"
	additiveAttributionAlignedAlias  = "__metis_attribution_aligned"
	additiveAttributionTotalAlias    = "__metis_attribution_total"

	additiveAttributionBaselineValueAlias = "baseline_value"
	additiveAttributionCurrentValueAlias  = "current_value"
	additiveAttributionDeltaAlias         = "delta"
	additiveAttributionTotalDeltaAlias    = "total_delta"
	additiveAttributionContributionAlias  = "contribution_pct"
)

// PlanOwnedAdditiveAttributionSQLPlan lowers the additive attribution
// semantic operator into a deterministic five-block physical plan. The
// lowering consumes only evidence already carried by semanticplan.SemanticPlan. It does not
// consult the manifest or infer new relationships.
func PlanOwnedAdditiveAttributionSQLPlan(plan *semanticplan.SemanticPlan, expressionDialect string) (*sqlplan.Plan, error) {
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, err
	}

	attribution, producer, err := additiveAttributionPhysicalInputs(plan)
	if err != nil {
		return nil, err
	}
	if err := validateAdditiveAttributionPhysicalSource(attribution, producer); err != nil {
		return nil, err
	}

	baseline, err := additiveAttributionPeriodBlock(plan, producer, attribution, attribution.Baseline, additiveAttributionBaselineBlockID, expressionDialect)
	if err != nil {
		return nil, err
	}
	current, err := additiveAttributionPeriodBlock(plan, producer, attribution, attribution.Current, additiveAttributionCurrentBlockID, expressionDialect)
	if err != nil {
		return nil, err
	}

	aligned := additiveAttributionAlignedBlock(attribution, producer)
	total := additiveAttributionTotalBlock()
	root := additiveAttributionResultBlock(attribution)

	physical := &sqlplan.Plan{
		Root:   additiveAttributionRootBlockID,
		Blocks: []sqlplan.QueryBlock{baseline, current, aligned, total, root},
	}
	if err := sqlplan.ValidateForRenderer(physical, expressionDialect); err != nil {
		return nil, fmt.Errorf("additive attribution SQLPlan is invalid: %w", err)
	}
	return sqlplan.Clone(physical), nil
}

func additiveAttributionPhysicalInputs(plan *semanticplan.SemanticPlan) (semanticplan.AdditiveAttributionNode, semanticplan.SourceAggregateNode, error) {
	var attribution *semanticplan.AdditiveAttributionNode
	for _, node := range plan.Nodes {
		candidate, ok := node.(semanticplan.AdditiveAttributionNode)
		if !ok {
			continue
		}
		if attribution != nil {
			return semanticplan.AdditiveAttributionNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("additive attribution lowering supports exactly one attribution node")
		}
		copy := candidate
		attribution = &copy
	}
	if attribution == nil {
		return semanticplan.AdditiveAttributionNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("additive attribution lowering requires one attribution node")
	}
	if len(attribution.Base.Inputs) != 1 {
		return semanticplan.AdditiveAttributionNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("additive attribution lowering requires exactly one semantic input")
	}

	producerID := attribution.Base.Inputs[0].NodeID
	var producer *semanticplan.SourceAggregateNode
	for _, node := range plan.Nodes {
		if node.NodeBase().ID != producerID {
			continue
		}
		candidate, ok := node.(semanticplan.SourceAggregateNode)
		if !ok {
			return semanticplan.AdditiveAttributionNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("additive attribution input %q must be a source aggregate during additive-attribution lowering", producerID)
		}
		copy := candidate
		producer = &copy
		break
	}
	if producer == nil {
		return semanticplan.AdditiveAttributionNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("additive attribution input %q is missing", producerID)
	}
	return *attribution, *producer, nil
}

func validateAdditiveAttributionPhysicalSource(attribution semanticplan.AdditiveAttributionNode, producer semanticplan.SourceAggregateNode) error {
	if producer.Base.ID != attribution.MetricRef {
		return fmt.Errorf("additive attribution producer %q does not match metric %q", producer.Base.ID, attribution.MetricRef)
	}
	if producer.Metric == nil {
		return fmt.Errorf("additive attribution metric %q has no governed metric evidence", attribution.MetricRef)
	}
	zeroFill, err := metricUsesZeroFill(producer.Metric)
	if err != nil {
		return fmt.Errorf("resolve additive attribution fill policy for %q: %w", attribution.MetricRef, err)
	}
	if !zeroFill {
		return fmt.Errorf("additive attribution metric %q cannot preserve the union population because its fill policy does not admit additive identity zero", attribution.MetricRef)
	}
	if len(producer.Base.OutputGrain) != 1 || producer.Base.OutputGrain[0].Name != attribution.DimensionRef {
		return fmt.Errorf("additive attribution producer grain must be exactly decomposition dimension %q", attribution.DimensionRef)
	}
	if !producer.Base.OutputGrain[0].Expression.IsResolved() {
		return fmt.Errorf("additive attribution decomposition dimension %q lacks resolved expression evidence", attribution.DimensionRef)
	}
	if !producer.Expression.IsResolved() {
		return fmt.Errorf("additive attribution metric %q lacks resolved physical expression", attribution.MetricRef)
	}

	work, err := semanticplan.SourceScanWorkOfNode(producer)
	if err != nil {
		return fmt.Errorf("resolve additive attribution source work: %w", err)
	}
	if !sourceWorkContainsDataset(work, attribution.TimeDimension.Dataset) {
		return fmt.Errorf("additive attribution time-dimension dataset %q is not present in the governed source work; lowering will not infer a new join", attribution.TimeDimension.Dataset)
	}
	return nil
}

func sourceWorkContainsDataset(work semanticplan.SourceScanWork, dataset string) bool {
	dataset = strings.TrimSpace(dataset)
	if dataset == "" {
		return false
	}
	if work.Root.Name == dataset {
		return true
	}
	for _, candidate := range work.RequiredDatasets {
		if candidate == dataset {
			return true
		}
	}
	for _, join := range work.Joins {
		if join.FromDataset == dataset || join.ToDataset == dataset {
			return true
		}
	}
	return false
}

func additiveAttributionPeriodBlock(plan *semanticplan.SemanticPlan, producer semanticplan.SourceAggregateNode, attribution semanticplan.AdditiveAttributionNode, period semanticplan.MetricAttributionTimeRange, id sqlplan.QueryBlockID, expressionDialect string) (sqlplan.QueryBlock, error) {
	block, err := metricSourceNodesSQLPlan([]semanticplan.SemanticPlanNode{producer}, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.ID = id
	timeExpr, err := fieldSQLPlanExpr(attribution.TimeDimension.Dataset, attribution.TimeDimension.Expression, nil, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.Predicates = append(block.Predicates,
		sqlplan.Predicate{Left: timeExpr, Operator: query.FilterGTE, Values: []any{physicalMetricAttributionInstant(period.Start)}},
		sqlplan.Predicate{Left: timeExpr, Operator: query.FilterLT, Values: []any{physicalMetricAttributionInstant(period.End)}},
	)
	return block, nil
}

// physicalMetricAttributionInstant is the target-neutral SQL timestamp lexical
// form used for bound period predicates. Semantic fingerprints and explain
// retain canonical RFC3339; physical engines receive the same UTC instant in a
// form accepted by every registered SQL target.
func physicalMetricAttributionInstant(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05.999999999")
}

func additiveAttributionAlignedBlock(attribution semanticplan.AdditiveAttributionNode, producer semanticplan.SourceAggregateNode) sqlplan.QueryBlock {
	dimension := attribution.DimensionRef
	baselineDimension := sqlplan.ColumnRef{Table: additiveAttributionBaselineAlias, Name: dimension}
	currentDimension := sqlplan.ColumnRef{Table: additiveAttributionCurrentAlias, Name: dimension}
	baselineMetric := zeroFilledSQLPlanExpr(sqlplan.ColumnRef{Table: additiveAttributionBaselineAlias, Name: producer.Base.ID})
	currentMetric := zeroFilledSQLPlanExpr(sqlplan.ColumnRef{Table: additiveAttributionCurrentAlias, Name: producer.Base.ID})

	return sqlplan.QueryBlock{
		ID: additiveAttributionAlignedBlockID,
		Inputs: []sqlplan.QueryInput{
			{Alias: additiveAttributionBaselineAlias, Block: additiveAttributionBaselineBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: additiveAttributionCurrentAlias, Block: additiveAttributionCurrentBlockID, Mode: sqlplan.QueryInputCTE},
		},
		From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: additiveAttributionBaselineAlias}, Alias: additiveAttributionBaselineAlias},
		Joins: []sqlplan.Join{{
			Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: additiveAttributionCurrentAlias}, Alias: additiveAttributionCurrentAlias},
			Kind:     sqlplan.JoinFullOuter,
			On:       sqlplan.BinaryExpr{Left: baselineDimension, Operator: "=", Right: currentDimension},
		}},
		Projections: []sqlplan.Projection{
			{Expr: sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{baselineDimension, currentDimension}}, Alias: dimension},
			{Expr: baselineMetric, Alias: additiveAttributionBaselineValueAlias},
			{Expr: currentMetric, Alias: additiveAttributionCurrentValueAlias},
			{Expr: sqlplan.BinaryExpr{Left: currentMetric, Operator: "-", Right: baselineMetric}, Alias: additiveAttributionDeltaAlias},
		},
	}
}

func additiveAttributionTotalBlock() sqlplan.QueryBlock {
	return sqlplan.QueryBlock{
		ID:     additiveAttributionTotalBlockID,
		Inputs: []sqlplan.QueryInput{{Alias: additiveAttributionAlignedAlias, Block: additiveAttributionAlignedBlockID, Mode: sqlplan.QueryInputCTE}},
		From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: additiveAttributionAlignedAlias}, Alias: additiveAttributionAlignedAlias},
		Projections: []sqlplan.Projection{{
			Expr:  sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: additiveAttributionAlignedAlias, Name: additiveAttributionDeltaAlias}}},
			Alias: additiveAttributionTotalDeltaAlias,
		}},
	}
}

func additiveAttributionResultBlock(attribution semanticplan.AdditiveAttributionNode) sqlplan.QueryBlock {
	dimension := sqlplan.ColumnRef{Table: additiveAttributionAlignedAlias, Name: attribution.DimensionRef}
	baseline := sqlplan.ColumnRef{Table: additiveAttributionAlignedAlias, Name: additiveAttributionBaselineValueAlias}
	current := sqlplan.ColumnRef{Table: additiveAttributionAlignedAlias, Name: additiveAttributionCurrentValueAlias}
	delta := sqlplan.ColumnRef{Table: additiveAttributionAlignedAlias, Name: additiveAttributionDeltaAlias}
	total := sqlplan.ColumnRef{Table: additiveAttributionTotalAlias, Name: additiveAttributionTotalDeltaAlias}
	hundred := sqlplan.OpaqueExpr{SQL: "100", Dialect: sqlplan.ANSISQLExpressionDialect}
	contribution := sqlplan.BinaryExpr{
		Left:     sqlplan.NullOnZeroDivideExpr{Numerator: delta, Denominator: total},
		Operator: "*",
		Right:    hundred,
	}

	return sqlplan.QueryBlock{
		ID: additiveAttributionRootBlockID,
		Inputs: []sqlplan.QueryInput{
			{Alias: additiveAttributionAlignedAlias, Block: additiveAttributionAlignedBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: additiveAttributionTotalAlias, Block: additiveAttributionTotalBlockID, Mode: sqlplan.QueryInputCTE},
		},
		From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: additiveAttributionAlignedAlias}, Alias: additiveAttributionAlignedAlias},
		Joins: []sqlplan.Join{{
			Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: additiveAttributionTotalAlias}, Alias: additiveAttributionTotalAlias},
			Kind:     sqlplan.JoinCross,
		}},
		Projections: []sqlplan.Projection{
			{Expr: dimension, Alias: attribution.DimensionRef},
			{Expr: baseline, Alias: additiveAttributionBaselineValueAlias},
			{Expr: current, Alias: additiveAttributionCurrentValueAlias},
			{Expr: delta, Alias: additiveAttributionDeltaAlias},
			{Expr: total, Alias: additiveAttributionTotalDeltaAlias},
			{Expr: sqlDecimal(contribution), Alias: additiveAttributionContributionAlias},
		},
	}
}

func sqlDecimal(expr sqlplan.Expr) sqlplan.Expr {
	return sqlplan.CastExpr{Expr: expr, Type: sqlplan.CastDecimal38Scale18}
}
