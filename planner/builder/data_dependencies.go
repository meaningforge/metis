package builder

import (
	"fmt"
	"sort"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
)

// DataDependencies is canonical source/field evidence, never a second plan.
// It contains no executable expressions, predicates, placement or identities
// for authentication. Application policy adds transitive field-expression
// dependencies before submitting this workload to its adapter.
type DataDependencies struct {
	Datasets []string
	Fields   []manifest.SemanticReference
}

// RequiredDataDependencies enumerates source obligations before SemanticPlan
// construction, using the same source-path and advanced-metric helpers as Build.
// The supplied MetricEvaluationPlan remains the sole metric identity authority;
// this function must not rebuild it or consume the raw resolver metric list.
func RequiredDataDependencies(q *resolver.SemanticQuerySpec, metrics *evaluation.MetricEvaluationPlan) (DataDependencies, error) {
	if q == nil || q.Model == nil || q.Model.Model == nil {
		return DataDependencies{}, fmt.Errorf("resolved query is required")
	}
	if evaluation.RequiresMetricEvaluation(q) != (metrics != nil) {
		return DataDependencies{}, fmt.Errorf("metric evaluation evidence is inconsistent")
	}
	if metrics != nil {
		if err := evaluation.ValidateMetricEvaluationPlan(metrics); err != nil {
			return DataDependencies{}, err
		}
	}
	c := &dataDependencyCollector{q: q, datasets: map[string]bool{}, fields: map[manifest.SemanticReference]bool{}}
	c.dataset(q.RootDataset)
	groups := make([]semanticplan.GroupBy, 0, len(q.Dimensions))
	for _, dimension := range q.Dimensions {
		if dimension.Field == nil {
			return DataDependencies{}, fmt.Errorf("resolved dimension field is required")
		}
		c.field(dimension.Dataset, dimension.Field.Name)
		_, group, err := PlanResolvedDimension(dimension)
		if err != nil {
			return DataDependencies{}, err
		}
		groups = append(groups, group)
		if calendar := dimension.CustomCalendar; calendar != nil {
			if calendar.Dataset == nil || calendar.BaseTimeField == nil || calendar.BucketField == nil || calendar.OrdinalField == nil {
				return DataDependencies{}, fmt.Errorf("calendar dependency evidence is incomplete")
			}
			c.field(calendar.Dataset.Name, calendar.BaseTimeField.Name)
			c.field(calendar.Dataset.Name, calendar.BucketField.Name)
			c.field(calendar.Dataset.Name, calendar.OrdinalField.Name)
		}
	}
	var predicates []semanticplan.Predicate
	for _, filter := range q.Filters {
		if filter.Kind == resolver.FilterTargetField {
			if filter.Field == nil {
				return DataDependencies{}, fmt.Errorf("resolved filter field is required")
			}
			c.field(filter.Dataset, filter.Field.Name)
		}
		predicates = append(predicates, semanticplan.Predicate{Filter: filter.Filter, Dataset: filter.Dataset, Field: filter.Field, Expression: filter.Expression})
	}
	for _, order := range q.OrderBy {
		if order.Kind == resolver.OrderTargetDimension {
			if order.Field == nil {
				return DataDependencies{}, fmt.Errorf("resolved order field is required")
			}
			c.field(order.Dataset, order.Field.Name)
		}
	}
	for _, relationship := range q.Relationships {
		c.relationship(relationship)
	}
	if spine := q.TimeSpine; spine != nil {
		if spine.Dataset == nil || spine.TimeField == nil {
			return DataDependencies{}, fmt.Errorf("time spine dependency evidence is incomplete")
		}
		c.field(spine.Dataset.Name, spine.TimeField.Name)
	}
	if metrics != nil {
		effectivePredicates, err := conversionEvaluationPredicates(metrics, predicates)
		if err != nil {
			return DataDependencies{}, err
		}
		pre, _, err := SplitEvaluationPredicates(metrics, groups, effectivePredicates)
		if err != nil {
			return DataDependencies{}, err
		}
		byID := make(map[string]evaluation.MetricEvaluationNode, len(metrics.Nodes))
		conversionSide := map[string]bool{}
		for _, node := range metrics.Nodes {
			byID[node.ID] = node
			if node.Spec.Conversion != nil {
				conversionSide[node.Spec.Conversion.Spec.ConversionMetric] = true
			}
		}
		for _, node := range metrics.Nodes {
			dependency, ok := q.Model.MetricDependency(node.ID)
			if !ok {
				return DataDependencies{}, fmt.Errorf("metric source dependency evidence is missing")
			}
			for _, dataset := range dependency.Datasets {
				c.dataset(dataset)
			}
			for _, field := range dependency.References {
				c.field(field.Dataset, field.Field)
			}
			if node.TimeBinding != nil {
				c.dimension(node.TimeBinding.TimeDimension)
			}
			definition, present, err := ossie.MetricDefinitionFilter(node.Metric)
			if err != nil {
				return DataDependencies{}, err
			}
			var intrinsic []semanticplan.Predicate
			if present && ossie.EffectiveMetricDefinitionFilterStage(definition) == ossie.MetricDefinitionFilterStagePreAggregation {
				for _, filter := range definition.Filters {
					handle, err := ResolveDefinitionFilterField(q.Model, filter.Field)
					if err != nil {
						return DataDependencies{}, err
					}
					c.field(handle.Dataset, handle.Field.Name)
					selected, ok := q.FieldExpression(handle.Dataset, handle.Field.Name)
					if !ok {
						return DataDependencies{}, fmt.Errorf("definition filter expression is missing")
					}
					intrinsic = append(intrinsic, semanticplan.Predicate{Filter: filter, Dataset: handle.Dataset, Field: handle.Field, Expression: selected})
				}
			}
			switch node.Kind {
			case evaluation.MetricEvaluationSource:
				requirement, err := BuildMetricSourceRequirement(q.Model, node.ID, dependency)
				if err != nil {
					return DataDependencies{}, err
				}
				sourceGroups, sourcePredicates := groups, pre
				if conversionSide[node.ID] {
					sourceGroups, sourcePredicates = nil, nil
				}
				sourcePredicates = append(append([]semanticplan.Predicate(nil), sourcePredicates...), intrinsic...)
				root, joins, required, _, err := PlanSourceEvaluation(q.Model, node.ID, node.Expression, requirement, sourceGroups, sourcePredicates)
				if err != nil {
					return DataDependencies{}, err
				}
				c.dataset(root)
				for _, dataset := range required {
					c.dataset(dataset)
				}
				for _, join := range joins {
					c.relationship(join.Relationship)
				}
			case evaluation.MetricEvaluationDerived:
			case evaluation.MetricEvaluationCumulative:
				c.dimension(node.Spec.Cumulative.Spec.TimeDimension)
				for _, dimension := range q.Dimensions {
					if dimension.CustomCalendar == nil {
						continue
					}
					if level := dimension.CustomCalendar.Levels[node.Spec.Cumulative.Spec.Window.Unit]; level != nil {
						c.field(dimension.CustomCalendar.Dataset.Name, level.BucketField.Name)
						c.field(dimension.CustomCalendar.Dataset.Name, level.OrdinalField.Name)
					}
				}
			case evaluation.MetricEvaluationTimeOffset:
				c.dimension(node.Spec.TimeOffset.Spec.TimeDimension)
			case evaluation.MetricEvaluationOffsetToGrain:
				c.dimension(node.Spec.OffsetToGrain.Spec.TimeDimension)
				boundary, err := PlanOffsetToGrain(node.ID, node.Spec.OffsetToGrain.Spec, groups)
				if err != nil {
					return DataDependencies{}, err
				}
				if boundary.CustomCalendar {
					c.field(boundary.Dataset.Name, boundary.BoundaryBucket.Name)
					c.field(boundary.Dataset.Name, boundary.QueryOrdinal.Name)
				}
			case evaluation.MetricEvaluationSemiAdditive:
				spec := node.Spec.SemiAdditive.Spec
				c.dimension(spec.NonAdditiveDimension)
				if spec.TieBreakDimension != "" {
					c.dimension(spec.TieBreakDimension)
				}
				for _, field := range spec.WindowGroupings {
					c.dimension(field)
				}
			case evaluation.MetricEvaluationConversion:
				conversion, err := planConversion(q.Model, node.ID, node.Spec.Conversion.Spec, byID)
				if err != nil {
					return DataDependencies{}, err
				}
				for _, field := range append(append([]semanticplan.ConversionFieldRef{}, conversion.BaseEventKey...), conversion.ConversionEventKey...) {
					c.field(field.Dataset, field.Name)
				}
				c.field(conversion.BaseTime.Dataset, conversion.BaseTime.Name)
				c.field(conversion.ConversionTime.Dataset, conversion.ConversionTime.Name)
				pairs := append([]semanticplan.ConversionPropertyPlan{conversion.Entity}, conversion.ConstantProperties...)
				for _, pair := range pairs {
					c.field(pair.Base.Dataset, pair.Base.Name)
					c.field(pair.Conversion.Dataset, pair.Conversion.Name)
				}
			default:
				return DataDependencies{}, fmt.Errorf("unsupported metric dependency kind")
			}
		}
	}
	if c.err != nil {
		return DataDependencies{}, c.err
	}
	out := DataDependencies{}
	for dataset := range c.datasets {
		out.Datasets = append(out.Datasets, dataset)
	}
	for field := range c.fields {
		out.Fields = append(out.Fields, field)
	}
	sort.Strings(out.Datasets)
	sort.Slice(out.Fields, func(i, j int) bool {
		if out.Fields[i].Dataset != out.Fields[j].Dataset {
			return out.Fields[i].Dataset < out.Fields[j].Dataset
		}
		return out.Fields[i].Field < out.Fields[j].Field
	})
	return out, nil
}

