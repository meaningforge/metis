//go:build duckdb

package readiness

import (
	"context"
	"path/filepath"
	"testing"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	duckdbfixture "github.com/meaningforge/metis/cmd/s2sbench/bench/duckdbfixture"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

func TestCumulativeCaseDistinguishesPeriodValuesFromRunningTotal(t *testing.T) {
	scenario, ok := scenarios.ByName(CumulativeCaseScenarios[0].Name)
	if !ok {
		t.Fatal("cumulative case scenario is not registered")
	}
	backend, err := duckdbfixture.New(filepath.Join(t.TempDir(), "cumulative-case.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	if err := backend.Prepare(context.Background(), scenario); err != nil {
		t.Fatal(err)
	}

	periodOnly := `SELECT CAST(DATE_TRUNC('month', order_date) AS DATE) AS month, SUM(amount) AS revenue_to_date
FROM analytics.orders
GROUP BY 1
ORDER BY 1`
	actual, err := backend.RunSQL(context.Background(), periodOnly)
	if err != nil {
		t.Fatal(err)
	}
	verdict, judgeErr := s2sbench.Judge(scenario, actual)
	if verdict != s2sbench.VerdictWrong || resultMismatchCategory(judgeErr) != "result_value_mismatch" {
		t.Fatalf("period-only query verdict=%q category=%q error=%v", verdict, resultMismatchCategory(judgeErr), judgeErr)
	}

	runningTotal := `WITH monthly AS (
  SELECT CAST(DATE_TRUNC('month', order_date) AS DATE) AS month, SUM(amount) AS revenue
  FROM analytics.orders
  GROUP BY 1
)
SELECT month, SUM(revenue) OVER (ORDER BY month ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS revenue_to_date
FROM monthly
ORDER BY month`
	actual, err = backend.RunSQL(context.Background(), runningTotal)
	if err != nil {
		t.Fatal(err)
	}
	verdict, judgeErr = s2sbench.Judge(scenario, actual)
	if verdict != s2sbench.VerdictCorrect || judgeErr != nil {
		t.Fatalf("running-total query verdict=%q error=%v", verdict, judgeErr)
	}
}
