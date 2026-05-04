package vm

// CHANGE(taiko): setZkGasLimitErr stores the sticky zk-gas-limit error on the
// EVM so it cannot be swallowed by op*Call returning ok=false. Mirrors
// alethia-reth's set_custom_error guard at adapter.rs:399-403, which only
// writes when the slot is currently Ok.
func (evm *EVM) setZkGasLimitErr() {
	if evm.zkGasErr == nil {
		evm.zkGasErr = ErrZkGasLimitExceeded
	}
}

// CHANGE(taiko): ResetZkGasErr clears the sticky zk-gas-limit slot. Called by
// the block executor between transactions so a previously-truncated tx's slot
// does not poison subsequent txs in the same block-execution context.
func (evm *EVM) ResetZkGasErr() {
	evm.zkGasErr = nil
}
