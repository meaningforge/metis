package conversion

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

var simpleIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// BuildSQLPlan is the complete semantic-to-physical producer boundary. Every
// rewrite that still needs semanticplan.SemanticPlan authority runs here; consumers receive
// a validated, fully owned SQLPlan that requires no semantic-aware mutation.
func BuildSQLPlan(plan *semanticplan.SemanticPlan, renderer renderer.Renderer) (*sqlplan.Plan, error) {
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, err
	}
	if renderer == nil || strings.TrimSpace(renderer.ExpressionDialect()) == "" {
		return nil, serrors.Internal("selected Renderer is required for SQLPlan lowering", nil)
	}
	expressionDialect := renderer.ExpressionDialect()
	strategy := LoweringStrategyForPlan(plan)
	var physical *sqlplan.Plan
	var err error
	switch strategy {
	case SemanticLoweringCompact:
		physical, err = PlanOwnedCompactSQLPlan(plan, expressionDialect)
	case SemanticLoweringComposed:
		physical, err = PlanOwnedComposedSQLPlan(plan, expressionDialect)
	case SemanticLoweringAdditiveAttribution:
		physical, err = PlanOwnedAdditiveAttributionSQLPlan(plan, expressionDialect)
	case SemanticLoweringRatioAttribution:
		physical, err = PlanOwnedRatioAttributionSQLPlan(plan, expressionDialect)
	default:
		err = serrors.Internal("semantic plan has an unsupported SQLPlan lowering strategy", nil)
	}
	if err != nil {
		return nil, err
	}
	// Attribution has an explicit population-preservation boundary and output
	// schema. Its lowering consumes the same governed fill authority but must
	// not be reinterpreted by ordinary metric-output rewrites afterward.
	if strategy != SemanticLoweringAdditiveAttribution && strategy != SemanticLoweringRatioAttribution {
		if err := ApplySemiAdditiveComposabilityToSQLPlan(physical, plan); err != nil {
			return nil, err
		}
		if err := NormalizeSemiAdditiveComposableInputsToSQLPlan(physical, plan); err != nil {
			return nil, err
		}
		if err := ApplyPostEvaluationPredicatesToSQLPlan(physical, plan); err != nil {
			return nil, err
		}
		if err := ApplyMetricFillPoliciesToSQLPlan(physical, plan); err != nil {
			return nil, err
		}
		if err := applyOutputDatatypeContractsToSQLPlan(physical, plan); err != nil {
			return nil, err
		}
	}
	if err := sqlplan.ValidateForRenderer(physical, expressionDialect); err != nil {
		return nil, serrors.Internal("complete SQLPlan is invalid", map[string]any{"cause": err.Error()})
	}
	return sqlplan.Clone(physical), nil
}

// applyOutputDatatypeContractsToSQLPlan makes the physical root projection
// honor the exact Agent-facing datatype contract. In particular, database
// division and AVG commonly widen exact Decimal inputs to a binary float;
// execution/runner correctly rejects that lossy representation. The explicit
// typed physical cast keeps the physical query and OutputSchema atomic instead of
// asking a Driver or test adapter to repair an already lossy result. The root
// uses Decimal(38,18) directly: Decimal(20,12) is an intermediate ratio scale
// contract and would unnecessarily cap ordinary metrics at eight integer digits.
func applyOutputDatatypeContractsToSQLPlan(physical *sqlplan.Plan, semantic *semanticplan.SemanticPlan) error {
	if physical == nil || semantic == nil {
		return serrors.Internal("output datatype contract requires semantic and SQL plans", nil)
	}
	rootIndex := -1
	for index := range physical.Blocks {
		if physical.Blocks[index].ID == physical.Root {
			rootIndex = index
			break
		}
	}
	if rootIndex < 0 {
		return serrors.Internal("output datatype contract cannot find SQLPlan root", nil)
	}
	root := &physical.Blocks[rootIndex]
	if len(root.Projections) != len(semantic.Projections) {
		return serrors.Internal("SQLPlan root does not match semantic output projection count", map[string]any{
			"sql_projections": len(root.Projections), "semantic_projections": len(semantic.Projections),
		})
	}
	for index, projection := range semantic.Projections {
		if root.Projections[index].Alias != projection.Name {
			return serrors.Internal("SQLPlan root does not match semantic output projection order", map[string]any{
				"projection_index": index, "sql_alias": root.Projections[index].Alias, "semantic_name": projection.Name,
			})
		}
		if projection.Kind != semanticplan.ProjectionMetric || projection.Metric == nil || projection.Metric.Datatype != ossie.DataTypeDecimal {
			continue
		}
		if existing, ok := root.Projections[index].Expr.(sqlplan.CastExpr); ok && existing.Type == sqlplan.CastDecimal38Scale18 {
			continue
		}
		root.Projections[index].Expr = sqlplan.CastExpr{
			Expr: root.Projections[index].Expr,
			Type: sqlplan.CastDecimal38Scale18,
		}
	}
	return nil
}

