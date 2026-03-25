package vm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/zkvm"
)

var (
	ErrOutOfGas                = errors.New("out of gas")
	ErrStackOverflow           = errors.New("stack overflow")
	ErrStackUnderflow          = errors.New("stack underflow")
	ErrInvalidJump             = errors.New("invalid jump destination")
	ErrWriteProtection         = errors.New("write protection")
	ErrExecutionReverted       = errors.New("execution reverted")
	ErrMaxCallDepthExceeded    = errors.New("max call depth exceeded")
	ErrInvalidOpCode           = errors.New("invalid opcode")
	ErrReturnDataOutOfBounds   = errors.New("return data out of bounds")
	ErrMaxInitCodeSizeExceeded = errors.New("max initcode size exceeded")
)

// GetHashFunc returns the hash of the block with the given number.
type GetHashFunc func(uint64) types.Hash

// BlockContext provides the EVM with block-level information.
type BlockContext struct {
	GetHash     GetHashFunc
	BlockNumber *big.Int
	Time        uint64
	Coinbase    types.Address
	GasLimit    uint64
	BaseFee     *big.Int
	PrevRandao  types.Hash
	BlobBaseFee *big.Int
	SlotNumber  uint64 // EIP-7843: beacon chain slot number
}

// TxContext provides the EVM with transaction-level information.
type TxContext struct {
	Origin     types.Address
	GasPrice   *big.Int
	BlobHashes []types.Hash
}

// StateDB provides the EVM with access to Ethereum world state.
// This interface is defined in the vm package to avoid circular imports
// with core/state. Any implementation of core/state.StateDB satisfies it.
type StateDB interface {
	// Account operations
	CreateAccount(addr types.Address)
	GetBalance(addr types.Address) *big.Int
	AddBalance(addr types.Address, amount *big.Int)
	SubBalance(addr types.Address, amount *big.Int)
	GetNonce(addr types.Address) uint64
	SetNonce(addr types.Address, nonce uint64)
	GetCode(addr types.Address) []byte
	SetCode(addr types.Address, code []byte)
	GetCodeHash(addr types.Address) types.Hash

	GetCodeSize(addr types.Address) int

	// Storage
	GetState(addr types.Address, key types.Hash) types.Hash
	SetState(addr types.Address, key types.Hash, value types.Hash)
	GetCommittedState(addr types.Address, key types.Hash) types.Hash

	// Transient storage (EIP-1153)
	GetTransientState(addr types.Address, key types.Hash) types.Hash
	SetTransientState(addr types.Address, key types.Hash, value types.Hash)
	ClearTransientStorage()

	// Self-destruct
	SelfDestruct(addr types.Address)
	HasSelfDestructed(addr types.Address) bool

	// Account existence
	Exist(addr types.Address) bool
	Empty(addr types.Address) bool

	// Snapshot and revert
	Snapshot() int
	RevertToSnapshot(id int)

	// Logs
	AddLog(log *types.Log)

	// Refund counter (EIP-3529)
	AddRefund(gas uint64)
	SubRefund(gas uint64)
	GetRefund() uint64

	// Access list (EIP-2929 warm/cold tracking)
	AddAddressToAccessList(addr types.Address)
	AddSlotToAccessList(addr types.Address, slot types.Hash)
	AddressInAccessList(addr types.Address) bool
	SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool)
}

// Config holds EVM configuration options.
type Config struct {
	Debug        bool
	Tracer       EVMLogger
	MaxCallDepth int
}

// EVM is the Ethereum Virtual Machine execution environment.
type EVM struct {
	Context              BlockContext
	TxContext            TxContext
	Config               Config
	StateDB              StateDB
	chainID              uint64
	depth                int
	readOnly             bool
	jumpTable            JumpTable
	precompiles          map[types.Address]PrecompiledContract
	returnData           []byte              // return data from the last CALL/CREATE
	callGasTemp          uint64              // temporary storage for CALL gas (set by dynamic gas, read by opCall)
	callReservoirForward uint64              // reservoir forwarded to child call (set by opCall, read by evm.Call)
	callReservoirReturn  uint64              // reservoir returned from child call (set by evm.Call, read by opCall)
	dimGasUsage          *TxDimGasUsage      // per-dimension gas tracking (GAP-2.1, nil if not active)
	witnessGas           *WitnessGasTracker  // EIP-4762: witness gas tracking (nil if not Verkle)
	balTracker           BALTracker          // EIP-7928: BAL state access tracking (nil if not active)
	txIndex              uint64              // EIP-7928: current transaction index for BAL
	forkRules            ForkRules           // active fork rules for this block
	FrameCtx             *FrameContext       // EIP-8141: frame transaction approval context (nil if not frame tx)
	guestRegistry        *zkvm.GuestRegistry // EL-3: RISC-V guest registry for RVCREATE (nil if not I+)
}

