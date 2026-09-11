package expression

import "testing"

// AVG is invariant only under uniform replication. A fan-out join may assign
// each source row a different multiplicity, which reweights the mean.
func TestAverageIsSensitiveToNonUniformPerRowMultiplicity(t *testing.T) {
	values := []float64{10, 30}
	multiplicities := []int{1, 3}
	original := averageWithMultiplicities(values, []int{1, 1})
	duplicated := averageWithMultiplicities(values, multiplicities)
	if original != 20 {
		t.Fatalf("original average = %v, want 20", original)
	}
	if duplicated != 25 {
		t.Fatalf("non-uniformly duplicated average = %v, want 25", duplicated)
	}
	if duplicated == original {
		t.Fatal("non-uniform per-row multiplicity unexpectedly preserved AVG")
	}
	if props := mustDerive(t, "AVG(amount)"); props.Duplicate != DuplicateSensitive {
		t.Fatalf("AVG duplicate sensitivity = %s, want %s", props.Duplicate, DuplicateSensitive)
	}
}

func averageWithMultiplicities(values []float64, multiplicities []int) float64 {
	var sum float64
	var count int
	for i, value := range values {
		sum += value * float64(multiplicities[i])
		count += multiplicities[i]
	}
	return sum / float64(count)
}

func deriveForSource(t *testing.T, source string) (AggregationProperties, bool) {
	t.Helper()
	expr, err := Parse(source, DialectProfile{})
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	call, ok := expr.(*FunctionCallExpr)
	if !ok {
		t.Fatalf("expression %q is %T, want *FunctionCallExpr", source, expr)
	}
	return DeriveAggregationProperties(nil, call)
}

func mustDerive(t *testing.T, source string) AggregationProperties {
	t.Helper()
	props, carries := deriveForSource(t, source)
	if !carries {
		t.Fatalf("expression %q does not carry aggregation properties", source)
	}
	return props
}

func TestDeriveAggregationPropertiesCoversModelledAggregates(t *testing.T) {
	for _, tc := range []struct {
		source    string
		duplicate DuplicateSensitivity
		rollup    RollupAlgebra
		state     []PartialStateComponent
		merge     string
		finalize  FinalizeKind
	}{
		{
			source: "SUM(amount)", duplicate: DuplicateSensitive, rollup: RollupDistributive,
			state: []PartialStateComponent{{Name: PartialStateSum}}, merge: "SUM", finalize: FinalizeIdentity,
		},
		{
			source: "MIN(amount)", duplicate: DuplicateInvariant, rollup: RollupDistributive,
			state: []PartialStateComponent{{Name: PartialStateValue}}, merge: "MIN", finalize: FinalizeIdentity,
		},
		{
			source: "MAX(amount)", duplicate: DuplicateInvariant, rollup: RollupDistributive,
			state: []PartialStateComponent{{Name: PartialStateValue}}, merge: "MAX", finalize: FinalizeIdentity,
		},
		{
			source: "AVG(amount)", duplicate: DuplicateSensitive, rollup: RollupAlgebraic,
			state: []PartialStateComponent{{Name: PartialStateSum}, {Name: PartialStateCount}}, merge: "SUM", finalize: FinalizeRatio,
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			props := mustDerive(t, tc.source)
			if !props.Derived {
				t.Fatal("Derived = false, want true")
			}
			if props.Duplicate != tc.duplicate || props.Rollup != tc.rollup {
				t.Fatalf("axes = %s/%s, want %s/%s", props.Duplicate, props.Rollup, tc.duplicate, tc.rollup)
			}
			if props.Merge != tc.merge || props.Finalize != tc.finalize {
				t.Fatalf("merge/finalize = %q/%s, want %q/%s", props.Merge, props.Finalize, tc.merge, tc.finalize)
			}
			assertPartialState(t, props.PartialState, tc.state)
		})
	}
}

// COUNT merges through SUM. Merging counts by counting them would return the
// number of partials rather than the number of rows.
func TestDeriveAggregationPropertiesCountMergesThroughSum(t *testing.T) {
	props := mustDerive(t, "COUNT(order_id)")
	if props.Merge != "SUM" {
		t.Fatalf("COUNT merge = %q, want SUM", props.Merge)
	}
	if props.Rollup != RollupDistributive {
		t.Fatalf("COUNT rollup = %s, want %s", props.Rollup, RollupDistributive)
	}
	assertPartialState(t, props.PartialState, []PartialStateComponent{{Name: PartialStateCount}})
}

// The predicate belongs to the partial state, not to the merge. Re-applying it
// while merging would filter values already accumulated under it.
func TestDeriveAggregationPropertiesSumIfPredicateBelongsToPartialState(t *testing.T) {
	props := mustDerive(t, "SUMIF(amount, status = 'paid')")
	if props.Duplicate != DuplicateSensitive || props.Rollup != RollupDistributive {
		t.Fatalf("axes = %s/%s, want %s/%s", props.Duplicate, props.Rollup, DuplicateSensitive, RollupDistributive)
	}
	if props.Merge != "SUM" {
		t.Fatalf("merge = %q, want SUM", props.Merge)
	}
	assertPartialState(t, props.PartialState, []PartialStateComponent{{Name: PartialStateSum, Predicated: true}})
	if props.Finalize != FinalizeIdentity {
		t.Fatalf("finalize = %s, want %s", props.Finalize, FinalizeIdentity)
	}
}

