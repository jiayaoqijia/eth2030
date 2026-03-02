package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/zkvm"
)

// mockStorage implements StorageResolver for testing.
type mockStorage struct {
	data    map[types.Hash]types.Hash
	balance *big.Int
	acl     map[types.Address]bool
	slotACL map[types.Address]map[types.Hash]bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data:    make(map[types.Hash]types.Hash),
		balance: new(big.Int),
		acl:     make(map[types.Address]bool),
		slotACL: make(map[types.Address]map[types.Hash]bool),
	}
}

func (m *mockStorage) SLoad(_ types.Address, key types.Hash) types.Hash {
	return m.data[key]
}

func (m *mockStorage) SStore(_ types.Address, key, value types.Hash) {
	m.data[key] = value
}

func (m *mockStorage) GetBalance(_ types.Address) *big.Int {
	return new(big.Int).Set(m.balance)
}

func (m *mockStorage) AddressInAccessList(addr types.Address) bool {
	return m.acl[addr]
}

func (m *mockStorage) AddAddressToAccessList(addr types.Address) {
	m.acl[addr] = true
}

func (m *mockStorage) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	if slots, ok := m.slotACL[addr]; ok {
		return true, slots[slot]
	}
	return m.acl[addr], false
}

func (m *mockStorage) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	m.acl[addr] = true
	if m.slotACL[addr] == nil {
		m.slotACL[addr] = make(map[types.Hash]bool)
	}
	m.slotACL[addr][slot] = true
}

// --- InterpretEVM Tests ---

func TestInterpretEVM_PushAddReturn(t *testing.T) {
	// PUSH1 3, PUSH1 5, ADD, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x03, // PUSH1 3
		0x60, 0x05, // PUSH1 5
		0x01,       // ADD
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, gasUsed, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasUsed == 0 {
		t.Error("expected non-zero gas usage")
	}
	if len(output) != 32 {
		t.Fatalf("output length = %d, want 32", len(output))
	}
	// Last byte should be 8 (3 + 5).
	if output[31] != 8 {
		t.Errorf("output[31] = %d, want 8", output[31])
	}
}

func TestInterpretEVM_CounterIncrement(t *testing.T) {
	// Load slot 0, add 1, store back to slot 0, then stop.
	// PUSH1 0, SLOAD, PUSH1 1, ADD, PUSH1 0, SSTORE, STOP
	code := []byte{
		0x60, 0x00, // PUSH1 0 (key)
		0x54,       // SLOAD
		0x60, 0x01, // PUSH1 1
		0x01,       // ADD
		0x60, 0x00, // PUSH1 0 (key)
		0x55,       // SSTORE
		0x00,       // STOP
	}

	storage := newMockStorage()
	addr := types.Address{0x01}

	// First execution: 0 + 1 = 1
	_, _, err := InterpretEVM(code, nil, storage, addr, 100000)
	if err != nil {
		t.Fatalf("first exec error: %v", err)
	}

	key := types.Hash{}
	val := storage.data[key]
	if val[31] != 1 {
		t.Errorf("after first exec: slot 0 = %d, want 1", val[31])
	}

	// Second execution: 1 + 1 = 2
	_, _, err = InterpretEVM(code, nil, storage, addr, 100000)
	if err != nil {
		t.Fatalf("second exec error: %v", err)
	}

	val = storage.data[key]
	if val[31] != 2 {
		t.Errorf("after second exec: slot 0 = %d, want 2", val[31])
	}
}

func TestInterpretEVM_JumpJumpi(t *testing.T) {
	// Test JUMP: PUSH1 5, JUMP, INVALID, JUMPDEST, PUSH1 42, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x04, // PUSH1 4 (destination)
		0x56,       // JUMP
		0xFE,       // INVALID (should be skipped)
		0x5B,       // JUMPDEST (offset 4)
		0x60, 0x2A, // PUSH1 42
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("JUMP test error: %v", err)
	}
	if len(output) != 32 || output[31] != 42 {
		t.Errorf("JUMP test: output[31] = %d, want 42", output[31])
	}
}

func TestInterpretEVM_JumpiTaken(t *testing.T) {
	// PUSH1 1 (true condition), PUSH1 6, JUMPI, INVALID, INVALID, INVALID, JUMPDEST, STOP
	code := []byte{
		0x60, 0x01, // PUSH1 1 (condition: true)
		0x60, 0x08, // PUSH1 8 (destination)
		0x57,       // JUMPI
		0xFE,       // INVALID
		0xFE,       // INVALID
		0xFE,       // INVALID
		0x5B,       // JUMPDEST (offset 8)
		0x00,       // STOP
	}

	_, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("JUMPI taken error: %v", err)
	}
}

