package policy

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

func TestFieldClosureExpandsTransitiveFieldsAndRetainsIdentity(t *testing.T) {
	q := bindingQuery("tenant_id", ossie.DataTypeString)["sales"]
	q.FieldExpressions["orders.amount"] = expression.NewResolvedExpression("ANSI_SQL", "secret * 2")
	q.FieldExpressions["orders.secret"] = expression.NewResolvedExpression("ANSI_SQL", "raw_secret")
	seed := workload().Sources[0].RequiredFields[0]
	got, err := FieldClosure(q, []FieldRef{seed, seed})
	want := []FieldRef{seed, {Dataset: seed.Dataset, Field: "secret"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("closure: %v %v", got, err)
	}
	// A metric/projected alias must not erase the denied canonical dependency.
	request := workload()
	request.Sources[0].RequiredFields = got
	if err := validateDecision(request, constrained()); err != ErrDenied {
		t.Fatal("transitive deny was not enforced")
	}
}

func TestFieldClosureCrossSourceAndCycles(t *testing.T) {
	q := bindingQuery("tenant_id", ossie.DataTypeString)["sales"]
	q.Model.Datasets["customers"] = &ossie.Dataset{Name: "customers"}
	q.Model.Fields["customers.factor"] = &manifest.FieldHandle{Dataset: "customers", Field: &ossie.Field{Name: "factor"}}
	q.FieldExpressions["customers.factor"] = expression.NewResolvedExpression("ANSI_SQL", "factor")
	q.FieldExpressions["orders.amount"] = expression.NewResolvedExpression("ANSI_SQL", "customers.factor * 2")
	seed := workload().Sources[0].RequiredFields[0]
	got, err := FieldClosure(q, []FieldRef{seed})
	if err != nil || len(got) != 2 || got[0].Dataset.Dataset != "customers" {
		t.Fatalf("cross-source dependency: %v", err)
	}
	q.FieldExpressions["customers.factor"] = expression.NewResolvedExpression("ANSI_SQL", "orders.amount")
	if got, err := FieldClosure(q, []FieldRef{seed}); err != ErrInvalid || got != nil {
		t.Fatal("cyclic closure returned evidence")
	}
	q.FieldExpressions["orders.amount"] = expression.NewResolvedExpression("ANSI_SQL", "customers.unregistered")
	if _, err := FieldClosure(q, []FieldRef{seed}); err != ErrInvalid {
		t.Fatal("unregistered cross-source column accepted")
	}
}

func TestFieldClosureFailsOnUnprovableEvidence(t *testing.T) {
	seed := workload().Sources[0].RequiredFields[0]
	for _, source := range []string{"", "other.amount", "catalog.orders.amount", "amount[0]", "bad ("} {
		q := bindingQuery("tenant_id", ossie.DataTypeString)["sales"]
		q.FieldExpressions["orders.amount"] = expression.NewResolvedExpression("ANSI_SQL", source)
		if got, err := FieldClosure(q, []FieldRef{seed}); err != ErrInvalid || got != nil {
			t.Fatalf("unprovable expression accepted: %s", source)
		}
	}
	if _, err := FieldClosure(nil, []FieldRef{seed}); err != ErrInvalid {
		t.Fatal("nil query accepted")
	}
	q := bindingQuery("tenant_id", ossie.DataTypeString)["sales"]
	if _, err := FieldClosure(q, make([]FieldRef, MaxFields+1)); err != ErrInvalid {
		t.Fatal("oversized seeds accepted")
	}
	seed.Dataset.Model = "other"
	if _, err := FieldClosure(q, []FieldRef{seed}); err != ErrInvalid {
		t.Fatal("foreign model accepted")
	}
}