// The two axes are independent: COUNT DISTINCT is invariant under duplication
// and simultaneously non-rollupable. A single additivity class gets both wrong.
func TestDeriveAggregationPropertiesCountDistinctSeparatesTheTwoAxes(t *testing.T) {
	plain := mustDerive(t, "COUNT(customer_id)")
	distinct := mustDerive(t, "COUNT(DISTINCT customer_id)")

	if plain.Duplicate != DuplicateSensitive || plain.Rollup != RollupDistributive {
		t.Fatalf("COUNT axes = %s/%s, want %s/%s", plain.Duplicate, plain.Rollup, DuplicateSensitive, RollupDistributive)
	}
	if distinct.Duplicate != DuplicateInvariant {
		t.Fatalf("COUNT DISTINCT duplicate = %s, want %s", distinct.Duplicate, DuplicateInvariant)
	}
	if distinct.Rollup != RollupHolistic {
		t.Fatalf("COUNT DISTINCT rollup = %s, want %s", distinct.Rollup, RollupHolistic)
	}
	if distinct.Duplicate == plain.Duplicate && distinct.Rollup == plain.Rollup {
		t.Fatal("DISTINCT changed neither axis")
	}
	if len(distinct.PartialState) != 0 || distinct.Merge != "" || distinct.Finalize != FinalizeNone {
		t.Fatalf("holistic aggregation exposes merge state: %+v", distinct)
	}
}

// Deduplication absorbs multiplicity for any aggregation, but only additively
// merged ones lose rollup. MIN and MAX ignore multiplicity already.
func TestDeriveAggregationPropertiesDistinctKeepsExtremumDistributive(t *testing.T) {
	props := mustDerive(t, "MIN(DISTINCT amount)")
	if props.Duplicate != DuplicateInvariant {
		t.Fatalf("duplicate = %s, want %s", props.Duplicate, DuplicateInvariant)
	}
	if props.Rollup != RollupDistributive || props.Merge != "MIN" {
		t.Fatalf("rollup/merge = %s/%q, want %s/MIN", props.Rollup, props.Merge, RollupDistributive)
	}

	sumDistinct := mustDerive(t, "SUM(DISTINCT amount)")
	if sumDistinct.Rollup != RollupHolistic {
		t.Fatalf("SUM DISTINCT rollup = %s, want %s", sumDistinct.Rollup, RollupHolistic)
	}
}

// A target-native aggregate reaches the analyzer through AllowUnknownFunctions
// and is typed as scalar by default. That default is a placeholder, so property
// derivation must not read it as a claim.
func TestDeriveAggregationPropertiesUnknownFunctionFailsClosed(t *testing.T) {
	for _, source := range []string{"uniqExact(customer_id)", "quantile(0.9)(latency)"} {
		t.Run(source, func(t *testing.T) {
			expr, err := Parse(source, DialectProfile{})
			if err != nil {
				t.Skipf("dialect profile does not parse %q: %v", source, err)
			}
			props := firstAggregationProperties(t, expr)
			if props.Derived {
				t.Fatalf("Derived = true for unknown function %q", source)
			}
			if props.Duplicate != DuplicateUnknown || props.Rollup != RollupUnknown {
				t.Fatalf("axes = %s/%s, want both UNKNOWN", props.Duplicate, props.Rollup)
			}
			if props.Merge != "" || props.Finalize != FinalizeNone {
				t.Fatalf("unknown function exposes merge state: %+v", props)
			}
		})
	}
}

func TestDeriveAggregationPropertiesSkipsKnownScalarFunctions(t *testing.T) {
	if _, carries := deriveForSource(t, "LOWER(region)"); carries {
		t.Fatal("scalar function carries aggregation properties, want skipped")
	}
}

func TestCollectAggregationPropertiesWalksNestedCallsInSourceOrder(t *testing.T) {
	expr, err := Parse("SUM(amount) / COUNT(DISTINCT customer_id)", DialectProfile{})
	if err != nil {
		t.Fatal(err)
	}
	collected := CollectAggregationProperties(expr, nil)
	if len(collected) != 2 {
		t.Fatalf("collected %d properties, want 2", len(collected))
	}
	if collected[0].Rollup != RollupDistributive {
		t.Fatalf("first = %s, want %s", collected[0].Rollup, RollupDistributive)
	}
	if collected[1].Rollup != RollupHolistic {
		t.Fatalf("second = %s, want %s", collected[1].Rollup, RollupHolistic)
	}
}

func firstAggregationProperties(t *testing.T, expr Expr) AggregationProperties {
	t.Helper()
	collected := CollectAggregationProperties(expr, nil)
	if len(collected) == 0 {
		t.Fatal("no aggregation properties collected")
	}
	return collected[0]
}

func assertPartialState(t *testing.T, got, want []PartialStateComponent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("partial state = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("partial state[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