func TestInterpretEVM_JumpiNotTaken(t *testing.T) {
	// PUSH1 0 (false condition), PUSH1 dest, JUMPI, PUSH1 99, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x00, // PUSH1 0 (condition: false)
		0x60, 0xFF, // PUSH1 255 (bad dest, but should not jump)
		0x57,       // JUMPI (not taken)
		0x60, 0x63, // PUSH1 99
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("JUMPI not-taken error: %v", err)
	}
	if output[31] != 99 {
		t.Errorf("JUMPI not-taken: output[31] = %d, want 99", output[31])
	}
}

func TestInterpretEVM_Revert(t *testing.T) {
	// PUSH1 0, PUSH1 0, REVERT
	code := []byte{
		0x60, 0x00, // PUSH1 0
		0x60, 0x00, // PUSH1 0
		0xFD,       // REVERT
	}

	_, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != ErrEVMRevert {
		t.Errorf("expected ErrEVMRevert, got %v", err)
	}
}

func TestInterpretEVM_StackOverflow(t *testing.T) {
	// Push 1025 items (exceeds 1024 limit).
	code := make([]byte, 0, 1025*2+1)
	for i := 0; i < 1025; i++ {
		code = append(code, 0x60, 0x01) // PUSH1 1
	}
	code = append(code, 0x00) // STOP

	_, _, err := InterpretEVM(code, nil, nil, types.Address{}, 10000000)
	if err != ErrEVMStackOverflow {
		t.Errorf("expected ErrEVMStackOverflow, got %v", err)
	}
}

func TestInterpretEVM_StackUnderflow(t *testing.T) {
	// ADD with empty stack.
	code := []byte{0x01} // ADD

	_, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != ErrEVMStackUnderflow {
		t.Errorf("expected ErrEVMStackUnderflow, got %v", err)
	}
}

func TestInterpretEVM_OutOfGas(t *testing.T) {
	// PUSH1 with only 1 gas (needs 3).
	code := []byte{0x60, 0x01}

	_, _, err := InterpretEVM(code, nil, nil, types.Address{}, 1)
	if err != ErrEVMInterpOOG {
		t.Errorf("expected ErrEVMInterpOOG, got %v", err)
	}
}

func TestInterpretEVM_DupSwap(t *testing.T) {
	// PUSH1 10, PUSH1 20, DUP2 (should push 10), PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x0A, // PUSH1 10
		0x60, 0x14, // PUSH1 20
		0x81,       // DUP2 (copy item at depth 1: 10)
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("DUP2 test error: %v", err)
	}
	if output[31] != 10 {
		t.Errorf("DUP2 test: output[31] = %d, want 10", output[31])
	}
}

func TestInterpretEVM_SwapReturn(t *testing.T) {
	// PUSH1 10, PUSH1 20, SWAP1, POP, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	// After SWAP1: stack is [20, 10], then POP gives [20], returned.
	code := []byte{
		0x60, 0x0A, // PUSH1 10
		0x60, 0x14, // PUSH1 20
		0x90,       // SWAP1
		0x50,       // POP (remove top = 10)
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("SWAP test error: %v", err)
	}
	if output[31] != 20 {
		t.Errorf("SWAP test: output[31] = %d, want 20", output[31])
	}
}

func TestInterpretEVM_SubMulDiv(t *testing.T) {
	// EVM stack: first pop is 'a', second pop is 'b'.
	// SUB: a - b; MUL: a * b; DIV: a / b.
	// Push order is reversed (last pushed = first popped).
	code := []byte{
		0x60, 0x03, // PUSH1 3   → stack: [3]
		0x60, 0x0A, // PUSH1 10  → stack: [3, 10]
		0x03,       // SUB: a=10, b=3 → 10-3=7  → stack: [7]
		0x60, 0x02, // PUSH1 2   → stack: [7, 2]
		0x02,       // MUL: a=2, b=7 → 2*7=14  → stack: [14]
		0x60, 0x07, // PUSH1 7   → stack: [14, 7]
		0x04,       // DIV: a=7, b=14 → 7/14=0 ... wrong order
	}
	// We need 14/7=2, so push 14 last:
	code = []byte{
		0x60, 0x03, // PUSH1 3   → stack: [3]
		0x60, 0x0A, // PUSH1 10  → stack: [3, 10]
		0x03,       // SUB: a=10, b=3 → 7  → stack: [7]
		0x60, 0x02, // PUSH1 2   → stack: [7, 2]
		0x02,       // MUL: a=2, b=7 → 14  → stack: [14]
		0x80,       // DUP1       → stack: [14, 14]
		0x60, 0x02, // PUSH1 2   → stack: [14, 14, 2]
		0x04,       // DIV: a=2, b=14 → 0 ... still wrong
	}
	// Actually for EVM DIV: a / b where a=first pop, b=second pop.
	// To get 14/7: push 7 first, push 14 second → a=14, b=7 → 14/7=2.
	code = []byte{
		0x60, 0x03, // PUSH1 3
		0x60, 0x0A, // PUSH1 10
		0x03,       // SUB (10 - 3 = 7)
		0x60, 0x02, // PUSH1 2
		0x02,       // MUL (2 * 7 = 14)
		0x60, 0x07, // PUSH1 7  → stack: [14, 7]
		0x90,       // SWAP1    → stack: [7, 14]
		0x04,       // DIV: a=14, b=7 → 14/7=2
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("SUB/MUL/DIV error: %v", err)
	}
	if output[31] != 2 {
		t.Errorf("SUB/MUL/DIV: output[31] = %d, want 2", output[31])
	}
}

