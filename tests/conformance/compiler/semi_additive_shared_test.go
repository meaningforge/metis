package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestSemiAdditiveLatestSnapshotDialectLowering(t *testing.T) {
	scenario, ok := scenarios.ByName("semi_additive_latest_snapshot")
	if !ok {
		t.Fatal("semi-additive shared scenario is not registered")
	}

	_, duckdb, err := compile(t, scenario.Query, "DUCKDB")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToUpper(duckdb.SQL), "MAX_BY(") || strings.Contains(strings.ToLower(duckdb.SQL), "argmax(") {
		t.Fatalf("DuckDB SQL must use structural latest-key lowering, not engine-specific latest-value functions:\n%s", duckdb.SQL)
	}
	for _, fragment := range []string{"MAX(", "__metis_ordered_key", "JOIN", "snapshot_date", "inventory_quantity"} {
		if !strings.Contains(duckdb.SQL, fragment) {
			t.Fatalf("DuckDB SQL missing %q:\n%s", fragment, duckdb.SQL)
		}
	}

	_, doris, err := compile(t, scenario.Query, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(doris.SQL), "MAX_BY(") {
		t.Fatalf("Doris SQL must use MAX_BY latest-value lowering:\n%s", doris.SQL)
	}

	_, clickhouse, err := compile(t, scenario.Query, "CLICKHOUSE")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(clickhouse.SQL), "argmax(") {
		t.Fatalf("ClickHouse SQL must use argMax latest-value lowering:\n%s", clickhouse.SQL)
	}
}

func TestSemiAdditiveTimeFiltersRestrictCandidateSnapshotsBeforeLatestSelection(t *testing.T) {
	cases := []struct {
		name           string
		op             query.FilterOperator
		value          any
		parameterCount int
	}{
		{name: "as_of", op: query.FilterLTE, value: "2026-06-30", parameterCount: 1},
		{name: "since", op: query.FilterGTE, value: "2026-01-01", parameterCount: 1},
		{name: "between", op: query.FilterBetween, value: []string{"2026-01-01", "2026-06-30"}, parameterCount: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := query.SemanticQuery{Metrics: []query.MetricRef{{Name: "inventory_balance"}}, Dimensions: []query.DimensionRef{{Name: "warehouse"}}, Filters: []query.Filter{{Field: "snapshot_date", Operator: tc.op, Value: tc.value}}}
			plan, sqlQuery, err := compile(t, q, "DORIS")
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Nodes) != 2 {
				t.Fatalf("nodes = %#v", plan.Nodes)
			}
			base, ok := plan.Nodes[0].(semanticplan.SourceAggregateNode)
			if !ok {
				t.Fatalf("base node = %T, want SourceAggregateNode", plan.Nodes[0])
			}
			latest, ok := plan.Nodes[1].(semanticplan.SemiAdditiveNode)
			if !ok || latest.Kind() != semanticplan.SemanticPlanNodeSemiAdditiveLast {
				t.Fatalf("semi-additive node = %#v", plan.Nodes[1])
			}
			predicates := base.NodeBase().Predicates
			if len(predicates) != 1 || predicates[0].Predicate == nil || predicates[0].Predicate.Filter.Field != "snapshot_date" || predicates[0].Predicate.Filter.Operator != tc.op {
				t.Fatalf("base predicates = %#v, want candidate-set time predicate", predicates)
			}
			if len(plan.Output.Predicates) != 0 {
				t.Fatalf("post predicates = %#v, semi-additive time filters must not run after latest selection", plan.Output.Predicates)
			}
			if len(sqlQuery.Parameters) != tc.parameterCount {
				t.Fatalf("parameters = %d, want %d", len(sqlQuery.Parameters), tc.parameterCount)
			}
			whereIndex := strings.Index(sqlQuery.SQL, "WHERE")
			latestIndex := strings.Index(strings.ToUpper(sqlQuery.SQL), "MAX_BY(")
			if whereIndex < 0 || latestIndex < 0 || whereIndex > latestIndex {
				t.Fatalf("candidate snapshot WHERE must precede latest-value selection:\n%s", sqlQuery.SQL)
			}
		})
	}
}
