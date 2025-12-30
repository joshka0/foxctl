// Package buildinfo exposes metadata about the agentctl binary.
package buildinfo

import "runtime"

var (
	// Version is overridden at build-time via -ldflags.
	Version = "dev"
	// Commit captures the git commit for the build.
	Commit = ""
	// Date captures the RFC3339 timestamp for the build.
	Date = ""
)

// Info describes the build metadata surfaced to end users.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Date      string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
	CGO       bool   `json:"cgo,omitempty"`
}

// Current returns the build metadata for the running binary.
func Current() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		CGO:       isCGO,
	}

	if info.Version == "" {
		info.Version = "dev"
	}

	return info
}

// IsCGO returns true if the binary was built with CGO enabled.
func IsCGO() bool {
	return isCGO
}