func TestInterpretEVM_CalldataLoad(t *testing.T) {
	// PUSH1 0, CALLDATALOAD, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x00, // PUSH1 0
		0x35,       // CALLDATALOAD
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	calldata := make([]byte, 32)
	calldata[31] = 0xFF

	output, _, err := InterpretEVM(code, calldata, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("CALLDATALOAD error: %v", err)
	}
	if output[31] != 0xFF {
		t.Errorf("CALLDATALOAD: output[31] = 0x%02x, want 0xFF", output[31])
	}
}

// --- Dispatcher Tests ---

func TestDispatcher_PreFork(t *testing.T) {
	d := NewEVMRISCVDispatcher()

	if d.IsStage3Active() {
		t.Error("expected Stage3 inactive by default")
	}

	// Simple PUSH1 7, PUSH1 3, ADD, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x07, // PUSH1 7
		0x60, 0x03, // PUSH1 3
		0x01,       // ADD
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := d.Execute(code, nil, nil, 100000)
	if err != nil {
		t.Fatalf("pre-fork execute error: %v", err)
	}
	if len(output) != 32 || output[31] != 10 {
		t.Errorf("pre-fork: output[31] = %d, want 10", output[31])
	}
}

func TestDispatcher_PostFork(t *testing.T) {
	d := NewEVMRISCVDispatcher()
	d.SetStage3Active(true)

	if !d.IsStage3Active() {
		t.Error("expected Stage3 active after SetStage3Active(true)")
	}

	code := []byte{
		0x60, 0x07, // PUSH1 7
		0x60, 0x03, // PUSH1 3
		0x01,       // ADD
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := d.Execute(code, nil, nil, 100000)
	if err != nil {
		t.Fatalf("post-fork execute error: %v", err)
	}
	if len(output) != 32 || output[31] != 10 {
		t.Errorf("post-fork: output[31] = %d, want 10", output[31])
	}
}

func TestDispatcher_SameResultBothModes(t *testing.T) {
	d := NewEVMRISCVDispatcher()

	code := []byte{
		0x60, 0x0A, // PUSH1 10
		0x60, 0x14, // PUSH1 20
		0x01,       // ADD
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	// Pre-fork
	output1, gas1, err := d.Execute(code, nil, nil, 100000)
	if err != nil {
		t.Fatalf("pre-fork error: %v", err)
	}

	// Post-fork
	d.SetStage3Active(true)
	output2, gas2, err := d.Execute(code, nil, nil, 100000)
	if err != nil {
		t.Fatalf("post-fork error: %v", err)
	}

	// Same output and gas.
	if len(output1) != len(output2) {
		t.Fatalf("output length mismatch: pre=%d, post=%d", len(output1), len(output2))
	}
	for i := range output1 {
		if output1[i] != output2[i] {
			t.Errorf("output[%d] mismatch: pre=0x%02x, post=0x%02x", i, output1[i], output2[i])
		}
	}
	if gas1 != gas2 {
		t.Errorf("gas mismatch: pre=%d, post=%d", gas1, gas2)
	}
}

// --- Transpiler Tests ---

func TestTranspiler_Deterministic(t *testing.T) {
	tr := NewEVMTranspiler()

	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x00} // PUSH1 1, PUSH1 2, ADD, STOP

	out1, err := tr.Transpile(code)
	if err != nil {
		t.Fatalf("first transpile error: %v", err)
	}

	out2, err := tr.Transpile(code)
	if err != nil {
		t.Fatalf("second transpile error: %v", err)
	}

	if len(out1) != len(out2) {
		t.Fatalf("non-deterministic: len1=%d, len2=%d", len(out1), len(out2))
	}
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Errorf("non-deterministic at byte %d: 0x%02x != 0x%02x", i, out1[i], out2[i])
		}
	}
}

