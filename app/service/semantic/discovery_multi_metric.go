package semantic

import (
	"context"
	"sort"
	"strings"

	"github.com/meaningforge/metis/serrors"
)

type GetMetricsDimensionsRequest struct {
	Project string   `json:"project"`
	Model   string   `json:"model"`
	Metrics []string `json:"metrics"`
}

type MetricSetDimensionCompatibilityResult struct {
	Project    string                            `json:"project"`
	Model      string                            `json:"model"`
	Metrics    []string                          `json:"metrics"`
	Dimensions []MetricSetDimensionCompatibility `json:"dimensions"`
}

type MetricSetDimensionCompatibility struct {
	Name      string                                 `json:"name"`
	Qualified string                                 `json:"qualified_name"`
	Dataset   string                                 `json:"dataset"`
	Status    CompatibilityStatus                    `json:"status"`
	PerMetric []MetricDimensionCompatibilityEvidence `json:"per_metric"`
}

type MetricDimensionCompatibilityEvidence struct {
	Metric         string                `json:"metric"`
	SourceDatasets []string              `json:"source_datasets"`
	Status         CompatibilityStatus   `json:"status"`
	Paths          []DimensionSourcePath `json:"paths,omitempty"`
	Issues         []DimensionPathIssue  `json:"issues,omitempty"`
}

func (s *DiscoveryService) GetMetricsDimensions(ctx context.Context, req GetMetricsDimensionsRequest) (*MetricSetDimensionCompatibilityResult, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	metrics, err := metricSetFocus(req.Metrics)
	if err != nil {
		return nil, err
	}
	result := &MetricSetDimensionCompatibilityResult{Project: req.Project, Model: req.Model, Metrics: metrics}
	byQualified := map[string]*MetricSetDimensionCompatibility{}

	for _, metric := range metrics {
		compatibility, err := s.getMetricDimensions(ctx, req.Project, req.Model, metric)
		if err != nil {
			return nil, err
		}
		for _, dimension := range compatibility.Dimensions {
			item := byQualified[dimension.Qualified]
			if item == nil {
				item = &MetricSetDimensionCompatibility{
					Name:      dimension.Name,
					Qualified: dimension.Qualified,
					Dataset:   dimension.Dataset,
					Status:    CompatibilityCompatible,
				}
				byQualified[dimension.Qualified] = item
			}
			item.PerMetric = append(item.PerMetric, MetricDimensionCompatibilityEvidence{
				Metric:         metric,
				SourceDatasets: append([]string(nil), compatibility.SourceDatasets...),
				Status:         dimension.Status,
				Paths:          append([]DimensionSourcePath(nil), dimension.Paths...),
				Issues:         append([]DimensionPathIssue(nil), dimension.Issues...),
			})
			item.Status = aggregateCompatibilityStatus(item.Status, dimension.Status)
		}
	}

	qualified := make([]string, 0, len(byQualified))
	for name := range byQualified {
		qualified = append(qualified, name)
	}
	sort.Strings(qualified)
	for _, name := range qualified {
		result.Dimensions = append(result.Dimensions, *byQualified[name])
	}
	return result, nil
}

func metricSetFocus(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		metric := strings.TrimSpace(value)
		if metric == "" {
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "metric compatibility request contains an empty metric"}
		}
		if _, ok := seen[metric]; ok {
			continue
		}
		seen[metric] = struct{}{}
		out = append(out, metric)
	}
	if len(out) == 0 {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "metric compatibility request requires at least one metric"}
	}
	return out, nil
}

func aggregateCompatibilityStatus(current, next CompatibilityStatus) CompatibilityStatus {
	if current == CompatibilityAmbiguous || next == CompatibilityAmbiguous {
		return CompatibilityAmbiguous
	}
	if current == CompatibilityUnreachable || next == CompatibilityUnreachable {
		return CompatibilityUnreachable
	}
	return CompatibilityCompatible
}
