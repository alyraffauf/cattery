package buildinfo

import (
	"runtime"
	"time"
)

// Version, Commit, and BuildTimestamp are the three linker-populated package
// variables permitted by Section 12.1. They describe a development build until
// a release overrides them with -ldflags -X.
var (
	Version        = "dev"
	Commit         = "unknown"
	BuildTimestamp = "unknown"
)

// Snapshot is an immutable copy of build and runtime identity captured at one
// moment. Timestamp holds the raw linker value so rendering can show
// "unknown"; BuiltAt is the parsed UTC time when one exists.
type Snapshot struct {
	Version         string
	Commit          string
	Timestamp       string
	BuiltAt         time.Time
	HasTimestamp    bool
	GoVersion       string
	OperatingSystem string
	Architecture    string
}

// Current captures the linker-populated values plus runtime fields.
func Current() Snapshot {
	return FromValues(Version, Commit, BuildTimestamp)
}

// FromValues builds a snapshot from explicit strings, leaving runtime fields
// populated. It never panics on a malformed timestamp.
func FromValues(version, commit, timestamp string) Snapshot {
	builtAt, hasTimestamp := parseTimestamp(timestamp)
	return Snapshot{
		Version:         version,
		Commit:          commit,
		Timestamp:       timestamp,
		BuiltAt:         builtAt,
		HasTimestamp:    hasTimestamp,
		GoVersion:       runtime.Version(),
		OperatingSystem: runtime.GOOS,
		Architecture:    runtime.GOARCH,
	}
}

// parseTimestamp accepts an RFC 3339 UTC release timestamp and rejects the
// development default or any malformed value without panicking.
func parseTimestamp(timestamp string) (time.Time, bool) {
	if timestamp == "" || timestamp == "unknown" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