// PlanOwnedCompactSQLPlan constructs the single-block SQLPlan described by a
// compact semanticplan.SemanticPlan. BuildSQLPlan selects this producer for compact plans
// before applying the shared typed physical rewrites.
func PlanOwnedCompactSQLPlan(plan *semanticplan.SemanticPlan, expressionDialect string) (*sqlplan.Plan, error) {
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, err
	}
	shape, err := planOwnedQueryShape(plan)
	if err != nil {
		return nil, err
	}
	block, err := buildSQLPlanBlockFromQueryShape(shape, expressionDialect)
	if err != nil {
		return nil, err
	}
	physicalPlan := &sqlplan.Plan{Root: block.ID, Blocks: []sqlplan.QueryBlock{block}}
	if err := sqlplan.ValidateForRenderer(physicalPlan, expressionDialect); err != nil {
		return nil, serrors.Internal("compact SQLPlan is invalid", map[string]any{"cause": err.Error()})
	}
	return sqlplan.Clone(physicalPlan), nil
}

func buildSQLPlanBlockFromQueryShape(shape sqlQueryShape, expressionDialect string) (sqlplan.QueryBlock, error) {
	block := sqlplan.QueryBlock{
		ID:   "root",
		From: sourceRelation(shape.Root),
	}
	if shape.Limit != nil {
		limit := *shape.Limit
		block.Limit = &limit
	}
	for _, projection := range shape.Projections {
		expr, err := projectionSQLPlanExpr(projection, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		block.Projections = append(block.Projections, sqlplan.Projection{Expr: expr, Alias: projection.Name})
	}
	for _, join := range shape.Joins {
		on, err := joinSQLPlanExpr(join)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		block.Joins = append(block.Joins, sqlplan.Join{
			Relation: sourceRelation(semanticplan.DatasetRef{Name: join.ToDataset, Source: join.ToSource, Policy: join.Policy}),
			On:       on,
			Kind:     sqlplan.JoinInner,
		})
	}
	for _, predicate := range shape.Predicates {
		left, err := fieldSQLPlanExpr(predicate.Dataset, predicate.Expression, nil, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		values, err := predicateValues(predicate.Filter.Operator, predicate.Filter.Value)
		if err != nil {
			return sqlplan.QueryBlock{}, &serrors.Error{Code: serrors.ErrInvalidFilterValue, Message: "invalid predicate value", Details: map[string]any{"field": predicate.Filter.Field, "cause": err.Error()}}
		}
		block.Predicates = append(block.Predicates, sqlplan.Predicate{Left: left, Operator: predicate.Filter.Operator, Values: values})
	}
	for _, group := range shape.Groups {
		resolved := group.Expression
		if group.CustomCalendar != nil {
			resolved.SourceDialect = expressionDialect
		}
		expr, err := fieldSQLPlanExpr(group.Dataset, resolved, group.Grain, expressionDialect)
		if err != nil {
			return sqlplan.QueryBlock{}, err
		}
		block.GroupBy = append(block.GroupBy, expr)
	}
	for _, sort := range shape.Sorts {
		var expr sqlplan.Expr
		var err error
		switch sort.Kind {
		case semanticplan.SortMetric:
			expr, err = rawSQLPlanExpression(sort.Expression, sort.Name, expressionDialect)
		case semanticplan.SortDimension:
			expr, err = fieldSQLPlanExpr(sort.Dataset, sort.Expression, nil, expressionDialect)
		default:
			err = fmt.Errorf("unsupported sort kind %q", sort.Kind)
		}
		if err != nil {
			return sqlplan.QueryBlock{}, &serrors.Error{Code: serrors.ErrInvalidSort, Message: "failed to plan sort expression", Details: map[string]any{"field": sort.Name, "cause": err.Error()}}
		}
		block.OrderBy = append(block.OrderBy, sqlplan.Order{Expr: expr, Direction: sort.Direction})
	}
	return block, nil
}

func projectionSQLPlanExpr(projection semanticplan.Projection, expressionDialect string) (sqlplan.Expr, error) {
	switch projection.Kind {
	case semanticplan.ProjectionMetric:
		return rawSQLPlanExpression(projection.Expression, projection.Name, expressionDialect)
	case semanticplan.ProjectionDimension:
		resolved := projection.Expression
		if projection.CustomCalendar != nil {
			// The Resolver already selected the expressionDialect-compatible bucket
			// expression. "custom_calendar" records its semantic origin, not a
			// renderer dialect.
			resolved.SourceDialect = expressionDialect
		}
		return fieldSQLPlanExpr(projection.Dataset, resolved, projection.Grain, expressionDialect)
	default:
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "unsupported projection kind", Details: map[string]any{"projection": projection.Name, "kind": projection.Kind}}
	}
}

func rawSQLPlanExpression(resolved expression.ResolvedExpression, name string, expressionDialect string) (sqlplan.Expr, error) {
	if !resolved.IsResolved() {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved expression is required before SQLPlan construction", Details: map[string]any{"field": name}}
	}
	dialect, ok := normalizedSQLPlanExpressionDialect(resolved.SourceDialect, expressionDialect)
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved expression is incompatible with SQLPlan expressionDialect", Details: map[string]any{"field": name, "expression_dialect": resolved.SourceDialect, "target_dialect": expressionDialect}}
	}
	return sqlplan.OpaqueExpr{SQL: resolved.Source, Dialect: dialect}, nil
}

