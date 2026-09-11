// Package s2sbench holds the frame for the agent semantic benchmark (#375):
// which scenarios it asks about, how a model is asked, and how an answer is
// judged. It does not call a model. See the S2SBench guide
// for the frozen experimental frame and what each decision costs.
package s2sbench

import (
	"fmt"
	"sort"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

// Stratum groups scenarios by how easily a competent model writing SQL straight
// from the model files could produce an answer that is wrong without looking
// wrong. That is the property the benchmark is about: Metis's claim is not that
// it makes easy questions easier, it is that it removes a class of confident
// wrong answers.
type Stratum string

const (
	// StratumSilentSemantics: the metric's meaning is not recoverable from
	// column names. Cumulative windows, time offsets, offset-to-grain
	// boundaries, semi-additive selection, conversion windows, and custom
	// calendars all have a natural-looking SQL spelling that returns a
	// different number than the metric defines.
	StratumSilentSemantics Stratum = "silent_semantics"

	// StratumFanout: the join or composition shape can duplicate or drop rows.
	// A wrong answer here is a plausible number, usually too large, and the
	// query that produced it looks ordinary.
	StratumFanout Stratum = "fanout"

	// StratumEdge: the answer key itself lands on an edge -- it contains a
	// NULL, it is empty, or its row order is part of the contract. These are
	// the cases where a query that is right on the common path is wrong at the
	// boundary, which is exactly what a spot check misses.
	StratumEdge Stratum = "edge"

	// StratumControl: ordinary aggregation, dimensions, and filters.
	//
	// This stratum is not expected to show a difference, and that is its job.
	// It is how a broken run is told apart from a finding: if path A fails
	// here, the model, the prompt, or the harness is at fault and no other
	// number in the run means anything. Dropping it would leave a uniformly
	// low path A score unable to distinguish "Metis helps" from "the
	// experiment does not work".
	StratumControl Stratum = "control"
)

// Strata in fixed order: reports, logs, and the selection all iterate this, so
// the order is part of the frozen frame rather than a map iteration.
var Strata = []Stratum{StratumSilentSemantics, StratumFanout, StratumEdge, StratumControl}

// silentSemanticsCapabilities and fanoutCapabilities are the derivation rules.
// A scenario's stratum comes from what the corpus already declares about it,
// not from a hand-kept list of names -- a list would be a second description of
// the corpus, and would stop matching it the first time a scenario changed.
var silentSemanticsCapabilities = map[scenarios.Capability]struct{}{
	scenarios.CapabilityCumulative:                 {},
	scenarios.CapabilityTimeOffset:                 {},
	scenarios.CapabilityOffsetToGrain:              {},
	scenarios.CapabilityConversion:                 {},
	scenarios.CapabilityCustomCalendar:             {},
	scenarios.CapabilityDenseCalendar:              {},
	scenarios.CapabilitySemiAdditive:               {},
	scenarios.CapabilitySemiAdditiveTieBreak:       {},
	scenarios.CapabilitySemiAdditiveNullSkip:       {},
	scenarios.CapabilitySemiAdditiveQueryGrain:     {},
	scenarios.CapabilitySemiAdditiveWindowGrouping: {},
	scenarios.CapabilitySemiAdditiveRollup:         {},
	scenarios.CapabilityDefinitionFilter:           {},
}

var fanoutCapabilities = map[scenarios.Capability]struct{}{
	scenarios.CapabilityMultiSource:          {},
	scenarios.CapabilityRelationship:         {},
	scenarios.CapabilityTemporalRelationship: {},
	scenarios.CapabilityRatio:                {},
	scenarios.CapabilityDerived:              {},
	scenarios.CapabilityMetricFilter:         {},
}

// StratumOf assigns one scenario to exactly one stratum, highest risk first.
// A scenario that is both a cumulative metric and a join is placed in
// silent_semantics: the harder property is the one the benchmark is asking
// about, and counting it twice would overstate whichever stratum it landed in.
func StratumOf(scenario scenarios.Scenario) Stratum {
	for _, capability := range scenario.Requires {
		if _, ok := silentSemanticsCapabilities[capability]; ok {
			return StratumSilentSemantics
		}
	}
	for _, capability := range scenario.Requires {
		if _, ok := fanoutCapabilities[capability]; ok {
			return StratumFanout
		}
	}
	if landsOnAnEdge(scenario) {
		return StratumEdge
	}
	return StratumControl
}

// landsOnAnEdge reads the answer key rather than the query. Whether a question
// is an edge case is a property of what the right answer turns out to be, and
// the corpus already knows that.
func landsOnAnEdge(scenario scenarios.Scenario) bool {
	if scenario.ExpectedResult == nil {
		return false
	}
	if scenario.ExpectedResult.Comparison == scenarios.ResultOrdered {
		return true
	}
	if len(scenario.ExpectedResult.Rows) == 0 {
		return true
	}
	for _, row := range scenario.ExpectedResult.Rows {
		for _, value := range row {
			if value.Null {
				return true
			}
		}
	}
	return false
}

// SelectionSize is the number of scenarios per stratum, and the whole of the
// v0 sample. 25 of 97: the issue caps v0 at 20-30 because breadth past that
// buys variance rather than resolution, and every scenario is paid for twice,
// once per path, times the repetition count.
//
// The weighting is not proportional to the corpus. It is proportional to where
// the thesis can be falsified: silent_semantics and fanout carry it, edge
// probes the boundary, and control exists to detect a broken run rather than to
// be compared.
var SelectionSize = map[Stratum]int{
	StratumSilentSemantics: 9,
	StratumFanout:          8,
	StratumEdge:            4,
	StratumControl:         4,
}

// Selection returns the frozen v0 sample, grouped by stratum.
//
// Sampling is deterministic and spread rather than truncated: taking the first
// N of a sorted stratum would draw the whole sample from one alphabetic
// neighbourhood, and scenario names in this corpus share prefixes by feature
// (every semi_additive_*, every custom_calendar_*), so the first N is close to
// "one feature, sampled repeatedly". Even strides across the sorted stratum
// keep the sample spread over the stratum while staying reproducible.
func Selection() (map[Stratum][]scenarios.Scenario, error) {
	byStratum := map[Stratum][]scenarios.Scenario{}
	for _, scenario := range scenarios.Core {
		stratum := StratumOf(scenario)
		byStratum[stratum] = append(byStratum[stratum], scenario)
	}

	out := map[Stratum][]scenarios.Scenario{}
	for _, stratum := range Strata {
		population := byStratum[stratum]
		sort.Slice(population, func(i, j int) bool { return population[i].Name < population[j].Name })

		want := SelectionSize[stratum]
		if len(population) < want {
			return nil, fmt.Errorf("stratum %q has %d scenarios, cannot sample %d", stratum, len(population), want)
		}
		out[stratum] = strideSample(population, want)
	}
	return out, nil
}

// SelectedScenarios flattens Selection in stratum order.
func SelectedScenarios() ([]scenarios.Scenario, error) {
	selection, err := Selection()
	if err != nil {
		return nil, err
	}
	var out []scenarios.Scenario
	for _, stratum := range Strata {
		out = append(out, selection[stratum]...)
	}
	return out, nil
}

func strideSample(population []scenarios.Scenario, want int) []scenarios.Scenario {
	out := make([]scenarios.Scenario, 0, want)
	for i := 0; i < want; i++ {
		// Midpoint of the i-th of `want` equal buckets, so the sample is
		// spread across the stratum and never repeats an index.
		index := (i*2 + 1) * len(population) / (want * 2)
		out = append(out, population[index])
	}
	return out
}
