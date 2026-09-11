package conversion

import (
	"fmt"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

const (
	ratioAttributionBaselineBlockID       sqlplan.QueryBlockID = "ratio_attribution_baseline"
	ratioAttributionCurrentBlockID        sqlplan.QueryBlockID = "ratio_attribution_current"
	ratioAttributionAlignedBlockID        sqlplan.QueryBlockID = "ratio_attribution_aligned"
	ratioAttributionTotalsBlockID         sqlplan.QueryBlockID = "ratio_attribution_totals"
	ratioAttributionEffectsBlockID        sqlplan.QueryBlockID = "ratio_attribution_effects"
	ratioAttributionReconciliationBlockID sqlplan.QueryBlockID = "ratio_attribution_reconciliation"
	ratioAttributionRootBlockID           sqlplan.QueryBlockID = "root"

	ratioAttributionBaselineAlias       = "__metis_ratio_baseline"
	ratioAttributionCurrentAlias        = "__metis_ratio_current"
	ratioAttributionAlignedAlias        = "__metis_ratio_aligned"
	ratioAttributionTotalsAlias         = "__metis_ratio_totals"
	ratioAttributionEffectsAlias        = "__metis_ratio_effects"
	ratioAttributionReconciliationAlias = "__metis_ratio_reconciliation"

	ratioAttributionBaselinePresentAlias     = "baseline_present"
	ratioAttributionCurrentPresentAlias      = "current_present"
	ratioAttributionBaselineNumeratorAlias   = "baseline_numerator"
	ratioAttributionBaselineDenominatorAlias = "baseline_denominator"
	ratioAttributionCurrentNumeratorAlias    = "current_numerator"
	ratioAttributionCurrentDenominatorAlias  = "current_denominator"
	ratioAttributionBaselineRateAlias        = "baseline_rate"
	ratioAttributionCurrentRateAlias         = "current_rate"
	ratioAttributionBaselineWeightAlias      = "baseline_weight"
	ratioAttributionCurrentWeightAlias       = "current_weight"
	ratioAttributionBaselineDefinedAlias     = "baseline_defined"
	ratioAttributionCurrentDefinedAlias      = "current_defined"
	ratioAttributionSegmentDefinedAlias      = "segment_defined"
	ratioAttributionRateEffectAlias          = "rate_effect"
	ratioAttributionMixEffectAlias           = "mix_effect"
	ratioAttributionEntryEffectAlias         = "entry_effect"
	ratioAttributionExitEffectAlias          = "exit_effect"
	ratioAttributionSegmentEffectAlias       = "segment_effect"
	ratioAttributionBaselineTotalNumAlias    = "baseline_total_numerator"
	ratioAttributionBaselineTotalDenAlias    = "baseline_total_denominator"
	ratioAttributionCurrentTotalNumAlias     = "current_total_numerator"
	ratioAttributionCurrentTotalDenAlias     = "current_total_denominator"
	ratioAttributionBaselineRatioAlias       = "baseline_ratio"
	ratioAttributionCurrentRatioAlias        = "current_ratio"
	ratioAttributionRatioDeltaAlias          = "ratio_delta"
	ratioAttributionDecomposedDeltaAlias     = "decomposed_delta"
	ratioAttributionResidualAlias            = "reconciliation_residual"
	ratioAttributionDefinedAlias             = "attribution_defined"
	ratioAttributionUndefinedCountAlias      = "undefined_segment_count"
)

// PlanOwnedRatioAttributionSQLPlan lowers one proven ratio attribution into
// independent period aggregates, full-union alignment, symmetric mix/rate
// effects, and explicit reconciliation evidence. It consumes only node-owned
// semantic evidence and never reparses metric definitions or consults manifest.
func PlanOwnedRatioAttributionSQLPlan(plan *semanticplan.SemanticPlan, expressionDialect string) (*sqlplan.Plan, error) {
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, err
	}
	attribution, numerator, denominator, err := ratioAttributionPhysicalInputs(plan)
	if err != nil {
		return nil, err
	}
	if err := validateRatioAttributionPhysicalSources(attribution, numerator, denominator); err != nil {
		return nil, err
	}

	baseline, err := ratioAttributionPeriodBlock(plan, numerator, denominator, attribution, attribution.Baseline, ratioAttributionBaselineBlockID, ratioAttributionBaselinePresentAlias, expressionDialect)
	if err != nil {
		return nil, err
	}
	current, err := ratioAttributionPeriodBlock(plan, numerator, denominator, attribution, attribution.Current, ratioAttributionCurrentBlockID, ratioAttributionCurrentPresentAlias, expressionDialect)
	if err != nil {
		return nil, err
	}
	physical := &sqlplan.Plan{
		Root: ratioAttributionRootBlockID,
		Blocks: []sqlplan.QueryBlock{
			baseline,
			current,
			ratioAttributionAlignedBlock(attribution, numerator, denominator),
			ratioAttributionTotalsBlock(),
			ratioAttributionEffectsBlock(attribution),
			ratioAttributionReconciliationBlock(),
			ratioAttributionResultBlock(attribution, numerator.Metric.Datatype, denominator.Metric.Datatype),
		},
	}
	if err := sqlplan.ValidateForRenderer(physical, expressionDialect); err != nil {
		return nil, fmt.Errorf("ratio attribution SQLPlan is invalid: %w", err)
	}
	return sqlplan.Clone(physical), nil
}

