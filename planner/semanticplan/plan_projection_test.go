package semanticplan

import (
	"strings"
	"testing"
)

func TestFingerprintAndProjectionOwnSemanticPlanStructure(t *testing.T) {
	plan := &SemanticPlan{
		Model:       ModelRef{Project: "analytics", Name: "sales"},
		Root:        DatasetRef{Name: "orders", Source: "warehouse.orders"},
		Projections: []Projection{{Name: "revenue", Datasets: []string{"customers", "orders"}}},
	}

	fingerprint, err := Fingerprint(plan)
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("fingerprint = %q, %v; want SHA-256 fingerprint", fingerprint, err)
	}
	var projection strings.Builder
	if err := ProjectPlan(&projection, plan); err != nil {
		t.Fatalf("project semantic plan: %v", err)
	}
	if got := projection.String(); !strings.Contains(got, "plan{") || !strings.Contains(got, "projections[") {
		t.Fatalf("projection does not encode plan structure: %q", got)
	}
}

func TestSourceScanWorkCanonicalizesScanSets(t *testing.T) {
	work, err := SourceScanWorkOfNode(SourceAggregateNode{
		Base: SemanticPlanNodeBase{ID: "revenue"},
		Source: SemanticSourceState{
			Root:             DatasetRef{Name: "orders"},
			SourceRoots:      []string{"orders", "customers", "orders"},
			RequiredDatasets: []string{"customers", "orders", "customers"},
		},
	})
	if err != nil {
		t.Fatalf("source scan work: %v", err)
	}
	if got, want := strings.Join(work.SourceRoots, ","), "customers,orders"; got != want {
		t.Fatalf("source roots = %q, want %q", got, want)
	}
	if identity, err := work.Identity(); err != nil || len(identity) != 64 {
		t.Fatalf("source scan identity = %q, %v; want SHA-256 identity", identity, err)
	}
}
