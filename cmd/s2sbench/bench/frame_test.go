package s2sbench

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

// The sample is the experiment. If it drifts, every number recorded before the
// drift stops being comparable with every number after it -- silently, because
// a benchmark has no failing test of its own to notice.
//
// So the selection is pinned by name. Recording it here is not duplication of
// the rule in selection.go: the rule says how to sample, this says what the
// rule produced when the frame was frozen, and a change to the corpus that
// moves the sample has to be seen and versioned rather than absorbed.
func TestSelectionIsTheFrozenSample(t *testing.T) {
	frozen := map[Stratum][]string{
		StratumSilentSemantics: {
			"conversion_rate_by_campaign",
			"cumulative_metric_by_quarter_and_region",
			"custom_calendar_dense_missing_period",
			"custom_offset_to_grain_missing_boundary_stays_zero",
			"offset_to_grain_missing_boundary_stays_zero",
			"semi_additive_first_skip_null",
			"semi_additive_queried_week",
			"semi_additive_window_group_sum",
			"time_offset_previous_quarter",
		},
		StratumFanout: {
			"derived_null_negative_inputs",
			"independent_multi_source_at_grain",
			"metric_filter_derived_metric",
			"metric_filter_with_dimension_filter",
			"multi_hop_repeated_dimension_values",
			"multiple_metrics_joined_dimension",
			"relationship_unmatched_facts",
			"temporal_join_open_ended_version",
		},
		StratumEdge: {
			"distinct_dimension_values",
			"filters_order_limit",
			"order_by_metric_ungrouped",
			"ordered_ties_secondary_key",
		},
		StratumControl: {
			"aggregation_variants",
			"multiple_filters_same_dimension",
			"multiple_metrics_with_filters",
			"time_filter_with_month_grain",
		},
	}

	selection, err := Selection()
	if err != nil {
		t.Fatal(err)
	}
	for _, stratum := range Strata {
		var got []string
		for _, scenario := range selection[stratum] {
			got = append(got, scenario.Name)
		}
		if strings.Join(got, ",") != strings.Join(frozen[stratum], ",") {
			t.Errorf("stratum %s sample changed\n  frozen: %s\n  now:    %s\n\n"+
				"Results from before and after this change are not comparable. "+
				"If the change is intended, bump PromptVersion and record it here.",
				stratum, strings.Join(frozen[stratum], ", "), strings.Join(got, ", "))
		}
	}
}

func TestQuestionsStateRequiredVisibleColumns(t *testing.T) {
	for scenario, required := range map[string][]string{
		"metric_filter_with_dimension_filter": {"order status", "revenue"},
		"temporal_join_open_ended_version":    {"order ID", "customer tier"},
	} {
		question := Questions[scenario]
		for _, phrase := range required {
			if !strings.Contains(question, phrase) {
				t.Fatalf("question %q = %q, want visible-column phrase %q", scenario, question, phrase)
			}
		}
	}
}

// The control stratum only does its job -- telling a broken run apart from a
// finding -- if it is genuinely easier than the rest. If a capability that can
// silently produce a wrong number ever lands in it, a path A failure there
// stops being evidence that the experiment is broken, and the run loses its
// only validity check.
func TestControlStratumHoldsNothingThatCanBeSilentlyWrong(t *testing.T) {
	selection, err := Selection()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range selection[StratumControl] {
		for _, capability := range scenario.Requires {
			if _, ok := silentSemanticsCapabilities[capability]; ok {
				t.Errorf("control scenario %s requires %q, which belongs to silent_semantics", scenario.Name, capability)
			}
			if _, ok := fanoutCapabilities[capability]; ok {
				t.Errorf("control scenario %s requires %q, which belongs to fanout", scenario.Name, capability)
			}
		}
		if landsOnAnEdge(scenario) {
			t.Errorf("control scenario %s lands on an edge, so it is not a control", scenario.Name)
		}
	}
}