func TestTranspiler_Cache(t *testing.T) {
	tr := NewEVMTranspiler()

	code := []byte{0x60, 0x01, 0x00} // PUSH1 1, STOP

	if tr.CacheSize() != 0 {
		t.Errorf("cache size = %d, want 0", tr.CacheSize())
	}

	_, err := tr.Transpile(code)
	if err != nil {
		t.Fatalf("transpile error: %v", err)
	}

	if tr.CacheSize() != 1 {
		t.Errorf("cache size = %d, want 1", tr.CacheSize())
	}

	// Second transpile should hit cache.
	_, err = tr.Transpile(code)
	if err != nil {
		t.Fatalf("cached transpile error: %v", err)
	}

	if tr.CacheSize() != 1 {
		t.Errorf("cache size after re-transpile = %d, want 1", tr.CacheSize())
	}
}

func TestTranspiler_Push1Add(t *testing.T) {
	tr := NewEVMTranspiler()

	// Transpile PUSH1 + ADD.
	push1Instrs, err := tr.TranspileOpcode(0x60)
	if err != nil {
		t.Fatalf("PUSH1 transpile error: %v", err)
	}
	if len(push1Instrs) != 3 {
		t.Errorf("PUSH1 instruction count = %d, want 3", len(push1Instrs))
	}

	addInstrs, err := tr.TranspileOpcode(0x01)
	if err != nil {
		t.Fatalf("ADD transpile error: %v", err)
	}
	if len(addInstrs) != 7 {
		t.Errorf("ADD instruction count = %d, want 7", len(addInstrs))
	}
}

func TestTranspiler_SloadSstore(t *testing.T) {
	tr := NewEVMTranspiler()

	sloadInstrs, err := tr.TranspileOpcode(0x54)
	if err != nil {
		t.Fatalf("SLOAD transpile error: %v", err)
	}
	if len(sloadInstrs) != 4 {
		t.Errorf("SLOAD instruction count = %d, want 4", len(sloadInstrs))
	}

	sstoreInstrs, err := tr.TranspileOpcode(0x55)
	if err != nil {
		t.Fatalf("SSTORE transpile error: %v", err)
	}
	if len(sstoreInstrs) != 6 {
		t.Errorf("SSTORE instruction count = %d, want 6", len(sstoreInstrs))
	}
}

func TestTranspiler_JumpJumpi(t *testing.T) {
	tr := NewEVMTranspiler()

	jumpInstrs, err := tr.TranspileOpcode(0x56)
	if err != nil {
		t.Fatalf("JUMP transpile error: %v", err)
	}
	if len(jumpInstrs) != 5 {
		t.Errorf("JUMP instruction count = %d, want 5", len(jumpInstrs))
	}

	jumpiInstrs, err := tr.TranspileOpcode(0x57)
	if err != nil {
		t.Fatalf("JUMPI transpile error: %v", err)
	}
	if len(jumpiInstrs) != 7 {
		t.Errorf("JUMPI instruction count = %d, want 7", len(jumpiInstrs))
	}
}

func TestTranspiler_InvalidOpcode(t *testing.T) {
	tr := NewEVMTranspiler()

	_, err := tr.TranspileOpcode(0xFE) // INVALID
	if err != ErrEVMInvalidOp {
		t.Errorf("expected ErrEVMInvalidOp, got %v", err)
	}
}

func TestTranspiler_AllSupportedOpcodes(t *testing.T) {
	tr := NewEVMTranspiler()

	supported := []byte{
		0x00, // STOP
		0x01, // ADD
		0x02, // MUL
		0x03, // SUB
		0x50, // POP
		0x51, // MLOAD
		0x52, // MSTORE
		0x54, // SLOAD
		0x55, // SSTORE
		0x56, // JUMP
		0x57, // JUMPI
		0x5B, // JUMPDEST
		0x60, // PUSH1
		0x80, // DUP1
		0xF3, // RETURN
	}

	for _, op := range supported {
		instrs, err := tr.TranspileOpcode(op)
		if err != nil {
			t.Errorf("opcode 0x%02X: unexpected error: %v", op, err)
		}
		if len(instrs) == 0 {
			t.Errorf("opcode 0x%02X: produced 0 instructions", op)
		}
	}
}

