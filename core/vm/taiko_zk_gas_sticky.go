package vm

// CHANGE(taiko): setZkGasErr stores the sticky zk-gas-limit error on the EVM
// so it cannot be swallowed by op*Call returning ok=false. Only the first
// error survives — re-entrant call sites can invoke it freely without
// overwriting prior state.
func (evm *EVM) setZkGasErr() {
	if evm.zkGasErr == nil {
		evm.zkGasErr = ErrZkGasLimitExceeded
	}
}

// CHANGE(taiko): ResetZkGasErr clears the sticky zk-gas-limit slot. The block
// executor calls it between transactions so a previously-truncated tx's slot
// does not poison subsequent txs in the same block-execution context.
func (evm *EVM) ResetZkGasErr() {
	evm.zkGasErr = nil
}
