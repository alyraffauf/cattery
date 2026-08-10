package deployment

import "fmt"

// Scope names the repository region a deployable entry belongs to. An empty
// Group identifies the root scope; any other Group names a top-level group
// directory beneath the repository root.
type Scope struct {
	Group string
}

// NewScope constructs a Scope. Pass an empty group for the root scope.
func NewScope(group string) Scope {
	return Scope{Group: group}
}

// IsRoot reports whether this scope is the ungrouped repository root.
func (s Scope) IsRoot() bool {
	return s.Group == ""
}

// Layer names the platform stratum a source entry belongs to. The base layer
// applies on every runtime; darwin and linux overlays replace or extend it on
// the matching platform.
type Layer string

const (
	LayerBase   Layer = "base"
	LayerDarwin Layer = "darwin"
	LayerLinux  Layer = "linux"
)

// ParseLayer converts a raw string into a Layer, rejecting unknown values.
func ParseLayer(value string) (Layer, error) {
	layer := Layer(value)
	if !layer.Valid() {
		return "", fmt.Errorf("deployment: unknown layer %q", value)
	}
	return layer, nil
}

// Valid reports whether layer is one of the supported constants.
func (l Layer) Valid() bool {
	switch l {
	case LayerBase, LayerDarwin, LayerLinux:
		return true
	}
	return false
}

// InactiveOn reports whether layer targets a platform other than platform. The
// base layer applies on every runtime so it is never inactive; a named platform
// layer is inactive when it does not equal the runtime platform. This replaces
// ad-hoc string comparisons so the file-layer "base" and alias-layer "all"
// rules cannot be conflated.
func (l Layer) InactiveOn(platform string) bool {
	if l == LayerBase {
		return false
	}
	return string(l) != platform
}