type dataDependencyCollector struct {
	q        *resolver.SemanticQuerySpec
	datasets map[string]bool
	fields   map[manifest.SemanticReference]bool
	err      error
}

func (c *dataDependencyCollector) dataset(name string) {
	if c.q.Model.Datasets[name] == nil {
		c.err = fmt.Errorf("source dependency is not indexed")
		return
	}
	c.datasets[name] = true
}
func (c *dataDependencyCollector) field(dataset, name string) {
	c.dataset(dataset)
	h := c.q.Model.Fields[dataset+"."+name]
	if h == nil || h.Field == nil || h.Dataset != dataset || h.Field.Name != name {
		c.err = fmt.Errorf("field dependency is not indexed")
		return
	}
	c.fields[manifest.SemanticReference{Dataset: dataset, Field: name}] = true
}
func (c *dataDependencyCollector) dimension(name string) {
	h, err := c.q.Model.Dimension(name)
	if err != nil || h == nil || h.Field == nil {
		c.err = fmt.Errorf("dimension dependency is not resolved")
		return
	}
	c.field(h.Dataset, h.Field.Name)
}
func (c *dataDependencyCollector) relationship(rel *ossie.Relationship) {
	if rel == nil {
		c.err = fmt.Errorf("relationship dependency is missing")
		return
	}
	for _, field := range rel.FromColumns {
		c.field(rel.From, field)
	}
	for _, field := range rel.ToColumns {
		c.field(rel.To, field)
	}
	spec, ok, err := ossie.TemporalRelationship(rel)
	if err != nil {
		c.err = fmt.Errorf("temporal dependency evidence is invalid")
		return
	}
	if ok {
		c.field(rel.From, spec.FromTimeDimension)
		c.field(rel.To, spec.ToValidFrom)
		c.field(rel.To, spec.ToValidTo)
	}
}
