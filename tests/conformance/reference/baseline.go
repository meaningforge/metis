package reference

import (
	"fmt"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

type Importance string

const (
	ImportanceRequired     Importance = "required"
	ImportanceOptional     Importance = "optional"
	ImportanceExperimental Importance = "experimental"
)

type Origin string

const (
	OriginExternalReference Origin = "external_reference"
	OriginProductionBug     Origin = "production_bug"
	OriginOssie             Origin = "ossie"
	OriginManualOracle      Origin = "manual_oracle"
)

type TargetStatus string

const (
	TargetRequired              TargetStatus = "required"
	TargetUnsupported           TargetStatus = "unsupported"
	TargetIntentionalDifference TargetStatus = "intentional_difference"
)

type TargetExpectation struct {
	Status         TargetStatus
	Reason         string
	ExpectedResult *scenarios.ResultExpectation
}

type Case struct {
	Scenario        string
	Importance      Importance
	Origin          Origin
	TargetOverrides map[string]TargetExpectation
}

// Baseline is the generic reality-testing contract. Scenario importance is a
// semantic-product concern; target support is a backend-execution concern.
// Target support is derived from the registered engine capability set by
// default. Per-case target overrides are intentionally sparse and exist only for
// backend-specific exceptions that capabilities cannot express precisely.
var Baseline = []Case{
	// Foundational aggregation, grouping, query-shape, and ordering behavior.
	{Scenario: "simple_metric", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "multiple_metrics_same_source", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "aggregation_variants", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "local_dimension", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "multiple_local_dimensions", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "dimensions_only", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "dimensions_only_filter_order_limit", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "distinct_dimension_values", Importance: ImportanceRequired, Origin: OriginExternalReference},

	// Relationship pressure includes multi-hop predicate placement, multiple
	// measures over one joined dimension, and temporal half-open/open-ended
	// boundaries. These cases are deliberately result-level oracles rather than
	// SQL-shape snapshots.
	{Scenario: "multi_hop_intermediate_filter", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "multiple_metrics_joined_dimension", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "duplicate_invariant_filtered_fanout", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "temporal_join_half_open_boundary", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "temporal_join_open_ended_version", Importance: ImportanceRequired, Origin: OriginManualOracle},

	// High-risk semantic composition cases. These stay canonical and generic;
	// provenance records where the pressure came from without branding the case.
	{Scenario: "derived_metric", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "ratio_metric", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "nested_derived_metric", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "cumulative_metric_by_month_and_region", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "time_offset_current_and_previous_by_region", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "metric_filter_with_dimension_filter", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "conversion_rate_by_campaign", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "conversion_rate_filtered_campaign", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "multi_source_derived_at_grain", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "independent_multi_source_ungrouped", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "metric_definition_filter_pre_aggregation", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "metric_definition_filter_post_aggregation", Importance: ImportanceRequired, Origin: OriginManualOracle},

	// Regression-pressure cases promoted into the required reality baseline.
	// These cover failure classes that repeatedly surface in mature semantic
	// compilers: joined filtering, offset/filter ordering, period-over-period
	// composition, cumulative filtering, and grouping-sensitive cumulative work.
	{Scenario: "multiple_filters_same_dimension", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "joined_dimension_filter", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "time_filter_with_month_grain", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "time_offset_period_over_period_growth", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "metric_filter_time_offset_metric", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "metric_filter_cumulative_metric", Importance: ImportanceRequired, Origin: OriginExternalReference},
	{Scenario: "cumulative_metric_by_quarter_and_region", Importance: ImportanceRequired, Origin: OriginExternalReference},

	// Stateful evaluation pressure spans null-skipping selection, explicit
	// tie-breaks, query-grain ownership, grouped rollups, dense periods, and
	// custom-calendar boundaries. Together these keep every capability claimed
	// by the real-engine registry attached to at least one required reality case.
	{Scenario: "semi_additive_latest_snapshot", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "semi_additive_first_skip_null", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "semi_additive_queried_week", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "semi_additive_window_group_sum", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "offset_to_grain_missing_boundary_stays_zero", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "custom_calendar_dense_missing_period", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "custom_calendar_cumulative_with_filter", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "custom_calendar_semi_additive_last_snapshot", Importance: ImportanceRequired, Origin: OriginManualOracle},

	// Adversarial data-shape pressure. These cases intentionally reuse the
	// canonical commerce semantics while changing only the physical data world,
	// so NULL, zero, negative, unmatched, tied, and boundary values remain
	// observable correctness obligations on every capable real backend.
	{Scenario: "aggregation_null_zero_negative", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "grouping_null_dimension", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "relationship_unmatched_facts", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "multi_hop_repeated_dimension_values", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "joined_ratio_repeated_entities", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "metric_filter_zero_and_negative_groups", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "ordered_ties_secondary_key", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "time_filter_inclusive_boundaries", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "cumulative_zero_negative_periods", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "time_offset_zero_negative_periods", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "derived_null_negative_inputs", Importance: ImportanceRequired, Origin: OriginManualOracle},
	{Scenario: "ratio_empty_population_null", Importance: ImportanceRequired, Origin: OriginManualOracle},
}

func Scenario(c Case) (scenarios.Scenario, bool) {
	return scenarios.ByName(c.Scenario)
}

func ByScenario(name string) (Case, bool) {
	for _, c := range Baseline {
		if c.Scenario == name {
			return c, true
		}
	}
	return Case{}, false
}

// TargetExpectationFor resolves the execution contract for one scenario/target.
// Capability coverage is the scalable default; sparse overrides record only
// true backend exceptions. Intentional differences remain executable contracts:
// they must carry a target-specific normalized result expectation rather than
// becoming a permanent skip.
func TargetExpectationFor(c Case, dialect string, capabilities scenarios.CapabilitySet) (TargetExpectation, error) {
	scenario, ok := Scenario(c)
	if !ok {
		return TargetExpectation{}, fmt.Errorf("reference scenario %q is not canonical", c.Scenario)
	}
	if override, ok := c.TargetOverrides[dialect]; ok {
		switch override.Status {
		case TargetRequired:
			if override.Reason != "" {
				return TargetExpectation{}, fmt.Errorf("required target %s for %q must not carry an exception reason", dialect, c.Scenario)
			}
			if override.ExpectedResult != nil {
				return TargetExpectation{}, fmt.Errorf("required target %s for %q must use the canonical expected result", dialect, c.Scenario)
			}
			if missing := capabilities.Missing(scenario.Requires); len(missing) != 0 {
				return TargetExpectation{}, fmt.Errorf("reference scenario %q is required on %s but capabilities are missing: %v", c.Scenario, dialect, missing)
			}
			return override, nil
		case TargetUnsupported:
			if override.Reason == "" {
				return TargetExpectation{}, fmt.Errorf("reference scenario %q target %s status %q requires a reason", c.Scenario, dialect, override.Status)
			}
			if override.ExpectedResult != nil {
				return TargetExpectation{}, fmt.Errorf("unsupported reference scenario %q target %s must not declare an expected result", c.Scenario, dialect)
			}
			return override, nil
		case TargetIntentionalDifference:
			if override.Reason == "" {
				return TargetExpectation{}, fmt.Errorf("reference scenario %q target %s status %q requires a reason", c.Scenario, dialect, override.Status)
			}
			if override.ExpectedResult == nil {
				return TargetExpectation{}, fmt.Errorf("reference scenario %q target %s intentional difference requires a target expected result", c.Scenario, dialect)
			}
			if missing := capabilities.Missing(scenario.Requires); len(missing) != 0 {
				return TargetExpectation{}, fmt.Errorf("reference scenario %q intentional difference on %s is not executable; missing capabilities: %v", c.Scenario, dialect, missing)
			}
			return override, nil
		default:
			return TargetExpectation{}, fmt.Errorf("reference scenario %q target %s has unknown status %q", c.Scenario, dialect, override.Status)
		}
	}
	if missing := capabilities.Missing(scenario.Requires); len(missing) != 0 {
		return TargetExpectation{Status: TargetUnsupported, Reason: fmt.Sprintf("missing capabilities: %v", missing)}, nil
	}
	return TargetExpectation{Status: TargetRequired}, nil
}
