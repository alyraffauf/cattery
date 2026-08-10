package add

import (
	"runtime"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// Service performs one add batch against the injectable ports. Construction
// is side-effect-free: every repository, filesystem, secret, and state effect
// happens inside Add. The runtime platform layer is read once at
// construction via runtime.GOOS so an explicit --platform must equal it.
type Service struct {
	deps     Dependencies
	platform deployment.Layer
}

// NewService binds the dependencies and resolves the runtime platform layer
// (linux or darwin on a supported host; the zero layer otherwise, which Add
// rejects).
func NewService(deps Dependencies) *Service {
	return &Service{deps: deps, platform: runtimeLayer()}
}

// runtimeLayer resolves the host platform layer, returning the zero layer
// when the host is neither linux nor darwin.
func runtimeLayer() deployment.Layer {
	layer, err := deployment.ParseLayer(runtime.GOOS)
	if err != nil {
		return ""
	}
	return layer
}
