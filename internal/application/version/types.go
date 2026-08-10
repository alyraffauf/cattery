// Package version implements `cattery version` (PLAN.md Section 11.7): it
// returns the linker-populated build identity and the current runtime
// environment as typed fields. The package is Cobra-free: no CLI type
// appears here, and the CLI talks to the service through the frozen Result
// shape below. No repository, state, clock, or external dependency is
// reachable; buildinfo is the sole import.
package version

import (
	"time"

	"github.com/alyraffauf/cattery/internal/buildinfo"
)

// Result is the frozen outcome of one version query, projected from a
// buildinfo snapshot so the CLI renders only version-owned fields.
// Timestamp holds the raw linker value so rendering can show "unknown";
// BuiltAt is the parsed UTC time when one exists.
type Result struct {
	Version         string
	Commit          string
	Timestamp       string
	BuiltAt         time.Time
	HasTimestamp    bool
	GoVersion       string
	OperatingSystem string
	Architecture    string
}

// FromSnapshot projects a buildinfo snapshot into the version Result.
func FromSnapshot(snapshot buildinfo.Snapshot) Result {
	return Result{
		Version:         snapshot.Version,
		Commit:          snapshot.Commit,
		Timestamp:       snapshot.Timestamp,
		BuiltAt:         snapshot.BuiltAt,
		HasTimestamp:    snapshot.HasTimestamp,
		GoVersion:       snapshot.GoVersion,
		OperatingSystem: snapshot.OperatingSystem,
		Architecture:    snapshot.Architecture,
	}
}
