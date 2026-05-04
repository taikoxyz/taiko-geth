package vm

import "testing"

func TestEVMSetZkGasLimitErr_IdempotentWhenSlotEmpty(t *testing.T) {
	evm := &EVM{}
	evm.setZkGasLimitErr()
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded", evm.zkGasErr)
	}
}

func TestEVMSetZkGasLimitErr_DoesNotClobberExistingError(t *testing.T) {
	evm := &EVM{zkGasErr: ErrZkGasLimitExceeded}
	evm.setZkGasLimitErr()
	if evm.zkGasErr != ErrZkGasLimitExceeded {
		t.Fatalf("zkGasErr = %v, want ErrZkGasLimitExceeded preserved", evm.zkGasErr)
	}
}

func TestEVMResetZkGasErr_ClearsSlot(t *testing.T) {
	evm := &EVM{zkGasErr: ErrZkGasLimitExceeded}
	evm.ResetZkGasErr()
	if evm.zkGasErr != nil {
		t.Fatalf("zkGasErr = %v, want nil after reset", evm.zkGasErr)
	}
}