func ratioAttributionPhysicalInputs(plan *semanticplan.SemanticPlan) (semanticplan.RatioAttributionNode, semanticplan.SourceAggregateNode, semanticplan.SourceAggregateNode, error) {
	var attribution *semanticplan.RatioAttributionNode
	for _, node := range plan.Nodes {
		candidate, ok := node.(semanticplan.RatioAttributionNode)
		if !ok {
			continue
		}
		if attribution != nil {
			return semanticplan.RatioAttributionNode{}, semanticplan.SourceAggregateNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("ratio attribution lowering supports exactly one attribution node")
		}
		copy := candidate
		attribution = &copy
	}
	if attribution == nil {
		return semanticplan.RatioAttributionNode{}, semanticplan.SourceAggregateNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("ratio attribution lowering requires one attribution node")
	}
	if len(attribution.Base.Inputs) != 2 {
		return semanticplan.RatioAttributionNode{}, semanticplan.SourceAggregateNode{}, semanticplan.SourceAggregateNode{}, fmt.Errorf("ratio attribution lowering requires ordered numerator and denominator inputs")
	}
	findProducer := func(id string) (semanticplan.SourceAggregateNode, error) {
		for _, node := range plan.Nodes {
			if node.NodeBase().ID != id {
				continue
			}
			producer, ok := node.(semanticplan.SourceAggregateNode)
			if !ok {
				return semanticplan.SourceAggregateNode{}, fmt.Errorf("ratio attribution input %q must be a source aggregate during ratio-attribution lowering", id)
			}
			return producer, nil
		}
		return semanticplan.SourceAggregateNode{}, fmt.Errorf("ratio attribution input %q is missing", id)
	}
	numerator, err := findProducer(attribution.NumeratorRef)
	if err != nil {
		return semanticplan.RatioAttributionNode{}, semanticplan.SourceAggregateNode{}, semanticplan.SourceAggregateNode{}, err
	}
	denominator, err := findProducer(attribution.DenominatorRef)
	if err != nil {
		return semanticplan.RatioAttributionNode{}, semanticplan.SourceAggregateNode{}, semanticplan.SourceAggregateNode{}, err
	}
	return *attribution, numerator, denominator, nil
}

