// Package runtime owns current in-memory Project snapshots and request pinning.
// Trusted hosts can replace a snapshot with CAS. There is no version catalog,
// Release store, history, publication, rollback workflow or management endpoint.
package runtime