// Every stratum has to be assignable and every selected scenario has to have an
// answer key, or a run produces verdicts nobody can interpret.
func TestEverySelectedScenarioIsUsable(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 25 {
		t.Fatalf("selection has %d scenarios, want 25", len(selected))
	}
	seen := map[string]struct{}{}
	for _, scenario := range selected {
		if _, ok := seen[scenario.Name]; ok {
			t.Errorf("%s is selected twice", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
		if scenario.ExpectedResult == nil {
			t.Errorf("%s has no answer key", scenario.Name)
		}
		if scenario.ExpectedResult != nil && scenario.ExpectedResult.Comparison == "" {
			t.Errorf("%s declares no result comparison mode", scenario.Name)
		}
	}
}

// One question per selected scenario, and no question for anything else. A
// question left behind after a sample change would be asked of nothing; a
// scenario without one would be run with an empty prompt.
func TestQuestionsCoverExactlyTheSelection(t *testing.T) {
	selected, err := SelectedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]struct{}{}
	for _, scenario := range selected {
		wanted[scenario.Name] = struct{}{}
		if _, err := QuestionFor(scenario.Name); err != nil {
			t.Errorf("%v", err)
		}
	}
	var orphaned []string
	for name := range Questions {
		if _, ok := wanted[name]; !ok {
			orphaned = append(orphaned, name)
		}
	}
	sort.Strings(orphaned)
	if len(orphaned) != 0 {
		t.Errorf("these questions are not asked of any selected scenario:\n  %s", strings.Join(orphaned, "\n  "))
	}
}

// A question that explains how to compute the answer has already done the work
// being measured, and one written in Metis's vocabulary tells the model which
// arm it is on. Neither can be caught by reading the list once; both can be
// caught mechanically.
func TestQuestionsLeakNeitherSQLShapeNorMetisVocabulary(t *testing.T) {
	for name, question := range Questions {
		if strings.TrimSpace(question) == "" {
			t.Errorf("%s has an empty question", name)
			continue
		}
		if leaked := LeakedVocabulary(question); len(leaked) != 0 {
			t.Errorf("%s leaks %s:\n  %q", name, strings.Join(leaked, ", "), question)
		}
	}

	byQuestion := map[string]string{}
	for name, question := range Questions {
		normalized := strings.ToLower(strings.TrimSpace(question))
		if other, ok := byQuestion[normalized]; ok {
			t.Errorf("%s and %s ask the same question, so their results cannot be told apart", other, name)
		}
		byQuestion[normalized] = name
	}
}

// The oracle decides the headline number, so it has to be checked against the
// answers it must accept and the answers it must not.
func TestOracleJudgesSemanticsAndNotPresentation(t *testing.T) {
	scenario := scenarios.Scenario{
		Name: "probe",
		ExpectedResult: &scenarios.ResultExpectation{
			Comparison: scenarios.ResultUnordered,
			ResultSet: scenarios.ResultSet{
				Columns: []scenarios.ResultColumn{
					{Name: "status", ValueKind: scenarios.ResultString},
					{Name: "revenue", ValueKind: scenarios.ResultInteger},
				},
				Rows: []scenarios.ResultRow{
					{{ValueKind: scenarios.ResultString, Canonical: "paid"}, {ValueKind: scenarios.ResultInteger, Canonical: "300"}},
					{{ValueKind: scenarios.ResultString, Canonical: "refunded"}, {ValueKind: scenarios.ResultInteger, Canonical: "100"}},
				},
			},
		},
	}
	answer := func(rows []scenarios.ResultRow, names ...string) scenarios.ResultSet {
		columns := []scenarios.ResultColumn{
			{Name: names[0], ValueKind: scenarios.ResultString},
			{Name: names[1], ValueKind: scenarios.ResultInteger},
		}
		return scenarios.ResultSet{Columns: columns, Rows: rows}
	}
	right := []scenarios.ResultRow{
		{{ValueKind: scenarios.ResultString, Canonical: "paid"}, {ValueKind: scenarios.ResultInteger, Canonical: "300"}},
		{{ValueKind: scenarios.ResultString, Canonical: "refunded"}, {ValueKind: scenarios.ResultInteger, Canonical: "100"}},
	}

	for _, tt := range []struct {
		name   string
		actual scenarios.ResultSet
		want   Verdict
	}{
		{
			name:   "different aliases, same numbers",
			actual: answer(right, "order_status", "total_revenue"),
			want:   VerdictCorrect,
		},
		{
			name:   "unordered rows in another sequence",
			actual: answer([]scenarios.ResultRow{right[1], right[0]}, "status", "revenue"),
			want:   VerdictCorrect,
		},
		{
			name:   "a duplicated row, which is what fanout looks like",
			actual: answer([]scenarios.ResultRow{right[0], right[0], right[1]}, "status", "revenue"),
			want:   VerdictWrong,
		},
		{
			name: "one value off",
			actual: answer([]scenarios.ResultRow{
				right[0],
				{{ValueKind: scenarios.ResultString, Canonical: "refunded"}, {ValueKind: scenarios.ResultInteger, Canonical: "101"}},
			}, "status", "revenue"),
			want: VerdictWrong,
		},
		{
			name: "a NULL where a value was expected",
			actual: answer([]scenarios.ResultRow{
				right[0],
				{{ValueKind: scenarios.ResultString, Canonical: "refunded"}, {ValueKind: scenarios.ResultInteger, Null: true}},
			}, "status", "revenue"),
			want: VerdictWrong,
		},
		{
			name: "columns swapped, so the wrong thing is being reported",
			actual: scenarios.ResultSet{
				Columns: []scenarios.ResultColumn{
					{Name: "revenue", ValueKind: scenarios.ResultInteger},
					{Name: "status", ValueKind: scenarios.ResultString},
				},
				Rows: []scenarios.ResultRow{
					{{ValueKind: scenarios.ResultInteger, Canonical: "300"}, {ValueKind: scenarios.ResultString, Canonical: "paid"}},
					{{ValueKind: scenarios.ResultInteger, Canonical: "100"}, {ValueKind: scenarios.ResultString, Canonical: "refunded"}},
				},
			},
			want: VerdictWrong,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			verdict, err := Judge(scenario, tt.actual)
			if verdict != tt.want {
				t.Fatalf("verdict = %q, want %q (%v)", verdict, tt.want, err)
			}
			if tt.want == VerdictCorrect && err != nil {
				t.Fatalf("correct answer reported an error: %v", err)
			}
			if tt.want == VerdictWrong && err == nil {
				t.Fatal("wrong answer reported no reason")
			}
		})
	}
}

