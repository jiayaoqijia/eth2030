package vm

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/zkvm"
)

// mockCodeStore implements CodeStore for testing.
type mockCodeStore struct {
	codes map[types.Address][]byte
}

func newMockCodeStore() *mockCodeStore {
	return &mockCodeStore{codes: make(map[types.Address][]byte)}
}

func (m *mockCodeStore) SetCode(addr types.Address, code []byte) {
	m.codes[addr] = code
}

func (m *mockCodeStore) GetCode(addr types.Address) []byte {
	return m.codes[addr]
}

// mockStorageResolver implements StorageResolver for testing.
type mockStorageResolver struct {
	storage    map[types.Address]map[types.Hash]types.Hash
	balances   map[types.Address]*big.Int
	accessList map[types.Address]bool
	slotList   map[types.Address]map[types.Hash]bool
}

func newMockStorageResolver() *mockStorageResolver {
	return &mockStorageResolver{
		storage:    make(map[types.Address]map[types.Hash]types.Hash),
		balances:   make(map[types.Address]*big.Int),
		accessList: make(map[types.Address]bool),
		slotList:   make(map[types.Address]map[types.Hash]bool),
	}
}

func (m *mockStorageResolver) SLoad(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.storage[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *mockStorageResolver) SStore(addr types.Address, key, value types.Hash) {
	if _, ok := m.storage[addr]; !ok {
		m.storage[addr] = make(map[types.Hash]types.Hash)
	}
	m.storage[addr][key] = value
}

func (m *mockStorageResolver) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *mockStorageResolver) AddressInAccessList(addr types.Address) bool {
	return m.accessList[addr]
}

func (m *mockStorageResolver) AddAddressToAccessList(addr types.Address) {
	m.accessList[addr] = true
}

func (m *mockStorageResolver) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	addrOk := m.accessList[addr]
	if slots, ok := m.slotList[addr]; ok {
		return addrOk, slots[slot]
	}
	return addrOk, false
}

func (m *mockStorageResolver) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	m.accessList[addr] = true
	if _, ok := m.slotList[addr]; !ok {
		m.slotList[addr] = make(map[types.Hash]bool)
	}
	m.slotList[addr][slot] = true
}

// --- Code Type Detection Tests ---

func TestDetectCodeType_RISCV(t *testing.T) {
	// ADDI x1, x0, 42 -- opcode bits [6:0] = 0x13
	instr := zkvm.EncodeIType(0x13, 1, 0, 0, 42)
	code := make([]byte, 4)
	binary.LittleEndian.PutUint32(code, instr)

	ct := DetectCodeType(code)
	if ct != CodeTypeRISCV {
		t.Errorf("expected CodeTypeRISCV, got 0x%02x", ct)
	}
}

func TestDetectCodeType_EVM(t *testing.T) {
	// EVM bytecode: PUSH1 0x60 (0x60 & 0x7F = 0x60, not a valid RV32I opcode)
	code := []byte{0x60, 0x80, 0x60, 0x40}
	ct := DetectCodeType(code)
	if ct != CodeTypeEVM {
		t.Errorf("expected CodeTypeEVM, got 0x%02x", ct)
	}
}

func TestDetectCodeType_TooShort(t *testing.T) {
	ct := DetectCodeType([]byte{0x13, 0x00})
	if ct != CodeTypeEVM {
		t.Errorf("expected CodeTypeEVM for short code, got 0x%02x", ct)
	}
}

func TestDetectCodeType_AllValidOpcodes(t *testing.T) {
	validOpcodes := []byte{0x33, 0x13, 0x03, 0x23, 0x63, 0x67, 0x6F, 0x37, 0x17, 0x73}
	for _, op := range validOpcodes {
		code := []byte{op, 0x00, 0x00, 0x00}
		ct := DetectCodeType(code)
		if ct != CodeTypeRISCV {
			t.Errorf("opcode 0x%02x: expected CodeTypeRISCV, got 0x%02x", op, ct)
		}
	}
}

// --- Binary Validation Tests ---

func TestValidateRISCVBinary_Valid(t *testing.T) {
	code := make([]byte, 8) // Two instructions, aligned.
	if err := ValidateRISCVBinary(code); err != nil {
		t.Errorf("expected valid binary, got: %v", err)
	}
}

