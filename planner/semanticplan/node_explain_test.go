package semanticplan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestExplainProjectsCanonicalNodeOwnedEvidence(t *testing.T) {
	plan := &SemanticPlan{
		Requested: []string{"region"},
		Output:    SemanticOutputContract{Grain: []GroupBy{{Name: "region", Dataset: "customers"}}},
		RelationshipExistence: []RelationshipExistencePredicate{{
			Relationship: &ossie.Relationship{Name: "orders_to_customers", From: "orders", To: "customers", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}},
			Source:       DatasetRef{Name: "orders", Source: "analytics.orders"},
			Target:       DatasetRef{Name: "customers", Source: "analytics.customers"},
			Correlations: []RelationshipCorrelation{{SourceColumn: "customer_id", TargetColumn: "customer_id"}},
			Predicate: BooleanPredicate{Leaf: &Predicate{
				Filter:     query.Filter{Field: "customers.region", Operator: query.FilterEQ, Value: "secret"},
				Dataset:    "customers",
				Expression: expression.ResolvedExpression{SourceDialect: "ANSI_SQL", Source: "region"},
			}},
		}},
		Nodes: []SemanticPlanNode{SourceSelectionNode{
			Base: SemanticPlanNodeBase{
				ID:                        "regions",
				Boundary:                  SemanticPlanNodeBoundarySourceSelection,
				PredicateBoundaryEvidence: PredicateBoundaryEvidence(SemanticPlanNodeBoundarySourceSelection),
				Dimensions:                []string{"region"},
				OutputGrain:               []GroupBy{{Name: "region", Dataset: "customers"}},
			},
			Source: SemanticSourceState{
				Root:             DatasetRef{Name: "customers"},
				SourceRoots:      []string{"customers"},
				RequiredDatasets: []string{"customers"},
			},
			Mode: SourceSelectionGroupedValues,
		}},
	}

	explanation, err := Explain(plan)
	if err != nil {
		t.Fatalf("explain semantic plan: %v", err)
	}
	if len(explanation.Nodes) != 1 || explanation.Nodes[0].ID != "regions" {
		t.Fatalf("nodes = %#v", explanation.Nodes)
	}
	if got := explanation.RelationshipExistence; len(got) != 1 || got[0].Relationship != "orders_to_customers" || got[0].PredicateKind != "filter" {
		t.Fatalf("relationship existence = %#v", got)
	}
	encoded, _ := json.Marshal(explanation)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("relationship explanation leaked predicate value: %s", encoded)
	}
	want := []SemanticOutputLineage{{
		Kind:           SemanticOutputDimension,
		Name:           "region",
		NodeIDs:        []string{"regions"},
		SourceDatasets: []string{"customers"},
	}}
	if !reflect.DeepEqual(explanation.Lineage, want) {
		t.Fatalf("lineage = %#v, want %#v", explanation.Lineage, want)
	}
}