// Row order matters exactly where the scenario says it does. Judging an
// ordered scenario as unordered would silently pass a query with no ORDER BY,
// and four of the twenty-five selected scenarios are in the edge stratum
// precisely because ordering is part of their contract.
func TestOracleHonoursTheOrderedContract(t *testing.T) {
	rows := []scenarios.ResultRow{
		{{ValueKind: scenarios.ResultInteger, Canonical: "2"}},
		{{ValueKind: scenarios.ResultInteger, Canonical: "1"}},
	}
	set := func(rows []scenarios.ResultRow) scenarios.ResultSet {
		return scenarios.ResultSet{
			Columns: []scenarios.ResultColumn{{Name: "n", ValueKind: scenarios.ResultInteger}},
			Rows:    rows,
		}
	}
	for _, comparison := range []scenarios.ResultComparison{scenarios.ResultOrdered, scenarios.ResultUnordered} {
		t.Run(string(comparison), func(t *testing.T) {
			scenario := scenarios.Scenario{
				Name:           "probe",
				ExpectedResult: &scenarios.ResultExpectation{Comparison: comparison, ResultSet: set(rows)},
			}
			reversed := set([]scenarios.ResultRow{rows[1], rows[0]})
			verdict, _ := Judge(scenario, reversed)

			want := VerdictCorrect
			if comparison == scenarios.ResultOrdered {
				want = VerdictWrong
			}
			if verdict != want {
				t.Fatalf("reversed rows under %s = %q, want %q", comparison, verdict, want)
			}
		})
	}
}

func TestOracleUsesConformanceNumericTolerance(t *testing.T) {
	expected := scenarios.ResultSet{
		Columns: []scenarios.ResultColumn{{Name: "amount", ValueKind: scenarios.ResultNumber}},
		Rows:    []scenarios.ResultRow{{{ValueKind: scenarios.ResultNumber, Canonical: "1"}}},
	}
	scenario := scenarios.Scenario{Name: "decimal", ExpectedResult: &scenarios.ResultExpectation{ResultSet: expected, Comparison: scenarios.ResultUnordered}}
	actual := scenarios.ResultSet{
		Columns: []scenarios.ResultColumn{{Name: "different_alias", ValueKind: scenarios.ResultNumber}},
		Rows:    []scenarios.ResultRow{{{ValueKind: scenarios.ResultNumber, Canonical: "1.0000000001"}}},
	}
	verdict, err := Judge(scenario, actual)
	if err != nil || verdict != VerdictCorrect {
		t.Fatalf("Judge() = %q, %v; want numeric values within tolerance to match", verdict, err)
	}
}

// A scenario with no answer key cannot be scored, and scoring it as wrong
// would blame the model for the corpus.
func TestOracleRefusesAScenarioWithNoAnswerKey(t *testing.T) {
	verdict, err := Judge(scenarios.Scenario{Name: "probe"}, scenarios.ResultSet{})
	if verdict != VerdictFailed || err == nil {
		t.Fatalf("verdict = %q, err = %v; want failed with a reason", verdict, err)
	}
}

// Both arms must be given the same rope. An arm with more attempts or more
// tool calls will look better for a reason that has nothing to do with what is
// being measured, and the budget is the only place that can go wrong quietly.
func TestBudgetIsIdenticalAcrossPaths(t *testing.T) {
	if len(Paths) != 2 {
		t.Fatalf("expected exactly two paths, got %d", len(Paths))
	}
	if FrozenBudget.Repetitions < 2 {
		t.Fatalf("repetitions = %d; a single run per question cannot separate a difference from run-to-run spread",
			FrozenBudget.Repetitions)
	}
	if FrozenBudget.Attempts < 1 || FrozenBudget.ToolCalls < 1 || FrozenBudget.QuestionTimeoutMS != 60_000 {
		t.Fatalf("budget is unusable: %+v", FrozenBudget)
	}
	// The budget is one value, shared. This is the assertion that it stays one
	// value: a per-path budget would have to be introduced here first.
	if fmt.Sprintf("%T", FrozenBudget) != "s2sbench.TaskBudget" {
		t.Fatalf("budget type changed to %T; a per-path budget breaks the comparison", FrozenBudget)
	}
}

