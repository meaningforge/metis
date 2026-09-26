package regression

import "testing"

func TestExactScalarSemantics(t *testing.T) {
	for _, tc := range []struct {
		kind, datatype, a, b string
		equal                bool
	}{
		{"integer", "Integer", "9007199254740993", "9007199254740992", false},
		{"decimal", "Decimal", "1.00", "1", true},
		{"decimal", "Decimal", "0.000000000000001", "0.000000000000002", false},
		{"datetime_tz", "DateTimeTz", "2026-09-26T08:00:00+08:00", "2026-09-26T00:00:00Z", true},
		{"time", "Time", "12:00:00.1", "12:00:00.100", true},
	} {
		if got := cellMatches(Scalar{Type: tc.kind, Value: tc.a}, Scalar{Type: tc.kind, Value: tc.b}, tc.datatype, Tolerance{}); got != tc.equal {
			t.Errorf("%#v: %v", tc, got)
		}
	}
	if cellMatches(Scalar{Type: "null"}, Scalar{Type: "integer", Value: "0"}, "Integer", Tolerance{Abs: "1"}) {
		t.Fatal("null equals zero")
	}
	if !cellMatches(Scalar{Type: "decimal", Value: "100"}, Scalar{Type: "decimal", Value: "100.1"}, "Decimal", Tolerance{Rel: "0.001"}) {
		t.Fatal("relative tolerance")
	}
	if cellMatches(Scalar{Type: "decimal", Value: "100"}, Scalar{Type: "decimal", Value: "100.10001"}, "Decimal", Tolerance{Rel: "0.001"}) {
		t.Fatal("tolerance overreach")
	}
	for _, text := range []string{"NaN", "Inf", "1/2", "1e99999"} {
		if _, err := exactNumber(text); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}

func TestRowsPreserveMultiplicityAndMatchToleranceByKey(t *testing.T) {
	cols := []ExpectedColumn{{Name: "key", Datatype: "String"}, {Name: "value", Datatype: "Decimal"}}
	row := func(key, value string) []Scalar {
		return []Scalar{{Type: "string", Value: key}, {Type: "decimal", Value: value}}
	}
	a, b := row("a", "1"), row("b", "2")
	expect := ExpectedRows{Mode: "unordered", Values: [][]Scalar{a, a, b}}
	if diff := compareRows(expect, [][]Scalar{b, a, a}, cols); len(diff) != 0 {
		t.Fatal(diff)
	}
	if diff := compareRows(expect, [][]Scalar{a, b, b}, cols); len(diff) == 0 {
		t.Fatal("ignored duplicate counts")
	}
	expect.Values, expect.KeyColumns, expect.Tolerances = [][]Scalar{a, b}, []string{"key"}, map[string]Tolerance{"value": {Abs: "0.1"}}
	if diff := compareRows(expect, [][]Scalar{row("b", "2.1"), row("a", "0.9")}, cols); len(diff) != 0 {
		t.Fatal(diff)
	}
	if diff := compareRows(expect, [][]Scalar{a, a}, cols); len(diff) == 0 {
		t.Fatal("accepted duplicate key")
	}
	if diff := compareRows(expect, [][]Scalar{a, row("c", "2")}, cols); len(diff) == 0 {
		t.Fatal("accepted different keys")
	}
	expect.Mode = "ordered"
	if diff := compareRows(expect, [][]Scalar{b, a}, cols); len(diff) == 0 {
		t.Fatal("ignored ordering")
	}
}

func TestOrderedRuntimeRowsRequireExplicitUniqueOrdering(t *testing.T) {
	suite, _, err := ParseSuite([]byte(runtimeSuite))
	if err != nil {
		t.Fatal(err)
	}
	c := suite.Cases[0]
	count := int64(2)
	c.Expect.RowCount = &count
	c.Expect.Rows = &ExpectedRows{Mode: "ordered", Values: [][]Scalar{{{Type: "decimal", Value: "1"}}, {{Type: "decimal", Value: "2"}}}}
	if err := validateRuntimeCase(c); err == nil {
		t.Fatal("accepted missing key")
	}
	c.Expect.Rows.KeyColumns = []string{"total_revenue"}
	if err := validateRuntimeCase(c); err == nil {
		t.Fatal("accepted missing order_by")
	}
	c.Request.Query.OrderBy = []OrderInput{{Field: "total_revenue", Direction: "asc"}}
	if err := validateRuntimeCase(c); err != nil {
		t.Fatal(err)
	}
	c.Expect.Rows.Values[1][0].Value = "1.00"
	if err := validateRuntimeCase(c); err == nil {
		t.Fatal("accepted duplicate canonical key")
	}
}
