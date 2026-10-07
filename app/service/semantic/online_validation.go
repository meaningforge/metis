package semantic

import (
	"context"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/service/policy"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

type PhysicalColumnCheck struct {
	Name     string
	Datatype ossie.DataType // Empty for expression dependencies: engine checks the expression.
}
type PhysicalRelationCheck struct {
	Reference driver.CatalogReference
	Columns   []PhysicalColumnCheck
}
type PreparedValidation struct {
	Route     runner.ResolvedDataSource
	Compiled  *artifact.CompiledQuery
	Relations []PhysicalRelationCheck
}

// PrepareValidation applies compile visibility, the production model route,
// Renderer and data-policy preflight. It performs no credential lookup or I/O.
func (s *QueryMetricsService) PrepareValidation(ctx context.Context, q query.SemanticQuery) (PreparedValidation, error) {
	var out PreparedValidation
	if s == nil || s.compile == nil || s.projects == nil || s.runtime == nil {
		return out, policy.ErrUnavailable
	}
	for _, action := range []ProjectAction{ProjectActionAuthor, ProjectActionCompile} {
		if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, q.Project, action); err != nil {
			return out, err
		}
	}
	if s.compile.discovery != nil {
		for _, action := range []ProjectAction{ProjectActionAuthor, ProjectActionCompile} {
			if err := s.compile.discovery.authorizeSemanticQueryAssets(ctx, q, action); err != nil {
				return out, err
			}
		}
	}
	name, ok := dataSourceForModel(s.projects, q.Project, q.Model)
	if !ok {
		return out, policy.ErrUnavailable
	}
	route, err := s.runtime.ResolveDataSource(name)
	if err != nil {
		return out, err
	}
	work, err := s.compile.prepareQueryWork(ctx, []query.SemanticQuery{q}, route.Backend.Renderer)
	if err != nil {
		return out, err
	}
	constraints, err := s.compile.prepareDataPolicy(ctx, work)
	if err != nil {
		return out, err
	}
	plan, err := s.compile.planner.PlanPrepared(ctx, work[0].Query, work[0].Evaluation, route.Backend.Renderer, constraints.scope, constraints.models[q.Model])
	if err != nil {
		return out, err
	}
	compiled, err := s.compile.compilePreparedPlan(ctx, q, plan, route.Backend.Renderer)
	if err != nil {
		return out, err
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || principal == nil {
		return out, policy.ErrDenied
	}
	dependencies, err := policy.CollectWorkload(*principal, q.Project, work)
	if err != nil {
		return out, err
	}
	for _, source := range dependencies.Sources {
		dataset := work[0].Query.Model.Datasets[source.Dataset.Dataset]
		if dataset == nil {
			return out, policy.ErrInvalid
		}
		relation := PhysicalRelationCheck{Reference: driver.CatalogReference{ID: dataset.Name, Parts: strings.Split(dataset.Source, ".")}}
		if driver.ValidateCatalogReference(string(route.Backend.Type), relation.Reference) != nil {
			return out, policy.ErrInvalid
		}
		columns := map[string]ossie.DataType{}
		for _, field := range source.RequiredFields {
			selected, ok := work[0].Query.FieldExpression(field.Dataset.Dataset, field.Field)
			if !ok {
				return out, policy.ErrInvalid
			}
			parsed, err := expression.Parse(selected.Source, expression.ProfileForDialect(selected.SourceDialect))
			if err != nil {
				return out, policy.ErrInvalid
			}
			refs := expression.CollectReferences(parsed)
			for _, ref := range refs {
				if ref.Qualifier != "" && ref.Qualifier != dataset.Name {
					continue
				} // covered by transitive closure
				if _, exists := columns[ref.Name]; !exists {
					columns[ref.Name] = ""
				}
			}
			if identifier, ok := parsed.(*expression.IdentifierExpr); ok && len(identifier.Parts) <= 2 && len(refs) == 1 {
				handle := work[0].Query.Model.Fields[dataset.Name+"."+field.Field]
				if handle != nil && handle.Field != nil {
					columns[refs[0].Name] = handle.Field.Datatype
				}
			}
		}
		if constraint := constraints.models[q.Model][dataset.Name]; constraint != nil {
			for _, predicate := range constraint.Predicates() {
				columns[predicate.Column] = predicate.Datatype
			}
		}
		for name, datatype := range columns {
			relation.Columns = append(relation.Columns, PhysicalColumnCheck{Name: name, Datatype: datatype})
		}
		sort.Slice(relation.Columns, func(i, j int) bool { return relation.Columns[i].Name < relation.Columns[j].Name })
		out.Relations = append(out.Relations, relation)
	}
	out.Route, out.Compiled = route, compiled
	return out, nil
}