func validateRatioAttributionPhysicalSources(attribution semanticplan.RatioAttributionNode, numerator, denominator semanticplan.SourceAggregateNode) error {
	operands := []struct {
		role     string
		producer semanticplan.SourceAggregateNode
		want     string
	}{
		{role: "numerator", producer: numerator, want: attribution.NumeratorRef},
		{role: "denominator", producer: denominator, want: attribution.DenominatorRef},
	}
	for _, operand := range operands {
		role, producer, want := operand.role, operand.producer, operand.want
		if producer.Base.ID != want || producer.Metric == nil {
			return fmt.Errorf("ratio attribution %s producer does not carry governed metric %q", role, want)
		}
		zeroFill, err := metricUsesZeroFill(producer.Metric)
		if err != nil {
			return fmt.Errorf("resolve ratio attribution %s fill policy for %q: %w", role, want, err)
		}
		if !zeroFill {
			return fmt.Errorf("ratio attribution %s %q cannot preserve the union population because its fill policy does not admit additive identity zero", role, want)
		}
		if len(producer.Base.OutputGrain) != 1 || producer.Base.OutputGrain[0].Name != attribution.DimensionRef || !producer.Base.OutputGrain[0].Expression.IsResolved() {
			return fmt.Errorf("ratio attribution %s grain must be exactly resolved decomposition dimension %q", role, attribution.DimensionRef)
		}
		if !producer.Expression.IsResolved() {
			return fmt.Errorf("ratio attribution %s %q lacks resolved physical expression", role, want)
		}
	}
	numeratorWork, err := semanticplan.SourceScanWorkOfNode(numerator)
	if err != nil {
		return fmt.Errorf("resolve ratio attribution numerator source work: %w", err)
	}
	denominatorWork, err := semanticplan.SourceScanWorkOfNode(denominator)
	if err != nil {
		return fmt.Errorf("resolve ratio attribution denominator source work: %w", err)
	}
	numeratorIdentity, err := numeratorWork.Identity()
	if err != nil {
		return err
	}
	denominatorIdentity, err := denominatorWork.Identity()
	if err != nil {
		return err
	}
	if numeratorIdentity != denominatorIdentity {
		return fmt.Errorf("ratio attribution operands must use one governed source population")
	}
	if !sourceWorkContainsDataset(numeratorWork, attribution.TimeDimension.Dataset) {
		return fmt.Errorf("ratio attribution time-dimension dataset %q is not present in governed source work; lowering will not infer a new join", attribution.TimeDimension.Dataset)
	}
	return nil
}

func ratioAttributionPeriodBlock(plan *semanticplan.SemanticPlan, numerator, denominator semanticplan.SourceAggregateNode, attribution semanticplan.RatioAttributionNode, period semanticplan.MetricAttributionTimeRange, id sqlplan.QueryBlockID, presenceAlias, expressionDialect string) (sqlplan.QueryBlock, error) {
	block, err := metricSourceNodesSQLPlan([]semanticplan.SemanticPlanNode{numerator, denominator}, expressionDialect)
	if err != nil {
		return sqlplan.QueryBlock{}, err
	}
	block.ID = id
	block.Projections = append(block.Projections, sqlplan.Projection{Expr: sqlLiteral("1"), Alias: presenceAlias})
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

func ratioAttributionAlignedBlock(attribution semanticplan.RatioAttributionNode, numerator, denominator semanticplan.SourceAggregateNode) sqlplan.QueryBlock {
	dimension := attribution.DimensionRef
	baselineDimension := sqlplan.ColumnRef{Table: ratioAttributionBaselineAlias, Name: dimension}
	currentDimension := sqlplan.ColumnRef{Table: ratioAttributionCurrentAlias, Name: dimension}
	return sqlplan.QueryBlock{
		ID: ratioAttributionAlignedBlockID,
		Inputs: []sqlplan.QueryInput{
			{Alias: ratioAttributionBaselineAlias, Block: ratioAttributionBaselineBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionCurrentAlias, Block: ratioAttributionCurrentBlockID, Mode: sqlplan.QueryInputCTE},
		},
		From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionBaselineAlias}, Alias: ratioAttributionBaselineAlias},
		Joins: []sqlplan.Join{{
			Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionCurrentAlias}, Alias: ratioAttributionCurrentAlias},
			Kind:     sqlplan.JoinFullOuter,
			On:       sqlplan.BinaryExpr{Left: baselineDimension, Operator: "=", Right: currentDimension},
		}},
		Projections: []sqlplan.Projection{
			{Expr: sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{baselineDimension, currentDimension}}, Alias: dimension},
			{Expr: sqlplan.ColumnRef{Table: ratioAttributionBaselineAlias, Name: ratioAttributionBaselinePresentAlias}, Alias: ratioAttributionBaselinePresentAlias},
			{Expr: sqlplan.ColumnRef{Table: ratioAttributionCurrentAlias, Name: ratioAttributionCurrentPresentAlias}, Alias: ratioAttributionCurrentPresentAlias},
			{Expr: zeroFilledSQLPlanExpr(sqlplan.ColumnRef{Table: ratioAttributionBaselineAlias, Name: numerator.Base.ID}), Alias: ratioAttributionBaselineNumeratorAlias},
			{Expr: zeroFilledSQLPlanExpr(sqlplan.ColumnRef{Table: ratioAttributionBaselineAlias, Name: denominator.Base.ID}), Alias: ratioAttributionBaselineDenominatorAlias},
			{Expr: zeroFilledSQLPlanExpr(sqlplan.ColumnRef{Table: ratioAttributionCurrentAlias, Name: numerator.Base.ID}), Alias: ratioAttributionCurrentNumeratorAlias},
			{Expr: zeroFilledSQLPlanExpr(sqlplan.ColumnRef{Table: ratioAttributionCurrentAlias, Name: denominator.Base.ID}), Alias: ratioAttributionCurrentDenominatorAlias},
		},
	}
}

