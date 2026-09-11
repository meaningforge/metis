package compiler_test

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestP0InvalidQueryContracts(t *testing.T) {
	zero := 0
	cases := []struct {
		name string
		q    query.SemanticQuery
	}{
		{name: "empty query", q: query.SemanticQuery{}},
		{name: "non-positive limit", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Limit: &zero}},
		{name: "invalid order direction", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, OrderBy: []query.OrderBy{{Field: "revenue", Direction: query.SortDirection("sideways")}}}},
		{name: "between with wrong arity", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Filters: []query.Filter{{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-01-01"}}}}},
		{name: "in with empty list", q: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}, Filters: []query.Filter{{Field: "status", Operator: query.FilterIN, Value: []string{}}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := compile(t, tc.q, "DORIS")
			var apiErr *serrors.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %#v, want typed serrors.Error", err)
			}
			if apiErr.Code != serrors.ErrInvalidQuery {
				t.Fatalf("error code = %s, want %s", apiErr.Code, serrors.ErrInvalidQuery)
			}
		})
	}
}
