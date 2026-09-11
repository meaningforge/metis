package semanticplan

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

type projector struct {
	w   io.Writer
	err error
}

func (p *projector) write(format string, args ...any) {
	if p.err != nil {
		return
	}
	if _, err := fmt.Fprintf(p.w, format, args...); err != nil {
		p.err = err
	}
}

func (p *projector) node(name string, body func()) {
	p.write("%d:%s{", len(name), name)
	body()
	p.write("}")
}

func (p *projector) absent(name string) { p.write("%d:%s~", len(name), name) }
func (p *projector) text(name, value string) {
	p.write("%d:%s=%d:%s;", len(name), name, len(value), value)
}
func (p *projector) flag(name string, value bool)  { p.write("%d:%s=%t;", len(name), name, value) }
func (p *projector) number(name string, value int) { p.write("%d:%s=%d;", len(name), name, value) }
func (p *projector) optionalNumber(name string, value *int) {
	if value == nil {
		p.absent(name)
		return
	}
	p.number(name, *value)
}
func (p *projector) optionalText(name string, value *string) {
	if value == nil {
		p.absent(name)
		return
	}
	p.text(name, *value)
}
func (p *projector) sequence(name string, length int, each func(index int)) {
	p.write("%d:%s[%d", len(name), name, length)
	for i := 0; i < length; i++ {
		p.write("|")
		each(i)
	}
	p.write("]")
}
func (p *projector) textSequence(name string, values []string) {
	p.sequence(name, len(values), func(i int) { p.write("%d:%s;", len(values[i]), values[i]) })
}
func (p *projector) textSet(name string, values []string) {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	p.textSequence(name, sorted)
}
func (p *projector) mapping(name string, keys []string, each func(key string)) {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	p.sequence(name, len(sorted), func(i int) {
		p.text("key", sorted[i])
		each(sorted[i])
	})
}
func (p *projector) opaque(name string, value any) {
	if value == nil {
		p.absent(name)
		return
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		if p.err == nil {
			p.err = fmt.Errorf("project %s: %w", name, err)
		}
		return
	}
	p.write("%d:%s=%d:%s;", len(name), name, len(encoded), encoded)
}

// ProjectPlan writes the stable structural projection used by deterministic
// semantic-plan observation. It intentionally excludes planner trace and
// non-structural declaration metadata.
func ProjectPlan(w io.Writer, plan *SemanticPlan) error {
	p := &projector{w: w}
	projectSemanticPlan(p, plan)
	return p.err
}

func projectSemanticPlan(p *projector, plan *SemanticPlan) {
	p.node("plan", func() {
		if plan.PolicyScope != "" {
			p.text("policy_scope", plan.PolicyScope)
		}
		projectModelRef(p, "model", plan.Model)
		projectDatasetRef(p, "root", plan.Root)
		p.sequence("joins", len(plan.Joins), func(i int) { projectJoin(p, plan.Joins[i]) })
		p.sequence("projections", len(plan.Projections), func(i int) { projectProjection(p, plan.Projections[i]) })
		p.sequence("predicates", len(plan.Predicates), func(i int) { projectPredicate(p, plan.Predicates[i]) })
		p.sequence("groups", len(plan.Groups), func(i int) { projectGroupBy(p, plan.Groups[i]) })
		p.sequence("sorts", len(plan.Sorts), func(i int) { projectSort(p, plan.Sorts[i]) })
		p.optionalNumber("limit", plan.Limit)
		p.textSet("requested", plan.Requested)
		p.sequence("nodes", len(plan.Nodes), func(i int) { projectSemanticPlanNode(p, plan.Nodes[i]) })
		projectSemanticOutputContract(p, "output", plan.Output)
		if plan.SharedGrain == nil {
			p.absent("shared_grain")
		} else {
			projectSharedGrain(p, "shared_grain", *plan.SharedGrain)
		}
		projectDenseCalendar(p, "dense_calendar", plan.DenseCalendar)
		projectCustomDenseCalendar(p, "custom_dense_calendar", plan.CustomDenseCalendar)
	})
}

