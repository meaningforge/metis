// Package observability defines Metis runtime observation contracts.
//
// Its metric and trace vocabularies are intentionally closed and low-cardinality.
// Instrumentation must never change semantic behavior, and raw query, plan, or
// exception data must never become metric labels.
package observability
