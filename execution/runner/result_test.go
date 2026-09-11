package runner

import (
	"errors"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

func TestNormalizeResultRowReturnsValueFreeContractEvidence(t *testing.T) {
	_, err := normalizeResultRow([]any{"password=must-not-leak"}, artifact.OutputSchema{Columns: []artifact.OutputColumn{{
		Name: "metric:sales.revenue", Datatype: ossie.DataTypeInteger,
	}}})
	var contractErr *ResultContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("normalizeResultRow error = %#v", err)
	}
	if contractErr.Column != "metric:sales.revenue" || contractErr.ExpectedDatatype != ossie.DataTypeInteger || contractErr.ObservedFamily != ResultFamilyText {
		t.Fatalf("contract error = %#v", contractErr)
	}
	if contractErr.Error() == "password=must-not-leak" {
		t.Fatalf("contract error leaked result value: %v", contractErr)
	}
}

func TestNormalizeTemporalResultValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		datatype ossie.DataType
		want     string
	}{
		{name: "date bytes", value: []byte("2026-09-02"), datatype: ossie.DataTypeDate, want: "2026-09-02"},
		{name: "time text", value: "12:34:56.1200", datatype: ossie.DataTypeTime, want: "12:34:56.12"},
		{name: "datetime database text", value: "2026-09-02 12:34:56.1200", datatype: ossie.DataTypeDateTime, want: "2026-09-02T12:34:56.12"},
		{name: "datetime canonical text", value: "2026-09-02T12:34:56", datatype: ossie.DataTypeDateTime, want: "2026-09-02T12:34:56"},
		{name: "datetime with timezone", value: "2026-09-02T12:34:56.1200+08:00", datatype: ossie.DataTypeDateTimeTz, want: "2026-09-02T12:34:56.12+08:00"},
		{name: "driver time date", value: time.Date(2026, 9, 2, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)), datatype: ossie.DataTypeDate, want: "2026-09-02"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeResultValue(test.value, test.datatype)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("normalized temporal value = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNormalizeTemporalResultValueRejectsSchemaMismatch(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		datatype ossie.DataType
	}{
		{name: "timestamp is not date", value: "2026-09-02 00:00:00", datatype: ossie.DataTypeDate},
		{name: "driver timestamp is not date", value: time.Date(2026, 9, 2, 12, 34, 56, 0, time.UTC), datatype: ossie.DataTypeDate},
		{name: "invalid calendar date", value: "2026-02-30", datatype: ossie.DataTypeDate},
		{name: "timezone is not naive datetime", value: "2026-09-02T12:34:56+08:00", datatype: ossie.DataTypeDateTime},
		{name: "timezone required", value: "2026-09-02T12:34:56", datatype: ossie.DataTypeDateTimeTz},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, err := normalizeResultValue(test.value, test.datatype); err == nil {
				t.Fatalf("normalizeResultValue(%#v, %s) = %#v, want error", test.value, test.datatype, got)
			}
		})
	}
}
