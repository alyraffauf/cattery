package version

import "github.com/alyraffauf/cattery/internal/buildinfo"

// Service returns the build and runtime identity of the running binary.
// Construction is side-effect-free and needs no dependencies: the invocation
// reads only the linker-populated buildinfo values and runtime constants.
type Service struct{}

// NewService constructs the version service.
func NewService() *Service {
	return &Service{}
}

// Version returns the current build and runtime identity as typed fields.
func (service *Service) Version() Result {
	return FromSnapshot(buildinfo.Current())
}
