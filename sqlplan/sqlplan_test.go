package sqlplan_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/sqlplan"
)

func TestValidateAcceptsExplicitTopologicalDAG(t *testing.T) {
	if err := sqlplan.Validate(validPlan()); err != nil {
		t.Fatalf("validate plan: %v", err)
	}
}

func TestValidateFailsClosedOnStructuralErrors(t *testing.T) {
	tests := []struct {
		name string
		edit func(*sqlplan.Plan)
		want string
	}{
		{name: "missing root", edit: func(plan *sqlplan.Plan) { plan.Root = "missing" }, want: "does not exist"},
		{name: "duplicate block", edit: func(plan *sqlplan.Plan) { plan.Blocks[1].ID = plan.Blocks[0].ID }, want: "duplicate query block"},
		{name: "missing input", edit: func(plan *sqlplan.Plan) { plan.Blocks[1].Inputs[0].Block = "missing" }, want: "missing block"},
		{name: "non topological input", edit: func(plan *sqlplan.Plan) {
			plan.Blocks[0].Inputs = []sqlplan.QueryInput{{Alias: "later", Block: "root", Mode: sqlplan.QueryInputCTE}}
		}, want: "not topologically earlier"},
		{name: "unreachable block", edit: func(plan *sqlplan.Plan) {
			plan.Blocks = append([]sqlplan.QueryBlock{validSourceBlock("orphan", "other")}, plan.Blocks...)
		}, want: "unreachable"},
		{name: "duplicate input alias", edit: func(plan *sqlplan.Plan) {
			plan.Blocks[1].Inputs = append(plan.Blocks[1].Inputs, plan.Blocks[1].Inputs[0])
		}, want: "duplicate input alias"},
		{name: "invalid relation", edit: func(plan *sqlplan.Plan) { plan.Blocks[0].From.Input = &sqlplan.InputRef{Alias: "x"} }, want: "exactly one"},
		{name: "missing relation input", edit: func(plan *sqlplan.Plan) { plan.Blocks[1].From.Input.Alias = "missing" }, want: "is not declared"},
		{name: "empty projection alias", edit: func(plan *sqlplan.Plan) { plan.Blocks[1].Projections[0].Alias = "" }, want: "empty alias"},
		{name: "unavailable expression input", edit: func(plan *sqlplan.Plan) {
			plan.Blocks[1].Projections[0].Expr = sqlplan.ColumnRef{Table: "missing", Name: "amount"}
		}, want: "unavailable relation alias"},
		{name: "predicate cardinality", edit: func(plan *sqlplan.Plan) { plan.Blocks[1].Predicates[0].Values = nil }, want: "exactly one value"},
		{name: "opaque target mismatch", edit: func(plan *sqlplan.Plan) {
			plan.Blocks[1].Projections[0].Expr = sqlplan.OpaqueExpr{SQL: "amount", Dialect: "SNOWFLAKE"}
		}, want: "incompatible"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := validPlan()
			test.edit(plan)
			err := sqlplan.ValidateForRenderer(plan, "DUCKDB")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsRendererDialectMismatch(t *testing.T) {
	err := sqlplan.ValidateForRenderer(validPlan(), "DORIS")
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("Validate() error = %v, want renderer dialect mismatch", err)
	}
}

func TestCloneOwnsNestedState(t *testing.T) {
	plan := validPlan()
	plan.Blocks[1].Predicates[0].Values[0] = map[string]any{"nested": []any{"secret", map[string]string{"key": "value"}}}
	clone := sqlplan.Clone(plan)

	plan.Blocks[0].From.Source.Name = "mutated"
	plan.Blocks[1].Inputs[0].Alias = "mutated"
	plan.Blocks[1].Projections[0].Expr = sqlplan.ColumnRef{Table: "base", Name: "mutated"}
	plan.Blocks[1].GroupBy[0] = sqlplan.ColumnRef{Table: "base", Name: "mutated"}
	plan.Blocks[1].OrderBy[0].Expr = sqlplan.ColumnRef{Table: "base", Name: "mutated"}
	*plan.Blocks[1].Limit = 99
	nested := plan.Blocks[1].Predicates[0].Values[0].(map[string]any)["nested"].([]any)
	nested[0] = "mutated"
	nested[1].(map[string]string)["key"] = "mutated"

	if clone.Blocks[0].From.Source.Name != "analytics.orders" || clone.Blocks[1].Inputs[0].Alias != "base" || *clone.Blocks[1].Limit != 10 {
		t.Fatalf("clone shares plan structure with caller: %#v", clone)
	}
	clonedNested := clone.Blocks[1].Predicates[0].Values[0].(map[string]any)["nested"].([]any)
	if clonedNested[0] != "secret" || clonedNested[1].(map[string]string)["key"] != "value" {
		t.Fatalf("clone shares nested predicate state: %#v", clonedNested)
	}
}

func TestCanonicalProjectionOwnsPointerBackedState(t *testing.T) {
	plan := validPlan()
	projection, err := sqlplan.Project(plan)
	if err != nil {
		t.Fatalf("project plan: %v", err)
	}
	*plan.Blocks[1].Limit = 99
	if projection.Blocks[1].Limit == nil || *projection.Blocks[1].Limit != 10 {
		t.Fatalf("canonical projection shares limit pointer: %#v", projection.Blocks[1].Limit)
	}
}

func TestCanonicalProjectionRejectsUnmodeledPredicateValueTypes(t *testing.T) {
	plan := validPlan()
	plan.Blocks[1].Predicates[0].Values[0] = make(chan int)
	_, err := sqlplan.Project(plan)
	if err == nil || !strings.Contains(err.Error(), "unsupported predicate value type") {
		t.Fatalf("Project() error = %v, want explicit unsupported-type failure", err)
	}
}

func TestFingerprintMovesForCanonicalFields(t *testing.T) {
	base := validPlan()
	baseline, err := sqlplan.Fingerprint(base)
	if err != nil {
		t.Fatalf("fingerprint baseline: %v", err)
	}

	mutations := map[string]func(*sqlplan.Plan){
		"root and block identity": func(plan *sqlplan.Plan) { plan.Root = "output"; plan.Blocks[1].ID = "output" },
		"input alias": func(plan *sqlplan.Plan) {
			plan.Blocks[1].Inputs[0].Alias = "source_alias"
			plan.Blocks[1].From.Input.Alias = "source_alias"
		},
		"input block": func(plan *sqlplan.Plan) { plan.Blocks[0].ID = "source_2"; plan.Blocks[1].Inputs[0].Block = "source_2" },
		"input mode":  func(plan *sqlplan.Plan) { plan.Blocks[1].Inputs[0].Mode = sqlplan.QueryInputDerivedTable },
		"source name": func(plan *sqlplan.Plan) { plan.Blocks[0].From.Source.Name = "analytics.orders_v2" },
		"relation alias": func(plan *sqlplan.Plan) {
			plan.Blocks[0].From.Alias = "orders_2"
			plan.Blocks[0].Projections[0].Expr = sqlplan.ColumnRef{Table: "orders_2", Name: "amount"}
		},
		"projection alias": func(plan *sqlplan.Plan) { plan.Blocks[1].Projections[0].Alias = "revenue" },
		"projection expression": func(plan *sqlplan.Plan) {
			plan.Blocks[1].Projections[0].Expr = sqlplan.OpaqueExpr{SQL: "base.amount + 1", Dialect: "DUCKDB"}
		},
		"predicate operator": func(plan *sqlplan.Plan) { plan.Blocks[1].Predicates[0].Operator = query.FilterGTE },
		"predicate value":    func(plan *sqlplan.Plan) { plan.Blocks[1].Predicates[0].Values[0] = 101 },
		"group expression": func(plan *sqlplan.Plan) {
			plan.Blocks[1].GroupBy[0] = sqlplan.OpaqueExpr{SQL: "base.amount + 0", Dialect: "DUCKDB"}
		},
		"order expression": func(plan *sqlplan.Plan) {
			plan.Blocks[1].OrderBy[0].Expr = sqlplan.OpaqueExpr{SQL: "base.amount + 0", Dialect: "DUCKDB"}
		},
		"order direction": func(plan *sqlplan.Plan) { plan.Blocks[1].OrderBy[0].Direction = query.SortAsc },
		"limit":           func(plan *sqlplan.Plan) { *plan.Blocks[1].Limit = 11 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			plan := sqlplan.Clone(base)
			mutate(plan)
			got, err := sqlplan.Fingerprint(plan)
			if err != nil {
				t.Fatalf("fingerprint mutation: %v", err)
			}
			if got == baseline {
				t.Fatal("canonical field mutation did not move fingerprint")
			}
		})
	}
}

func TestExplainIsDeterministicAndRedactsValues(t *testing.T) {
	plan := validPlan()
	plan.Blocks[1].Predicates[0].Values[0] = "do-not-print"
	first, err := sqlplan.Explain(plan)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	second, err := sqlplan.Explain(plan)
	if err != nil {
		t.Fatalf("explain again: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("explanation is nondeterministic:\n%s\n%s", firstJSON, secondJSON)
	}
	if strings.Contains(string(firstJSON), "do-not-print") || !strings.Contains(string(firstJSON), "redacted") {
		t.Fatalf("explanation did not redact predicate values: %s", firstJSON)
	}
}

func validPlan() *sqlplan.Plan {
	limit := 10
	return &sqlplan.Plan{
		Root: "root",
		Blocks: []sqlplan.QueryBlock{
			validSourceBlock("source", "orders"),
			{
				ID:          "root",
				Inputs:      []sqlplan.QueryInput{{Alias: "base", Block: "source", Mode: sqlplan.QueryInputCTE}},
				From:        sqlplan.RelationRef{Input: &sqlplan.InputRef{Alias: "base"}, Alias: "base"},
				Projections: []sqlplan.Projection{{Expr: sqlplan.OpaqueExpr{SQL: "base.amount", Dialect: "DUCKDB"}, Alias: "amount"}},
				Predicates:  []sqlplan.Predicate{{Left: sqlplan.ColumnRef{Table: "base", Name: "amount"}, Operator: query.FilterGT, Values: []any{100}}},
				GroupBy:     []sqlplan.Expr{sqlplan.ColumnRef{Table: "base", Name: "amount"}},
				OrderBy:     []sqlplan.Order{{Expr: sqlplan.ColumnRef{Table: "base", Name: "amount"}, Direction: query.SortDesc}},
				Limit:       &limit,
			},
		},
	}
}

func validSourceBlock(id sqlplan.QueryBlockID, alias string) sqlplan.QueryBlock {
	return sqlplan.QueryBlock{
		ID:          id,
		From:        sqlplan.RelationRef{Source: &sqlplan.TableSource{Name: "analytics.orders"}, Alias: alias},
		Projections: []sqlplan.Projection{{Expr: sqlplan.ColumnRef{Table: alias, Name: "amount"}, Alias: "amount"}},
	}
}