func projectModelRef(p *projector, name string, model ModelRef) {
	p.node(name, func() {
		p.text("project", model.Project)
		p.text("name", model.Name)
	})
}
func projectDatasetRef(p *projector, name string, dataset DatasetRef) {
	p.node(name, func() {
		p.text("name", dataset.Name)
		p.text("source", dataset.Source)
		projectRelationPolicy(p, dataset.Policy)
	})
}
func projectJoin(p *projector, join Join) {
	p.node("join", func() {
		projectRelationPolicy(p, join.Policy)
		projectRelationship(p, "relationship", join.Relationship)
		projectTemporalSpec(p, "temporal", join.Temporal)
		p.text("from_dataset", join.FromDataset)
		p.text("from_source", join.FromSource)
		p.text("to_dataset", join.ToDataset)
		p.text("to_source", join.ToSource)
	})
}
func projectProjection(p *projector, projection Projection) {
	p.node("projection", func() {
		p.text("name", projection.Name)
		p.text("kind", string(projection.Kind))
		projectMetric(p, "metric", projection.Metric)
		projectField(p, "field", projection.Field)
		p.text("dataset", projection.Dataset)
		p.textSet("datasets", projection.Datasets)
		projectGrain(p, "grain", projection.Grain)
		projectResolvedExpression(p, "expression", projection.Expression)
		projectCustomCalendarGrouping(p, "custom_calendar", projection.CustomCalendar)
	})
}
func projectPredicate(p *projector, predicate Predicate) {
	p.node("predicate", func() {
		projectFilter(p, "filter", predicate.Filter)
		p.text("dataset", predicate.Dataset)
		projectField(p, "field", predicate.Field)
		projectResolvedExpression(p, "expression", predicate.Expression)
	})
}
func projectGroupBy(p *projector, group GroupBy) {
	p.node("group", func() {
		p.text("name", group.Name)
		p.text("dataset", group.Dataset)
		projectField(p, "field", group.Field)
		projectGrain(p, "grain", group.Grain)
		projectResolvedExpression(p, "expression", group.Expression)
		projectCustomCalendarGrouping(p, "custom_calendar", group.CustomCalendar)
	})
}
func projectSort(p *projector, sort Sort) {
	p.node("sort", func() {
		p.text("name", sort.Name)
		p.text("kind", string(sort.Kind))
		p.text("direction", string(sort.Direction))
		projectMetric(p, "metric", sort.Metric)
		projectField(p, "field", sort.Field)
		p.text("dataset", sort.Dataset)
		projectResolvedExpression(p, "expression", sort.Expression)
	})
}
func projectFilter(p *projector, name string, filter query.Filter) {
	p.node(name, func() {
		p.text("field", filter.Field)
		p.text("operator", string(filter.Operator))
		p.opaque("value", filter.Value)
	})
}
func projectGrain(p *projector, name string, grain *query.TimeGrain) {
	if grain == nil {
		p.absent(name)
		return
	}
	p.text(name, string(*grain))
}
func projectResolvedExpression(p *projector, name string, resolved expression.ResolvedExpression) {
	p.node(name, func() {
		p.text("dialect", resolved.SourceDialect)
		p.text("source", resolved.Source)
	})
}
func projectField(p *projector, name string, field *ossie.Field) {
	if field == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		p.text("name", field.Name)
		p.text("datatype", string(field.Datatype))
	})
}
func projectMetric(p *projector, name string, metric *ossie.Metric) {
	if metric == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		p.text("name", metric.Name)
		p.text("datatype", string(metric.Datatype))
	})
}
func projectRelationship(p *projector, name string, relationship *ossie.Relationship) {
	if relationship == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		p.text("name", relationship.Name)
		p.text("from", relationship.From)
		p.textSequence("from_columns", relationship.FromColumns)
		p.text("to", relationship.To)
		p.textSequence("to_columns", relationship.ToColumns)
	})
}
func projectTemporalSpec(p *projector, name string, spec *ossie.TemporalRelationshipSpec) {
	if spec == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		p.text("kind", spec.Kind)
		p.text("from_time_dimension", spec.FromTimeDimension)
		p.text("to_valid_from", spec.ToValidFrom)
		p.text("to_valid_to", spec.ToValidTo)
		p.text("cardinality", spec.Cardinality)
	})
}
func projectSharedGrain(p *projector, name string, shared SharedGrainResolution) {
	p.node(name, func() {
		p.sequence("grain", len(shared.Grain), func(i int) { projectGroupBy(p, shared.Grain[i]) })
		p.text("grain_key", shared.GrainKey)
		p.sequence("metrics", len(shared.Metrics), func(i int) { projectMetricSharedGrainEvidence(p, "metric", shared.Metrics[i]) })
	})
}
func projectSourceScanWork(p *projector, work SourceScanWork) {
	p.node("source_subplan", func() {
		projectDatasetRef(p, "root", work.Root)
		p.textSequence("source_roots", work.SourceRoots)
		p.textSequence("required_datasets", work.RequiredDatasets)
		p.sequence("joins", len(work.Joins), func(i int) { projectJoin(p, work.Joins[i]) })
		p.sequence("output_grain", len(work.OutputGrain), func(i int) { projectGroupBy(p, work.OutputGrain[i]) })
		p.sequence("pre_aggregation_predicates", len(work.Predicates), func(i int) { projectPredicate(p, work.Predicates[i]) })
	})
}
func projectMetricSharedGrainEvidence(p *projector, name string, evidence MetricSharedGrainEvidence) {
	p.node(name, func() {
		p.text("metric", evidence.Metric)
		p.text("root_dataset", evidence.RootDataset)
		p.textSet("root_datasets", evidence.RootDatasets)
		p.text("grain_key", evidence.GrainKey)
		p.textSequence("relationship_path", evidence.RelationshipPath)
		p.sequence("relationship_paths", len(evidence.RelationshipPaths), func(i int) { p.textSequence("path", evidence.RelationshipPaths[i]) })
		p.flag("fanout_safe", evidence.FanoutSafe)
	})
}
func projectDenseCalendar(p *projector, name string, plan *DenseCalendarPlan) {
	if plan == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		projectDatasetRef(p, "dataset", plan.Dataset)
		projectField(p, "time_field", plan.TimeField)
		p.text("time_expression", plan.TimeExpression)
		p.text("query_time_dimension", plan.QueryTimeDimension)
		p.text("grain", string(plan.Grain))
		p.sequence("output_predicates", len(plan.OutputPredicates), func(i int) { projectPredicate(p, plan.OutputPredicates[i]) })
		p.sequence("read_predicates", len(plan.ReadPredicates), func(i int) { projectPredicate(p, plan.ReadPredicates[i]) })
	})
}
func projectCustomDenseCalendar(p *projector, name string, plan *CustomDenseCalendarPlan) {
	if plan == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		projectDatasetRef(p, "dataset", plan.Dataset)
		p.text("query_time_dimension", plan.QueryTimeDimension)
		p.text("grain", string(plan.Grain))
		projectField(p, "bucket_field", plan.BucketField)
		p.text("bucket_expression", plan.BucketExpression)
		projectField(p, "ordinal_field", plan.OrdinalField)
		p.text("ordinal_expression", plan.OrdinalExpression)
	})
}
func projectCustomCalendarOffset(p *projector, name string, plan CustomCalendarOffsetPlan) {
	p.node(name, func() {
		p.text("grain", string(plan.Grain))
		p.number("count", plan.Count)
		projectDatasetRef(p, "dataset", plan.Dataset)
		projectField(p, "bucket_field", plan.BucketField)
		p.text("bucket_expression", plan.BucketExpression)
		projectField(p, "ordinal_field", plan.OrdinalField)
		p.text("ordinal_expression", plan.OrdinalExpression)
	})
}
func projectCustomCalendarCumulative(p *projector, name string, plan CustomCalendarCumulativePlan) {
	p.node(name, func() {
		p.text("grain", string(plan.Grain))
		p.number("count", plan.Count)
		projectDatasetRef(p, "dataset", plan.Dataset)
		projectField(p, "bucket_field", plan.BucketField)
		p.text("bucket_expression", plan.BucketExpression)
		projectField(p, "ordinal_field", plan.OrdinalField)
		p.text("ordinal_expression", plan.OrdinalExpression)
	})
}
func projectCustomCalendarGrainToDate(p *projector, name string, plan CustomCalendarGrainToDatePlan) {
	p.node(name, func() {
		p.text("query_grain", string(plan.QueryGrain))
		p.text("reset_grain", string(plan.ResetGrain))
		projectDatasetRef(p, "dataset", plan.Dataset)
		projectField(p, "reset_bucket_field", plan.ResetBucketField)
		p.text("reset_bucket_expression", plan.ResetBucketExpression)
		projectField(p, "query_ordinal_field", plan.QueryOrdinalField)
		p.text("query_ordinal_expression", plan.QueryOrdinalExpression)
	})
}
func projectCustomCalendarGrouping(p *projector, name string, grouping *CustomCalendarGrouping) {
	if grouping == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		projectCustomCalendarSpec(p, "spec", grouping.Spec)
		p.text("grain", string(grouping.Grain))
		p.text("dataset", grouping.Dataset)
		p.text("dataset_source", grouping.DatasetSource)
		projectField(p, "base_time_field", grouping.BaseTimeField)
		p.text("base_time_expression", grouping.BaseTimeExpression)
		projectField(p, "bucket_field", grouping.BucketField)
		p.text("bucket_expression", grouping.BucketExpression)
		projectField(p, "ordinal_field", grouping.OrdinalField)
		p.text("ordinal_expression", grouping.OrdinalExpression)
		p.mapping("levels", mapKeys(grouping.Levels), func(key string) {
			level := grouping.Levels[key]
			p.node("level", func() {
				projectCustomCalendarGrain(p, "grain", level.Grain)
				projectField(p, "bucket_field", level.BucketField)
				p.text("bucket_expression", level.BucketExpression)
				projectField(p, "ordinal_field", level.OrdinalField)
				p.text("ordinal_expression", level.OrdinalExpression)
			})
		})
	})
}
func projectCustomCalendarSpec(p *projector, name string, spec ossie.CustomCalendarSpec) {
	p.node(name, func() {
		p.text("kind", spec.Kind)
		p.text("dataset", spec.Dataset)
		p.text("base_time", spec.BaseTime)
		p.sequence("grains", len(spec.Grains), func(i int) { projectCustomCalendarGrain(p, "grain", spec.Grains[i]) })
	})
}
func projectCustomCalendarGrain(p *projector, name string, grain ossie.CustomCalendarGrain) {
	p.node(name, func() {
		p.text("name", grain.Name)
		p.text("bucket_dimension", grain.BucketDimension)
		p.text("ordinal_dimension", grain.OrdinalDimension)
		p.flag("dense_mapping", grain.DenseMapping)
		p.text("parent_grain", grain.ParentGrain)
	})
}