func ratioAttributionTotalsBlock() sqlplan.QueryBlock {
	return sqlplan.QueryBlock{
		ID:     ratioAttributionTotalsBlockID,
		Inputs: []sqlplan.QueryInput{{Alias: ratioAttributionAlignedAlias, Block: ratioAttributionAlignedBlockID, Mode: sqlplan.QueryInputCTE}},
		From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionAlignedAlias}, Alias: ratioAttributionAlignedAlias},
		Projections: []sqlplan.Projection{
			{Expr: sqlSumColumn(ratioAttributionAlignedAlias, ratioAttributionBaselineNumeratorAlias), Alias: ratioAttributionBaselineTotalNumAlias},
			{Expr: sqlSumColumn(ratioAttributionAlignedAlias, ratioAttributionBaselineDenominatorAlias), Alias: ratioAttributionBaselineTotalDenAlias},
			{Expr: sqlSumColumn(ratioAttributionAlignedAlias, ratioAttributionCurrentNumeratorAlias), Alias: ratioAttributionCurrentTotalNumAlias},
			{Expr: sqlSumColumn(ratioAttributionAlignedAlias, ratioAttributionCurrentDenominatorAlias), Alias: ratioAttributionCurrentTotalDenAlias},
		},
	}
}

func ratioAttributionEffectsBlock(attribution semanticplan.RatioAttributionNode) sqlplan.QueryBlock {
	a := func(name string) sqlplan.Expr {
		return sqlplan.ColumnRef{Table: ratioAttributionAlignedAlias, Name: name}
	}
	t := func(name string) sqlplan.Expr {
		return sqlplan.ColumnRef{Table: ratioAttributionTotalsAlias, Name: name}
	}
	bp, cp := a(ratioAttributionBaselinePresentAlias), a(ratioAttributionCurrentPresentAlias)
	bn, bd := a(ratioAttributionBaselineNumeratorAlias), a(ratioAttributionBaselineDenominatorAlias)
	cn, cd := a(ratioAttributionCurrentNumeratorAlias), a(ratioAttributionCurrentDenominatorAlias)
	br := guardedDivide(bn, bd)
	cr := guardedDivide(cn, cd)
	bw := guardedDivide(bd, t(ratioAttributionBaselineTotalDenAlias))
	cw := guardedDivide(cd, t(ratioAttributionCurrentTotalDenAlias))
	bPresent, cPresent := sqlNotNull(bp), sqlNotNull(cp)
	bAbsent, cAbsent := sqlIsNull(bp), sqlIsNull(cp)
	continuing := sqlAnd(bPresent, cPresent)
	entry := sqlAnd(bAbsent, cPresent)
	exit := sqlAnd(bPresent, cAbsent)
	bDefined := sqlAnd(bPresent, sqlNotZero(bd))
	cDefined := sqlAnd(cPresent, sqlNotZero(cd))
	segmentDefined := sqlAnd(sqlOr(bAbsent, sqlNotZero(bd)), sqlOr(cAbsent, sqlNotZero(cd)))
	rateEffect := searchedCase(continuing,
		sqlMul(sqlLiteral("0.5"), sqlMul(sqlAdd(bw, cw), sqlSub(cr, br))),
		sqlLiteral("0"),
	)
	mixEffect := searchedCase(continuing,
		sqlMul(sqlLiteral("0.5"), sqlMul(sqlAdd(br, cr), sqlSub(cw, bw))),
		sqlLiteral("0"),
	)
	entryEffect := searchedCase(entry, sqlMul(cw, cr), sqlLiteral("0"))
	exitEffect := searchedCase(exit, sqlMul(sqlLiteral("-1"), sqlMul(bw, br)), sqlLiteral("0"))
	segmentEffect := sqlAdd(sqlAdd(rateEffect, mixEffect), sqlAdd(entryEffect, exitEffect))

	return sqlplan.QueryBlock{
		ID: ratioAttributionEffectsBlockID,
		Inputs: []sqlplan.QueryInput{
			{Alias: ratioAttributionAlignedAlias, Block: ratioAttributionAlignedBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionTotalsAlias, Block: ratioAttributionTotalsBlockID, Mode: sqlplan.QueryInputCTE},
		},
		From:  sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionAlignedAlias}, Alias: ratioAttributionAlignedAlias},
		Joins: []sqlplan.Join{{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionTotalsAlias}, Alias: ratioAttributionTotalsAlias}, Kind: sqlplan.JoinCross}},
		Projections: []sqlplan.Projection{
			{Expr: a(attribution.DimensionRef), Alias: attribution.DimensionRef},
			{Expr: bPresent, Alias: ratioAttributionBaselinePresentAlias},
			{Expr: cPresent, Alias: ratioAttributionCurrentPresentAlias},
			{Expr: bn, Alias: ratioAttributionBaselineNumeratorAlias},
			{Expr: bd, Alias: ratioAttributionBaselineDenominatorAlias},
			{Expr: cn, Alias: ratioAttributionCurrentNumeratorAlias},
			{Expr: cd, Alias: ratioAttributionCurrentDenominatorAlias},
			{Expr: br, Alias: ratioAttributionBaselineRateAlias},
			{Expr: cr, Alias: ratioAttributionCurrentRateAlias},
			{Expr: bw, Alias: ratioAttributionBaselineWeightAlias},
			{Expr: cw, Alias: ratioAttributionCurrentWeightAlias},
			{Expr: bDefined, Alias: ratioAttributionBaselineDefinedAlias},
			{Expr: cDefined, Alias: ratioAttributionCurrentDefinedAlias},
			{Expr: segmentDefined, Alias: ratioAttributionSegmentDefinedAlias},
			{Expr: rateEffect, Alias: ratioAttributionRateEffectAlias},
			{Expr: mixEffect, Alias: ratioAttributionMixEffectAlias},
			{Expr: entryEffect, Alias: ratioAttributionEntryEffectAlias},
			{Expr: exitEffect, Alias: ratioAttributionExitEffectAlias},
			{Expr: segmentEffect, Alias: ratioAttributionSegmentEffectAlias},
		},
	}
}

