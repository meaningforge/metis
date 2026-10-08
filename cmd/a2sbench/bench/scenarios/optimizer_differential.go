package scenarios

// OptimizerExpectation declares what the semantic optimizer is expected to do
// with a differential scenario.
//
// This answers a different question from the differential execution it sits
// next to, and conflating the two is the reason it exists. Executing a scenario
// with and without the optimizer asks "if an executable difference arose, do the
// results still agree". It cannot ask "did the optimizer do anything at all":
// when the optimizer rewrites a plan the renderer then folds back to the same
// SQL, the execution differential runs one statement against itself and reports
// success, which is indistinguishable from the optimizer having been idle.
//
// The declaration is therefore checked against the SemanticPlan fingerprint, not
// against emitted SQL. Two scenarios in this corpus rewrite the plan and emit
// byte-identical SQL; an SQL-shaped rule would file them permanently under
// no-op, which is exactly the case worth watching.
type OptimizerExpectation string

const (
	// OptimizerRewritesPlan means the optimizer must produce a different
	// SemanticPlan than the unoptimized planner. A scenario that stops
	// rewriting has lost the coverage it was added for.
	OptimizerRewritesPlan OptimizerExpectation = "rewrites-plan"

	// OptimizerNoOp means the optimizer must leave the plan untouched. These
	// scenarios still earn their place: they are the regression guard for a
	// future rule that starts firing where nothing did before. What they
	// cannot do is prove the optimizer correct, and declaring them says so.
	OptimizerNoOp OptimizerExpectation = "no-op"
)

// OptimizerDifferentialCase pairs a canonical scenario with what the optimizer
// is expected to do to it.
type OptimizerDifferentialCase struct {
	Scenario    Scenario
	Expectation OptimizerExpectation
}

// optimizerDifferentialDeclarations is the single source of truth for both the
// corpus membership and the expectation. Counts are derived from it rather than
// maintained beside it, so a scenario added without an expectation does not
// compile and one added with the wrong expectation fails the classification.
var optimizerDifferentialDeclarations = []struct {
	Name        string
	Expectation OptimizerExpectation
}{
	{"derived_metric", OptimizerRewritesPlan},
	{"independent_multi_source_at_grain", OptimizerNoOp},
	{"cumulative_metric_by_month_and_region", OptimizerNoOp},
	{"time_offset_current_and_previous_by_region", OptimizerNoOp},
	{"metric_filter_derived_metric", OptimizerRewritesPlan},
	{"metric_definition_filter_post_aggregation", OptimizerRewritesPlan},
	{"conversion_rate_filtered_campaign", OptimizerNoOp},
	{"joined_dimension_filter_order_limit", OptimizerNoOp},
	{"temporal_join_half_open_boundary", OptimizerNoOp},
	{"time_offset_period_over_period_growth", OptimizerNoOp},
	{"metric_filter_cumulative_metric", OptimizerNoOp},
	{"semi_additive_as_of_with_dimension_filter", OptimizerRewritesPlan},
	{"semi_additive_first_with_tie_break", OptimizerNoOp},
	{"semi_additive_first_skip_null", OptimizerNoOp},
	{"semi_additive_window_group_sum", OptimizerNoOp},
	{"offset_to_grain_missing_boundary_stays_zero", OptimizerNoOp},
	{"custom_calendar_dense_missing_period", OptimizerNoOp},
	{"custom_calendar_cumulative_with_filter", OptimizerNoOp},
	{"custom_calendar_fiscal_quarter_to_date", OptimizerNoOp},
	{"custom_calendar_semi_additive_last_snapshot", OptimizerNoOp},
	{"distinct_dimension_values", OptimizerNoOp},
	{"relationship_unmatched_facts", OptimizerNoOp},
	{"metric_filter_zero_and_negative_groups", OptimizerNoOp},
	{"ordered_ties_secondary_key", OptimizerNoOp},
	{"cumulative_zero_negative_periods", OptimizerNoOp},
	{"time_offset_zero_negative_periods", OptimizerNoOp},
	{"derived_null_negative_inputs", OptimizerRewritesPlan},
	{"ratio_empty_population_null", OptimizerRewritesPlan},
}

// OptimizerDifferentialCases returns the differential corpus with each
// scenario's declared optimizer expectation.
func OptimizerDifferentialCases() []OptimizerDifferentialCase {
	cases := make([]OptimizerDifferentialCase, 0, len(optimizerDifferentialDeclarations))
	for _, declaration := range optimizerDifferentialDeclarations {
		scenario, ok := ByName(declaration.Name)
		if !ok {
			panic("unknown optimizer differential scenario " + declaration.Name)
		}
		switch declaration.Expectation {
		case OptimizerRewritesPlan, OptimizerNoOp:
		default:
			panic("unknown optimizer expectation " + string(declaration.Expectation) + " for " + declaration.Name)
		}
		cases = append(cases, OptimizerDifferentialCase{Scenario: scenario, Expectation: declaration.Expectation})
	}
	return cases
}

// OptimizerDifferentialCore is the canonical result-level corpus used to prove
// that semantic optimization preserves observable query results. Keep this
// focused on representative high-risk transforms rather than duplicating the
// full execution corpus on every real-engine run.
func OptimizerDifferentialCore() []Scenario {
	cases := OptimizerDifferentialCases()
	out := make([]Scenario, 0, len(cases))
	for _, differential := range cases {
		out = append(out, differential.Scenario)
	}
	return out
}
