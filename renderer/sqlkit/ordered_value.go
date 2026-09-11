package sqlkit

import (
	"fmt"

	"github.com/meaningforge/metis/sqlplan"
)

const (
	orderedPrimaryCTE   = "__metis_ordered_primary"
	orderedSecondaryCTE = "__metis_ordered_secondary"
	orderedPrimaryAlias = "__metis_primary_key"
	orderedTieAlias     = "__metis_tie_break_key"
)

// LowerDeterministicOrderedValuePlan lowers ordered-value expressions with a
// tie-break into a deterministic typed SQLPlan. It is target-neutral; a
// Renderer may apply additional target-specific lowering afterward.
func LowerDeterministicOrderedValuePlan(plan *sqlplan.Plan) (*sqlplan.Plan, error) {
	owned := sqlplan.Clone(plan)
	blocks := make([]sqlplan.QueryBlock, 0, len(owned.Blocks))
	for _, block := range owned.Blocks {
		prefix, rewritten, err := lowerDeterministicOrderedValueBlock(block)
		if err != nil {
			return nil, fmt.Errorf("query block %q: %w", block.ID, err)
		}
		blocks = append(blocks, prefix...)
		blocks = append(blocks, rewritten)
	}
	owned.Blocks = blocks
	return owned, nil
}

func lowerDeterministicOrderedValueBlock(block sqlplan.QueryBlock) ([]sqlplan.QueryBlock, sqlplan.QueryBlock, error) {
	orderedIndex := -1
	var value, primary, tieBreak sqlplan.Expr
	aggregate := ""
	for i, item := range block.Projections {
		switch expr := item.Expr.(type) {
		case sqlplan.LatestValueExpr:
			if expr.TieBreak == nil {
				continue
			}
			if orderedIndex >= 0 {
				return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic ordered-value lowering supports one ordered-value expression per block")
			}
			orderedIndex, value, primary, tieBreak, aggregate = i, expr.Value, expr.OrderBy, expr.TieBreak, "MAX"
		case sqlplan.EarliestValueExpr:
			if expr.TieBreak == nil {
				continue
			}
			if orderedIndex >= 0 {
				return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic ordered-value lowering supports one ordered-value expression per block")
			}
			orderedIndex, value, primary, tieBreak, aggregate = i, expr.Value, expr.OrderBy, expr.TieBreak, "MIN"
		}
	}
	if orderedIndex < 0 {
		return nil, block, nil
	}
	if len(block.Joins) != 0 {
		return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic ordered-value lowering does not support pre-existing joins")
	}
	if len(block.Predicates) != 0 {
		return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic ordered-value lowering does not support block-local predicates")
	}

	primaryID := sqlplan.QueryBlockID(string(block.ID) + "__ordered_primary")
	secondaryID := sqlplan.QueryBlockID(string(block.ID) + "__ordered_secondary")
	primaryBlock := sqlplan.QueryBlock{ID: primaryID, Inputs: cloneInputs(block.Inputs), From: block.From}
	secondaryBlock := sqlplan.QueryBlock{ID: secondaryID, Inputs: cloneInputs(block.Inputs), From: block.From}
	outer := block
	outer.Projections = nil
	outer.GroupBy = nil
	outer.Joins = nil
	primaryJoinTerms := make([]sqlplan.Expr, 0, len(block.GroupBy)+1)
	secondaryJoinTerms := make([]sqlplan.Expr, 0, len(block.GroupBy)+2)
	for i, item := range block.Projections {
		if i == orderedIndex {
			continue
		}
		column, ok := item.Expr.(sqlplan.ColumnRef)
		if !ok {
			return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic ordered-value grouping projection must be a column reference")
		}
		primaryBlock.Projections = append(primaryBlock.Projections, sqlplan.Projection{Expr: column, Alias: item.Alias})
		primaryBlock.GroupBy = append(primaryBlock.GroupBy, column)
		secondaryBlock.Projections = append(secondaryBlock.Projections, sqlplan.Projection{Expr: column, Alias: item.Alias})
		secondaryBlock.GroupBy = append(secondaryBlock.GroupBy, column)
		outer.Projections = append(outer.Projections, sqlplan.Projection{Expr: column, Alias: item.Alias})
		primaryJoinTerms = append(primaryJoinTerms, sqlplan.BinaryExpr{Left: column, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedPrimaryCTE, Name: item.Alias}})
		secondaryJoinTerms = append(secondaryJoinTerms, sqlplan.BinaryExpr{Left: column, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedSecondaryCTE, Name: item.Alias}})
	}
	primaryBlock.Projections = append(primaryBlock.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: aggregate, Args: []sqlplan.Expr{primary}}, Alias: orderedPrimaryAlias})
	primaryJoinTerms = append(primaryJoinTerms, sqlplan.BinaryExpr{Left: primary, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedPrimaryCTE, Name: orderedPrimaryAlias}})
	secondaryBlock.Inputs = append(secondaryBlock.Inputs, sqlplan.QueryInput{Alias: orderedPrimaryCTE, Block: primaryID, Mode: sqlplan.QueryInputCTE})
	secondaryBlock.Joins = append(secondaryBlock.Joins, sqlplan.Join{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: orderedPrimaryCTE}, Alias: orderedPrimaryCTE}, On: logicalAnd(primaryJoinTerms), Kind: sqlplan.JoinInner})
	secondaryBlock.Projections = append(secondaryBlock.Projections, sqlplan.Projection{Expr: sqlplan.ColumnRef{Table: orderedPrimaryCTE, Name: orderedPrimaryAlias}, Alias: orderedPrimaryAlias})
	secondaryBlock.GroupBy = append(secondaryBlock.GroupBy, sqlplan.ColumnRef{Table: orderedPrimaryCTE, Name: orderedPrimaryAlias})
	secondaryBlock.Projections = append(secondaryBlock.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: aggregate, Args: []sqlplan.Expr{tieBreak}}, Alias: orderedTieAlias})
	secondaryJoinTerms = append(secondaryJoinTerms,
		sqlplan.BinaryExpr{Left: primary, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedSecondaryCTE, Name: orderedPrimaryAlias}},
		sqlplan.BinaryExpr{Left: tieBreak, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedSecondaryCTE, Name: orderedTieAlias}},
	)
	outer.Inputs = append(outer.Inputs,
		sqlplan.QueryInput{Alias: orderedPrimaryCTE, Block: primaryID, Mode: sqlplan.QueryInputCTE},
		sqlplan.QueryInput{Alias: orderedSecondaryCTE, Block: secondaryID, Mode: sqlplan.QueryInputCTE},
	)
	outer.Joins = append(outer.Joins, sqlplan.Join{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: orderedSecondaryCTE}, Alias: orderedSecondaryCTE}, On: logicalAnd(secondaryJoinTerms), Kind: sqlplan.JoinInner})
	outer.Projections = append(outer.Projections, sqlplan.Projection{Expr: value, Alias: block.Projections[orderedIndex].Alias})
	return []sqlplan.QueryBlock{primaryBlock, secondaryBlock}, outer, nil
}

func cloneInputs(inputs []sqlplan.QueryInput) []sqlplan.QueryInput {
	return append([]sqlplan.QueryInput(nil), inputs...)
}

func logicalAnd(terms []sqlplan.Expr) sqlplan.Expr {
	if len(terms) == 1 {
		return terms[0]
	}
	return sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
}
