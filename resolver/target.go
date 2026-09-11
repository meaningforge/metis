package resolver

import (
	"context"
	"fmt"
	"strings"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/serrors"
)

func (r *Resolver) ResolveForRenderer(ctx context.Context, q query.SemanticQuery, renderer renderer.Renderer) (*SemanticQuerySpec, error) {
	if err := validateQueryIntentShape(q); err != nil {
		return nil, err
	}
	resolved, err := r.Resolve(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := validateResolvedQueryIntent(q, resolved); err != nil {
		return nil, err
	}
	if err := applyMetricTimeBindings(resolved); err != nil {
		return nil, err
	}
	if err := resolveTimeSpine(resolved); err != nil {
		return nil, err
	}
	if renderer == nil || strings.TrimSpace(renderer.ExpressionDialect()) == "" {
		return nil, &serrors.Error{Code: serrors.ErrTargetRequired, Message: "selected Renderer with an expression dialect is required for semantic resolution"}
	}
	expressionDialect := renderer.ExpressionDialect()
	rendererContext := extension.RendererContext{Dialect: string(renderer.SQLDialect())}
	resolved.FieldExpressions = make(map[string]expression.ResolvedExpression, len(resolved.Model.Fields))
	for key, handle := range resolved.Model.Fields {
		if handle == nil || handle.Field == nil {
			continue
		}
		selected, selectErr := selectExpression(handle.Field.Expression, expressionDialect)
		if selectErr == nil {
			resolved.FieldExpressions[key] = selected
		}
	}
	for i := range resolved.Metrics {
		expr, err := r.selectMetricExpression(resolved.Model, resolved.Metrics[i].Name, expressionDialect, rendererContext)
		if err != nil {
			return nil, err
		}
		resolved.Metrics[i].Expression = expr
	}
	for i := range resolved.EvaluationMetrics {
		expr, err := r.selectMetricExpression(resolved.Model, resolved.EvaluationMetrics[i].Name, expressionDialect, rendererContext)
		if err != nil {
			return nil, err
		}
		resolved.EvaluationMetrics[i].Expression = expr
	}
	for i := range resolved.Dimensions {
		expr, err := selectExpression(resolved.Dimensions[i].Field.Expression, expressionDialect)
		if err != nil {
			return nil, unsupportedExpressionError("dimension", resolved.Dimensions[i].Name, expressionDialect, resolved.Dimensions[i].Field.Expression)
		}
		resolved.Dimensions[i].Expression = expr
		if custom := resolved.Dimensions[i].CustomCalendar; custom != nil {
			for name, level := range custom.Levels {
				if level == nil || level.BucketField == nil || level.OrdinalField == nil {
					return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "custom calendar hierarchy level is incomplete", Details: map[string]any{"grain": name}}
				}
				bucket, err := selectExpression(level.BucketField.Expression, expressionDialect)
				if err != nil {
					return nil, unsupportedExpressionError("custom calendar bucket", level.BucketField.Name, expressionDialect, level.BucketField.Expression)
				}
				ordinal, err := selectExpression(level.OrdinalField.Expression, expressionDialect)
				if err != nil {
					return nil, unsupportedExpressionError("custom calendar ordinal", level.OrdinalField.Name, expressionDialect, level.OrdinalField.Expression)
				}
				level.BucketSelectedExpression = bucket.Source
				level.OrdinalSelectedExpression = ordinal.Source
			}
			current := custom.Levels[custom.Grain.Name]
			if current == nil {
				return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "custom calendar query grain hierarchy level is missing", Details: map[string]any{"grain": custom.Grain.Name}}
			}
			custom.BucketSelectedExpression = current.BucketSelectedExpression
			custom.OrdinalSelectedExpression = current.OrdinalSelectedExpression
		}
	}
	if resolved.TimeSpine != nil && resolved.TimeSpine.TimeField != nil {
		expr, err := selectExpression(resolved.TimeSpine.TimeField.Expression, expressionDialect)
		if err != nil {
			return nil, unsupportedExpressionError("time spine field", resolved.TimeSpine.Spec.TimeDimension, expressionDialect, resolved.TimeSpine.TimeField.Expression)
		}
		resolved.TimeSpine.Expression = expr
	}
	for i := range resolved.Filters {
		switch resolved.Filters[i].Kind {
		case FilterTargetMetric:
			continue
		case FilterTargetField:
			expr, err := selectExpression(resolved.Filters[i].Field.Expression, expressionDialect)
			if err != nil {
				return nil, unsupportedExpressionError("filter field", resolved.Filters[i].Filter.Field, expressionDialect, resolved.Filters[i].Field.Expression)
			}
			resolved.Filters[i].Expression = expr
		default:
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "unsupported resolved filter target", Details: map[string]any{"field": resolved.Filters[i].Filter.Field, "kind": resolved.Filters[i].Kind}}
		}
	}
	for i := range resolved.OrderBy {
		var expr expression.ResolvedExpression
		var err error
		switch resolved.OrderBy[i].Kind {
		case OrderTargetMetric:
			expr, err = r.selectMetricExpression(resolved.Model, resolved.OrderBy[i].Name, expressionDialect, rendererContext)
		case OrderTargetDimension:
			expr, err = selectExpression(resolved.OrderBy[i].Field.Expression, expressionDialect)
		default:
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "unsupported resolved order target", Details: map[string]any{"field": resolved.OrderBy[i].Name, "kind": resolved.OrderBy[i].Kind}}
		}
		if err != nil {
			if resolved.OrderBy[i].Kind == OrderTargetMetric {
				return nil, err
			}
			var source ossie.Expression
			source = resolved.OrderBy[i].Field.Expression
			return nil, unsupportedExpressionError("order field", resolved.OrderBy[i].Name, expressionDialect, source)
		}
		resolved.OrderBy[i].Expression = expr
	}
	return resolved, nil
}

