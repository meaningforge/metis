package policy

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/query"
)

type policyFunc func(context.Context, Request) (Decision, error)

func (f policyFunc) Evaluate(ctx context.Context, req Request) (Decision, error) { return f(ctx, req) }

func workload() Request {
	dataset := DatasetRef{Model: "sales", Dataset: "orders"}
	return Request{
		Principal: auth.Principal{TenantID: "tenant", SubjectID: "subject", APIKeyID: "key", Scopes: []string{"semantic:execute"}},
		ProjectID: "project",
		Sources:   []Source{{Dataset: dataset, RequiredFields: []FieldRef{{Dataset: dataset, Field: "amount"}}}},
	}
}
func constrained() Decision {
	dataset := workload().Sources[0].Dataset
	return Decision{Effect: Constrained, Sources: []SourceConstraint{{Dataset: dataset,
		RowPredicates: []Predicate{{Field: FieldRef{Dataset: dataset, Field: "tenant_id"}, Operator: query.FilterEQ, Values: []any{"private-tenant"}}},
		DeniedFields:  []FieldRef{{Dataset: dataset, Field: "secret"}},
	}}}
}

func TestDecisionConjunctionOrderDoesNotChangeScope(t *testing.T) {
	d := constrained()
	d.Sources[0].RowPredicates = append(d.Sources[0].RowPredicates, Predicate{Field: FieldRef{Dataset: d.Sources[0].Dataset, Field: "region"}, Operator: query.FilterEQ, Values: []any{"west"}})
	a, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) { return d, nil }), workload())
	if err != nil {
		t.Fatal(err)
	}
	d.Sources[0].RowPredicates[0], d.Sources[0].RowPredicates[1] = d.Sources[0].RowPredicates[1], d.Sources[0].RowPredicates[0]
	b, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) { return d, nil }), workload())
	if err != nil {
		t.Fatal(err)
	}
	if a.ScopeID() != b.ScopeID() || !reflect.DeepEqual(a.Constraints(), b.Constraints()) {
		t.Fatal("adapter ordering changed canonical scope")
	}
}

func TestEvaluateCallsOnceAndOwnsSnapshot(t *testing.T) {
	req, decision := workload(), constrained()
	original := cloneRequest(req)
	calls := 0
	snapshot, err := Evaluate(context.Background(), policyFunc(func(_ context.Context, got Request) (Decision, error) {
		calls++
		if !reflect.DeepEqual(got, original) {
			t.Fatal("adapter did not receive exact workload")
		}
		got.Principal.Scopes[0] = "*"
		got.Sources[0].RequiredFields[0].Field = "mutated"
		return decision, nil
	}), req)
	if err != nil || calls != 1 || snapshot.Effect() != Constrained || !snapshot.Matches(original) {
		t.Fatalf("evaluation: calls=%d effect=%s error=%v", calls, snapshot.Effect(), err)
	}
	if !reflect.DeepEqual(req, original) {
		t.Fatal("adapter mutated caller request")
	}
	decision.Sources[0].RowPredicates[0].Values[0] = "changed"
	decision.Sources[0].DeniedFields[0].Field = "changed"
	decision.Sources[0].Dataset.Dataset = "changed"
	first := snapshot.Constraints()
	if !reflect.DeepEqual(first, constrained().Sources) {
		t.Fatal("adapter mutated snapshot")
	}
	first[0].RowPredicates[0].Values[0] = "consumer-change"
	first[0].DeniedFields[0].Field = "consumer-change"
	if !reflect.DeepEqual(snapshot.Constraints(), constrained().Sources) {
		t.Fatal("consumer mutated snapshot")
	}
	req.Principal.Scopes[0] = "*"
	if snapshot.Matches(req) || !snapshot.Matches(original) {
		t.Fatal("principal scope mismatch accepted")
	}
	req = cloneRequest(original)
	req.ProjectID = "other"
	if snapshot.Matches(req) {
		t.Fatal("project mismatch accepted")
	}
	req = cloneRequest(original)
	req.Sources[0].RequiredFields[0].Field = "other"
	if snapshot.Matches(req) {
		t.Fatal("workload mismatch accepted")
	}
	if (Snapshot{}).Matches(original) {
		t.Fatal("zero snapshot accepted")
	}
}

func TestDecisionValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Decision)
		want   error
	}{
		{"valid", func(*Decision) {}, nil},
		{"unknown effect", func(d *Decision) { d.Effect = "allow" }, ErrInvalid},
		{"missing source", func(d *Decision) { d.Sources = nil }, ErrInvalid},
		{"extra source", func(d *Decision) { d.Sources = append(d.Sources, d.Sources[0]) }, ErrInvalid},
		{"foreign source", func(d *Decision) { d.Sources[0].Dataset.Model = "foreign" }, ErrInvalid},
		{"foreign predicate", func(d *Decision) { d.Sources[0].RowPredicates[0].Field.Dataset.Model = "foreign" }, ErrInvalid},
		{"empty field", func(d *Decision) { d.Sources[0].RowPredicates[0].Field.Field = "" }, ErrInvalid},
		{"foreign deny", func(d *Decision) { d.Sources[0].DeniedFields[0].Dataset.Model = "foreign" }, ErrInvalid},
		{"duplicate deny", func(d *Decision) {
			d.Sources[0].DeniedFields = append(d.Sources[0].DeniedFields, d.Sources[0].DeniedFields[0])
		}, ErrInvalid},
		{"required field denied", func(d *Decision) { d.Sources[0].DeniedFields[0].Field = "amount" }, ErrDenied},
		{"policy field denied", func(d *Decision) { d.Sources[0].DeniedFields[0].Field = "tenant_id" }, ErrInvalid},
		{"explicit denial", func(d *Decision) { *d = Decision{Effect: Denied} }, ErrDenied},
		{"denial with constraints", func(d *Decision) { d.Effect = Denied }, ErrInvalid},
		{"explicit unrestricted", func(d *Decision) { *d = Decision{Effect: Unrestricted} }, nil},
		{"unrestricted with constraints", func(d *Decision) { d.Effect = Unrestricted }, ErrInvalid},
		{"source explicitly unrestricted", func(d *Decision) { d.Sources[0].RowPredicates = nil; d.Sources[0].DeniedFields = nil }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := constrained()
			tt.change(&d)
			snapshot, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) { return d, nil }), workload())
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v want %v", err, tt.want)
			}
			if err != nil && snapshot.Matches(workload()) {
				t.Fatal("failure released snapshot")
			}
		})
	}
}

func TestClosedPredicateDomain(t *testing.T) {
	tests := []struct {
		op     query.FilterOperator
		values []any
		valid  bool
	}{
		{query.FilterEQ, []any{"literal ' OR 1=1 --"}, true},
		{query.FilterNEQ, []any{int64(9007199254740993)}, true},
		{query.FilterGT, []any{1.25}, true}, {query.FilterGTE, []any{int64(2)}, true},
		{query.FilterLT, []any{"2026-01-01"}, true}, {query.FilterLTE, []any{int64(2)}, true},
		{query.FilterEQ, []any{true}, true},
		{query.FilterIN, []any{"a", "b"}, true}, {query.FilterNotIn, []any{int64(1)}, true},
		{query.FilterBetween, []any{int64(1), int64(2)}, true},
		{query.FilterIsNull, nil, true}, {query.FilterIsNotNull, nil, true},
		{query.FilterIsNull, []any{nil}, false}, {query.FilterIsNotNull, []any{false}, false},
		{query.FilterIN, nil, false}, {query.FilterNotIn, nil, false},
		{query.FilterBetween, []any{int64(1)}, false},
		{query.FilterBetween, []any{int64(1), int64(2), int64(3)}, false},
		{query.FilterEQ, nil, false}, {query.FilterEQ, []any{"a", "b"}, false},
		{query.FilterEQ, []any{nil}, false}, {query.FilterEQ, []any{math.NaN()}, false},
		{query.FilterEQ, []any{math.Inf(1)}, false}, {query.FilterEQ, []any{math.Inf(-1)}, false},
		{query.FilterEQ, []any{[]any{"nested"}}, false},
		{query.FilterEQ, []any{map[string]string{"sql": "injected"}}, false},
		{query.FilterEQ, []any{new(string)}, false}, {query.FilterEQ, []any{int(1)}, false},
		{query.FilterIN, []any{int64(1), float64(2)}, false}, {"sql", []any{"x"}, false},
	}
	for i, tt := range tests {
		d := constrained()
		d.Sources[0].RowPredicates[0].Operator = tt.op
		d.Sources[0].RowPredicates[0].Values = tt.values
		_, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) { return d, nil }), workload())
		if (err == nil) != tt.valid {
			t.Errorf("case %d operator %s: valid=%v err=%v", i, tt.op, tt.valid, err)
		}
	}
}