func projectSemanticPlanNode(p *projector, node SemanticPlanNode) {
	if err := ValidateNode(node); err != nil {
		if p.err == nil {
			p.err = err
		}
		return
	}
	base := node.NodeBase()
	source, _ := NodeSourceState(node)
	metric, _ := NodeMetricState(node)
	p.node("node", func() {
		p.text("id", base.ID)
		p.text("kind", string(node.Kind()))
		p.text("boundary", string(base.Boundary))
		p.node("predicate_boundary_evidence", func() {
			p.text("movement", string(base.PredicateBoundaryEvidence.Movement))
			p.text("proof", string(base.PredicateBoundaryEvidence.Proof))
		})
		p.sequence("inputs", len(base.Inputs), func(i int) {
			input := base.Inputs[i]
			p.node("input", func() {
				p.text("node_id", input.NodeID)
				p.sequence("grain", len(input.Grain), func(j int) { projectGroupBy(p, input.Grain[j]) })
			})
		})
		p.textSet("metrics", metric.Metrics)
		p.textSet("dimensions", base.Dimensions)
		p.sequence("output_grain", len(base.OutputGrain), func(i int) { projectGroupBy(p, base.OutputGrain[i]) })
		p.textSet("source_roots", source.SourceRoots)
		p.textSet("required_datasets", source.RequiredDatasets)
		projectDatasetRef(p, "root", source.Root)
		p.sequence("joins", len(source.Joins), func(i int) { projectJoin(p, source.Joins[i]) })
		if len(source.PopulationPreservationEvidence) != 0 {
			p.sequence("population_preservation_evidence", len(source.PopulationPreservationEvidence), func(i int) {
				projectPopulationPreservationEvidence(p, source.PopulationPreservationEvidence[i])
			})
		}
		p.sequence("predicates", len(base.Predicates), func(i int) { projectSemanticPlanNodePredicate(p, base.Predicates[i]) })
		p.text("share_group", metric.ShareGroup)
		if metric.SharedGrainEvidence == nil {
			p.absent("shared_grain_evidence")
		} else {
			projectMetricSharedGrainEvidence(p, "shared_grain_evidence", *metric.SharedGrainEvidence)
		}
		projectSemanticPlanNodeEvaluation(p, "evaluation", node)
	})
}