// NewEVM creates a new EVM instance.
func NewEVM(blockCtx BlockContext, txCtx TxContext, config Config) *EVM {
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 1024
	}
	return &EVM{
		Context:   blockCtx,
		TxContext: txCtx,
		Config:    config,
		jumpTable: NewCancunJumpTable(),
	}
}

// NewEVMWithState creates a new EVM instance with state access.
func NewEVMWithState(blockCtx BlockContext, txCtx TxContext, config Config, stateDB StateDB) *EVM {
	evm := NewEVM(blockCtx, txCtx, config)
	evm.StateDB = stateDB
	return evm
}

// SetJumpTable replaces the EVM's jump table. Use SelectJumpTable to pick
// the correct table for a given fork.
func (evm *EVM) SetJumpTable(jt JumpTable) {
	evm.jumpTable = jt
}

// SetPrecompiles replaces the EVM's precompile map.
func (evm *EVM) SetPrecompiles(p map[types.Address]PrecompiledContract) {
	evm.precompiles = p
}

// SetGuestRegistry sets the RISC-V guest registry used by RVCREATE (EL-3).
// Pass nil to disable RISC-V contract routing.
func (evm *EVM) SetGuestRegistry(r *zkvm.GuestRegistry) {
	evm.guestRegistry = r
}

// SetForkRules sets the active fork rules for this EVM instance.
func (evm *EVM) SetForkRules(rules ForkRules) {
	evm.forkRules = rules
}

// GetForkRules returns the active fork rules.
func (evm *EVM) GetForkRules() ForkRules {
	return evm.forkRules
}

// SetDimGasUsage attaches per-dimension gas usage tracking to this EVM.
// When non-nil, gasSstoreGlamst routes SSTORE state-creation premiums to
// DimStorage and records all SSTORE ops in the tracker (GAP-2.1).
func (evm *EVM) SetDimGasUsage(u *TxDimGasUsage) {
	evm.dimGasUsage = u
}

// DimGasUsage returns the per-dimension gas tracker, or nil if not set.
func (evm *EVM) DimGasUsage() *TxDimGasUsage {
	return evm.dimGasUsage
}

// SetInitialReservoir seeds the state-creation gas reservoir forwarded to
// the first contract call. Call this after NewEVM before evm.Call/Create
// so the top-level contract receives the reservoir (GAP-1.2/1.3).
func (evm *EVM) SetInitialReservoir(reservoir uint64) {
	evm.callReservoirForward = reservoir
}

// SetWitnessGasTracker enables EIP-4762 witness gas tracking. When set, the
// Verkle jump table charges gas based on witness size for state accesses.
func (evm *EVM) SetWitnessGasTracker(t *WitnessGasTracker) {
	evm.witnessGas = t
}

// GetWitnessGasTracker returns the current witness gas tracker (may be nil).
func (evm *EVM) GetWitnessGasTracker() *WitnessGasTracker {
	return evm.witnessGas
}

// SetBALTracker enables EIP-7928 Block Access List tracking. When set,
// EVM opcodes that access state will record accesses to the tracker.
func (evm *EVM) SetBALTracker(t BALTracker, txIdx uint64) {
	evm.balTracker = t
	evm.txIndex = txIdx
}

// GetBALTracker returns the current BAL tracker (may be nil).
func (evm *EVM) GetBALTracker() BALTracker {
	return evm.balTracker
}

// precompile returns the precompiled contract at addr, falling back to the
// default Cancun precompile set if no custom map has been set.
func (evm *EVM) precompile(addr types.Address) (PrecompiledContract, bool) {
	m := evm.precompiles
	if m == nil {
		m = PrecompiledContractsCancun
	}
	p, ok := m[addr]
	return p, ok
}

