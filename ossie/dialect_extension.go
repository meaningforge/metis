package ossie

// Metis extended source-expression dialects are intentionally defined outside
// model.go. model.go is generated from the pinned Apache Ossie Core schema and
// must remain a faithful upstream binding that can be regenerated verbatim.
const (
	DialectClickHouse Dialect = "CLICKHOUSE"
	DialectDoris      Dialect = "DORIS"
)

// IsCoreDialect reports whether d belongs to the pinned Apache Ossie Core
// expression-dialect enum.
func IsCoreDialect(d Dialect) bool {
	switch d {
	case DialectANSISQL, DialectSnowflake, DialectMDX, DialectTableau, DialectDatabricks, DialectMAQL, DialectBigQuery:
		return true
	default:
		return false
	}
}

// IsMetisExtendedDialect reports whether d is a Metis-owned extension used to
// express target-native semantic logic without modifying the upstream Ossie
// Core schema binding.
func IsMetisExtendedDialect(d Dialect) bool {
	switch d {
	case DialectClickHouse, DialectDoris:
		return true
	default:
		return false
	}
}

func IsSupportedExpressionDialect(d Dialect) bool {
	return IsCoreDialect(d) || IsMetisExtendedDialect(d)
}