func fieldSQLPlanExpr(dataset string, resolved expression.ResolvedExpression, grain *query.TimeGrain, expressionDialect string) (sqlplan.Expr, error) {
	if !resolved.IsResolved() {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved field expression is required before SQLPlan construction", Details: map[string]any{"dataset": dataset}}
	}
	dialect, ok := normalizedSQLPlanExpressionDialect(resolved.SourceDialect, expressionDialect)
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved field expression is incompatible with SQLPlan expressionDialect", Details: map[string]any{"dataset": dataset, "expression_dialect": resolved.SourceDialect, "target_dialect": expressionDialect}}
	}
	var result sqlplan.Expr
	if simpleIdentifier.MatchString(resolved.Source) {
		result = sqlplan.ColumnRef{Table: dataset, Name: resolved.Source}
	} else {
		if strings.TrimSpace(resolved.Source) == "" {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "physical field expression is required", Details: map[string]any{"dataset": dataset}}
		}
		result = sqlplan.OpaqueExpr{SQL: resolved.Source, Dialect: dialect}
	}
	if grain != nil {
		result = sqlplan.TimeGrainExpr{Grain: *grain, Expr: result}
	}
	return result, nil
}

func normalizedSQLPlanExpressionDialect(expressionDialect, targetDialect string) (string, bool) {
	switch {
	case strings.EqualFold(expressionDialect, sqlplan.ANSISQLExpressionDialect):
		return sqlplan.ANSISQLExpressionDialect, true
	case strings.EqualFold(expressionDialect, targetDialect):
		return targetDialect, true
	default:
		return "", false
	}
}

func predicateValues(op query.FilterOperator, value any) ([]any, error) {
	switch op {
	case query.FilterIsNull, query.FilterIsNotNull:
		return nil, nil
	case query.FilterIN, query.FilterNotIn:
		values, err := normalizeSliceValue(value)
		if err != nil || len(values) == 0 {
			return nil, fmt.Errorf("%s requires a non-empty array value", op)
		}
		return values, nil
	case query.FilterBetween:
		values, err := normalizeSliceValue(value)
		if err != nil || len(values) != 2 {
			return nil, fmt.Errorf("between requires exactly two values")
		}
		return values, nil
	default:
		return []any{value}, nil
	}
}

func normalizeSliceValue(value any) ([]any, error) {
	if value == nil {
		return nil, fmt.Errorf("array value is required")
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("array value is required")
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, nil
}

func joinSQLPlanExpr(join semanticplan.Join) (sqlplan.Expr, error) {
	relationship := join.Relationship
	if relationship == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "join relationship is required"}
	}
	if len(relationship.FromColumns) == 0 || len(relationship.FromColumns) != len(relationship.ToColumns) {
		return nil, &serrors.Error{Code: serrors.ErrInvalidRelationship, Message: "relationship join columns are invalid", Details: map[string]any{"relationship": relationship.Name}}
	}
	terms := make([]sqlplan.Expr, 0, len(relationship.FromColumns)+2)
	for i := range relationship.FromColumns {
		var leftDataset, leftColumn, rightDataset, rightColumn string
		if join.FromDataset == relationship.From && join.ToDataset == relationship.To {
			leftDataset, leftColumn, rightDataset, rightColumn = relationship.From, relationship.FromColumns[i], relationship.To, relationship.ToColumns[i]
		} else if join.FromDataset == relationship.To && join.ToDataset == relationship.From {
			leftDataset, leftColumn, rightDataset, rightColumn = relationship.To, relationship.ToColumns[i], relationship.From, relationship.FromColumns[i]
		} else {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "planned join does not match relationship endpoints", Details: map[string]any{"relationship": relationship.Name}}
		}
		terms = append(terms, sqlplan.BinaryExpr{Left: sqlplan.ColumnRef{Table: leftDataset, Name: leftColumn}, Operator: "=", Right: sqlplan.ColumnRef{Table: rightDataset, Name: rightColumn}})
	}
	if join.Temporal != nil {
		event := sqlplan.ColumnRef{Table: relationship.From, Name: join.Temporal.FromTimeDimension}
		validFrom := sqlplan.ColumnRef{Table: relationship.To, Name: join.Temporal.ToValidFrom}
		validTo := sqlplan.ColumnRef{Table: relationship.To, Name: join.Temporal.ToValidTo}
		terms = append(terms, sqlplan.BinaryExpr{Left: event, Operator: ">=", Right: validFrom})
		terms = append(terms, sqlplan.FunctionCallExpr{Name: "COALESCE", Args: []sqlplan.Expr{sqlplan.BinaryExpr{Left: event, Operator: "<", Right: validTo}, sqlplan.OpaqueExpr{SQL: "TRUE", Dialect: sqlplan.ANSISQLExpressionDialect}}})
	}
	if len(terms) == 1 {
		return terms[0], nil
	}
	return sqlplan.LogicalExpr{Operator: "AND", Terms: terms}, nil
}