// runPrecompile executes a precompiled contract and returns the output,
// remaining gas, and any error.
func runPrecompile(p PrecompiledContract, input []byte, gas uint64) ([]byte, uint64, error) {
	gasCost := p.RequiredGas(input)
	if gas < gasCost {
		return nil, 0, ErrOutOfGas
	}
	output, err := p.Run(input)
	return output, gas - gasCost, err
}

// ForkRules mirrors the chain configuration fork flags needed to select
// the correct jump table. The caller (processor) converts ChainConfig.Rules
// into this struct to avoid a circular import.
type ForkRules struct {
	IsVerkle         bool // EIP-4762: statelessness gas cost changes
	IsGlamsterdan    bool
	IsPrague         bool
	IsCancun         bool
	IsShanghai       bool
	IsMerge          bool
	IsLondon         bool
	IsBerlin         bool
	IsIstanbul       bool
	IsConstantinople bool
	IsByzantium      bool
	IsHomestead      bool
	IsEIP158         bool // EIP-158: empty account cleanup
	IsEIP7708        bool // EIP-7708: ETH transfers emit a log
	IsEIP7954        bool // EIP-7954: increased max contract code size
	IsIPlus          bool // I+ fork: NTT/NII precompiles (EIP-7885)
}

// SelectPrecompiles returns the correct precompile map for the given fork rules.
func SelectPrecompiles(rules ForkRules) map[types.Address]PrecompiledContract {
	if rules.IsIPlus {
		return PrecompiledContractsIPlus
	}
	if rules.IsGlamsterdan {
		return PrecompiledContractsGlamsterdan
	}
	return PrecompiledContractsCancun
}

// SelectJumpTable returns the correct jump table for the given fork rules.
func SelectJumpTable(rules ForkRules) JumpTable {
	switch {
	case rules.IsIPlus:
		return NewIPlusJumpTable()
	case rules.IsVerkle:
		return NewVerkleJumpTable()
	case rules.IsGlamsterdan:
		return NewGlamsterdanJumpTable()
	case rules.IsPrague:
		return NewPragueJumpTable()
	case rules.IsCancun:
		return NewCancunJumpTable()
	case rules.IsShanghai:
		return NewShanghaiJumpTable()
	case rules.IsMerge:
		return NewMergeJumpTable()
	case rules.IsLondon:
		return NewLondonJumpTable()
	case rules.IsBerlin:
		return NewBerlinJumpTable()
	case rules.IsIstanbul:
		return NewIstanbulJumpTable()
	case rules.IsConstantinople:
		return NewConstantinopleJumpTable()
	case rules.IsByzantium:
		return NewByzantiumJumpTable()
	case rules.IsHomestead:
		return NewHomesteadJumpTable()
	default:
		return NewFrontierJumpTable()
	}
}

