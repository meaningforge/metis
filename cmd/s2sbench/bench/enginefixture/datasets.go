package fixture

import (
	"sort"

	conformance "github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
)

const (
	AnalyticsWorkflows               conformance.ID = "analytics_workflows"
	RatioAttribution                 conformance.ID = "ratio_attribution"
	RatioAttributionUndefinedSegment conformance.ID = "ratio_attribution_undefined_segment"
	RatioAttributionUndefinedTotal   conformance.ID = "ratio_attribution_undefined_total"
	MetricScale                      conformance.ID = "metric_scale"
)

func Lookup(id conformance.ID) (Dataset, bool) {
	build, ok := datasets[id]
	if !ok {
		return Dataset{}, false
	}
	return Dataset{ID: id, Tables: build()}, true
}

// IDs returns every registered canonical dataset in stable order.
func IDs() []conformance.ID {
	ids := make([]conformance.ID, 0, len(datasets))
	for id := range datasets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

var datasets = map[conformance.ID]func() []Table{
	conformance.Commerce:                   commerce,
	conformance.CommerceAdversarial:        commerceAdversarial,
	conformance.TemporalRelationship:       temporalRelationship,
	conformance.DistinctValues:             distinctValues,
	conformance.DefinitionFilters:          definitionFilters,
	conformance.Conversion:                 conversion,
	conformance.OffsetToGrain:              offsetToGrain,
	conformance.OffsetToGrainDense:         offsetToGrainDense,
	conformance.CustomOffsetToGrainDense:   customOffsetToGrainDense,
	conformance.CustomCalendarOffset:       customCalendarOffset,
	conformance.CustomCalendarDense:        customCalendarDense,
	conformance.CustomCalendarRolling:      customCalendarRolling,
	conformance.CustomCalendarGrainToDate:  customCalendarGrainToDate,
	conformance.CustomCalendarSemiAdditive: customCalendarSemiAdditive,
	conformance.SemiAdditiveTieBreak:       semiAdditiveTieBreak,
	conformance.SemiAdditiveNullSkip:       semiAdditiveNullSkip,
	conformance.SemiAdditiveQueriedWindow:  semiAdditiveQueriedWindow,
	conformance.SemiAdditiveWindowGrouping: semiAdditiveWindowGrouping,
	AnalyticsWorkflows:                     analyticsWorkflows,
	RatioAttribution:                       ratioAttribution,
	RatioAttributionUndefinedSegment:       ratioAttributionUndefinedSegment,
	RatioAttributionUndefinedTotal:         ratioAttributionUndefinedTotal,
	MetricScale:                            metricScale,
}

func metricScale() []Table {
	return []Table{t("metric_scale_orders", []string{"id"}, []Column{c("id", String), c("amount", Decimal)},
		r("o1", 10), r("o2", 20))}
}

func c(name string, logicalType LogicalType) Column {
	return Column{Name: name, Type: logicalType}
}

func nullable(name string, logicalType LogicalType) Column {
	return Column{Name: name, Type: logicalType, Nullable: true}
}

func t(name string, keys []string, columns []Column, rows ...[]any) Table {
	return Table{Name: name, KeyColumns: keys, Columns: columns, Rows: rows}
}

func r(values ...any) []any { return values }

var (
	ordersColumns = []Column{
		c("order_id", String), c("customer_id", String), c("product_id", String),
		c("order_date", Date), c("ship_date", Date), c("payment_date", Date),
		c("amount", Decimal), c("discount", Decimal), c("status", String),
	}
	adversarialOrdersColumns = []Column{
		c("order_id", String), c("customer_id", String), c("product_id", String),
		c("order_date", Date), c("ship_date", Date), c("payment_date", Date),
		nullable("amount", Decimal), nullable("discount", Decimal), nullable("status", String),
	}
	customerColumns  = []Column{c("customer_id", String), c("region_code", String), c("region", String), c("segment", String)}
	geographyColumns = []Column{c("region_code", String), c("country", String)}
	productColumns   = []Column{c("product_id", String), c("category", String), c("brand", String)}
)

func commerce() []Table {
	return []Table{
		t("orders", []string{"order_id"}, ordersColumns,
			r("o1", "c1", "p1", "2026-01-15", "2026-01-16", "2026-01-15", 100, 10, "paid"),
			r("o2", "c2", "p2", "2026-01-20", "2026-01-22", "2026-01-20", 200, 20, "paid"),
			r("o3", "c3", "p1", "2026-02-10", "2026-02-12", "2026-02-10", 50, 5, "refunded")),
		t("customer", []string{"customer_id"}, customerColumns,
			r("c1", "r1", "APAC", "enterprise"), r("c2", "r1", "APAC", "smb"), r("c3", "r2", "EU", "smb")),
		t("geography", []string{"region_code"}, geographyColumns, r("r1", "JP"), r("r2", "DE")),
		t("product", []string{"product_id"}, productColumns, r("p1", "electronics", "acme"), r("p2", "apparel", "contoso")),
		t("costs", []string{"cost_date"}, []Column{c("cost_date", Date), c("amount", Decimal)}, r("2026-01-15", 40), r("2026-01-20", 80), r("2026-02-10", 20)),
		t("calendar", []string{"day"}, []Column{c("day", Date)}, r("2026-01-15"), r("2026-01-20"), r("2026-02-10")),
		t("inventory_snapshot", []string{"snapshot_date", "warehouse"}, []Column{c("snapshot_date", Date), c("warehouse", String), c("quantity", Decimal)},
			r("2026-01-01", "tokyo", 10), r("2026-01-01", "osaka", 5), r("2026-03-31", "tokyo", 20), r("2026-03-31", "osaka", 8),
			r("2026-06-30", "tokyo", 30), r("2026-06-30", "osaka", 12), r("2026-07-01", "tokyo", 40), r("2026-07-01", "osaka", 15)),
	}
}

func commerceAdversarial() []Table {
	return []Table{
		t("orders", []string{"order_id"}, adversarialOrdersColumns,
			r("o1", "c1", "p1", "2025-12-31", "2026-01-01", "2025-12-31", 100, 10, "paid"),
			r("o2", "c1", "p2", "2026-01-01", "2026-01-02", "2026-01-01", nil, nil, "paid"),
			r("o3", "c2", "p1", "2026-01-15", "2026-01-16", "2026-01-15", 0, 0, "refunded"),
			r("o4", "c3", "p3", "2026-02-01", "2026-02-02", "2026-02-01", -50, 5, "paid"),
			r("o5", "c_missing", "p_missing", "2026-02-01", "2026-02-02", "2026-02-01", 25, 0, "pending"),
			r("o6", "c2", "p2", "2026-03-01", "2026-03-02", "2026-03-01", 75, -5, "paid"),
			r("o7", "c1", "p1", "2026-03-01", "2026-03-02", "2026-03-01", 10, 2, nil),
			r("o8", "c3", "p2", "2026-03-15", "2026-03-16", "2026-03-15", 0, 0, "cancelled"),
			r("o9", "c3", "p1", "2026-03-20", "2026-03-21", "2026-03-20", 0, 0, "failed")),
		t("customer", []string{"customer_id"}, customerColumns,
			r("c1", "r1", "APAC", "enterprise"), r("c2", "r1", "APAC", "smb"), r("c3", "r2", "EU", "enterprise")),
		t("geography", []string{"region_code"}, geographyColumns, r("r1", "JP"), r("r2", "DE")),
		t("product", []string{"product_id"}, productColumns,
			r("p1", "electronics", "acme"), r("p2", "apparel", "contoso"), r("p3", "electronics", "acme")),
	}
}

func temporalRelationship() []Table {
	return []Table{
		t("temporal_orders", []string{"order_id"}, []Column{c("order_id", String), c("customer_id", String), c("order_time", DateTime), c("status", String), c("amount", Decimal)},
			r("o_before", "c1", "2026-01-31 23:59:59", "paid", 10), r("o_boundary", "c1", "2026-02-01 00:00:00", "paid", 20),
			r("o_current", "c1", "2026-03-01 12:00:00", "paid", 30), r("o_c2", "c2", "2026-01-15 00:00:00", "refunded", 40)),
		t("temporal_customer_history", []string{"history_id"}, []Column{c("history_id", String), c("customer_id", String), c("valid_from", DateTime), nullable("valid_to", DateTime), c("customer_tier", String)},
			r("h1", "c1", "2026-01-01 00:00:00", "2026-02-01 00:00:00", "bronze"), r("h2", "c1", "2026-02-01 00:00:00", nil, "gold"), r("h3", "c2", "2026-01-01 00:00:00", nil, "silver")),
	}
}

func distinctValues() []Table {
	return []Table{t("distinct_value_orders", []string{"region"}, []Column{c("region", String), c("country", String)},
		r("kanto", "JP"), r("kanto", "JP"), r("kansai", "JP"), r("kansai", "JP"), r("west", "US"))}
}

func definitionFilters() []Table {
	return []Table{
		t("definition_filter_orders", []string{"customer_id"}, []Column{c("customer_id", String), c("amount", Decimal)}, r("c1", 100), r("c1", 50), r("c2", 200), r("c3", 25)),
		t("definition_filter_customers", []string{"customer_id"}, []Column{c("customer_id", String), c("tier", String)}, r("c1", "gold"), r("c2", "silver"), r("c3", "gold")),
		t("post_definition_filter_orders", []string{"segment"}, []Column{c("segment", String), c("amount", Decimal), c("order_count_value", Integer)},
			r("enterprise", 120, 1), r("enterprise", 80, 1), r("self_serve", 20, 1), r("self_serve", 40, 1)),
	}
}

func conversion() []Table {
	return []Table{
		t("conversion_signups", []string{"signup_id"}, []Column{c("signup_id", String), c("signup_time", DateTime), c("user_id", String), c("campaign", String), c("signup_value", Integer)},
			r("s1", "2026-01-01 00:00:00", "u1", "A", 1), r("s2", "2026-01-05 00:00:00", "u1", "A", 1), r("s3", "2026-01-02 00:00:00", "u2", "A", 1), r("s4", "2026-01-03 00:00:00", "u4", "A", 1), r("s5", "2026-01-01 00:00:00", "u3", "B", 1)),
		t("conversion_purchases", []string{"purchase_id"}, []Column{c("purchase_id", String), c("purchase_time", DateTime), c("purchaser_id", String), c("purchase_value", Integer)},
			r("p1", "2026-01-06 00:00:00", "u1", 1), r("p2", "2026-01-02 00:00:00", "u3", 1)),
	}
}

func offsetToGrain() []Table {
	return []Table{t("offset_to_grain_orders", []string{"order_date", "region"}, []Column{c("order_date", Date), c("region", String), c("revenue", Decimal)},
		r("2026-01-05", "east", 10), r("2026-02-05", "east", 20), r("2026-03-05", "east", 30), r("2027-01-05", "east", 40), r("2027-02-05", "east", 50), r("2026-01-05", "west", 7), r("2026-02-05", "west", 9))}
}

func offsetToGrainDense() []Table {
	return []Table{
		t("offset_to_grain_dense_orders", []string{"order_date"}, []Column{c("order_date", Date), c("revenue", Decimal)}, r("2026-02-05", 20), r("2026-03-05", 30)),
		t("offset_to_grain_dense_calendar", []string{"day"}, []Column{c("day", Date)}, r("2026-01-01"), r("2026-02-01"), r("2026-03-01")),
	}
}

func customOffsetToGrainDense() []Table {
	return []Table{
		t("custom_offset_to_grain_dense_orders", []string{"order_date"}, []Column{c("order_date", Date), c("revenue", Decimal)}, r("2026-01-12", 20), r("2026-01-19", 30)),
		t("custom_offset_to_grain_dense_calendar", []string{"day"}, []Column{c("day", Date), c("fiscal_week_start", Date), c("fiscal_week_index", Integer), c("fiscal_year_start", Date), c("fiscal_year_index", Integer)},
			r("2026-01-05", "2026-01-05", 1, "2026-01-05", 2026), r("2026-01-12", "2026-01-12", 2, "2026-01-05", 2026), r("2026-01-19", "2026-01-19", 3, "2026-01-05", 2026)),
	}
}

func customCalendarOffset() []Table {
	return []Table{t("calendar", []string{"day"}, []Column{c("day", Date), c("fiscal_week_start", Date), c("fiscal_week_index", Integer), c("revenue", Decimal)},
		r("2026-01-05", "2026-01-05", 10, 10), r("2026-01-06", "2026-01-05", 10, 5), r("2026-01-12", "2026-01-12", 11, 20), r("2026-01-13", "2026-01-12", 11, 2), r("2026-01-19", "2026-01-19", 12, 7))}
}

func customCalendarDense() []Table {
	return []Table{
		t("dense_orders", []string{"order_day"}, []Column{c("order_day", Date), c("revenue", Decimal)}, r("2026-01-05", 10), r("2026-01-06", 5), r("2026-01-19", 7)),
		t("dense_calendar", []string{"calendar_day"}, []Column{c("calendar_day", Date), c("fiscal_week_start", Date), c("fiscal_week_index", Integer)},
			r("2026-01-05", "2026-01-05", 10), r("2026-01-06", "2026-01-05", 10), r("2026-01-12", "2026-01-12", 11), r("2026-01-13", "2026-01-12", 11), r("2026-01-19", "2026-01-19", 12), r("2026-01-20", "2026-01-19", 12)),
	}
}

func customCalendarRolling() []Table {
	return []Table{
		t("rolling_orders", []string{"order_day"}, []Column{c("order_day", Date), c("revenue", Decimal)}, r("2026-01-05", 10), r("2026-01-19", 30), r("2026-01-26", 40)),
		t("rolling_calendar", []string{"calendar_day"}, []Column{c("calendar_day", Date), c("fiscal_week_start", Date), c("fiscal_week_index", Integer)},
			r("2026-01-05", "2026-01-05", 10), r("2026-01-12", "2026-01-12", 11), r("2026-01-19", "2026-01-19", 12), r("2026-01-26", "2026-01-26", 13)),
	}
}

func customCalendarGrainToDate() []Table {
	return []Table{
		t("gtd_orders", []string{"order_day"}, []Column{c("order_day", Date), c("revenue", Decimal)}, r("2026-01-05", 10), r("2026-01-12", 20), r("2026-04-06", 30), r("2026-04-13", 40)),
		t("gtd_calendar", []string{"calendar_day"}, []Column{c("calendar_day", Date), c("fiscal_week_start", Date), c("fiscal_week_index", Integer), c("fiscal_quarter_start", Date), c("fiscal_quarter_index", Integer)},
			r("2026-01-05", "2026-01-05", 10, "2026-01-01", 1), r("2026-01-12", "2026-01-12", 11, "2026-01-01", 1), r("2026-04-06", "2026-04-06", 12, "2026-04-01", 2), r("2026-04-13", "2026-04-13", 13, "2026-04-01", 2)),
	}
}

func customCalendarSemiAdditive() []Table {
	return []Table{t("fiscal_inventory_snapshot", []string{"snapshot_date"}, []Column{c("snapshot_date", Date), c("fiscal_week_start", Date), c("fiscal_week_index", Integer), c("quantity", Decimal)},
		r("2026-01-05", "2026-01-05", 10, 10), r("2026-01-07", "2026-01-05", 10, 13), r("2026-01-12", "2026-01-12", 11, 20), r("2026-01-14", "2026-01-12", 11, 25))}
}

func inventoryEdges(rows ...[]any) []Table {
	return []Table{t("inventory_snapshot_edges", []string{"snapshot_date", "warehouse"}, []Column{c("snapshot_date", Date), c("warehouse", String), nullable("quantity", Decimal), c("snapshot_sequence", Integer)}, rows...)}
}

func semiAdditiveTieBreak() []Table {
	return inventoryEdges(
		r("2026-01-01", "w1", 10, 1), r("2026-01-01", "w1", 20, 2), r("2026-02-01", "w1", 30, 1), r("2026-02-01", "w1", 40, 2),
		r("2026-01-01", "w2", 5, 1), r("2026-01-01", "w2", 15, 2), r("2026-02-01", "w2", 25, 3))
}

func semiAdditiveNullSkip() []Table {
	return inventoryEdges(
		r("2026-01-01", "w1", nil, 1), r("2026-01-01", "w1", 10, 2), r("2026-02-01", "w1", 20, 1), r("2026-02-01", "w1", nil, 2),
		r("2026-01-01", "w2", nil, 1), r("2026-02-01", "w2", 5, 1), r("2026-03-01", "w2", nil, 1))
}

func semiAdditiveQueriedWindow() []Table {
	return inventoryEdges(r("2026-01-05", "w1", 10, 1), r("2026-01-07", "w1", 13, 1), r("2026-01-12", "w1", 20, 1), r("2026-01-14", "w1", 25, 1))
}

func semiAdditiveWindowGrouping() []Table {
	return inventoryEdges(r("2026-01-01", "w1", 10, 1), r("2026-02-01", "w1", 11, 1), r("2026-01-01", "w2", 20, 1), r("2026-03-01", "w2", 22, 1))
}

func analyticsWorkflows() []Table {
	return []Table{t("workflow_events", []string{"event_time", "region"}, []Column{
		c("event_time", DateTime), c("region", String), c("amount", Decimal), c("converted", Decimal), c("sessions", Decimal),
	},
		r("2026-01-15 00:00:00", "west", 5.25, 1, 2),
		r("2026-02-15 00:00:00", "west", 7.25, 3, 4),
	)}
}

func ratioAttribution() []Table {
	return ratioAttributionRows(
		r("2026-07-10 00:00:00", "A", "web", "included", 20, 100),
		r("2026-07-10 00:00:00", "B", "store", "included", 5, 50),
		r("2026-08-10 00:00:00", "A", "web", "included", 36, 120),
		r("2026-08-10 00:00:00", "C", "store", "included", 16, 80),
		r("2026-08-10 00:00:00", "X", "excluded", "excluded", 1000, 1000),
	)
}

func ratioAttributionUndefinedSegment() []Table {
	rows := ratioAttribution()[0].Rows
	return ratioAttributionRows(append(rows,
		r("2026-08-10 00:00:00", "D", "direct", "included", 1, 0),
	)...)
}

func ratioAttributionUndefinedTotal() []Table {
	return ratioAttributionRows(
		r("2026-07-10 00:00:00", "A", "web", "included", 20, 100),
		r("2026-08-10 00:00:00", "D", "direct", "included", 1, 0),
		r("2026-08-10 00:00:00", "X", "excluded", "excluded", 1000, 1000),
	)
}

func ratioAttributionRows(rows ...[]any) []Table {
	return []Table{t("ratio_attribution_events", []string{"event_time", "segment"}, []Column{
		c("event_time", DateTime), c("segment", String), c("channel", String), c("cohort", String), c("converted", Decimal), c("sessions", Decimal),
	}, rows...)}
}