func TestTranspiler_OutputIsValidRV32IM(t *testing.T) {
	tr := NewEVMTranspiler()

	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x00}
	out, err := tr.Transpile(code)
	if err != nil {
		t.Fatalf("transpile error: %v", err)
	}

	// Output should be a multiple of 4.
	if len(out)%4 != 0 {
		t.Errorf("output length %d is not a multiple of 4", len(out))
	}

	// Each 4-byte word should decode as a valid RV32IM instruction
	// (at least the opcode field should be recognized).
	for i := 0; i+4 <= len(out); i += 4 {
		instr := uint32(out[i]) | uint32(out[i+1])<<8 | uint32(out[i+2])<<16 | uint32(out[i+3])<<24
		opcode := instr & 0x7F
		if !validRV32IOpcodes[byte(opcode)] {
			t.Errorf("instruction at offset %d: opcode 0x%02x is not a valid RV32IM opcode", i, opcode)
		}
	}
}

// --- BuildEVMInterpreterGuest Tests ---

func TestBuildEVMInterpreterGuest_RunsOnRVCPU(t *testing.T) {
	guest := BuildEVMInterpreterGuest()

	if len(guest)%4 != 0 {
		t.Fatalf("guest binary length %d not aligned to 4 bytes", len(guest))
	}

	// Run the guest on the RISC-V CPU with some input.
	cpu := zkvm.NewRVCPU(0) // no gas limit for guest test
	const programBase uint32 = 0x00010000
	const inputBase uint32 = 0x40000000

	input := []byte{0x60, 0x01, 0x00} // PUSH1 1, STOP (example EVM bytecode)
	if err := cpu.LoadProgram(guest, programBase, programBase); err != nil {
		t.Fatalf("load program error: %v", err)
	}
	if err := cpu.Memory.LoadSegment(inputBase, input); err != nil {
		t.Fatalf("load input error: %v", err)
	}

	cpu.Regs[2] = 0x80000000 // sp
	cpu.Regs[10] = inputBase
	cpu.Regs[11] = uint32(len(input))
	cpu.InputBuf = input

	if err := cpu.Run(); err != nil {
		t.Fatalf("cpu.Run error: %v", err)
	}

	// Should produce 1 byte of output (XOR hash).
	if len(cpu.OutputBuf) != 1 {
		t.Fatalf("output length = %d, want 1", len(cpu.OutputBuf))
	}

	expected := EVMGuestCommitment(input)
	if cpu.OutputBuf[0] != expected {
		t.Errorf("output = 0x%02x, want 0x%02x", cpu.OutputBuf[0], expected)
	}
}

func TestEVMGuestCommitment(t *testing.T) {
	tests := []struct {
		input []byte
		want  byte
	}{
		{[]byte{}, 0},
		{[]byte{0xFF}, 0xFF},
		{[]byte{0x60, 0x01}, 0x60 ^ 0x01},
		{[]byte{0xAA, 0x55}, 0xAA ^ 0x55},
	}

	for _, tc := range tests {
		got := EVMGuestCommitment(tc.input)
		if got != tc.want {
			t.Errorf("EVMGuestCommitment(%v) = 0x%02x, want 0x%02x", tc.input, got, tc.want)
		}
	}
}

func TestInterpretEVM_EqIszero(t *testing.T) {
	// PUSH1 5, PUSH1 5, EQ → 1, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 0x05, // PUSH1 5
		0x60, 0x05, // PUSH1 5
		0x14,       // EQ (5 == 5 → 1)
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("EQ error: %v", err)
	}
	if output[31] != 1 {
		t.Errorf("EQ: output[31] = %d, want 1", output[31])
	}
}

func TestInterpretEVM_Mod(t *testing.T) {
	// PUSH1 3, PUSH1 10, MOD → 1
	code := []byte{
		0x60, 0x03, // PUSH1 3
		0x60, 0x0A, // PUSH1 10
		0x06,       // MOD (10 % 3 = 1)
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("MOD error: %v", err)
	}
	if output[31] != 1 {
		t.Errorf("MOD: output[31] = %d, want 1", output[31])
	}
}

func TestInterpretEVM_AndOrNot(t *testing.T) {
	// PUSH1 0x0F, PUSH1 0xFF, AND → 0x0F
	code := []byte{
		0x60, 0x0F, // PUSH1 0x0F
		0x60, 0xFF, // PUSH1 0xFF
		0x16,       // AND (0xFF & 0x0F = 0x0F)
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xF3,       // RETURN
	}

	output, _, err := InterpretEVM(code, nil, nil, types.Address{}, 100000)
	if err != nil {
		t.Fatalf("AND error: %v", err)
	}
	if output[31] != 0x0F {
		t.Errorf("AND: output[31] = 0x%02x, want 0x0F", output[31])
	}
}
