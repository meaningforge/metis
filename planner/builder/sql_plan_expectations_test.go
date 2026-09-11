package builder

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/sqlplan"
)

// Stable physical-plan names asserted by black-box conversion tests. The
// conversion package owns their production definitions; keeping expected
// literals here avoids widening its production API solely for white-box tests.
const (
	additiveAttributionBaselineBlockID    sqlplan.QueryBlockID = "attribution_baseline"
	additiveAttributionCurrentBlockID     sqlplan.QueryBlockID = "attribution_current"
	additiveAttributionAlignedBlockID     sqlplan.QueryBlockID = "attribution_aligned"
	additiveAttributionTotalBlockID       sqlplan.QueryBlockID = "attribution_total"
	additiveAttributionBaselineValueAlias                      = "baseline_value"
	additiveAttributionCurrentValueAlias                       = "current_value"
	additiveAttributionDeltaAlias                              = "delta"
	additiveAttributionTotalDeltaAlias                         = "total_delta"
	additiveAttributionContributionAlias                       = "contribution_pct"

	ratioAttributionBaselineBlockID       sqlplan.QueryBlockID = "ratio_attribution_baseline"
	ratioAttributionCurrentBlockID        sqlplan.QueryBlockID = "ratio_attribution_current"
	ratioAttributionAlignedBlockID        sqlplan.QueryBlockID = "ratio_attribution_aligned"
	ratioAttributionTotalsBlockID         sqlplan.QueryBlockID = "ratio_attribution_totals"
	ratioAttributionEffectsBlockID        sqlplan.QueryBlockID = "ratio_attribution_effects"
	ratioAttributionReconciliationBlockID sqlplan.QueryBlockID = "ratio_attribution_reconciliation"
	ratioAttributionBaselinePresentAlias                       = "baseline_present"
	ratioAttributionCurrentPresentAlias                        = "current_present"
	ratioAttributionBaselineRateAlias                          = "baseline_rate"
	ratioAttributionCurrentRateAlias                           = "current_rate"
	ratioAttributionSegmentDefinedAlias                        = "segment_defined"
	ratioAttributionRateEffectAlias                            = "rate_effect"
	ratioAttributionMixEffectAlias                             = "mix_effect"
	ratioAttributionEntryEffectAlias                           = "entry_effect"
	ratioAttributionExitEffectAlias                            = "exit_effect"
	ratioAttributionDecomposedDeltaAlias                       = "decomposed_delta"
	ratioAttributionResidualAlias                              = "reconciliation_residual"
	ratioAttributionDefinedAlias                               = "attribution_defined"
	ratioAttributionUndefinedCountAlias                        = "undefined_segment_count"
)

func sqlPlanBlock(plan *sqlplan.Plan, id sqlplan.QueryBlockID) *sqlplan.QueryBlock {
	for i := range plan.Blocks {
		if plan.Blocks[i].ID == id {
			return &plan.Blocks[i]
		}
	}
	return nil
}

func sqlPlanInputBlock(plan *sqlplan.Plan, owner *sqlplan.QueryBlock, alias string) *sqlplan.QueryBlock {
	if owner == nil {
		return nil
	}
	for _, input := range owner.Inputs {
		if input.Alias == alias {
			return sqlPlanBlock(plan, input.Block)
		}
	}
	return nil
}

func metricCTEName(index int, metric string) string {
	var normalized strings.Builder
	for _, r := range metric {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			normalized.WriteRune(r)
		} else {
			normalized.WriteByte('_')
		}
	}
	return fmt.Sprintf("metric_%03d_%s", index+1, normalized.String())
}

func semiAdditiveStateInput(metric string) string {
	return "__metis_state_input_" + strings.ReplaceAll(strings.TrimSpace(metric), ".", "_")
}

func semiAdditiveStateOrderInput(metric string) string {
	return "__metis_state_order_input_" + strings.ReplaceAll(strings.TrimSpace(metric), ".", "_")
}

func semiAdditiveStateOrderColumn(metric string) string {
	return "__metis_state_order_" + strings.ReplaceAll(strings.TrimSpace(metric), ".", "_")
}
