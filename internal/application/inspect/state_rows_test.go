package inspect

import "github.com/alyraffauf/cattery/internal/state"

type stateRows struct {
	files   []state.FileBaseline
	aliases []state.AliasBaseline
}
