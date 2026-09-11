package duckdb

import (
	"fmt"

	"github.com/meaningforge/metis/sqlplan"
)

const (
	orderedKeyCTE     = "__metis_ordered_key"
	orderedValueAlias = "__metis_ordered_value"
)

func lowerOrderedValuePlan(plan *sqlplan.Plan) (*sqlplan.Plan, error) {
	owned := sqlplan.Clone(plan)
	blocks := make([]sqlplan.QueryBlock, 0, len(owned.Blocks))
	for _, block := range owned.Blocks {
		prefix, rewritten, err := lowerOrderedValueBlock(block)
		if err != nil {
			return nil, fmt.Errorf("query block %q: %w", block.ID, err)
		}
		blocks = append(blocks, prefix...)
		blocks = append(blocks, rewritten)
	}
	owned.Blocks = blocks
	return owned, nil
}

func lowerOrderedValueBlock(block sqlplan.QueryBlock) ([]sqlplan.QueryBlock, sqlplan.QueryBlock, error) {
	orderedIndex := -1
	var value, orderBy sqlplan.Expr
	aggregate := ""
	for i, item := range block.Projections {
		switch expr := item.Expr.(type) {
		case sqlplan.LatestValueExpr:
			if expr.TieBreak != nil {
				return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic latest-value expression must be lowered before DuckDB single-key lowering")
			}
			if orderedIndex >= 0 {
				return nil, sqlplan.QueryBlock{}, fmt.Errorf("DuckDB ordered-value lowering supports one ordered-value expression per block")
			}
			orderedIndex, value, orderBy, aggregate = i, expr.Value, expr.OrderBy, "MAX"
		case sqlplan.EarliestValueExpr:
			if expr.TieBreak != nil {
				return nil, sqlplan.QueryBlock{}, fmt.Errorf("deterministic earliest-value expression must be lowered before DuckDB single-key lowering")
			}
			if orderedIndex >= 0 {
				return nil, sqlplan.QueryBlock{}, fmt.Errorf("DuckDB ordered-value lowering supports one ordered-value expression per block")
			}
			orderedIndex, value, orderBy, aggregate = i, expr.Value, expr.OrderBy, "MIN"
		}
	}
	if orderedIndex < 0 {
		return nil, block, nil
	}
	if len(block.Joins) != 0 {
		return nil, sqlplan.QueryBlock{}, fmt.Errorf("DuckDB ordered-value lowering does not support pre-existing joins")
	}
	if len(block.Predicates) != 0 {
		return nil, sqlplan.QueryBlock{}, fmt.Errorf("DuckDB ordered-value lowering does not support block-local predicates")
	}

	keyID := sqlplan.QueryBlockID(string(block.ID) + "__ordered_key")
	keyBlock := sqlplan.QueryBlock{ID: keyID, Inputs: append([]sqlplan.QueryInput(nil), block.Inputs...), From: block.From}
	outer := block
	outer.Projections = nil
	outer.GroupBy = nil
	outer.Joins = nil
	joinTerms := make([]sqlplan.Expr, 0, len(block.GroupBy)+1)
	for i, item := range block.Projections {
		if i == orderedIndex {
			continue
		}
		column, ok := item.Expr.(sqlplan.ColumnRef)
		if !ok {
			return nil, sqlplan.QueryBlock{}, fmt.Errorf("DuckDB ordered-value grouping projection must be a column reference")
		}
		keyBlock.Projections = append(keyBlock.Projections, sqlplan.Projection{Expr: column, Alias: item.Alias})
		keyBlock.GroupBy = append(keyBlock.GroupBy, column)
		outer.Projections = append(outer.Projections, sqlplan.Projection{Expr: column, Alias: item.Alias})
		joinTerms = append(joinTerms, sqlplan.BinaryExpr{Left: column, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedKeyCTE, Name: item.Alias}})
	}
	keyBlock.Projections = append(keyBlock.Projections, sqlplan.Projection{Expr: sqlplan.FunctionCallExpr{Name: aggregate, Args: []sqlplan.Expr{orderBy}}, Alias: orderedValueAlias})
	joinTerms = append(joinTerms, sqlplan.BinaryExpr{Left: orderBy, Operator: "=", Right: sqlplan.ColumnRef{Table: orderedKeyCTE, Name: orderedValueAlias}})
	outer.Inputs = append(outer.Inputs, sqlplan.QueryInput{Alias: orderedKeyCTE, Block: keyID, Mode: sqlplan.QueryInputCTE})
	outer.Joins = append(outer.Joins, sqlplan.Join{Relation: sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: orderedKeyCTE}, Alias: orderedKeyCTE}, On: logicalAnd(joinTerms), Kind: sqlplan.JoinInner})
	outer.Projections = append(outer.Projections, sqlplan.Projection{Expr: value, Alias: block.Projections[orderedIndex].Alias})
	return []sqlplan.QueryBlock{keyBlock}, outer, nil
}

func logicalAnd(terms []sqlplan.Expr) sqlplan.Expr {
	if len(terms) == 1 {
		return terms[0]
	}
	return sqlplan.LogicalExpr{Operator: "AND", Terms: terms}
}
