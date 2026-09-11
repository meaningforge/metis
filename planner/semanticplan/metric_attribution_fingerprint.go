package semanticplan

func projectMetricAttributionNodeIdentity(p *projector, nodes []SemanticPlanNode) {
	attributions := make([]AdditiveAttributionNode, 0)
	for _, node := range nodes {
		if attribution, ok := node.(AdditiveAttributionNode); ok {
			attributions = append(attributions, attribution)
		}
	}
	if len(attributions) != 0 {
		p.sequence("metric_attribution_nodes", len(attributions), func(i int) {
			node := attributions[i]
			p.node("additive_attribution", func() {
				p.text("node_id", node.Base.ID)
				p.text("metric_ref", node.MetricRef)
				p.text("time_dimension_ref", node.TimeDimensionRef)
				p.node("time_dimension", func() {
					p.text("dataset", node.TimeDimension.Dataset)
					p.text("expression_source", node.TimeDimension.Expression.Source)
					p.text("expression_dialect", node.TimeDimension.Expression.SourceDialect)
				})
				p.text("dimension_ref", node.DimensionRef)
				p.node("baseline", func() {
					p.text("start", canonicalAttributionInstant(node.Baseline.Start))
					p.text("end", canonicalAttributionInstant(node.Baseline.End))
				})
				p.node("current", func() {
					p.text("start", canonicalAttributionInstant(node.Current.Start))
					p.text("end", canonicalAttributionInstant(node.Current.End))
				})
				p.text("decomposition_kind", "additive")
				p.text("strategy", "additive_contribution")
				p.text("reconciliation", "sum_segment_delta_equals_metric_delta")
			})
		})
	}

	ratioAttributions := make([]RatioAttributionNode, 0)
	for _, node := range nodes {
		if attribution, ok := node.(RatioAttributionNode); ok {
			ratioAttributions = append(ratioAttributions, attribution)
		}
	}
	if len(ratioAttributions) == 0 {
		return
	}
	p.sequence("ratio_metric_attribution_nodes", len(ratioAttributions), func(i int) {
		node := ratioAttributions[i]
		p.node("ratio_attribution", func() {
			p.text("node_id", node.Base.ID)
			p.text("metric_ref", node.MetricRef)
			p.text("numerator_ref", node.NumeratorRef)
			p.text("denominator_ref", node.DenominatorRef)
			p.text("time_dimension_ref", node.TimeDimensionRef)
			p.node("time_dimension", func() {
				p.text("dataset", node.TimeDimension.Dataset)
				p.text("expression_source", node.TimeDimension.Expression.Source)
				p.text("expression_dialect", node.TimeDimension.Expression.SourceDialect)
			})
			p.text("dimension_ref", node.DimensionRef)
			p.node("baseline", func() {
				p.text("start", canonicalAttributionInstant(node.Baseline.Start))
				p.text("end", canonicalAttributionInstant(node.Baseline.End))
			})
			p.node("current", func() {
				p.text("start", canonicalAttributionInstant(node.Current.Start))
				p.text("end", canonicalAttributionInstant(node.Current.End))
			})
			p.text("decomposition_kind", "ratio")
			p.text("strategy", "ratio_mix_rate")
			p.text("reconciliation", "sum_mix_plus_rate_equals_metric_delta")
			p.text("undefined_ratio_policy", string(RatioAttributionUndefinedNullWithDefinedFlag))
			p.text("population_alignment", string(RatioAttributionFullUnionEntryExit))
		})
	})
}
