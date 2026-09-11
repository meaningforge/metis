package rest

import (
	"context"

	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	service "github.com/meaningforge/metis/app/service/semantic"
)

func semanticServices(ctx context.Context, projectID string) (runtimeservice.SemanticServices, bool, error) {
	generation, pinned, err := runtimeservice.FromContext(ctx, projectID)
	if err != nil || !pinned {
		return runtimeservice.SemanticServices{}, pinned, err
	}
	return generation.SemanticServices, true, nil
}

func discoveryFor(ctx context.Context, projectID string, fallback *service.DiscoveryService) (*service.DiscoveryService, error) {
	if services, ok, err := semanticServices(ctx, projectID); err != nil {
		return nil, err
	} else if ok {
		return services.Discovery, nil
	}
	return fallback, nil
}

func compileFor(ctx context.Context, projectID string, fallback *service.CompileService) (*service.CompileService, error) {
	if services, ok, err := semanticServices(ctx, projectID); err != nil {
		return nil, err
	} else if ok {
		return services.Compile, nil
	}
	return fallback, nil
}

func queryMetricsFor(ctx context.Context, projectID string, fallback *service.QueryMetricsService) (*service.QueryMetricsService, error) {
	if services, ok, err := semanticServices(ctx, projectID); err != nil {
		return nil, err
	} else if ok {
		return services.QueryMetrics, nil
	}
	return fallback, nil
}

func dimensionValuesFor(ctx context.Context, projectID string, fallback *service.DimensionValuesService) (*service.DimensionValuesService, error) {
	if services, ok, err := semanticServices(ctx, projectID); err != nil {
		return nil, err
	} else if ok {
		return services.DimensionValues, nil
	}
	return fallback, nil
}

func attributeMetricFor(ctx context.Context, projectID string, fallback *service.AttributeMetricService) (*service.AttributeMetricService, error) {
	if services, ok, err := semanticServices(ctx, projectID); err != nil {
		return nil, err
	} else if ok {
		return services.AttributeMetric, nil
	}
	return fallback, nil
}

func compareMetricsFor(ctx context.Context, projectID string, fallback *service.CompareMetricsService) (*service.CompareMetricsService, error) {
	if services, ok, err := semanticServices(ctx, projectID); err != nil {
		return nil, err
	} else if ok {
		return services.CompareMetrics, nil
	}
	return fallback, nil
}