// Run executes the contract bytecode using the interpreter loop.
// Gas charging order follows go-ethereum: constant gas -> dynamic gas
// (which includes memory expansion cost) -> resize memory -> execute.
func (evm *EVM) Run(contract *Contract, input []byte) ([]byte, error) {
	contract.Input = input

	var (
		pc    uint64
		stack = NewStack()
		mem   = NewMemory()
		debug = evm.Config.Debug && evm.Config.Tracer != nil
	)

	for {
		op := contract.GetOp(pc)
		operation := evm.jumpTable[op]
		if operation == nil || operation.execute == nil {
			return nil, ErrInvalidOpCode
		}

		// Stack validation
		sLen := stack.Len()
		if sLen < operation.minStack {
			return nil, ErrStackUnderflow
		}
		if sLen > operation.maxStack {
			return nil, ErrStackOverflow
		}

		// Calculate total gas cost for this step (for tracing).
		var stepCost uint64
		gasBefore := contract.Gas

		// Constant gas deduction
		if operation.constantGas > 0 {
			if !contract.UseGas(operation.constantGas) {
				return nil, ErrOutOfGas
			}
		}

		// Calculate required memory size (but don't resize yet).
		var memorySize uint64
		if operation.memorySize != nil {
			memSize, overflow := operation.memorySize(stack)
			if overflow {
				return nil, ErrOutOfGas
			}
			// Align to 32-byte words.
			if memSize > 0 {
				memorySize = (memSize + 31) / 32 * 32
			}
		}

		// Dynamic gas: includes memory expansion cost + operation-specific costs.
		// This is charged BEFORE memory is actually resized.
		if operation.dynamicGas != nil {
			cost, err := operation.dynamicGas(evm, contract, stack, mem, memorySize)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrOutOfGas, err)
			}
			if !contract.UseGas(cost) {
				return nil, ErrOutOfGas
			}
		}

		// Resize memory AFTER gas has been charged (by dynamic gas function).
		if memorySize > 0 && uint64(mem.Len()) < memorySize {
			mem.Resize(memorySize)
		}

		// Compute the total cost for this step (difference before/after gas charging).
		stepCost = gasBefore - contract.Gas

		// Trace: capture state before executing the opcode.
		if debug {
			evm.Config.Tracer.CaptureState(pc, op, gasBefore, stepCost, stack, mem, evm.depth, nil)
		}

		// Execute the opcode
		ret, err := operation.execute(&pc, evm, contract, mem, stack)

		if err != nil {
			if errors.Is(err, ErrExecutionReverted) {
				return ret, err
			}
			return nil, err
		}

		// Handle halting opcodes
		if operation.halts {
			return ret, nil
		}
		if operation.jumps {
			continue
		}

		pc++
	}
}

// Call executes a message call to the given address with the given input, gas, and value.
func (evm *EVM) Call(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	debug := evm.Config.Debug && evm.Config.Tracer != nil

	// Notify tracer at the top-level call (depth 0).
	if debug && evm.depth == 0 {
		evm.Config.Tracer.CaptureStart(caller, addr, false, input, gas, value)
	}

	// Check if the callee has sufficient balance for value transfer.
	transfersValue := value != nil && value.Sign() > 0
	if transfersValue && evm.StateDB != nil {
		callerBalance := evm.StateDB.GetBalance(caller)
		if callerBalance.Cmp(value) < 0 {
			if debug && evm.depth == 0 {
				evm.Config.Tracer.CaptureEnd(nil, 0, errors.New("insufficient balance for transfer"))
			}
			return nil, gas, errors.New("insufficient balance for transfer")
		}
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	// Snapshot state for revert on failure.
	snapshot := evm.StateDB.Snapshot()

	// Check for precompiled contract.
	p, isPrecompile := evm.precompile(addr)

	// Handle account creation / EIP-158 empty account rule.
	if !evm.StateDB.Exist(addr) {
		if !isPrecompile && evm.forkRules.IsEIP158 && !transfersValue {
			// EIP-158: do not create empty accounts for zero-value calls.
			if debug && evm.depth == 0 {
				evm.Config.Tracer.CaptureEnd(nil, 0, nil)
			}
			return nil, gas, nil
		}
		evm.StateDB.CreateAccount(addr)
	}

	// Transfer value (before running precompile or code).
	if transfersValue {
		if evm.readOnly {
			return nil, gas, ErrWriteProtection
		}
		// EIP-7928: capture pre-transfer balances for BAL tracking.
		var callerPreBal, addrPreBal *big.Int
		if evm.balTracker != nil {
			callerPreBal = new(big.Int).Set(evm.StateDB.GetBalance(caller))
			addrPreBal = new(big.Int).Set(evm.StateDB.GetBalance(addr))
		}
		evm.StateDB.SubBalance(caller, value)
		evm.StateDB.AddBalance(addr, value)
		// EIP-7928: record balance changes for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordBalanceChange(caller, callerPreBal, evm.StateDB.GetBalance(caller))
			if caller != addr {
				evm.balTracker.RecordBalanceChange(addr, addrPreBal, evm.StateDB.GetBalance(addr))
			}
		}

		// EIP-7708: emit transfer log for nonzero-value CALL to a different account.
		if evm.forkRules.IsEIP7708 && caller != addr {
			EmitTransferLog(evm.StateDB, caller, addr, value)
		}
	}

	// Execute precompile or contract code.
	if isPrecompile {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		if err != nil {
			evm.StateDB.RevertToSnapshot(snapshot)
		}
		if debug && evm.depth == 0 {
			evm.Config.Tracer.CaptureEnd(ret, gas-gasLeft, err)
		}
		return ret, gasLeft, err
	}

	// Get the code to execute.
	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		// No code to execute, the call succeeds with no return data.
		if debug && evm.depth == 0 {
			evm.Config.Tracer.CaptureEnd(nil, 0, nil)
		}
		return nil, gas, nil
	}

	// EIP-7702: Handle delegation designator (0xef0100 || address).
	// When an account has delegated its code, we execute the delegated contract's code
	// but keep the contract.Address as the original target (the delegator).
	execAddr := addr // Address for contract creation (may be updated for delegation)
	if delegatedAddr, ok := types.IsDelegated(code); ok {
		delegatedCode := evm.StateDB.GetCode(delegatedAddr)
		if len(delegatedCode) > 0 {
			code = delegatedCode
			// Note: We keep execAddr as the original target (delegator).
			// This ensures ADDRESS opcode returns the delegator's address,
			// not the delegated contract's address.
		}
	}

	// EL-3.3: if code starts with RISC-V magic bytes and I+ fork is active,
	// route execution through the RISC-V CPU emulator instead of the EVM.
	if evm.forkRules.IsIPlus && IsRVCode(code) {
		ret, gasLeft, rvErr := runRVContract(code, input, gas)
		if rvErr != nil {
			evm.StateDB.RevertToSnapshot(snapshot)
		}
		if debug && evm.depth == 0 {
			evm.Config.Tracer.CaptureEnd(ret, gas-gasLeft, rvErr)
		}
		return ret, gasLeft, rvErr
	}

	// Create the contract for execution.
	contract := NewContract(caller, execAddr, value, gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	// GAP-1.2: forward parent's state-creation reservoir to child (full amount, not 63/64).
	childReservoir := evm.callReservoirForward
	evm.callReservoirForward = 0
	contract.StateGasReservoir = childReservoir

	// Execute.
	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas
	// GAP-1.2: return unused child reservoir to caller via callReservoirReturn.
	evm.callReservoirReturn = contract.StateGasReservoir
	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.callReservoirReturn = 0
	}

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		// On error (not revert), revert state changes and consume all gas.
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		// On revert, revert state changes but return remaining gas.
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	if debug && evm.depth == 0 {
		evm.Config.Tracer.CaptureEnd(ret, gas-gasLeft, err)
	}

	return ret, gasLeft, err
}