func ratioAttributionReconciliationBlock() sqlplan.QueryBlock {
	effect := sqlplan.ColumnRef{Table: ratioAttributionEffectsAlias, Name: ratioAttributionSegmentEffectAlias}
	defined := sqlplan.ColumnRef{Table: ratioAttributionEffectsAlias, Name: ratioAttributionSegmentDefinedAlias}
	return sqlplan.QueryBlock{
		ID:     ratioAttributionReconciliationBlockID,
		Inputs: []sqlplan.QueryInput{{Alias: ratioAttributionEffectsAlias, Block: ratioAttributionEffectsBlockID, Mode: sqlplan.QueryInputCTE}},
		From:   sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionEffectsAlias}, Alias: ratioAttributionEffectsAlias},
		Projections: []sqlplan.Projection{
			{Expr: sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{effect}}, Alias: ratioAttributionDecomposedDeltaAlias},
			{Expr: sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{searchedCase(defined, sqlLiteral("0"), sqlLiteral("1"))}}, Alias: ratioAttributionUndefinedCountAlias},
		},
	}
}

func ratioAttributionResultBlock(attribution semanticplan.RatioAttributionNode, numeratorType, denominatorType ossie.DataType) sqlplan.QueryBlock {
	e := func(name string) sqlplan.Expr {
		return sqlplan.ColumnRef{Table: ratioAttributionEffectsAlias, Name: name}
	}
	t := func(name string) sqlplan.Expr {
		return sqlplan.ColumnRef{Table: ratioAttributionTotalsAlias, Name: name}
	}
	r := func(name string) sqlplan.Expr {
		return sqlplan.ColumnRef{Table: ratioAttributionReconciliationAlias, Name: name}
	}
	baselineRatio := guardedDivide(t(ratioAttributionBaselineTotalNumAlias), t(ratioAttributionBaselineTotalDenAlias))
	currentRatio := guardedDivide(t(ratioAttributionCurrentTotalNumAlias), t(ratioAttributionCurrentTotalDenAlias))
	ratioDelta := sqlSub(currentRatio, baselineRatio)
	attributionDefined := sqlAnd(
		sqlAnd(sqlNotZero(sqlCoalesceZero(t(ratioAttributionBaselineTotalDenAlias))), sqlNotZero(sqlCoalesceZero(t(ratioAttributionCurrentTotalDenAlias)))),
		sqlGrouped(sqlplan.BinaryExpr{Left: sqlCoalesceZero(r(ratioAttributionUndefinedCountAlias)), Operator: "=", Right: sqlLiteral("0")}),
	)
	decomposed := searchedCase(attributionDefined, r(ratioAttributionDecomposedDeltaAlias), sqlLiteral("NULL"))
	residual := searchedCase(attributionDefined, sqlSub(ratioDelta, r(ratioAttributionDecomposedDeltaAlias)), sqlLiteral("NULL"))
	nullRank := searchedCase(sqlIsNull(e(ratioAttributionSegmentEffectAlias)), sqlLiteral("1"), sqlLiteral("0"))

	projections := []sqlplan.Projection{
		{Expr: e(attribution.DimensionRef), Alias: attribution.DimensionRef},
		{Expr: e(ratioAttributionBaselinePresentAlias), Alias: ratioAttributionBaselinePresentAlias},
		{Expr: e(ratioAttributionCurrentPresentAlias), Alias: ratioAttributionCurrentPresentAlias},
		{Expr: exactMetricOutput(e(ratioAttributionBaselineNumeratorAlias), numeratorType), Alias: ratioAttributionBaselineNumeratorAlias},
		{Expr: exactMetricOutput(e(ratioAttributionBaselineDenominatorAlias), denominatorType), Alias: ratioAttributionBaselineDenominatorAlias},
		{Expr: exactMetricOutput(e(ratioAttributionCurrentNumeratorAlias), numeratorType), Alias: ratioAttributionCurrentNumeratorAlias},
		{Expr: exactMetricOutput(e(ratioAttributionCurrentDenominatorAlias), denominatorType), Alias: ratioAttributionCurrentDenominatorAlias},
		{Expr: sqlDecimal(e(ratioAttributionBaselineRateAlias)), Alias: ratioAttributionBaselineRateAlias},
		{Expr: sqlDecimal(e(ratioAttributionCurrentRateAlias)), Alias: ratioAttributionCurrentRateAlias},
		{Expr: sqlDecimal(e(ratioAttributionBaselineWeightAlias)), Alias: ratioAttributionBaselineWeightAlias},
		{Expr: sqlDecimal(e(ratioAttributionCurrentWeightAlias)), Alias: ratioAttributionCurrentWeightAlias},
		{Expr: e(ratioAttributionBaselineDefinedAlias), Alias: ratioAttributionBaselineDefinedAlias},
		{Expr: e(ratioAttributionCurrentDefinedAlias), Alias: ratioAttributionCurrentDefinedAlias},
		{Expr: e(ratioAttributionSegmentDefinedAlias), Alias: ratioAttributionSegmentDefinedAlias},
		{Expr: sqlDecimal(e(ratioAttributionRateEffectAlias)), Alias: ratioAttributionRateEffectAlias},
		{Expr: sqlDecimal(e(ratioAttributionMixEffectAlias)), Alias: ratioAttributionMixEffectAlias},
		{Expr: sqlDecimal(e(ratioAttributionEntryEffectAlias)), Alias: ratioAttributionEntryEffectAlias},
		{Expr: sqlDecimal(e(ratioAttributionExitEffectAlias)), Alias: ratioAttributionExitEffectAlias},
		{Expr: sqlDecimal(e(ratioAttributionSegmentEffectAlias)), Alias: ratioAttributionSegmentEffectAlias},
		{Expr: sqlDecimal(baselineRatio), Alias: ratioAttributionBaselineRatioAlias},
		{Expr: sqlDecimal(currentRatio), Alias: ratioAttributionCurrentRatioAlias},
		{Expr: sqlDecimal(ratioDelta), Alias: ratioAttributionRatioDeltaAlias},
		{Expr: sqlDecimal(decomposed), Alias: ratioAttributionDecomposedDeltaAlias},
		{Expr: sqlDecimal(residual), Alias: ratioAttributionResidualAlias},
		{Expr: attributionDefined, Alias: ratioAttributionDefinedAlias},
	}
	return sqlplan.QueryBlock{
		ID: ratioAttributionRootBlockID,
		Inputs: []sqlplan.QueryInput{
			{Alias: ratioAttributionBaselineAlias, Block: ratioAttributionBaselineBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionCurrentAlias, Block: ratioAttributionCurrentBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionAlignedAlias, Block: ratioAttributionAlignedBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionTotalsAlias, Block: ratioAttributionTotalsBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionEffectsAlias, Block: ratioAttributionEffectsBlockID, Mode: sqlplan.QueryInputCTE},
			{Alias: ratioAttributionReconciliationAlias, Block: ratioAttributionReconciliationBlockID, Mode: sqlplan.QueryInputCTE},
		},
		From: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionEffectsAlias}, Alias: ratioAttributionEffectsAlias},
		Joins: []sqlplan.Join{
			{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionTotalsAlias}, Alias: ratioAttributionTotalsAlias}, Kind: sqlplan.JoinCross},
			{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: ratioAttributionReconciliationAlias}, Alias: ratioAttributionReconciliationAlias}, Kind: sqlplan.JoinCross},
		},
		Projections: projections,
		OrderBy: []sqlplan.Order{
			{Expr: nullRank, Direction: query.SortAsc},
			{Expr: sqlplan.FunctionCallExpr{Name: "ABS", Args: []sqlplan.Expr{e(ratioAttributionSegmentEffectAlias)}}, Direction: query.SortDesc},
			{Expr: e(attribution.DimensionRef), Direction: query.SortAsc},
		},
	}
}