func TestValidateRISCVBinary_Empty(t *testing.T) {
	err := ValidateRISCVBinary(nil)
	if err != ErrRISCVEmptyBinary {
		t.Errorf("expected ErrRISCVEmptyBinary, got: %v", err)
	}
}

func TestValidateRISCVBinary_Unaligned(t *testing.T) {
	code := make([]byte, 5) // Not a multiple of 4.
	err := ValidateRISCVBinary(code)
	if err != ErrRISCVUnaligned {
		t.Errorf("expected ErrRISCVUnaligned, got: %v", err)
	}
}

func TestValidateRISCVBinary_TooLarge(t *testing.T) {
	code := make([]byte, MaxRISCVBinarySize+4)
	err := ValidateRISCVBinary(code)
	if err != ErrRISCVBinaryTooLarge {
		t.Errorf("expected ErrRISCVBinaryTooLarge, got: %v", err)
	}
}

// --- Deployer Tests ---

func TestRISCVContractDeployer_Deploy(t *testing.T) {
	store := newMockCodeStore()
	deployer := NewRISCVContractDeployer(store)

	// Build a trivial RISC-V program: ADDI x0, x0, 0 (NOP) + ECALL (halt).
	code := buildTrivialRISCVProgram()
	addr := types.BytesToAddress([]byte{0xAA})

	if err := deployer.Deploy(code, addr); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	// Verify stored code has the prefix.
	stored := store.GetCode(addr)
	if len(stored) == 0 {
		t.Fatal("expected stored code")
	}
	if stored[0] != CodeTypeRISCV {
		t.Errorf("expected CodeTypeRISCV prefix, got 0x%02x", stored[0])
	}
	if !IsRISCVContract(stored) {
		t.Error("IsRISCVContract should return true")
	}

	// Verify the binary can be extracted.
	binary := GetRISCVBinary(stored)
	if len(binary) != len(code) {
		t.Errorf("expected binary length %d, got %d", len(code), len(binary))
	}
}

func TestRISCVContractDeployer_RejectInvalid(t *testing.T) {
	store := newMockCodeStore()
	deployer := NewRISCVContractDeployer(store)
	addr := types.BytesToAddress([]byte{0xBB})

	// Empty binary.
	if err := deployer.Deploy(nil, addr); err != ErrRISCVEmptyBinary {
		t.Errorf("expected ErrRISCVEmptyBinary, got: %v", err)
	}

	// Unaligned binary.
	if err := deployer.Deploy([]byte{1, 2, 3}, addr); err != ErrRISCVUnaligned {
		t.Errorf("expected ErrRISCVUnaligned, got: %v", err)
	}
}

func TestIsRISCVContract(t *testing.T) {
	if IsRISCVContract(nil) {
		t.Error("nil should not be RISC-V")
	}
	if IsRISCVContract([]byte{CodeTypeEVM, 0x60}) {
		t.Error("EVM code should not be RISC-V")
	}
	if !IsRISCVContract([]byte{CodeTypeRISCV, 0x13}) {
		t.Error("RISC-V prefixed code should be detected")
	}
}

// --- Executor Tests ---

func TestRISCVExecutor_TrivialProgram(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0x01})
	target := types.BytesToAddress([]byte{0x02})

	exec := NewRISCVExecutor(resolver, caller, target, nil, 100000)
	code := buildHaltProgram(0) // Halt with exit code 0.

	output, gasUsed, err := exec.Execute(code, nil)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if gasUsed == 0 {
		t.Error("expected non-zero gas used")
	}
	_ = output
}

func TestRISCVExecutor_SstoreSload(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0x01})
	target := types.BytesToAddress([]byte{0x02})

	// Pre-store a value to test SLOAD.
	key := types.BytesToHash([]byte{0x42})
	val := types.BytesToHash([]byte{0xDE, 0xAD})
	resolver.SStore(target, key, val)

	exec := NewRISCVExecutor(resolver, caller, target, nil, 100000)

	// Build a program that does SLOAD via ECALL.
	code := buildSloadProgram(0x40000000) // key loaded from input area

	// Put the key in calldata.
	calldata := key[:]

	output, gasUsed, err := exec.Execute(code, calldata)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	_ = output
	if gasUsed == 0 {
		t.Error("expected non-zero gas used")
	}
}