// CallCode executes a CALLCODE operation. Runs the callee's code in the caller's context.
func (evm *EVM) CallCode(caller types.Address, addr types.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		return ret, gasLeft, err
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	snapshot := evm.StateDB.Snapshot()

	// Get the code to execute from the target address
	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	// CALLCODE executes the callee's code but in the caller's context
	// (caller's address is used for storage and msg.sender)
	contract := NewContract(caller, caller, value, gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// DelegateCall executes a DELEGATECALL operation.
// Like CALLCODE but preserves the original caller and value.
func (evm *EVM) DelegateCall(caller types.Address, addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		return ret, gasLeft, err
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	snapshot := evm.StateDB.Snapshot()

	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	// DELEGATECALL preserves the caller (msg.sender) and value from the current context.
	// Storage operations happen on the caller's storage, not the callee's.
	contract := NewContract(caller, caller, nil, gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// StaticCall executes a read-only message call. Any state modifications will cause an error.
func (evm *EVM) StaticCall(caller types.Address, addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, gas, ErrMaxCallDepthExceeded
	}

	if evm.StateDB == nil {
		return nil, gas, errors.New("no state database")
	}

	// Set readOnly mode for the duration of this call.
	prevReadOnly := evm.readOnly
	evm.readOnly = true
	defer func() { evm.readOnly = prevReadOnly }()

	// We take a snapshot here. Even a staticcall is considered a 'touch'.
	// On mainnet, static calls were introduced after all empty accounts
	// were deleted, so this is not required. However, certain tests (e.g.
	// stRevertTest/RevertPrecompiledTouchExactOOG) require this behavior.
	snapshot := evm.StateDB.Snapshot()

	// Check for precompiled contract.
	if p, ok := evm.precompile(addr); ok {
		ret, gasLeft, err := runPrecompile(p, input, gas)
		if err != nil {
			evm.StateDB.RevertToSnapshot(snapshot)
		}
		return ret, gasLeft, err
	}

	code := evm.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	// EIP-7702: Handle delegation designator (0xef0100 || address).
	// When an account has delegated its code, we execute the delegated contract's code
	// but keep the contract.Address as the original target (the delegator).
	execAddr := addr // Address for contract creation (may be updated for delegation)
	if delegatedAddr, ok := types.IsDelegated(code); ok {
		delegatedCode := evm.StateDB.GetCode(delegatedAddr)
		if len(delegatedCode) > 0 {
			code = delegatedCode
			// Note: We keep execAddr as the original target (delegator).
			// This ensures ADDRESS opcode returns the delegator's address,
			// not the delegated contract's address.
		}
	}

	contract := NewContract(caller, execAddr, new(big.Int), gas)
	contract.Code = code
	contract.CodeHash = evm.StateDB.GetCodeHash(addr)

	evm.depth++
	ret, err := evm.Run(contract, input)
	evm.depth--

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		evm.StateDB.RevertToSnapshot(snapshot)
	}

	return ret, gasLeft, err
}

// createAddress computes the address of a contract created with CREATE.
// Per the Yellow Paper: addr = keccak256(rlp([sender, nonce]))[12:]
func createAddress(caller types.Address, nonce uint64) types.Address {
	// RLP-encode the list [sender_address, nonce].
	// sender_address is a 20-byte string, nonce is an integer.
	addrEnc := encodeRLPBytes(caller[:])
	nonceEnc := encodeRLPUint(nonce)

	// Wrap both items in an RLP list.
	payload := append(addrEnc, nonceEnc...)
	data := wrapRLPList(payload)

	hash := crypto.Keccak256(data)
	return types.BytesToAddress(hash[12:])
}

// encodeRLPBytes encodes a byte slice as an RLP string.
func encodeRLPBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	if len(b) < 56 {
		return append([]byte{byte(0x80 + len(b))}, b...)
	}
	lenBytes := uintToMinBytes(uint64(len(b)))
	header := append([]byte{byte(0xb7 + len(lenBytes))}, lenBytes...)
	return append(header, b...)
}

// encodeRLPUint encodes a uint64 as an RLP integer.
func encodeRLPUint(v uint64) []byte {
	if v == 0 {
		return []byte{0x80}
	}
	if v < 128 {
		return []byte{byte(v)}
	}
	b := uintToMinBytes(v)
	return append([]byte{byte(0x80 + len(b))}, b...)
}

// wrapRLPList wraps payload bytes in an RLP list header.
func wrapRLPList(payload []byte) []byte {
	if len(payload) < 56 {
		return append([]byte{byte(0xc0 + len(payload))}, payload...)
	}
	lenBytes := uintToMinBytes(uint64(len(payload)))
	header := append([]byte{byte(0xf7 + len(lenBytes))}, lenBytes...)
	return append(header, payload...)
}

// uintToMinBytes encodes a uint64 as big-endian bytes with no leading zeros.
func uintToMinBytes(v uint64) []byte {
	if v == 0 {
		return nil
	}
	var buf [8]byte
	n := 0
	for i := 7; i >= 0; i-- {
		buf[i] = byte(v)
		v >>= 8
		if buf[i] != 0 || n > 0 {
			n = 8 - i
		}
	}
	return buf[8-n:]
}

// create2Address computes the address of a contract created with CREATE2.
func create2Address(caller types.Address, salt *big.Int, initCodeHash []byte) types.Address {
	// CREATE2 address = keccak256(0xff + caller + salt + keccak256(initCode))[12:]
	saltBytes := make([]byte, 32)
	if salt != nil {
		b := salt.Bytes()
		copy(saltBytes[32-len(b):], b)
	}
	data := make([]byte, 0, 85)
	data = append(data, 0xff)
	data = append(data, caller[:]...)
	data = append(data, saltBytes...)
	data = append(data, initCodeHash...)
	hash := crypto.Keccak256(data)
	return types.BytesToAddress(hash[12:])
}

// Create creates a new contract with the given code.
func (evm *EVM) Create(caller types.Address, code []byte, gas uint64, value *big.Int) ([]byte, types.Address, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, types.Address{}, gas, ErrMaxCallDepthExceeded
	}
	if evm.readOnly {
		return nil, types.Address{}, gas, ErrWriteProtection
	}
	if evm.StateDB == nil {
		return nil, types.Address{}, gas, errors.New("no state database")
	}

	// Compute the new contract address
	nonce := evm.StateDB.GetNonce(caller)
	evm.StateDB.SetNonce(caller, nonce+1)
	contractAddr := createAddress(caller, nonce)

	return evm.create(caller, code, gas, value, contractAddr)
}

