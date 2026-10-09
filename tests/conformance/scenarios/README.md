# Combination regression oracles

Requests and hand-calculated expectations are shared by the compiler corpus
and all three production Runner/Driver engine harnesses. Logical data lives in
`tests/engine/fixture`; no expected values are generated from Metis output.

| Risk | Canonical scenario | Independent oracle and wrong implementation caught |
| --- | --- | --- |
| Calendar/month end | `calendar_month_end_and_missing_period` | Jan 31 revenue 10 shifts to February's bucket; March 31 revenue 30 shifts to April. Missing current/previous values stay NULL, not current-period copies or implicit zero-fill. |
| Dense missing bucket (reused) | `custom_calendar_dense_missing_period`, `offset_to_grain_missing_boundary_stays_zero` | Existing explicit dense calendar supplies zero; this is distinct from sparse NULL. |
| Rolling input/output ranges | `rolling_window_before_output_range`, `custom_calendar_rolling_filter_preserves_lookback` | Built-in and custom-period windows retain inputs before the visible output range; clipping first produces a distinguishably wrong result. |
| Algebraic AVG rollup | `cumulative_average_uses_weighted_partial_state` | One row of 100 followed by nine rows of 0 produces cumulative AVG 10, not the unweighted average of period averages 50. |
| Nested derived/final top-one | `nested_derived_order_limit_after_aggregation` | APAC (50+70-0)/(50+70)=1 beats EU (100-90)/100=.1. Selecting the largest raw row first picks EU. |
| Aggregate/final top-one | `aggregate_order_limit_after_grouping` | APAC 50+70=120 beats EU 100 despite EU having the largest individual row. |
| DISTINCT/regroup | `distinct_entity_across_periods_global`, `distinct_entity_across_periods_grouped` | The same customer appears in January and March: global 1, each group 1; summing partial distinct counts incorrectly gives 2. |
| Declared one-to-many | `fanout_base_population_unchanged` | Orders 100, 50 and 100 have detail rows, but the unused relationship must not be traversed: 250. |
| Detail membership without fanout | `relationship_exists_filters_source_population` | Two matching orders both have amount 100; one has two matching details. Correct SUM/COUNT/AVG are 200/2/100. A direct join yields SUM 300, while `SUM(DISTINCT amount)` yields 100. |
| Last per account/ties/rollup | `semi_additive_last_ties_then_account_rollup` | Last values selected by date then sequence are w1=40, w2=25; rollup 65, not historical sum 145 or an arbitrary tied row. |

Existing `semi_additive_last_with_tie_break`, `semi_additive_queried_week`,
`semi_additive_window_group_sum` and custom-calendar rolling cases remain in
the same harness and are reused, not duplicated per engine.

## Deliberately rejected combinations

- Literal aggregate-inside-aggregate, such as `SUM(AVG(amount))`, remains
  unsupported (`TestSemanticAnalyzerRejectsNestedAggregate` is reused).
  The top-one result case above covers supported multi-stage derived metric
  composition, not a new nested aggregation capability.
- A sensitive SUM traversing the order-to-details relation is not proven safe.
  `TestOrderAmountFanoutFailsClosedAcrossTargets` requires
  `UNSUPPORTED_RELATIONSHIP_FANOUT`, not a claim that fanout repair exists.
- Re-aggregating finished DISTINCT counts loses entity state.
  `TestDistinctRegroupRejectsLostStateAcrossTargets` requires
  `INVALID_METRIC_ROLLUP` with HOLISTIC algebra on each compiler target.
- Custom fiscal rolling plus a time-range filter is covered by
  `custom_calendar_rolling_filter_preserves_lookback`. The January 19 output
  remains 40 because January 5 is retained for the three-period window; the
  visible range is applied only after ordinal-window evaluation.

Scenario presence proves neither execution nor success. Native execution must
be recorded for the exact commit and engine versions, using the existing
DuckDB job and explicit Doris/ClickHouse matrix. An unavailable environment is
not execution evidence.