func projectPopulationPreservationEvidence(p *projector, evidence PopulationPreservationEvidence) {
	p.node("population_preservation", func() {
		p.text("relationship", evidence.Relationship)
		p.text("from_dataset", evidence.FromDataset)
		p.text("to_dataset", evidence.ToDataset)
		p.text("multiplicity", string(evidence.Multiplicity))
		p.sequence("obligations", len(evidence.Obligations), func(i int) {
			obligation := evidence.Obligations[i]
			p.node("obligation", func() {
				p.text("kind", string(obligation.Obligation))
				p.text("status", string(obligation.Status))
				p.text("proof", string(obligation.Proof))
			})
		})
		if evidence.Admission == nil {
			p.absent("fanout_admission")
		} else {
			p.node("fanout_admission", func() {
				p.text("aggregation", evidence.Admission.Aggregation)
				p.text("duplicate_sensitivity", string(evidence.Admission.DuplicateSensitivity))
			})
		}
	})
}

func projectSemanticPlanNodePredicate(p *projector, predicate SemanticPlanNodePredicate) {
	p.node("node_predicate", func() {
		p.text("scope", string(predicate.Scope))
		p.text("owner_node_id", predicate.OwnerNodeID)
		p.text("proof", string(predicate.Proof))
		if predicate.Predicate == nil {
			p.absent("predicate")
		} else {
			projectPredicate(p, *predicate.Predicate)
		}
		if predicate.Post == nil {
			p.absent("post")
		} else {
			p.node("post", func() {
				p.text("name", predicate.Post.Name)
				projectFilter(p, "filter", predicate.Post.Filter)
			})
		}
	})
}
func projectPostEvaluationPredicate(p *projector, predicate PostEvaluationPredicate) {
	p.node("post_evaluation_predicate", func() {
		p.text("name", predicate.Name)
		projectFilter(p, "filter", predicate.Filter)
	})
}