// Create2 creates a new contract using CREATE2 with the given salt.
func (evm *EVM) Create2(caller types.Address, code []byte, gas uint64, endowment *big.Int, salt *big.Int) ([]byte, types.Address, uint64, error) {
	if evm.depth > evm.Config.MaxCallDepth {
		return nil, types.Address{}, gas, ErrMaxCallDepthExceeded
	}
	if evm.readOnly {
		return nil, types.Address{}, gas, ErrWriteProtection
	}
	if evm.StateDB == nil {
		return nil, types.Address{}, gas, errors.New("no state database")
	}

	initCodeHash := crypto.Keccak256(code)
	contractAddr := create2Address(caller, salt, initCodeHash)

	return evm.create(caller, code, gas, endowment, contractAddr)
}

// PreWarmAccessList pre-warms the access list with the sender, recipient, and
// all precompile addresses (0x01-0x0a) per EIP-2929.
func (evm *EVM) PreWarmAccessList(sender types.Address, to *types.Address) {
	if evm.StateDB == nil {
		return
	}
	// Warm the sender.
	evm.StateDB.AddAddressToAccessList(sender)
	// Warm the recipient (if non-nil, i.e. not a contract creation).
	if to != nil {
		evm.StateDB.AddAddressToAccessList(*to)
	}
	// Warm all precompile addresses (0x01 through 0x13).
	// Includes: ecrecover(1), sha256(2), ripemd160(3), identity(4),
	// modexp(5), bn254add(6), bn254mul(7), bn254pairing(8),
	// blake2f(9), kzg(10), and EIP-2537 BLS12-381 (11-19).
	for i := 1; i <= 0x13; i++ {
		evm.StateDB.AddAddressToAccessList(types.BytesToAddress([]byte{byte(i)}))
	}
}

