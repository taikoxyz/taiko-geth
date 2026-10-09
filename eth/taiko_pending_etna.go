package eth

import "time"

// taikoEtnaPendingUnavailable reports whether a missing pending block is
// expected because the head or the next block is an Etna block. An Etna
// block's parent beacon root is an L1 state root that only its proposer knows,
// so no pending Etna block is built locally; the pending block and header are
// then null without an error, as in the reference client. The next block's
// time is the wall-clock time the pending block would be built at.
func (b *EthAPIBackend) taikoEtnaPendingUnavailable() bool {
	config := b.ChainConfig()
	return config.IsEtna(b.eth.blockchain.CurrentBlock().Time) || config.IsEtna(uint64(time.Now().Unix()))
}
