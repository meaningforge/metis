package expression

import "testing"

func TestProfileForDialectSelectsKnownProfiles(t *testing.T) {
	tests := []struct {
		input string
		want  DialectProfile
	}{
		{input: "clickhouse", want: ClickHouse},
		{input: " CLICKHOUSE-native ", want: ClickHouse},
		{input: "snowflake", want: Snowflake},
		{input: "databricks", want: Databricks},
		{input: "spark_sql", want: Databricks},
		{input: "doris", want: Doris},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ProfileForDialect(tt.input); got != tt.want {
				t.Fatalf("ProfileForDialect(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProfileForDialectDefaultsToANSI(t *testing.T) {
	for _, input := range []string{"", "postgres", "unknown"} {
		t.Run(input, func(t *testing.T) {
			if got := ProfileForDialect(input); got != ANSI {
				t.Fatalf("ProfileForDialect(%q) = %#v, want ANSI", input, got)
			}
		})
	}
}