func selectExpression(source ossie.Expression, targetDialect string) (expression.ResolvedExpression, error) {
	if sourceDialect, ok := ossieDialectForTarget(targetDialect); ok {
		for _, candidate := range source.Dialects {
			if candidate.Dialect == sourceDialect && strings.TrimSpace(candidate.Expression) != "" {
				return expression.NewResolvedExpression(string(candidate.Dialect), candidate.Expression), nil
			}
		}
	}
	for _, candidate := range source.Dialects {
		if candidate.Dialect == ossie.DialectANSISQL && strings.TrimSpace(candidate.Expression) != "" {
			return expression.NewResolvedExpression(string(candidate.Dialect), candidate.Expression), nil
		}
	}
	return expression.ResolvedExpression{}, fmt.Errorf("no compatible expression for target dialect %q", targetDialect)
}

func (r *Resolver) selectMetricExpression(model *manifest.ModelIndex, name, expressionDialect string, renderer extension.RendererContext) (expression.ResolvedExpression, error) {
	metric, err := model.Metric(name)
	if err != nil {
		return expression.ResolvedExpression{}, err
	}
	selected, err := selectExpression(metric.Expression, expressionDialect)
	if err != nil {
		return expression.ResolvedExpression{}, unsupportedExpressionError("metric", name, expressionDialect, metric.Expression)
	}
	analysis, ok := model.MetricAnalysis(name)
	if !ok {
		return expression.ResolvedExpression{}, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "metric expression analysis is not available", Details: map[string]any{"metric": name}}
	}
	for _, candidate := range analysis.Expressions {
		if candidate.Dialect != selected.SourceDialect || candidate.Source != selected.Source {
			continue
		}
		properties, algebraEvidence, algebraErr := r.interpretAggregationAlgebra(metric, candidate.AggregationProperties, renderer)
		if algebraErr != nil {
			return expression.ResolvedExpression{}, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "invalid registered aggregation-algebra evidence", Details: map[string]any{"metric": name, "cause": algebraErr.Error()}}
		}
		resolved := selected.WithAnalysis(candidate.Bound, candidate.Typed).WithAggregationProperties(properties...)
		scaleEvidence, evidenceErr := extension.MetricScaleEvidenceForMetric(metric)
		if evidenceErr != nil {
			return expression.ResolvedExpression{}, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "invalid semantic-critical metric extension", Details: map[string]any{"metric": name, "cause": evidenceErr.Error()}}
		}
		if len(scaleEvidence) == 0 && len(algebraEvidence) == 0 {
			return resolved, nil
		}
		evidence := make([]extension.Evidence, 0, len(scaleEvidence)+len(algebraEvidence))
		for _, algebra := range algebraEvidence {
			evidence = append(evidence, algebra)
		}
		for _, scale := range scaleEvidence {
			resolved.Source = fmt.Sprintf("(%s) * %s", resolved.Source, scale.FactorSQL())
			evidence = append(evidence, scale)
		}
		return resolved.WithExtensionEvidence(evidence...), nil
	}
	return expression.ResolvedExpression{}, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "selected metric expression was not analyzed", Details: map[string]any{"metric": name, "source_dialect": selected.SourceDialect}}
}

func (r *Resolver) interpretAggregationAlgebra(metric *ossie.Metric, properties []expression.AggregationProperties, renderer extension.RendererContext) ([]expression.AggregationProperties, []extension.AggregationAlgebraEvidence, error) {
	out := append([]expression.AggregationProperties(nil), properties...)
	for i := range out {
		out[i].PartialState = append([]expression.PartialStateComponent(nil), properties[i].PartialState...)
	}
	if r == nil || r.extensionInventory == nil {
		return out, nil, nil
	}
	var evidence []extension.AggregationAlgebraEvidence
	for i := range out {
		if out[i].Derived {
			continue
		}
		interpreted, ok, err := r.extensionInventory.AggregationAlgebraForMetric(metric, renderer, out[i].Function)
		if err != nil {
			return nil, nil, err
		}
		if !ok || r.extensionCapabilities.Resolve(interpreted.Requirement(), renderer).State != extension.StateSupported {
			continue
		}
		mapped, err := aggregationPropertiesFromEvidence(interpreted, out[i].Function)
		if err != nil {
			return nil, nil, err
		}
		out[i] = mapped
		evidence = append(evidence, interpreted)
	}
	return out, evidence, nil
}

