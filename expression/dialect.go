package expression

import "strings"

type DialectProfile struct {
	Name                 string
	AllowArrow           bool
	AllowColonAccess     bool
	AllowDoubleColonCast bool
	AllowIndex           bool
}

var ANSI = DialectProfile{Name: "ansi", AllowIndex: true}
var Doris = DialectProfile{Name: "doris", AllowIndex: true}
var ClickHouse = DialectProfile{Name: "clickhouse", AllowArrow: true, AllowDoubleColonCast: true, AllowIndex: true}
var Snowflake = DialectProfile{Name: "snowflake", AllowColonAccess: true, AllowDoubleColonCast: true, AllowIndex: true}
var Databricks = DialectProfile{Name: "databricks", AllowArrow: true, AllowIndex: true}

func ProfileForDialect(name string) DialectProfile {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(n, "clickhouse"):
		return ClickHouse
	case strings.Contains(n, "snowflake"):
		return Snowflake
	case strings.Contains(n, "databricks") || strings.Contains(n, "spark"):
		return Databricks
	case strings.Contains(n, "doris"):
		return Doris
	default:
		return ANSI
	}
}
