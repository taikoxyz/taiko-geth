package consensus

import "github.com/ethereum/go-ethereum/core/types"

// AssemblyPreparer is implemented by engines that validate and normalize a
// block body before it is finalized and assembled. Block assembly is not part
// of the Engine interface, so every build path calls PrepareAssembly right
// before Engine.Finalize to keep these checks in force.
type AssemblyPreparer interface {
	// PrepareAssembly checks that the header and body can be assembled into a
	// block, and may normalize the body. It is called before Finalize.
	PrepareAssembly(chain ChainHeaderReader, header *types.Header, body *types.Body) error
}

// PrepareAssembly runs the engine's pre-assembly checks if the engine has any,
// and is a no-op otherwise.
func PrepareAssembly(engine Engine, chain ChainHeaderReader, header *types.Header, body *types.Body) error {
	if preparer, ok := engine.(AssemblyPreparer); ok {
		return preparer.PrepareAssembly(chain, header, body)
	}
	return nil
}