func aggregationPropertiesFromEvidence(evidence extension.AggregationAlgebraEvidence, function string) (expression.AggregationProperties, error) {
	if !strings.EqualFold(strings.TrimSpace(evidence.Function), strings.TrimSpace(function)) {
		return expression.AggregationProperties{}, fmt.Errorf("evidence function %q does not match unresolved call %q", evidence.Function, function)
	}
	duplicate := expression.DuplicateSensitivity(evidence.DuplicateSensitivity)
	if duplicate != expression.DuplicateSensitive && duplicate != expression.DuplicateInvariant {
		return expression.AggregationProperties{}, fmt.Errorf("unsupported duplicate sensitivity %q", evidence.DuplicateSensitivity)
	}
	rollup := expression.RollupAlgebra(evidence.RollupAlgebra)
	if rollup != expression.RollupDistributive && rollup != expression.RollupAlgebraic && rollup != expression.RollupHolistic {
		return expression.AggregationProperties{}, fmt.Errorf("unsupported rollup algebra %q", evidence.RollupAlgebra)
	}
	finalize := expression.FinalizeKind(evidence.Finalize)
	if finalize != expression.FinalizeIdentity && finalize != expression.FinalizeRatio && finalize != expression.FinalizeNone {
		return expression.AggregationProperties{}, fmt.Errorf("unsupported finalizer %q", evidence.Finalize)
	}
	state := make([]expression.PartialStateComponent, len(evidence.PartialState))
	for i, component := range evidence.PartialState {
		if strings.TrimSpace(component.Name) == "" {
			return expression.AggregationProperties{}, fmt.Errorf("partial-state component %d has no name", i)
		}
		state[i] = expression.PartialStateComponent{Name: component.Name, Predicated: component.Predicated}
	}
	switch rollup {
	case expression.RollupHolistic:
		if len(state) != 0 || strings.TrimSpace(evidence.Merge) != "" || finalize != expression.FinalizeNone {
			return expression.AggregationProperties{}, fmt.Errorf("holistic algebra must not expose mergeable partial state")
		}
	case expression.RollupDistributive:
		if len(state) != 1 || strings.TrimSpace(evidence.Merge) == "" || finalize != expression.FinalizeIdentity {
			return expression.AggregationProperties{}, fmt.Errorf("distributive algebra requires one partial-state component, a merge operator, and identity finalization")
		}
	case expression.RollupAlgebraic:
		if len(state) < 2 || strings.TrimSpace(evidence.Merge) == "" || finalize == expression.FinalizeNone {
			return expression.AggregationProperties{}, fmt.Errorf("algebraic aggregation requires multi-component partial state, a merge operator, and finalization")
		}
	}
	return expression.AggregationProperties{
		Function: function, Duplicate: duplicate, Rollup: rollup, PartialState: state,
		Merge: evidence.Merge, Finalize: finalize, Derived: true,
	}, nil
}

func ossieDialectForTarget(target string) (ossie.Dialect, bool) {
	switch strings.ToUpper(strings.TrimSpace(target)) {
	case "DORIS":
		return ossie.DialectDoris, true
	case "CLICKHOUSE":
		return ossie.DialectClickHouse, true
	case "SNOWFLAKE":
		return ossie.DialectSnowflake, true
	case "DATABRICKS":
		return ossie.DialectDatabricks, true
	case "BIGQUERY":
		return ossie.DialectBigQuery, true
	case "DUCKDB":
		return ossie.DialectANSISQL, true
	default:
		return "", false
	}
}

func unsupportedExpressionError(kind, name, targetDialect string, expression ossie.Expression) error {
	available := make([]string, 0, len(expression.Dialects))
	for _, candidate := range expression.Dialects {
		available = append(available, string(candidate.Dialect))
	}
	targetSource, _ := ossieDialectForTarget(targetDialect)
	return &serrors.Error{Code: serrors.ErrUnsupportedExpression, Message: fmt.Sprintf("%s %q has no compatible expression for target dialect %q", kind, name, targetDialect), Details: map[string]any{"target_dialect": targetDialect, "target_expression_dialect": string(targetSource), "available_expression_dialects": available, "resolution_order": []string{string(targetSource), string(ossie.DialectANSISQL)}}, Suggestions: []string{fmt.Sprintf("add a %s expression for %s %q", targetSource, kind, name), fmt.Sprintf("add an %s fallback with equivalent semantic meaning", ossie.DialectANSISQL)}}
}
