package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestRelationshipExistenceLowersWithoutFanoutOrMeasureDeduplication(t *testing.T) {
	scenario, ok := scenarios.ByName("relationship_exists_filters_source_population")
	if !ok {
		t.Fatal("relationship existence scenario is not registered")
	}
	for _, target := range compilerTargets {
		t.Run(target.Dialect, func(t *testing.T) {
			plan, rendered, err := compileScenario(t, scenario, target.Dialect)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.RelationshipExistence) != 1 || plan.RelationshipExistence[0].Relationship.Name != "orders_to_details" {
				t.Fatalf("semantic relationship existence = %#v", plan.RelationshipExistence)
			}
			upper := strings.ToUpper(rendered.SQL)
			if !strings.Contains(upper, "EXISTS (SELECT 1 FROM") {
				t.Fatalf("SQL does not contain correlated EXISTS:\n%s", rendered.SQL)
			}
			if strings.Contains(upper, "JOIN ANALYTICS.FANOUT_DETAILS") || strings.Contains(upper, "DISTINCT ORDERS.AMOUNT") {
				t.Fatalf("SQL changed source multiplicity instead of using membership:\n%s", rendered.SQL)
			}
			if len(rendered.Parameters) != 1 || rendered.Parameters[0].Value != "target" {
				t.Fatalf("parameters = %#v", rendered.Parameters)
			}
		})
	}
}
