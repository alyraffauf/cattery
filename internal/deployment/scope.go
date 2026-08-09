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
