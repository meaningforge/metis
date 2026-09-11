package conversion

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
)

// sourceRelation faithfully lowers already-bound relation-input constraints.
// It performs no identity resolution, policy evaluation or predicate relocation.
func sourceRelation(dataset semanticplan.DatasetRef) sqlplan.RelationRef {
	predicates := dataset.Policy.Predicates()
	if len(predicates) == 0 {
		return sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: dataset.Source}, Alias: dataset.Name}
	}
	source := &sqlplan.FilteredTableSource{Name: dataset.Source, Predicates: make([]sqlplan.Predicate, len(predicates))}
	for i, predicate := range predicates {
		source.Predicates[i] = sqlplan.Predicate{Left: sqlplan.ColumnRef{Name: predicate.Column}, Operator: predicate.Operator, Values: predicate.Values}
	}
	return sqlplan.RelationRef{FilteredSource: source, Alias: dataset.Name}
}
