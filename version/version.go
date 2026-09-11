// Package version owns Metis build version, commit, date, and Go runtime
// metadata shared by process entrypoints and protocol implementations.
package version

import "runtime"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go_version"`
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, Date: Date, Go: runtime.Version()}
}
