package ossie

import (
	"github.com/meaningforge/metis/serrors"
)

func ValidateDocument(doc *Document) error {
	if doc == nil {
		return invalid("document is nil", nil)
	}
	if doc.Version != SupportedSpecVersion {
		return invalid("unsupported Apache Ossie specification version", map[string]any{"got": doc.Version, "supported": SupportedSpecVersion})
	}
	if len(doc.SemanticModel) == 0 && len(doc.Ontology) == 0 && len(doc.OntologyMappings) == 0 {
		return invalid("document must contain semantic_model or ontology content", nil)
	}
	if len(doc.Ontology) > 0 && doc.Name == "" {
		return invalid("ontology document name is required", nil)
	}
	seenModels := map[string]struct{}{}
	for i := range doc.SemanticModel {
		m := &doc.SemanticModel[i]
		if m.Name == "" {
			return invalid("semantic model name is required", map[string]any{"index": i})
		}
		if _, ok := seenModels[m.Name]; ok {
			return invalid("duplicate semantic model name", map[string]any{"model": m.Name})
		}
		seenModels[m.Name] = struct{}{}
		if err := validateModel(m); err != nil {
			return err
		}
		if _, _, err := SemanticModelDataSource(m); err != nil {
			return invalid("invalid METIS data-source extension", map[string]any{"model": m.Name, "cause": err.Error()})
		}
		if spec, ok, err := TimeSpine(m); err != nil {
			return invalid("invalid METIS time-spine extension", map[string]any{"model": m.Name, "cause": err.Error()})
		} else if ok {
			if err := ValidateTimeSpineModel(m, spec); err != nil {
				return invalid("invalid METIS time-spine extension", map[string]any{"model": m.Name, "cause": err.Error()})
			}
		}
		if spec, ok, err := CustomCalendar(m); err != nil {
			return invalid("invalid METIS custom-calendar extension", map[string]any{"model": m.Name, "cause": err.Error()})
		} else if ok {
			if err := ValidateCustomCalendarModel(m, spec); err != nil {
				return invalid("invalid METIS custom-calendar extension", map[string]any{"model": m.Name, "cause": err.Error()})
			}
		}
	}
	if err := validateAssetGovernanceDocument(doc); err != nil {
		return invalid("invalid METIS asset-governance extension", map[string]any{"cause": err.Error()})
	}
	return nil
}
func validateModel(m *SemanticModel) error {
	if len(m.Datasets) == 0 {
		return invalid("semantic model must contain at least one dataset", map[string]any{"model": m.Name})
	}
	datasets := make(map[string]Dataset, len(m.Datasets))
	for _, ds := range m.Datasets {
		if ds.Name == "" || ds.Source == "" {
			return invalid("dataset name/source are required", map[string]any{"model": m.Name, "dataset": ds.Name})
		}
		if _, ok := datasets[ds.Name]; ok {
			return invalid("duplicate dataset name", map[string]any{"model": m.Name, "dataset": ds.Name})
		}
		datasets[ds.Name] = ds
		fields := map[string]struct{}{}
		for _, f := range ds.Fields {
			if f.Name == "" {
				return invalid("field name is required", map[string]any{"model": m.Name, "dataset": ds.Name})
			}
			if _, ok := fields[f.Name]; ok {
				return invalid("duplicate field name", map[string]any{"model": m.Name, "dataset": ds.Name, "field": f.Name})
			}
			fields[f.Name] = struct{}{}
			if err := validateExpression(f.Expression, map[string]any{"model": m.Name, "dataset": ds.Name, "field": f.Name}); err != nil {
				return err
			}
			if f.Datatype != "" && !validDataType(f.Datatype) {
				return invalid("unsupported Ossie datatype", map[string]any{"model": m.Name, "dataset": ds.Name, "field": f.Name, "datatype": f.Datatype})
			}
		}
	}
	metrics := map[string]struct{}{}
	cumulativeSpecs := map[string]CumulativeMetricSpec{}
	offsetSpecs := map[string]TimeOffsetMetricSpec{}
	semiSpecs := map[string]SemiAdditiveMetricSpec{}
	timeBindings := map[string]MetricTimeBindingSpec{}
	definitionFilters := map[string]MetricDefinitionFilterSpec{}
	conversionSpecs := map[string]ConversionMetricSpec{}
	for i := range m.Metrics {
		metric := &m.Metrics[i]
		if metric.Name == "" {
			return invalid("metric name is required", map[string]any{"model": m.Name})
		}
		if _, ok := metrics[metric.Name]; ok {
			return invalid("duplicate metric name", map[string]any{"model": m.Name, "metric": metric.Name})
		}
		metrics[metric.Name] = struct{}{}
		if err := validateExpression(metric.Expression, map[string]any{"model": m.Name, "metric": metric.Name}); err != nil {
			return err
		}
		if metric.Datatype != "" && !validDataType(metric.Datatype) {
			return invalid("unsupported Ossie datatype", map[string]any{"model": m.Name, "metric": metric.Name, "datatype": metric.Datatype})
		}
		if _, _, err := AgentDiscoverySpec(metric); err != nil {
			return invalid("invalid METIS agent-discovery metric extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		}
		if _, _, err := MetricQualityClaims(metric); err != nil {
			return invalid("invalid METIS metric quality-claims extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		}
		if spec, ok, err := CumulativeSpec(metric); err != nil {
			return invalid("invalid METIS cumulative metric extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		} else if ok {
			cumulativeSpecs[metric.Name] = spec
		}
		if spec, ok, err := TimeOffsetSpec(metric); err != nil {
			return invalid("invalid METIS time-offset metric extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		} else if ok {
			offsetSpecs[metric.Name] = spec
		}
		if spec, ok, err := SemiAdditiveSpec(metric); err != nil {
			return invalid("invalid METIS semi-additive metric extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		} else if ok {
			semiSpecs[metric.Name] = spec
		}
		if spec, ok, err := MetricTimeBinding(metric); err != nil {
			return invalid("invalid METIS metric time-binding extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		} else if ok {
			timeBindings[metric.Name] = spec
		}
		if spec, ok, err := MetricDefinitionFilter(metric); err != nil {
			return invalid("invalid METIS metric definition-filter extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		} else if ok {
			definitionFilters[metric.Name] = spec
		}
		if spec, ok, err := ConversionSpec(metric); err != nil {
			return invalid("invalid METIS conversion metric extension", map[string]any{"model": m.Name, "metric": metric.Name, "cause": err.Error()})
		} else if ok {
			conversionSpecs[metric.Name] = spec
		}
	}
	if err := validateOffsetToGrainMetrics(m, metrics); err != nil {
		return err
	}
	for metricName, spec := range cumulativeSpecs {
		if err := validateMetricReferenceAndTimeDimension(m, metrics, metricName, spec.BaseMetric, spec.TimeDimension, "cumulative"); err != nil {
			return err
		}
		if err := ValidateCumulativeCalendarModel(m, spec); err != nil {
			return invalid("invalid METIS cumulative metric extension", map[string]any{"model": m.Name, "metric": metricName, "cause": err.Error()})
		}
	}
	for metricName, spec := range offsetSpecs {
		if err := validateMetricReferenceAndTimeDimension(m, metrics, metricName, spec.BaseMetric, spec.TimeDimension, "time-offset"); err != nil {
			return err
		}
		if err := ValidateTimeOffsetCalendarModel(m, spec); err != nil {
			return invalid("invalid METIS time-offset extension", map[string]any{"model": m.Name, "metric": metricName, "cause": err.Error()})
		}
	}
	for metricName, spec := range semiSpecs {
		if err := validateMetricReferenceAndTimeDimension(m, metrics, metricName, spec.BaseMetric, spec.NonAdditiveDimension, "semi-additive"); err != nil {
			return err
		}
	}
	for metricName, spec := range timeBindings {
		if err := validateMetricTimeDimension(m, metricName, spec.TimeDimension, "time-binding"); err != nil {
			return err
		}
	}
	for metricName, spec := range definitionFilters {
		if err := ValidateMetricDefinitionFilterModel(m, metricName, spec); err != nil {
			return invalid("invalid METIS metric definition-filter extension", map[string]any{"model": m.Name, "metric": metricName, "cause": err.Error()})
		}
	}
	for metricName, spec := range conversionSpecs {
		if err := ValidateConversionModel(m, metricName, spec); err != nil {
			return invalid("invalid METIS conversion metric extension", map[string]any{"model": m.Name, "metric": metricName, "cause": err.Error()})
		}
	}
	relationships := map[string]struct{}{}
	for i := range m.Relationships {
		rel := &m.Relationships[i]
		if rel.Name == "" || rel.From == "" || rel.To == "" {
			return invalid("relationship name/from/to are required", map[string]any{"model": m.Name, "relationship": rel.Name})
		}
		if _, ok := relationships[rel.Name]; ok {
			return invalid("duplicate relationship name", map[string]any{"model": m.Name, "relationship": rel.Name})
		}
		relationships[rel.Name] = struct{}{}
		if _, ok := datasets[rel.From]; !ok {
			return invalid("relationship source dataset not found", map[string]any{"model": m.Name, "relationship": rel.Name, "dataset": rel.From})
		}
		if _, ok := datasets[rel.To]; !ok {
			return invalid("relationship target dataset not found", map[string]any{"model": m.Name, "relationship": rel.Name, "dataset": rel.To})
		}
		if len(rel.FromColumns) == 0 || len(rel.FromColumns) != len(rel.ToColumns) {
			return invalid("relationship columns must be non-empty and have matching cardinality", map[string]any{"model": m.Name, "relationship": rel.Name})
		}
		if spec, ok, err := TemporalRelationship(rel); err != nil {
			return invalid("invalid METIS temporal relationship extension", map[string]any{"model": m.Name, "relationship": rel.Name, "cause": err.Error()})
		} else if ok {
			if err := ValidateTemporalRelationshipModel(m, rel, spec); err != nil {
				return invalid("invalid METIS temporal relationship extension", map[string]any{"model": m.Name, "relationship": rel.Name, "cause": err.Error()})
			}
		}
	}
	return nil
}
func validateMetricReferenceAndTimeDimension(m *SemanticModel, metrics map[string]struct{}, metricName, baseMetric, dimensionName, kind string) error {
	if _, ok := metrics[baseMetric]; !ok {
		return invalid(kind+" base metric not found", map[string]any{"model": m.Name, "metric": metricName, "base_metric": baseMetric})
	}
	if baseMetric == metricName {
		return invalid(kind+" metric cannot reference itself", map[string]any{"model": m.Name, "metric": metricName})
	}
	return validateMetricTimeDimension(m, metricName, dimensionName, kind)
}
func validateMetricTimeDimension(m *SemanticModel, metricName, dimensionName, kind string) error {
	matches := 0
	validTime := false
	for _, ds := range m.Datasets {
		for _, field := range ds.Fields {
			if field.Name != dimensionName {
				continue
			}
			matches++
			if field.Dimension != nil && ((field.Dimension.IsTime != nil && *field.Dimension.IsTime) || (field.Dimension.IsTime == nil && isTemporalDataType(field.Datatype))) {
				validTime = true
			}
		}
	}
	if matches == 0 {
		return invalid(kind+" time dimension not found", map[string]any{"model": m.Name, "metric": metricName, "time_dimension": dimensionName})
	}
	if matches > 1 {
		return invalid(kind+" time dimension is ambiguous; use a unique field name", map[string]any{"model": m.Name, "metric": metricName, "time_dimension": dimensionName})
	}
	if !validTime {
		return invalid(kind+" time_dimension must reference a time dimension", map[string]any{"model": m.Name, "metric": metricName, "time_dimension": dimensionName})
	}
	return nil
}
func isTemporalDataType(v DataType) bool {
	switch v {
	case DataTypeDate, DataTypeTime, DataTypeDateTime, DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}
func validateExpression(expr Expression, details map[string]any) error {
	if len(expr.Dialects) == 0 {
		return invalid("expression must contain at least one dialect", details)
	}
	for _, d := range expr.Dialects {
		if !IsSupportedExpressionDialect(d.Dialect) {
			copy := cloneDetails(details)
			copy["dialect"] = d.Dialect
			return invalid("unsupported expression dialect", copy)
		}
		if d.Expression == "" {
			return invalid("dialect expression is required", details)
		}
	}
	return nil
}
func validDataType(v DataType) bool {
	switch string(v) {
	case "String", "Integer", "Decimal", "Float", "Boolean", "Date", "Time", "DateTime", "DateTimeTz", "Opaque":
		return true
	default:
		return false
	}
}
func cloneDetails(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
func invalid(msg string, details map[string]any) error {
	return &serrors.Error{Code: serrors.ErrInvalidModel, Message: msg, Details: details}
}