func TestRISCVExecutor_SstoreSloadRoundTrip(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0x01})
	target := types.BytesToAddress([]byte{0x02})

	// Write a value via SStore, then read it back.
	key := types.BytesToHash([]byte{0x01})
	val := types.BytesToHash([]byte{0xBE, 0xEF})

	// Store directly and verify through resolver.
	resolver.SStore(target, key, val)
	got := resolver.SLoad(target, key)
	if got != val {
		t.Fatalf("resolver round-trip failed: expected %x, got %x", val, got)
	}

	// Now test SSTORE via executor ecall handler directly.
	exec := NewRISCVExecutor(resolver, caller, target, nil, 100000)
	cpu := zkvm.NewRVCPU(0)
	cpu.Memory = zkvm.NewRVMemory()

	// Write key to memory at 0x1000.
	key2 := types.BytesToHash([]byte{0x99})
	val2 := types.BytesToHash([]byte{0xAA, 0xBB})
	writeHashToMemory(cpu, 0x1000, key2)
	writeHashToMemory(cpu, 0x1020, val2)

	cpu.Regs[10] = 0x1000 // a0 = key address
	cpu.Regs[11] = 0x1020 // a1 = value address
	cpu.Regs[17] = RVSyscallSstore

	gasCost, err := exec.handleStorageEcall(cpu, RVSyscallSstore)
	if err != nil {
		t.Fatalf("SSTORE ecall failed: %v", err)
	}
	if gasCost != GasSstoreNew {
		t.Errorf("expected gas cost %d, got %d", GasSstoreNew, gasCost)
	}

	// Now SLOAD it back.
	// Re-write key to memory (ecallSload overwrites keyAddr with the value).
	writeHashToMemory(cpu, 0x1000, key2)
	cpu.Regs[10] = 0x1000
	cpu.Regs[17] = RVSyscallSload
	exec.gasUsed = 0 // Reset for this test.

	gasCost, err = exec.handleStorageEcall(cpu, RVSyscallSload)
	if err != nil {
		t.Fatalf("SLOAD ecall failed: %v", err)
	}
	// Should be cold since we haven't accessed this slot yet.
	if gasCost != GasSloadCold {
		t.Errorf("expected cold SLOAD gas %d, got %d", GasSloadCold, gasCost)
	}

	// Second SLOAD should be warm (slot already in access list).
	writeHashToMemory(cpu, 0x1000, key2)
	cpu.Regs[10] = 0x1000
	exec.gasUsed = 0
	gasCost, err = exec.handleStorageEcall(cpu, RVSyscallSload)
	if err != nil {
		t.Fatalf("warm SLOAD ecall failed: %v", err)
	}
	if gasCost != GasSloadWarm {
		t.Errorf("expected warm SLOAD gas %d, got %d", GasSloadWarm, gasCost)
	}
}

func TestRISCVExecutor_Caller(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0xCA, 0xFE, 0xBA, 0xBE})
	target := types.BytesToAddress([]byte{0x02})

	exec := NewRISCVExecutor(resolver, caller, target, nil, 100000)
	cpu := zkvm.NewRVCPU(0)
	cpu.Regs[17] = RVSyscallCaller

	gasCost, err := exec.handleStorageEcall(cpu, RVSyscallCaller)
	if err != nil {
		t.Fatalf("CALLER ecall failed: %v", err)
	}
	if gasCost != 0 {
		t.Errorf("expected 0 gas for CALLER, got %d", gasCost)
	}

	// Low 4 bytes of caller address (big-endian).
	expected := binary.BigEndian.Uint32(caller[16:20])
	if cpu.Regs[10] != expected {
		t.Errorf("CALLER: expected 0x%08x, got 0x%08x", expected, cpu.Regs[10])
	}
}

func TestRISCVExecutor_Callvalue(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0x01})
	target := types.BytesToAddress([]byte{0x02})
	value := big.NewInt(12345)

	exec := NewRISCVExecutor(resolver, caller, target, value, 100000)
	cpu := zkvm.NewRVCPU(0)
	cpu.Regs[17] = RVSyscallCallvalue

	gasCost, err := exec.handleStorageEcall(cpu, RVSyscallCallvalue)
	if err != nil {
		t.Fatalf("CALLVALUE ecall failed: %v", err)
	}
	if gasCost != 0 {
		t.Errorf("expected 0 gas for CALLVALUE, got %d", gasCost)
	}
	if cpu.Regs[10] != 12345 {
		t.Errorf("CALLVALUE: expected 12345, got %d", cpu.Regs[10])
	}
}

