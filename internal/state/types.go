// Package state owns the SQLite-backed baseline store that records the last
// successfully deployed representation of every managed target. This file holds
// the data-transfer objects and enum validators only: no SQL, XDG, or file-lock
// type crosses the package boundary here. Provider and store seams live in
// later files.
package state

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// SourceStatus marks a baseline row as currently deployed or historically
// retired. A retired row keeps its baseline for diagnostics and safe
// reactivation.
type SourceStatus string

const (
	// StatusActive marks a row whose target is currently managed.
	StatusActive SourceStatus = "active"
	// StatusRetired marks a row whose source is gone but whose baseline remains.
	StatusRetired SourceStatus = "retired"
)

// ParseSourceStatus converts a raw string into a SourceStatus, rejecting
// unknown values verbatim.
func ParseSourceStatus(value string) (SourceStatus, error) {
	status := SourceStatus(value)
	if !status.Valid() {
		return "", fmt.Errorf("state: unknown source status %q", value)
	}
	return status, nil
}

// Valid reports whether status is one of the supported constants.
func (s SourceStatus) Valid() bool {
	switch s {
	case StatusActive, StatusRetired:
		return true
	}
	return false
}

// AliasLayer names the platform stratum an alias belongs to. Aliases, unlike
// files, admit an "all" layer that applies on every runtime.
type AliasLayer string

const (
	// LayerAll applies the alias on every supported platform.
	LayerAll AliasLayer = "all"
	// LayerDarwin applies the alias only on Darwin.
	LayerDarwin AliasLayer = "darwin"
	// LayerLinux applies the alias only on Linux.
	LayerLinux AliasLayer = "linux"
)

// ParseAliasLayer converts a raw string into an AliasLayer, rejecting unknown
// values verbatim.
func ParseAliasLayer(value string) (AliasLayer, error) {
	layer := AliasLayer(value)
	if !layer.Valid() {
		return "", fmt.Errorf("state: unknown alias layer %q", value)
	}
	return layer, nil
}

// Valid reports whether layer is one of the supported constants.
func (l AliasLayer) Valid() bool {
	switch l {
	case LayerAll, LayerDarwin, LayerLinux:
		return true
	}
	return false
}

// Repository is one registered (root, home) pair tracked by state. RootPath and
// HomePath are canonical absolute identity anchors; all other paths are
// relative.
type Repository struct {
	ID         int64
	RootPath   string
	HomePath   string
	IsDefault  bool
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// FileBaseline is one persisted file baseline row. Target and source paths are
// slash-normalized relative forms; the hashes are 32-byte BLAKE3 digests.
type FileBaseline struct {
	RepositoryID        int64
	TargetPath          string
	GroupName           string
	SourcePath          string
	SourceKind          deployment.FileKind
	Layer               deployment.Layer
	BaselineContentHash deployment.Digest
	BaselineSourceHash  deployment.Digest
	ExecutableBits      uint32
	Status              SourceStatus
	AppliedAt           time.Time
	RetiredAt           *time.Time
}

// AliasBaseline is one persisted alias baseline row. Alias and canonical target
// paths are slash-normalized relative forms.
type AliasBaseline struct {
	RepositoryID        int64
	AliasPath           string
	CanonicalTargetPath string
	GroupName           string
	Layer               AliasLayer
	Status              SourceStatus
	AppliedAt           time.Time
	RetiredAt           *time.Time
}

// IsSlashRelative reports whether path is a non-empty relative path expressed
// with forward slashes only: it must not be absolute and must not contain a
// backslash. State rows store target and source paths in this form so the
// database stays portable across platforms.
func IsSlashRelative(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	return !strings.ContainsRune(path, '\\')
}

// CloneTimestamp returns a defensive copy of when. A nil timestamp stays nil so
// callers can distinguish an active row from a retired one without allocating.
func CloneTimestamp(when *time.Time) *time.Time {
	if when == nil {
		return nil
	}
	copied := *when
	return &copied
}

// CopyRepositories returns a defensive copy of rows so a caller cannot mutate
// the source slice through the returned reference.
func CopyRepositories(rows []Repository) []Repository {
	if rows == nil {
		return nil
	}
	out := make([]Repository, len(rows))
	copy(out, rows)
	return out
}

// CopyFileBaselines returns a defensive copy of rows, cloning the optional
// retirement timestamps so a caller cannot mutate state-owned pointers.
func CopyFileBaselines(rows []FileBaseline) []FileBaseline {
	if rows == nil {
		return nil
	}
	out := make([]FileBaseline, len(rows))
	copy(out, rows)
	for index := range out {
		out[index].RetiredAt = CloneTimestamp(out[index].RetiredAt)
	}
	return out
}

// CopyAliasBaselines returns a defensive copy of rows, cloning the optional
// retirement timestamps so a caller cannot mutate state-owned pointers.
func CopyAliasBaselines(rows []AliasBaseline) []AliasBaseline {
	if rows == nil {
		return nil
	}
	out := make([]AliasBaseline, len(rows))
	copy(out, rows)
	for index := range out {
		out[index].RetiredAt = CloneTimestamp(out[index].RetiredAt)
	}
	return out
}