// fakeRunner answers from a script, so the run loop can be checked without a
// model. It reports the attempt index and tool calls the way a real arm must.
type fakeRunner struct {
	path      Path
	answers   map[string]scenarios.ResultSet
	index     int
	toolCalls int
	failWith  error
}

func (f *fakeRunner) Path() Path { return f.path }

func (f *fakeRunner) Answer(_ context.Context, _ string, scenario scenarios.Scenario, _ TaskBudget) (Attempt, error) {
	if f.failWith != nil {
		return Attempt{Index: f.index, ToolCalls: f.toolCalls}, f.failWith
	}
	result, ok := f.answers[scenario.Name]
	if !ok && scenario.ExpectedResult != nil {
		result = scenario.ExpectedResult.ResultSet
	}
	return Attempt{Index: f.index, ToolCalls: f.toolCalls, Result: result}, nil
}

// An arm that does not report its attempt index would have every correct
// answer counted as a first-try success. That inflates one of the four
// headline numbers, silently, for whichever arm forgot -- so the run refuses
// rather than scoring it.
func TestRunRefusesAnArmThatDoesNotReportItsAttemptIndex(t *testing.T) {
	_, err := Run(context.Background(), &fakeRunner{path: PathMetis, index: 0}, FrozenBudget)
	if err == nil || !strings.Contains(err.Error(), "attempt index") {
		t.Fatalf("err = %v; want a refusal naming the attempt index", err)
	}
}

// The budget is the only thing keeping the two arms comparable. An arm that
// exceeds it has been given more rope than the other, and its numbers are not
// evidence about the thesis.
func TestRunRefusesAnArmThatExceedsTheBudget(t *testing.T) {
	for _, tt := range []struct {
		name   string
		runner *fakeRunner
		want   string
	}{
		{
			name:   "too many attempts",
			runner: &fakeRunner{path: PathMetis, index: FrozenBudget.Attempts + 1},
			want:   "attempts",
		},
		{
			name:   "too many tool calls",
			runner: &fakeRunner{path: PathRawAssets, index: 1, toolCalls: FrozenBudget.ToolCalls + 1},
			want:   "tool calls",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Run(context.Background(), tt.runner, FrozenBudget); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v; want a refusal naming %q", err, tt.want)
			}
		})
	}
}

// A perfect arm scores every question correct in every stratum, and a run that
// cannot produce an answer at all is failed rather than wrong. Those are the
// two ends the summary has to get right before any number between them means
// anything.
func TestRunAndSummariseScoreTheEnds(t *testing.T) {
	perfect, err := Run(context.Background(), &fakeRunner{path: PathMetis, index: 1}, FrozenBudget)
	if err != nil {
		t.Fatal(err)
	}
	if want := 25 * FrozenBudget.Repetitions; len(perfect) != want {
		t.Fatalf("recorded %d attempts, want %d", len(perfect), want)
	}
	report, err := Summarise(PathMetis, perfect)
	if err != nil {
		t.Fatal(err)
	}
	var questions, correct int
	for _, stratum := range Strata {
		entry := report.ByStratum[stratum]
		if entry.Wrong != 0 || entry.Failed != 0 {
			t.Errorf("stratum %s: an arm answering from the key scored wrong=%d failed=%d", stratum, entry.Wrong, entry.Failed)
		}
		questions += entry.Questions
		correct += entry.Correct
	}
	if questions != 25 {
		t.Errorf("summary covers %d questions, want 25", questions)
	}
	if correct != 25*FrozenBudget.Repetitions {
		t.Errorf("correct = %d, want %d", correct, 25*FrozenBudget.Repetitions)
	}
	if len(report.Inconsistent) != 0 {
		t.Errorf("an arm answering identically every time was reported inconsistent: %v", report.Inconsistent)
	}

	refusing, err := Run(context.Background(), &fakeRunner{path: PathRawAssets, index: 1, failWith: errRefused}, FrozenBudget)
	if err != nil {
		t.Fatal(err)
	}
	refusedReport, err := Summarise(PathRawAssets, refusing)
	if err != nil {
		t.Fatal(err)
	}
	for _, stratum := range Strata {
		entry := refusedReport.ByStratum[stratum]
		if entry.Wrong != 0 {
			t.Errorf("stratum %s: refusing to answer was scored wrong=%d; refusing and misreporting are different findings",
				stratum, entry.Wrong)
		}
		if entry.Failed == 0 {
			t.Errorf("stratum %s: refusing to answer was not scored failed", stratum)
		}
	}
}

var errRefused = fmt.Errorf("arm could not answer")