func TestRISCVExecutor_Balance(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0x01})
	target := types.BytesToAddress([]byte{0x02})
	resolver.balances[target] = big.NewInt(99999)

	exec := NewRISCVExecutor(resolver, caller, target, nil, 100000)
	cpu := zkvm.NewRVCPU(0)
	cpu.Regs[17] = RVSyscallBalance

	gasCost, err := exec.handleStorageEcall(cpu, RVSyscallBalance)
	if err != nil {
		t.Fatalf("BALANCE ecall failed: %v", err)
	}
	if gasCost != GasBalanceCold {
		t.Errorf("expected cold balance gas %d, got %d", GasBalanceCold, gasCost)
	}
	if cpu.Regs[10] != 99999 {
		t.Errorf("BALANCE: expected 99999, got %d", cpu.Regs[10])
	}

	// Second call should be warm.
	exec.gasUsed = 0
	gasCost, err = exec.handleStorageEcall(cpu, RVSyscallBalance)
	if err != nil {
		t.Fatalf("warm BALANCE ecall failed: %v", err)
	}
	if gasCost != GasBalanceWarm {
		t.Errorf("expected warm balance gas %d, got %d", GasBalanceWarm, gasCost)
	}
}

func TestRISCVExecutor_GasExhaustion(t *testing.T) {
	resolver := newMockStorageResolver()
	caller := types.BytesToAddress([]byte{0x01})
	target := types.BytesToAddress([]byte{0x02})

	// Give only 1 gas -- not enough to run the full program.
	exec := NewRISCVExecutor(resolver, caller, target, nil, 1)
	code := buildHaltProgram(0)

	_, _, err := exec.Execute(code, nil)
	if err != ErrRVExecGasExhausted {
		t.Errorf("expected ErrRVExecGasExhausted, got: %v", err)
	}
}

// --- Gas Table Tests ---

func TestRISCVGasCost_Arithmetic(t *testing.T) {
	// ADD x3, x1, x2 (R-type, funct7=0)
	instr := zkvm.EncodeRType(0x33, 3, 0, 1, 2, 0)
	if cost := RISCVGasCost(instr); cost != 1 {
		t.Errorf("ADD gas: expected 1, got %d", cost)
	}
}

func TestRISCVGasCost_Multiply(t *testing.T) {
	// MUL x3, x1, x2 (R-type, funct7=1, funct3=0)
	instr := zkvm.EncodeRType(0x33, 3, 0, 1, 2, 1)
	if cost := RISCVGasCost(instr); cost != 3 {
		t.Errorf("MUL gas: expected 3, got %d", cost)
	}
}

func TestRISCVGasCost_Divide(t *testing.T) {
	// DIV x3, x1, x2 (R-type, funct7=1, funct3=4)
	instr := zkvm.EncodeRType(0x33, 3, 4, 1, 2, 1)
	if cost := RISCVGasCost(instr); cost != 5 {
		t.Errorf("DIV gas: expected 5, got %d", cost)
	}
}

func TestRISCVGasCost_Memory(t *testing.T) {
	// LW x1, 0(x2) (Load word)
	instr := zkvm.EncodeIType(0x03, 1, 2, 2, 0)
	if cost := RISCVGasCost(instr); cost != 3 {
		t.Errorf("LW gas: expected 3, got %d", cost)
	}

	// SW x1, 0(x2) (Store word)
	sinstr := zkvm.EncodeSType(0x23, 2, 2, 1, 0)
	if cost := RISCVGasCost(sinstr); cost != 3 {
		t.Errorf("SW gas: expected 3, got %d", cost)
	}
}

func TestRISCVGasCost_Branch(t *testing.T) {
	// BEQ x1, x2, 0
	instr := zkvm.EncodeBType(0x63, 0, 1, 2, 0)
	if cost := RISCVGasCost(instr); cost != 2 {
		t.Errorf("BEQ gas: expected 2, got %d", cost)
	}
}