func TestFailuresAreRedactedAndTerminal(t *testing.T) {
	secret := "private entitlement: tenant_id = secret"
	var typedNil policyFunc
	for name, adapter := range map[string]DataAccessPolicy{
		"nil": nil, "typed nil": typedNil,
		"error": policyFunc(func(context.Context, Request) (Decision, error) { return constrained(), errors.New(secret) }),
		"panic": policyFunc(func(context.Context, Request) (Decision, error) { panic(secret) }),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Evaluate(context.Background(), adapter, workload())
			if err != ErrUnavailable || s.Matches(workload()) || strings.Contains(err.Error(), secret) || errors.Unwrap(err) != nil {
				t.Fatal("failure was not closed and redacted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	adapter := policyFunc(func(context.Context, Request) (Decision, error) { calls++; return Decision{Effect: Unrestricted}, nil })
	for _, ctx := range []context.Context{ctx, nil} {
		if _, err := Evaluate(ctx, adapter, workload()); err != ErrUnavailable {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("called adapter for cancelled/nil context")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, err := Evaluate(ctx, policyFunc(func(context.Context, Request) (Decision, error) { cancel(); return constrained(), nil }), workload())
	if err != ErrUnavailable {
		t.Fatal("accepted successful result after cancellation")
	}
	if s, err := Evaluate(context.Background(), NoRestrictionDataAccessPolicy{}, workload()); err != nil || s.Effect() != Unrestricted {
		t.Fatal("explicit compatibility adapter failed")
	}
}

func TestInvalidRequestsNeverReachAdapter(t *testing.T) {
	changes := []func(*Request){
		func(r *Request) { r.ProjectID = "" }, func(r *Request) { r.ProjectID = " project " },
		func(r *Request) { r.Principal.SubjectID = "" }, func(r *Request) { r.Principal.TenantID = "" },
		func(r *Request) { r.Principal.Scopes = make([]string, MaxScopes+1) },
		func(r *Request) { r.Sources = nil }, func(r *Request) { r.Sources = make([]Source, MaxSources+1) },
		func(r *Request) { r.Sources = append(r.Sources, r.Sources[0]) },
		func(r *Request) { r.Sources[0].Dataset.Model = "" },
		func(r *Request) { r.Sources[0].RequiredFields[0].Dataset.Model = "foreign" },
		func(r *Request) {
			r.Sources[0].RequiredFields = append(r.Sources[0].RequiredFields, r.Sources[0].RequiredFields[0])
		},
		func(r *Request) { r.Sources[0].RequiredFields = make([]FieldRef, MaxFields+1) },
		func(r *Request) { r.ProjectID = strings.Repeat("x", MaxIdentityBytes+1) },
	}
	for i, change := range changes {
		r := workload()
		change(&r)
		_, err := Evaluate(context.Background(), policyFunc(func(context.Context, Request) (Decision, error) {
			t.Fatal("invalid request reached adapter")
			return Decision{}, nil
		}), r)
		if err != ErrInvalid {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestDecisionBounds(t *testing.T) {
	for _, max := range []int{MaxValueBytes, MaxValueBytes + 1} {
		d := constrained()
		d.Sources[0].RowPredicates[0].Values = []any{strings.Repeat("x", max)}
		err := validateDecision(workload(), d)
		if (err == nil) != (max == MaxValueBytes) {
			t.Fatalf("byte bound %d: %v", max, err)
		}
	}
	d := constrained()
	predicate := d.Sources[0].RowPredicates[0]
	predicate.Values = []any{strings.Repeat("x", MaxValueBytes/2+1)}
	d.Sources[0].RowPredicates = []Predicate{predicate, predicate}
	if validateDecision(workload(), d) != ErrInvalid {
		t.Fatal("byte bound is not aggregate")
	}
	for _, max := range []int{MaxValues, MaxValues + 1} {
		d = constrained()
		p := &d.Sources[0].RowPredicates[0]
		p.Operator = query.FilterIN
		p.Values = make([]any, max)
		for i := range p.Values {
			p.Values[i] = int64(i)
		}
		if (validateDecision(workload(), d) == nil) != (max == MaxValues) {
			t.Fatalf("value bound %d", max)
		}
	}
	d = constrained()
	d.Sources[0].RowPredicates = make([]Predicate, MaxPredicates+1)
	if validateDecision(workload(), d) != ErrInvalid {
		t.Fatal("predicate bound not enforced")
	}
	d = constrained()
	d.Sources[0].DeniedFields = make([]FieldRef, MaxFields+1)
	if validateDecision(workload(), d) != ErrInvalid {
		t.Fatal("deny field bound not enforced")
	}
}

func TestEverySourceCoveredExactlyOnce(t *testing.T) {
	r := workload()
	second := DatasetRef{Model: "sales", Dataset: "customers"}
	r.Sources = append(r.Sources, Source{Dataset: second})
	d := constrained()
	d.Sources = append(d.Sources, SourceConstraint{Dataset: second})
	if err := validateDecision(r, d); err != nil {
		t.Fatal(err)
	}
	d.Sources[1] = d.Sources[0]
	if validateDecision(r, d) != ErrInvalid {
		t.Fatal("duplicate source hid missing coverage")
	}
}