// gasEIP2929AccountCheck checks whether addr is warm. If cold, it warms the
// address and returns the extra cold gas (ColdAccountAccessCost - WarmStorageReadCost).
// If warm, it returns 0. The caller is expected to charge WarmStorageReadCost
// as the constant gas.
func gasEIP2929AccountCheck(evm *EVM, addr types.Address) uint64 {
	if evm.StateDB == nil {
		return 0
	}
	if evm.StateDB.AddressInAccessList(addr) {
		return 0
	}
	evm.StateDB.AddAddressToAccessList(addr)
	return ColdAccountAccessCost - WarmStorageReadCost
}

// gasEIP2929SlotCheck checks whether (addr, slot) is warm. If cold, it warms
// the slot and returns the extra cold gas (ColdSloadCost - WarmStorageReadCost).
// If warm, it returns 0. The caller is expected to charge WarmStorageReadCost
// as the constant gas.
func gasEIP2929SlotCheck(evm *EVM, addr types.Address, slot types.Hash) uint64 {
	if evm.StateDB == nil {
		return 0
	}
	_, slotWarm := evm.StateDB.SlotInAccessList(addr, slot)
	if slotWarm {
		return 0
	}
	evm.StateDB.AddSlotToAccessList(addr, slot)
	return ColdSloadCost - WarmStorageReadCost
}