func projectSemanticPlanNodeEvaluation(p *projector, name string, node SemanticPlanNode) {
	p.node(name, func() {
		var metric *ossie.Metric
		var resolved expression.ResolvedExpression
		var source *SourceAggregateNode
		derived := false
		var cumulative *CumulativeWindowNode
		var timeOffset *TimeOffsetNode
		var offsetToGrain *OffsetToGrainNode
		var conversion *ConversionNode
		var semiAdditive *SemiAdditiveNode
		var sourceSelection *SourceSelectionNode

		switch typed := node.(type) {
		case SourceAggregateNode:
			metric, resolved = typed.Metric, typed.Expression
			value := typed
			source = &value
		case PostAggregateNode:
			metric, resolved, derived = typed.Metric, typed.Expression, true
		case JoinAggregatesNode:
			metric, resolved, derived = typed.Metric, typed.Expression, true
		case CrossJoinAggregatesNode:
			metric, resolved, derived = typed.Metric, typed.Expression, true
		case CumulativeWindowNode:
			metric, resolved = typed.Metric, typed.Expression
			value := typed
			cumulative = &value
		case TimeOffsetNode:
			metric, resolved = typed.Metric, typed.Expression
			value := typed
			timeOffset = &value
		case OffsetToGrainNode:
			metric, resolved = typed.Metric, typed.Expression
			value := typed
			offsetToGrain = &value
		case ConversionNode:
			metric, resolved = typed.Metric, typed.Expression
			value := typed
			conversion = &value
		case SemiAdditiveNode:
			metric, resolved = typed.Metric, typed.Expression
			value := typed
			semiAdditive = &value
		case SourceSelectionNode:
			value := typed
			sourceSelection = &value
		}

		projectMetric(p, "metric", metric)
		projectResolvedExpression(p, "expression", resolved)
		if source == nil {
			p.absent("source")
		} else {
			rollup := source.Rollup
			p.node("source", func() {
				p.text("function", rollup.Function)
				p.text("algebra", string(rollup.Algebra))
				p.text("merge", rollup.Merge)
				p.flag("mergeable", rollup.Mergeable)
			})
		}
		if !derived {
			p.absent("derived")
		} else {
			p.node("derived", func() {})
		}
		if cumulative == nil {
			p.absent("cumulative")
		} else {
			p.node("cumulative", func() {
				p.node("spec", func() {
					p.text("kind", string(cumulative.Spec.Kind))
					p.text("base_metric", cumulative.Spec.BaseMetric)
					p.text("time_dimension", cumulative.Spec.TimeDimension)
					p.node("window", func() {
						p.text("type", cumulative.Spec.Window.Type)
						p.number("count", cumulative.Spec.Window.Count)
						p.text("unit", cumulative.Spec.Window.Unit)
					})
				})
				if cumulative.CustomCalendarRolling == nil {
					p.absent("custom_calendar_rolling")
				} else {
					projectCustomCalendarCumulative(p, "custom_calendar_rolling", *cumulative.CustomCalendarRolling)
				}
				if cumulative.CustomCalendarGrainToDate == nil {
					p.absent("custom_calendar_grain_to_date")
				} else {
					projectCustomCalendarGrainToDate(p, "custom_calendar_grain_to_date", *cumulative.CustomCalendarGrainToDate)
				}
			})
		}
		if timeOffset == nil {
			p.absent("time_offset")
		} else {
			p.node("time_offset", func() {
				p.node("spec", func() {
					p.text("kind", string(timeOffset.Spec.Kind))
					p.text("base_metric", timeOffset.Spec.BaseMetric)
					p.text("time_dimension", timeOffset.Spec.TimeDimension)
					p.node("offset", func() {
						p.number("count", timeOffset.Spec.Offset.Count)
						p.text("unit", timeOffset.Spec.Offset.Unit)
					})
				})
				if timeOffset.CustomCalendar == nil {
					p.absent("custom_calendar")
				} else {
					projectCustomCalendarOffset(p, "custom_calendar", *timeOffset.CustomCalendar)
				}
			})
		}
		if offsetToGrain == nil {
			p.absent("offset_to_grain")
		} else {
			p.node("offset_to_grain", func() {
				p.node("spec", func() {
					p.text("kind", string(offsetToGrain.Spec.Kind))
					p.text("base_metric", offsetToGrain.Spec.BaseMetric)
					p.text("time_dimension", offsetToGrain.Spec.TimeDimension)
					p.text("grain", offsetToGrain.Spec.Grain)
				})
				if offsetToGrain.OffsetPlan == nil {
					p.absent("boundary")
				} else {
					projectOffsetToGrainPlan(p, "boundary", *offsetToGrain.OffsetPlan)
				}
			})
		}
		if conversion == nil {
			p.absent("conversion")
		} else {
			p.node("conversion", func() {
				p.node("spec", func() {
					p.text("kind", string(conversion.Spec.Kind))
					p.text("base_metric", conversion.Spec.BaseMetric)
					p.text("conversion_metric", conversion.Spec.ConversionMetric)
					p.node("entity", func() {
						p.text("base_property", conversion.Spec.Entity.BaseProperty)
						p.text("conversion_property", conversion.Spec.Entity.ConversionProperty)
					})
					p.text("calculation", conversion.Spec.Calculation)
					if conversion.Spec.Window == nil {
						p.absent("window")
					} else {
						p.opaque("window", conversion.Spec.Window)
					}
					p.sequence("constant_properties", len(conversion.Spec.ConstantProperties), func(i int) {
						pair := conversion.Spec.ConstantProperties[i]
						p.node("pair", func() {
							p.text("base_property", pair.BaseProperty)
							p.text("conversion_property", pair.ConversionProperty)
						})
					})
				})
				if conversion.Conversion == nil {
					p.absent("plan")
				} else {
					projectConversionPlan(p, "plan", *conversion.Conversion)
				}
				if conversion.PhysicalInputs == nil {
					p.absent("physical_inputs")
				} else {
					projectConversionPhysicalInputs(p, "physical_inputs", *conversion.PhysicalInputs)
				}
			})
		}
		if semiAdditive == nil {
			p.absent("semi_additive")
		} else {
			spec := semiAdditive.Spec
			p.node("semi_additive", func() {
				p.text("kind", string(spec.Kind))
				p.text("base_metric", spec.BaseMetric)
				p.text("non_additive_dimension", spec.NonAdditiveDimension)
				p.text("aggregation", spec.Aggregation)
				p.text("tie_break_dimension", spec.TieBreakDimension)
				p.text("null_policy", spec.NullPolicy)
				p.textSequence("window_groupings", spec.WindowGroupings)
				p.text("rollup_aggregation", spec.RollupAggregation)
			})
		}
		if sourceSelection == nil {
			p.absent("source_selection")
		} else {
			p.node("source_selection", func() { p.text("mode", string(sourceSelection.Mode)) })
		}
	})
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func projectOffsetToGrainPlan(p *projector, name string, plan OffsetToGrainPlan) {
	p.node(name, func() {
		p.text("time_dimension", plan.TimeDimension)
		p.text("query_grain", string(plan.QueryGrain))
		p.text("boundary_grain", string(plan.BoundaryGrain))
		p.flag("custom_calendar", plan.CustomCalendar)
		projectDatasetRef(p, "dataset", plan.Dataset)
		projectField(p, "boundary_bucket", plan.BoundaryBucket)
		p.text("boundary_expression", plan.BoundaryExpression)
		projectField(p, "query_ordinal", plan.QueryOrdinal)
		p.text("ordinal_expression", plan.OrdinalExpression)
	})
}
func projectConversionPlan(p *projector, name string, plan ConversionPlan) {
	p.node(name, func() {
		p.text("base_metric", plan.BaseMetric)
		p.text("conversion_metric", plan.ConversionMetric)
		p.text("base_root", plan.BaseRoot)
		p.text("conversion_root", plan.ConversionRoot)
		p.sequence("base_event_key", len(plan.BaseEventKey), func(i int) { projectConversionFieldRef(p, "field", plan.BaseEventKey[i]) })
		p.sequence("conversion_event_key", len(plan.ConversionEventKey), func(i int) { projectConversionFieldRef(p, "field", plan.ConversionEventKey[i]) })
		projectConversionFieldRef(p, "base_time", plan.BaseTime)
		projectConversionFieldRef(p, "conversion_time", plan.ConversionTime)
		projectConversionPropertyPlan(p, "entity", plan.Entity)
		p.sequence("constant_properties", len(plan.ConstantProperties), func(i int) { projectConversionPropertyPlan(p, "property", plan.ConstantProperties[i]) })
		p.text("calculation", plan.Calculation)
		projectConversionWindow(p, "window", plan.Window)
		p.text("assignment", plan.Assignment)
		projectConversionCandidateMatch(p, "candidate_match", plan.CandidateMatch)
	})
}
func projectConversionCandidateMatch(p *projector, name string, match ConversionCandidateMatchPlan) {
	p.node(name, func() {
		p.sequence("partition_by", len(match.PartitionBy), func(i int) { projectConversionFieldRef(p, "field", match.PartitionBy[i]) })
		p.sequence("equality", len(match.Equality), func(i int) { projectConversionPropertyPlan(p, "property", match.Equality[i]) })
		projectConversionFieldRef(p, "base_time", match.BaseTime)
		projectConversionFieldRef(p, "conversion_time", match.ConversionTime)
		projectConversionWindow(p, "window", match.Window)
		p.sequence("order_by", len(match.OrderBy), func(i int) {
			order := match.OrderBy[i]
			p.node("order", func() {
				projectConversionFieldRef(p, "field", order.Field)
				p.text("direction", order.Direction)
			})
		})
		p.number("keep_rank", match.KeepRank)
		p.flag("aggregate_base_independently", match.AggregateBaseIndependently)
	})
}
func projectConversionFieldRef(p *projector, name string, ref ConversionFieldRef) {
	p.node(name, func() {
		p.text("dataset", ref.Dataset)
		p.text("name", ref.Name)
	})
}
func projectConversionPropertyPlan(p *projector, name string, property ConversionPropertyPlan) {
	p.node(name, func() {
		projectConversionFieldRef(p, "base", property.Base)
		projectConversionFieldRef(p, "conversion", property.Conversion)
	})
}
func projectConversionWindow(p *projector, name string, window *ossie.ConversionWindow) {
	if window == nil {
		p.absent(name)
		return
	}
	p.node(name, func() {
		p.number("count", window.Count)
		p.text("unit", window.Unit)
	})
}
func projectConversionPhysicalInputs(p *projector, name string, plan ConversionPhysicalInputPlan) {
	p.node(name, func() {
		projectDatasetRef(p, "base_dataset", plan.BaseDataset)
		projectDatasetRef(p, "conversion_dataset", plan.ConversionDataset)
		p.sequence("base_event_key", len(plan.BaseEventKey), func(i int) { projectConversionPhysicalFieldRef(p, "field", plan.BaseEventKey[i]) })
		p.sequence("conversion_event_key", len(plan.ConversionEventKey), func(i int) { projectConversionPhysicalFieldRef(p, "field", plan.ConversionEventKey[i]) })
		projectConversionPhysicalFieldRef(p, "base_time", plan.BaseTime)
		projectConversionPhysicalFieldRef(p, "conversion_time", plan.ConversionTime)
		projectConversionPhysicalProperty(p, "entity", plan.Entity)
		p.sequence("constant_properties", len(plan.ConstantProperties), func(i int) { projectConversionPhysicalProperty(p, "property", plan.ConstantProperties[i]) })
		projectConversionEventValue(p, "base_value", plan.BaseValue)
		projectConversionEventValue(p, "conversion_value", plan.ConversionValue)
	})
}
func projectConversionPhysicalFieldRef(p *projector, name string, ref ConversionPhysicalFieldRef) {
	p.node(name, func() {
		p.text("dataset", ref.Dataset)
		p.text("name", ref.Name)
		p.text("expression", ref.Expression)
	})
}
func projectConversionPhysicalProperty(p *projector, name string, property ConversionPhysicalPropertyPlan) {
	p.node(name, func() {
		projectConversionPhysicalFieldRef(p, "base", property.Base)
		projectConversionPhysicalFieldRef(p, "conversion", property.Conversion)
	})
}
func projectConversionEventValue(p *projector, name string, value ConversionEventValuePlan) {
	p.node(name, func() {
		p.text("metric", value.Metric)
		p.text("dataset", value.Dataset)
		p.text("kind", string(value.Kind))
		if value.Field == nil {
			p.absent("field")
		} else {
			projectConversionPhysicalFieldRef(p, "field", *value.Field)
		}
	})
}
func projectSemanticOutputContract(p *projector, name string, contract SemanticOutputContract) {
	p.node(name, func() {
		p.sequence("projections", len(contract.Projections), func(i int) { projectProjection(p, contract.Projections[i]) })
		p.sequence("grain", len(contract.Grain), func(i int) { projectGroupBy(p, contract.Grain[i]) })
		p.sequence("predicates", len(contract.Predicates), func(i int) { projectPostEvaluationPredicate(p, contract.Predicates[i]) })
		p.sequence("order_by", len(contract.OrderBy), func(i int) { projectSort(p, contract.OrderBy[i]) })
		p.optionalNumber("limit", contract.Limit)
	})
}
