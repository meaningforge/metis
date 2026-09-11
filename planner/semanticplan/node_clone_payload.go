package semanticplan

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
)

func cloneResolvedExpression(in expression.ResolvedExpression) expression.ResolvedExpression {
	return in.WithExtensionEvidence(in.ExtensionEvidenceValues()...)
}

func cloneMetric(in *ossie.Metric) *ossie.Metric {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneOffsetToGrainPlan(in *OffsetToGrainPlan) *OffsetToGrainPlan {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneCustomCalendarOffsetPlan(in *CustomCalendarOffsetPlan) *CustomCalendarOffsetPlan {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneCustomCalendarCumulativePlan(in *CustomCalendarCumulativePlan) *CustomCalendarCumulativePlan {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneCustomCalendarGrainToDatePlan(in *CustomCalendarGrainToDatePlan) *CustomCalendarGrainToDatePlan {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneConversionMetricSpec(in ossie.ConversionMetricSpec) ossie.ConversionMetricSpec {
	out := in
	if in.Window != nil {
		window := *in.Window
		out.Window = &window
	}
	out.ConstantProperties = append([]ossie.ConversionPropertyPair(nil), in.ConstantProperties...)
	return out
}

func cloneSemiAdditiveMetricSpec(in ossie.SemiAdditiveMetricSpec) ossie.SemiAdditiveMetricSpec {
	out := in
	out.WindowGroupings = append([]string(nil), in.WindowGroupings...)
	return out
}

func cloneConversionPlan(in *ConversionPlan) *ConversionPlan {
	if in == nil {
		return nil
	}
	out := *in
	out.BaseEventKey = append([]ConversionFieldRef(nil), in.BaseEventKey...)
	out.ConversionEventKey = append([]ConversionFieldRef(nil), in.ConversionEventKey...)
	out.ConstantProperties = append([]ConversionPropertyPlan(nil), in.ConstantProperties...)
	out.CandidateMatch.PartitionBy = append([]ConversionFieldRef(nil), in.CandidateMatch.PartitionBy...)
	out.CandidateMatch.Equality = append([]ConversionPropertyPlan(nil), in.CandidateMatch.Equality...)
	out.CandidateMatch.OrderBy = append([]ConversionCandidateOrder(nil), in.CandidateMatch.OrderBy...)
	if in.Window != nil {
		window := *in.Window
		out.Window = &window
	}
	if in.CandidateMatch.Window != nil {
		window := *in.CandidateMatch.Window
		out.CandidateMatch.Window = &window
	}
	return &out
}

func cloneConversionPhysicalInputPlan(in *ConversionPhysicalInputPlan) *ConversionPhysicalInputPlan {
	if in == nil {
		return nil
	}
	out := *in
	out.BaseEventKey = append([]ConversionPhysicalFieldRef(nil), in.BaseEventKey...)
	out.ConversionEventKey = append([]ConversionPhysicalFieldRef(nil), in.ConversionEventKey...)
	out.ConstantProperties = append([]ConversionPhysicalPropertyPlan(nil), in.ConstantProperties...)
	out.BaseValue = cloneConversionEventValuePlan(in.BaseValue)
	out.ConversionValue = cloneConversionEventValuePlan(in.ConversionValue)
	return &out
}

func cloneConversionEventValuePlan(in ConversionEventValuePlan) ConversionEventValuePlan {
	out := in
	if in.Field != nil {
		field := *in.Field
		out.Field = &field
	}
	return out
}

func CloneResolvedExpression(in expression.ResolvedExpression) expression.ResolvedExpression {
	return cloneResolvedExpression(in)
}

func CloneMetric(in *ossie.Metric) *ossie.Metric { return cloneMetric(in) }

func CloneOffsetToGrainPlan(in *OffsetToGrainPlan) *OffsetToGrainPlan {
	return cloneOffsetToGrainPlan(in)
}

func CloneCustomCalendarOffsetPlan(in *CustomCalendarOffsetPlan) *CustomCalendarOffsetPlan {
	return cloneCustomCalendarOffsetPlan(in)
}

func CloneCustomCalendarCumulativePlan(in *CustomCalendarCumulativePlan) *CustomCalendarCumulativePlan {
	return cloneCustomCalendarCumulativePlan(in)
}

func CloneCustomCalendarGrainToDatePlan(in *CustomCalendarGrainToDatePlan) *CustomCalendarGrainToDatePlan {
	return cloneCustomCalendarGrainToDatePlan(in)
}

func CloneConversionMetricSpec(in ossie.ConversionMetricSpec) ossie.ConversionMetricSpec {
	return cloneConversionMetricSpec(in)
}

func CloneSemiAdditiveMetricSpec(in ossie.SemiAdditiveMetricSpec) ossie.SemiAdditiveMetricSpec {
	return cloneSemiAdditiveMetricSpec(in)
}

func CloneConversionPlan(in *ConversionPlan) *ConversionPlan { return cloneConversionPlan(in) }

func CloneConversionPhysicalInputPlan(in *ConversionPhysicalInputPlan) *ConversionPhysicalInputPlan {
	return cloneConversionPhysicalInputPlan(in)
}