// create is the shared implementation for Create and Create2.
func (evm *EVM) create(caller types.Address, code []byte, gas uint64, value *big.Int, contractAddr types.Address) ([]byte, types.Address, uint64, error) {
	// EIP-3860 / EIP-7954: max init code size check.
	maxInit := MaxInitCodeSizeForFork(evm.forkRules)
	if len(code) > maxInit {
		return nil, types.Address{}, gas, ErrMaxInitCodeSizeExceeded
	}

	// Collision check: fail if address already has non-zero nonce or non-empty code.
	// Per go-ethereum, all gas is consumed on collision.
	contractHash := evm.StateDB.GetCodeHash(contractAddr)
	if evm.StateDB.GetNonce(contractAddr) != 0 ||
		(contractHash != (types.Hash{}) && contractHash != types.EmptyCodeHash) {
		return nil, types.Address{}, 0, errors.New("contract address collision")
	}

	// EIP-2929: warm the created contract address BEFORE taking snapshot.
	// Even if the creation fails, the access-list change should not be rolled back.
	evm.StateDB.AddAddressToAccessList(contractAddr)

	snapshot := evm.StateDB.Snapshot()

	// Only create a new account if it doesn't already exist.
	// It's possible the contract code is deployed to a pre-existent
	// account with non-zero balance.
	if !evm.StateDB.Exist(contractAddr) {
		evm.StateDB.CreateAccount(contractAddr)
	}

	// EIP-161: set contract nonce to 1 (post Spurious Dragon).
	evm.StateDB.SetNonce(contractAddr, 1)

	// Transfer value
	if value != nil && value.Sign() > 0 {
		callerBalance := evm.StateDB.GetBalance(caller)
		if callerBalance.Cmp(value) < 0 {
			return nil, types.Address{}, gas, errors.New("insufficient balance for transfer")
		}
		// EIP-7928: capture pre-transfer balances for BAL tracking.
		var creatorPreBal, contractPreBal *big.Int
		if evm.balTracker != nil {
			creatorPreBal = new(big.Int).Set(evm.StateDB.GetBalance(caller))
			contractPreBal = new(big.Int).Set(evm.StateDB.GetBalance(contractAddr))
		}
		evm.StateDB.SubBalance(caller, value)
		evm.StateDB.AddBalance(contractAddr, value)
		// EIP-7928: record balance changes for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordBalanceChange(caller, creatorPreBal, evm.StateDB.GetBalance(caller))
			evm.balTracker.RecordBalanceChange(contractAddr, contractPreBal, evm.StateDB.GetBalance(contractAddr))
		}

		// EIP-7708: emit transfer log for nonzero-value CREATE.
		if evm.forkRules.IsEIP7708 {
			EmitTransferLog(evm.StateDB, caller, contractAddr, value)
		}
	}

	// GasCreate and InitCodeWordGas are already charged by the jump table's
	// constantGas and dynamicGas functions. Do not charge them again here.

	// Apply the 63/64 rule (EIP-150) to gas available for init code.
	callGas := gas - gas/CallGasFraction
	gas -= callGas

	// Create the contract for init code execution
	contract := NewContract(caller, contractAddr, value, callGas)
	contract.Code = code

	// Execute init code
	evm.depth++
	ret, err := evm.Run(contract, nil)
	evm.depth--

	if err != nil {
		// On any error, revert state and return.
		evm.StateDB.RevertToSnapshot(snapshot)
		if !errors.Is(err, ErrExecutionReverted) {
			// Non-revert error: all gas sent to subcall is consumed.
			// Only the 1/64 retained by EIP-150 is returned.
			return ret, types.Address{}, gas, err
		}
		// REVERT: return unused gas from subcall.
		gas += contract.Gas
		return ret, types.Address{}, gas, err
	}

	// Success: return unused gas from subcall.
	gas += contract.Gas

	// Code deposit cost: 200 per byte of deployed code.
	if len(ret) > 0 {
		// EIP-170 / EIP-7954: max contract code size.
		maxCode := MaxCodeSizeForFork(evm.forkRules)
		if len(ret) > maxCode {
			evm.StateDB.RevertToSnapshot(snapshot)
			return nil, types.Address{}, 0, errors.New("max code size exceeded")
		}
		depositCost := uint64(len(ret)) * CreateDataGas
		if gas < depositCost {
			evm.StateDB.RevertToSnapshot(snapshot)
			return nil, types.Address{}, 0, ErrOutOfGas
		}
		gas -= depositCost
		evm.StateDB.SetCode(contractAddr, ret)
	}

	return ret, contractAddr, gas, nil
}
