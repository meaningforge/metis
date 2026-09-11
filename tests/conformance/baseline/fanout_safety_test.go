package baseline_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestDuplicateInvariantFilteredFanoutCarriesAdmissionEvidence(t *testing.T) {
	scenario, ok := scenarios.ByName("duplicate_invariant_filtered_fanout")
	if !ok {
		t.Fatal("fanout conformance scenario is not registered")
	}
	plan, _, err := baseline.PlanScenario(scenario, "duckdb")
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := semanticplan.Explain(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(explanation.Nodes) != 1 || len(explanation.Nodes[0].PopulationPreservation) != 1 {
		t.Fatalf("population-preservation explanation = %#v", explanation.Nodes)
	}
	evidence := explanation.Nodes[0].PopulationPreservation[0]
	if !evidence.Preserved || evidence.Multiplicity != semanticplan.RelationshipMayFanout || evidence.Admission == nil {
		t.Fatalf("fanout admission evidence = %#v", evidence)
	}
	if evidence.Admission.DuplicateSensitivity != expression.DuplicateInvariant {
		t.Fatalf("duplicate sensitivity = %q", evidence.Admission.DuplicateSensitivity)
	}
}

func TestSensitiveAggregateStillRejectsFilteredFanout(t *testing.T) {
	scenario, ok := scenarios.ByName("duplicate_invariant_filtered_fanout")
	if !ok {
		t.Fatal("fanout conformance scenario is not registered")
	}
	scenario.Query.Metrics = slices.Clone(scenario.Query.Metrics)
	scenario.Query.Metrics[0].Name = "customer_rows_with_matching_orders"
	_, _, err := baseline.PlanScenario(scenario, "duckdb")
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnsupportedRelationshipFanout {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrUnsupportedRelationshipFanout)
	}
	if got := apiErr.Details["duplicate_sensitivity"]; got != expression.DuplicateSensitive {
		t.Fatalf("duplicate sensitivity detail = %#v", got)
	}
}
