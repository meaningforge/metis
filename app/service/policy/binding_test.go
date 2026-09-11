package policy

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

func bindingQuery(source string, datatype ossie.DataType) map[string]*resolver.SemanticQuerySpec {
	fields := map[string]*manifest.FieldHandle{}
	for _, name := range []string{"amount", "tenant_id", "secret"} {
		fields["orders."+name] = &manifest.FieldHandle{Dataset: "orders", Field: &ossie.Field{Name: name, Datatype: datatype}}
	}
	return map[string]*resolver.SemanticQuerySpec{"sales": {
		Project: "project", Model: &manifest.ModelIndex{
			Model: &ossie.SemanticModel{Name: "sales"}, Fields: fields,
			Datasets: map[string]*ossie.Dataset{"orders": {Name: "orders"}},
		},
		FieldExpressions: map[string]expression.ResolvedExpression{
			"orders.tenant_id": expression.NewResolvedExpression("ANSI_SQL", source),
			"orders.amount":    expression.NewResolvedExpression("ANSI_SQL", "amount"),
			"orders.secret":    expression.NewResolvedExpression("ANSI_SQL", "secret"),
		},
	}}
}

func snapshotFor(t *testing.T, decision Decision) Snapshot {
	t.Helper()
	s, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) { return decision, nil }), workload())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBindDirectSourceColumn(t *testing.T) {
	for _, source := range []string{"tenant_id", "orders.tenant_id", "\"orders\".\"tenant_id\"", "physical_tenant", "\"physical tenant\""} {
		t.Run(source, func(t *testing.T) {
			snapshot := snapshotFor(t, constrained())
			bound, err := Bind(snapshot, workload(), bindingQuery(source, ossie.DataTypeString))
			if err != nil || len(bound) != 1 || len(bound[0].Predicates) != 1 {
				t.Fatalf("binding failed: %v", err)
			}
			p := bound[0].Predicates[0]
			if p.Values[0] != "private-tenant" || p.Datatype != ossie.DataTypeString || p.Field.Field != "tenant_id" {
				t.Fatal("binding lost typed evidence")
			}
			bound[0].Predicates[0].Values[0] = "changed"
			if snapshot.Constraints()[0].RowPredicates[0].Values[0] != "private-tenant" {
				t.Fatal("bound result aliases snapshot")
			}
		})
	}
}

func TestBindRejectsNonLocalAndComputedFields(t *testing.T) {
	for _, source := range []string{
		"upper(tenant_id)", "cast(tenant_id as varchar)", "tenant_id + 1", "orders.tenant_id + 1",
		"customers.tenant_id", "catalog.orders.tenant_id", "tenant_id[0]", "tenant_id::String",
		"'tenant_id'", "1", "(select tenant_id)", "secret", "orders.secret", "tenant_id; select 1", "",
	} {
		t.Run(source, func(t *testing.T) {
			bound, err := Bind(snapshotFor(t, constrained()), workload(), bindingQuery(source, ossie.DataTypeString))
			if err != ErrInvalid || bound != nil {
				t.Fatalf("unsafe expression accepted: %v", err)
			}
		})
	}
}

