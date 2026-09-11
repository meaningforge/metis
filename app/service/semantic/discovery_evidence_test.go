package semantic

import "testing"

func TestRankReturnsDeterministicEvidence(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		assetName  string
		qualified  string
		context    string
		wantScore  int
		wantReason MatchReason
	}{
		{
			name:       "exact qualified name",
			query:      "sales total revenue",
			assetName:  "total_revenue",
			qualified:  "sales.total_revenue",
			wantScore:  100,
			wantReason: MatchReasonExactQualified,
		},
		{
			name:       "exact name",
			query:      "total revenue",
			assetName:  "total_revenue",
			qualified:  "sales.total_revenue",
			wantScore:  95,
			wantReason: MatchReasonExactName,
		},
		{
			name:       "name contains",
			query:      "revenue",
			assetName:  "total_revenue",
			qualified:  "sales.total_revenue",
			wantScore:  80,
			wantReason: MatchReasonNameContains,
		},
		{
			name:       "qualified name contains",
			query:      "sales total",
			assetName:  "total_revenue",
			qualified:  "sales.total_revenue",
			wantScore:  70,
			wantReason: MatchReasonQualifiedContains,
		},
		{
			name:       "context contains",
			query:      "gross sales",
			assetName:  "revenue",
			qualified:  "sales.revenue",
			context:    "Gross sales amount",
			wantScore:  50,
			wantReason: MatchReasonContextContains,
		},
		{
			name:       "token contains",
			query:      "revenue amount",
			assetName:  "revenue",
			qualified:  "sales.revenue",
			context:    "recognized value",
			wantScore:  30,
			wantReason: MatchReasonTokenContains,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, reasons := rank(tt.query, tt.assetName, tt.qualified, tt.context)
			if score != tt.wantScore {
				t.Fatalf("score = %d, want %d", score, tt.wantScore)
			}
			if len(reasons) != 1 || reasons[0] != tt.wantReason {
				t.Fatalf("reasons = %#v, want [%q]", reasons, tt.wantReason)
			}
		})
	}
}

func TestAppendMatchCarriesRankingEvidence(t *testing.T) {
	matches := appendMatch(nil, AssetMetric, "finance", "sales", "total_revenue", "sales.total_revenue", "Total sales revenue", "", "revenue")
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
	if matches[0].Score != 80 || len(matches[0].MatchReasons) != 1 || matches[0].MatchReasons[0] != MatchReasonNameContains {
		t.Fatalf("match = %#v", matches[0])
	}
}