func TestRISCVGasCost_Jump(t *testing.T) {
	// JAL x1, 0
	instr := zkvm.EncodeJType(0x6F, 1, 0)
	if cost := RISCVGasCost(instr); cost != 2 {
		t.Errorf("JAL gas: expected 2, got %d", cost)
	}

	// JALR x1, x2, 0
	instr2 := zkvm.EncodeIType(0x67, 1, 0, 2, 0)
	if cost := RISCVGasCost(instr2); cost != 2 {
		t.Errorf("JALR gas: expected 2, got %d", cost)
	}
}

func TestRISCVGasCost_Immediate(t *testing.T) {
	// LUI x1, 0x12345
	instr := zkvm.EncodeUType(0x37, 1, 0x12345000)
	if cost := RISCVGasCost(instr); cost != 1 {
		t.Errorf("LUI gas: expected 1, got %d", cost)
	}

	// AUIPC x1, 0x12345
	instr2 := zkvm.EncodeUType(0x17, 1, 0x12345000)
	if cost := RISCVGasCost(instr2); cost != 1 {
		t.Errorf("AUIPC gas: expected 1, got %d", cost)
	}
}

func TestRISCVGasCost_System(t *testing.T) {
	// ECALL
	instr := zkvm.EncodeIType(0x73, 0, 0, 0, 0)
	if cost := RISCVGasCost(instr); cost != 0 {
		t.Errorf("ECALL gas: expected 0, got %d", cost)
	}
}

func TestCalibrateRISCVGas(t *testing.T) {
	// If EVM takes 10 ops for 1000 gas and RISC-V takes 50 ops,
	// RISC-V should get 5000 gas.
	result := CalibrateRISCVGas(10, 50, 1000)
	if result != 5000 {
		t.Errorf("expected 5000, got %d", result)
	}

	// Edge case: zero evmOps should return evmGas unchanged.
	result = CalibrateRISCVGas(0, 50, 1000)
	if result != 1000 {
		t.Errorf("expected 1000, got %d", result)
	}

	// Equal ops: same gas.
	result = CalibrateRISCVGas(10, 10, 1000)
	if result != 1000 {
		t.Errorf("expected 1000, got %d", result)
	}
}

// --- Test Helpers ---

// buildTrivialRISCVProgram builds a trivial program that halts immediately.
func buildTrivialRISCVProgram() []byte {
	return buildHaltProgram(0)
}

// buildHaltProgram builds a RISC-V program: set a0=exitCode, a7=0 (halt), ECALL.
func buildHaltProgram(exitCode int32) []byte {
	instrs := []uint32{
		zkvm.EncodeIType(0x13, 10, 0, 0, exitCode), // ADDI a0, x0, exitCode
		zkvm.EncodeIType(0x13, 17, 0, 0, 0),        // ADDI a7, x0, 0 (halt)
		zkvm.EncodeIType(0x73, 0, 0, 0, 0),         // ECALL
	}
	return encodeInstructions(instrs)
}

// buildSloadProgram builds a program that loads a key from memory at keyAddr
// and calls SLOAD via ECALL.
func buildSloadProgram(keyAddr uint32) []byte {
	// a0 = keyAddr (from input, already in a0 register set by executor)
	// a7 = 3 (SLOAD syscall)
	// ECALL
	// a7 = 0, ECALL (halt)
	instrs := []uint32{
		zkvm.EncodeIType(0x13, 17, 0, 0, 3),        // ADDI a7, x0, 3 (SLOAD)
		zkvm.EncodeIType(0x73, 0, 0, 0, 0),         // ECALL
		zkvm.EncodeIType(0x13, 17, 0, 0, 0),        // ADDI a7, x0, 0 (halt)
		zkvm.EncodeIType(0x13, 10, 0, 0, 0),        // ADDI a0, x0, 0 (exit code 0)
		zkvm.EncodeIType(0x73, 0, 0, 0, 0),         // ECALL
	}
	return encodeInstructions(instrs)
}

// encodeInstructions converts a slice of instruction words to little-endian bytes.
func encodeInstructions(instrs []uint32) []byte {
	buf := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(buf[i*4:], instr)
	}
	return buf
}

// writeHashToMemory writes a 32-byte hash to CPU memory.
func writeHashToMemory(cpu *zkvm.RVCPU, addr uint32, h types.Hash) {
	for i := uint32(0); i < 32; i++ {
		_ = cpu.Memory.WriteByteAt(addr+i, h[i])
	}
}