func exactMetricOutput(expr sqlplan.Expr, datatype ossie.DataType) sqlplan.Expr {
	if datatype == ossie.DataTypeDecimal {
		return sqlDecimal(expr)
	}
	return expr
}

func sqlLiteral(value string) sqlplan.Expr {
	return sqlplan.OpaqueExpr{SQL: value, Dialect: sqlplan.ANSISQLExpressionDialect}
}

func sqlSumColumn(table, name string) sqlplan.Expr {
	return sqlplan.FunctionCallExpr{Name: "SUM", Args: []sqlplan.Expr{sqlplan.ColumnRef{Table: table, Name: name}}}
}

func sqlCoalesceZero(expr sqlplan.Expr) sqlplan.Expr {
	return sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{expr, sqlLiteral("0")}}
}

func guardedDivide(left, right sqlplan.Expr) sqlplan.Expr {
	division := sqlGrouped(sqlplan.NullOnZeroDivideExpr{Numerator: left, Denominator: right})
	// Keep the quotient below maximum DECIMAL precision before composing
	// multiple ratio effects. Engines such as Doris promote SUM(DECIMAL) to
	// maximum precision, then must otherwise discard fractional scale during
	// the following multiplications.
	return sqlplan.CastExpr{Expr: division, Type: sqlplan.CastDecimal20Scale12}
}

