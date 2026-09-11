package optimizer

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// State is the bounded mutable rewrite surface for semantic-plan optimizer
// rules. It intentionally exposes no resolver, renderer, or SQLPlan state.
type State struct {
	Root                     semanticplan.DatasetRef
	Joins                    []semanticplan.Join
	Projections              []semanticplan.Projection
	Predicates               []semanticplan.Predicate
	Groups                   []semanticplan.GroupBy
	Sorts                    []semanticplan.Sort
	Requested                []string
	Nodes                    []semanticplan.SemanticPlanNode
	OutputGrain              []semanticplan.GroupBy
	PostEvaluationPredicates []semanticplan.PostEvaluationPredicate
	DenseCalendar            *semanticplan.DenseCalendarPlan
	CustomDenseCalendar      *semanticplan.CustomDenseCalendarPlan
}

// SemanticRule is the graph-aware optimizer contract. Rules receive only the
// owned semantic-plan rewrite surface, never planner construction state.
type SemanticRule interface {
	Name() string
	ApplySemanticPlan(*State) (bool, error)
}

// ApplySemanticRule dispatches a graph-aware rule and rejects generic rules
// that have not declared the bounded semantic-plan contract.
func ApplySemanticRule(state *State, rule Rule) (bool, error) {
	graphRule, ok := rule.(SemanticRule)
	if !ok {
		return false, serrors.Internal("semantic plan optimizer rule does not implement the semantic graph contract", map[string]any{"rule": rule.Name()})
	}
	return graphRule.ApplySemanticPlan(state)
}

func applyRuleToState(state *State, rule Rule) (bool, error) {
	view := state.View()
	changed, err := rule.Apply(view)
	if err != nil {
		return false, err
	}
	state.Update(view)
	return changed, nil
}

func NewState(plan *semanticplan.SemanticPlan) *State {
	if plan == nil {
		return nil
	}
	return &State{Root: plan.Root, Joins: plan.Joins, Projections: plan.Projections, Predicates: plan.Predicates, Groups: plan.Groups, Sorts: plan.Sorts, Requested: plan.Requested, Nodes: plan.Nodes, OutputGrain: plan.Output.Grain, PostEvaluationPredicates: plan.Output.Predicates, DenseCalendar: plan.DenseCalendar, CustomDenseCalendar: plan.CustomDenseCalendar}
}

// View projects the bounded rewrite surface as a SemanticPlan for legacy rule
// compatibility; callers must return changes through Update.
func (state *State) View() *semanticplan.SemanticPlan {
	if state == nil {
		return nil
	}
	return &semanticplan.SemanticPlan{Root: state.Root, Joins: state.Joins, Projections: state.Projections, Predicates: state.Predicates, Groups: state.Groups, Sorts: state.Sorts, Requested: state.Requested, Nodes: state.Nodes, DenseCalendar: state.DenseCalendar, CustomDenseCalendar: state.CustomDenseCalendar, Output: semanticplan.SemanticOutputContract{Projections: state.Projections, Grain: state.OutputGrain, Predicates: state.PostEvaluationPredicates, OrderBy: state.Sorts}}
}

func (state *State) Update(plan *semanticplan.SemanticPlan) {
	if state == nil || plan == nil {
		return
	}
	state.Root, state.Joins, state.Projections, state.Predicates, state.Groups, state.Sorts = plan.Root, plan.Joins, plan.Projections, plan.Predicates, plan.Groups, plan.Sorts
	state.Requested, state.Nodes, state.OutputGrain, state.PostEvaluationPredicates = plan.Requested, plan.Nodes, plan.Output.Grain, plan.Output.Predicates
	state.DenseCalendar, state.CustomDenseCalendar = plan.DenseCalendar, plan.CustomDenseCalendar
}

func (state *State) Write(plan *semanticplan.SemanticPlan) {
	if state == nil || plan == nil {
		return
	}
	plan.Root, plan.Joins, plan.Projections, plan.Predicates, plan.Groups, plan.Sorts = state.Root, state.Joins, state.Projections, state.Predicates, state.Groups, state.Sorts
	plan.Requested, plan.Nodes, plan.Output.Grain, plan.Output.Predicates = state.Requested, state.Nodes, state.OutputGrain, state.PostEvaluationPredicates
	plan.DenseCalendar, plan.CustomDenseCalendar = state.DenseCalendar, state.CustomDenseCalendar
}
