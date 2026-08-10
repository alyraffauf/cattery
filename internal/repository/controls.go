// Package repository classifies scope-root entries that govern how the
// repository deploys.
package repository

import "strings"

// SecretDirectoryName is the reserved scope directory for encrypted sources.
const SecretDirectoryName = "_secrets"

// Control classifies one scope-root entry name.
type Control int

const (
	// ControlNone is an ordinary deployable entry.
	ControlNone Control = iota
	// ControlDarwin is the _darwin platform overlay directory.
	ControlDarwin
	// ControlLinux is the _linux platform overlay directory.
	ControlLinux
	// ControlSecrets is the _secrets encrypted source tree.
	ControlSecrets
	// ControlHooks is the _hooks directory.
	ControlHooks
	// ControlRoutes is the _routes.toml alias manifest.
	ControlRoutes
	// ControlIgnoredUnderscore is an unrecognized _... entry that is never
	// deployed and is not an error.
	ControlIgnoredUnderscore
	// ControlMetadata is version-control or SOPS metadata ignored at the
	// repository root only.
	ControlMetadata
)

// ClassifyRoot classifies a name at a repository or group scope root.
func ClassifyRoot(name string) Control {
	switch name {
	case "_darwin":
		return ControlDarwin
	case "_linux":
		return ControlLinux
	case SecretDirectoryName:
		return ControlSecrets
	case "_hooks":
		return ControlHooks
	case "_routes.toml":
		return ControlRoutes
	case ".git", ".github", ".gitignore", ".gitattributes", ".gitmodules", ".sops.yaml":
		return ControlMetadata
	}
	if strings.HasPrefix(name, "_") {
		return ControlIgnoredUnderscore
	}
	return ControlNone
}

// ClassifyPlatformLayer classifies a name at a _darwin or _linux layer root.
// The layer recognizes _secrets and ignores every other leading-underscore
// entry; ordinary names are literal deployable entries and repository-root
// metadata is not special here.
func ClassifyPlatformLayer(name string) Control {
	if name == SecretDirectoryName {
		return ControlSecrets
	}
	if strings.HasPrefix(name, "_") {
		return ControlIgnoredUnderscore
	}
	return ControlNone
}

// IsScopeControl reports whether name is one of the recognized scope-root
// controls. Unknown underscore entries, metadata, and ordinary names are not
// scope controls.
func IsScopeControl(name string) bool {
	switch ClassifyRoot(name) {
	case ControlDarwin, ControlLinux, ControlSecrets, ControlHooks, ControlRoutes:
		return true
	}
	return false
}

// String renders control as a stable lowercase label for diagnostics.
func (control Control) String() string {
	switch control {
	case ControlNone:
		return "none"
	case ControlDarwin:
		return "darwin"
	case ControlLinux:
		return "linux"
	case ControlSecrets:
		return "secrets"
	case ControlHooks:
		return "hooks"
	case ControlRoutes:
		return "routes"
	case ControlIgnoredUnderscore:
		return "ignored-underscore"
	case ControlMetadata:
		return "metadata"
	}
	return "unknown"
}