func sqlGrouped(expr sqlplan.Expr) sqlplan.Expr { return sqlplan.ParenthesizedExpr{Expr: expr} }
func sqlAdd(left, right sqlplan.Expr) sqlplan.Expr {
	return sqlGrouped(sqlplan.BinaryExpr{Left: left, Operator: "+", Right: right})
}
func sqlSub(left, right sqlplan.Expr) sqlplan.Expr {
	return sqlGrouped(sqlplan.BinaryExpr{Left: left, Operator: "-", Right: right})
}
func sqlMul(left, right sqlplan.Expr) sqlplan.Expr {
	return sqlGrouped(sqlplan.BinaryExpr{Left: left, Operator: "*", Right: right})
}
func sqlNotZero(expr sqlplan.Expr) sqlplan.Expr {
	return sqlGrouped(sqlplan.BinaryExpr{Left: expr, Operator: "<>", Right: sqlLiteral("0")})
}
func sqlIsNull(expr sqlplan.Expr) sqlplan.Expr { return sqlplan.NullTestExpr{Expr: expr} }
func sqlNotNull(expr sqlplan.Expr) sqlplan.Expr {
	return sqlplan.NullTestExpr{Expr: expr, Negated: true}
}
func sqlAnd(terms ...sqlplan.Expr) sqlplan.Expr {
	return sqlGrouped(sqlplan.LogicalExpr{Operator: "AND", Terms: terms})
}
func sqlOr(terms ...sqlplan.Expr) sqlplan.Expr {
	return sqlGrouped(sqlplan.LogicalExpr{Operator: "OR", Terms: terms})
}

func searchedCase(when, then, otherwise sqlplan.Expr) sqlplan.Expr {
	return sqlplan.CaseExpr{Branches: []sqlplan.CaseWhen{{When: when, Then: then}}, Else: otherwise}
}