func TestBindUnknownIdentitiesAndSnapshotMismatch(t *testing.T) {
	snapshot := snapshotFor(t, constrained())
	mutations := []func(map[string]*resolver.SemanticQuerySpec){
		func(q map[string]*resolver.SemanticQuerySpec) { delete(q, "sales") },
		func(q map[string]*resolver.SemanticQuerySpec) { q["sales"].Project = "other" },
		func(q map[string]*resolver.SemanticQuerySpec) { q["sales"].Model = nil },
		func(q map[string]*resolver.SemanticQuerySpec) { q["sales"].Model.Model.Name = "other" },
		func(q map[string]*resolver.SemanticQuerySpec) { delete(q["sales"].Model.Datasets, "orders") },
		func(q map[string]*resolver.SemanticQuerySpec) { delete(q["sales"].Model.Fields, "orders.amount") },
		func(q map[string]*resolver.SemanticQuerySpec) { delete(q["sales"].Model.Fields, "orders.secret") },
		func(q map[string]*resolver.SemanticQuerySpec) { delete(q["sales"].Model.Fields, "orders.tenant_id") },
		func(q map[string]*resolver.SemanticQuerySpec) { q["sales"].FieldExpressions = nil },
		func(q map[string]*resolver.SemanticQuerySpec) {
			q["sales"].Model.Fields["orders.tenant_id"].Dataset = "other"
		},
	}
	for i, mutate := range mutations {
		q := bindingQuery("tenant_id", ossie.DataTypeString)
		mutate(q)
		if result, err := Bind(snapshot, workload(), q); err != ErrInvalid || result != nil {
			t.Fatalf("mutation %d accepted", i)
		}
	}
	r := workload()
	r.Principal.SubjectID = "other"
	if _, err := Bind(snapshot, r, bindingQuery("tenant_id", ossie.DataTypeString)); err != ErrInvalid {
		t.Fatal("snapshot mismatch accepted")
	}
	if _, err := Bind(Snapshot{}, workload(), nil); err != ErrInvalid {
		t.Fatal("zero snapshot accepted")
	}
	if result, err := Bind(snapshotFor(t, Decision{Effect: Unrestricted}), workload(), bindingQuery("tenant_id", ossie.DataTypeString)); err != nil || result != nil {
		t.Fatal("unrestricted binding failed")
	}
}

func TestBindRejectsOmittedTransitiveWorkload(t *testing.T) {
	q := bindingQuery("tenant_id", ossie.DataTypeString)
	q["sales"].FieldExpressions["orders.amount"] = expression.NewResolvedExpression("ANSI_SQL", "secret * 2")
	if bound, err := Bind(snapshotFor(t, constrained()), workload(), q); err != ErrInvalid || bound != nil {
		t.Fatal("adapter request omitted computed field dependency but binding succeeded")
	}
}

func TestBindPolicyFieldTypes(t *testing.T) {
	tests := []struct {
		datatype ossie.DataType
		value    any
		valid    bool
	}{
		{ossie.DataTypeString, "tenant", true}, {ossie.DataTypeString, int64(1), false},
		{ossie.DataTypeInteger, int64(9007199254740993), true}, {ossie.DataTypeInteger, 1.0, false},
		{ossie.DataTypeDecimal, int64(10), true}, {ossie.DataTypeDecimal, 1.5, true},
		{ossie.DataTypeFloat, 1.5, true}, {ossie.DataTypeFloat, "1.5", false},
		{ossie.DataTypeBoolean, true, true}, {ossie.DataTypeBoolean, "true", false},
		{ossie.DataTypeDate, "2026-09-09", true}, {ossie.DataTypeDate, "2026-02-30", false},
		{ossie.DataTypeTime, "12:30:45.123", true}, {ossie.DataTypeTime, "25:00:00", false},
		{ossie.DataTypeDateTime, "2026-09-09T12:30:45.123", true},
		{ossie.DataTypeDateTime, "2026-09-09", false},
		{ossie.DataTypeDateTimeTz, "2026-09-09T12:30:45+08:00", true},
		{ossie.DataTypeDateTimeTz, "2026-09-09T12:30:45", false},
		{ossie.DataTypeOpaque, "tenant", false}, {"", "tenant", false},
	}
	for i, tt := range tests {
		d := constrained()
		d.Sources[0].RowPredicates[0].Values = []any{tt.value}
		_, err := Bind(snapshotFor(t, d), workload(), bindingQuery("tenant_id", tt.datatype))
		if (err == nil) != tt.valid {
			t.Errorf("case %d type %s: %v", i, tt.datatype, err)
		}
	}
	d := constrained()
	p := &d.Sources[0].RowPredicates[0]
	p.Operator = query.FilterIsNull
	p.Values = nil
	if _, err := Bind(snapshotFor(t, d), workload(), bindingQuery("tenant_id", ossie.DataTypeOpaque)); err != ErrInvalid {
		t.Fatal("null check accepted unknown datatype")
	}
	p.Operator = query.FilterGT
	p.Values = []any{true}
	if _, err := Bind(snapshotFor(t, d), workload(), bindingQuery("tenant_id", ossie.DataTypeBoolean)); err != ErrInvalid {
		t.Fatal("boolean ordering accepted")
	}
}
