// genesis_alloc.go provides extended genesis allocation functionality including
// pre-funded testnet accounts, system contract initialization for post-merge
// chains, and genesis state encoding/serialization utilities.
package config

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// System contract addresses not yet in core/types.
var (
	// beaconRootsAddress is the EIP-4788 beacon block root system contract.
	beaconRootsAddress = types.HexToAddress("0x000F3df6D732807Ef1319fB7B8bB8522d0Beac02")
	// historyStorageAddress is the EIP-2935 block hash history system contract.
	historyStorageAddress = types.HexToAddress("0x0aae40965e6800cd9b1f4b05ff21581047e3f91e")
)

// Well-known testnet accounts: 10 pre-funded addresses with 10000 ETH each.
// These use deterministic addresses derived from simple keys for testing.
var TestnetPrefundedAccounts = []types.Address{
	types.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"),
	types.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
	types.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"),
	types.HexToAddress("0x90F79bf6EB2c4f870365E785982E1f101E93b906"),
	types.HexToAddress("0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65"),
	types.HexToAddress("0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc"),
	types.HexToAddress("0x976EA74026E726554dB657fA54763abd0C3a0aa9"),
	types.HexToAddress("0x14dC79964da2C08daa4967015E5BCE323219B84f"),
	types.HexToAddress("0x23618e81E3f5cdF7f54C3d65f7FBc0aBf5B21E8f"),
	types.HexToAddress("0xa0Ee7A142d267C1f36714E4a8F75612F20a79720"),
}

// TestnetPrefundAmount is 10000 ETH in Wei for testnet prefunded accounts.
var TestnetPrefundAmount = new(big.Int).Mul(
	big.NewInt(10000),
	new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil),
)

// TestnetGenesisAlloc creates a genesis allocation with the standard testnet
// prefunded accounts. Each account receives TestnetPrefundAmount (10000 ETH).
func TestnetGenesisAlloc() GenesisAlloc {
	alloc := make(GenesisAlloc)
	for _, addr := range TestnetPrefundedAccounts {
		alloc[addr] = GenesisAccount{
			Balance: new(big.Int).Set(TestnetPrefundAmount),
		}
	}
	return alloc
}

// TestnetGenesisBlock returns a testnet genesis with prefunded accounts and
// the Sepolia chain config. All pre-merge forks are active at genesis.
func TestnetGenesisBlock() *Genesis {
	return &Genesis{
		Config:     SepoliaConfig,
		Nonce:      0,
		Timestamp:  1633267481,
		ExtraData:  []byte("ETH2030 testnet"),
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc:      TestnetGenesisAlloc(),
	}
}

// SystemContractAlloc returns a genesis allocation that includes the system
// contracts required by post-merge Ethereum. These are the EIP-4788 beacon
// root contract, EIP-2935 block hash history, and Prague deposit/withdrawal/
// consolidation request contracts.
func SystemContractAlloc() GenesisAlloc {
	alloc := ExecutionRequestContractAlloc()

	alloc[beaconRootsAddress] = GenesisAccount{
		Code: beaconRootStubCode(), Balance: new(big.Int), Nonce: 1,
	}
	alloc[historyStorageAddress] = GenesisAccount{
		Code: blockHashStubCode(), Balance: new(big.Int), Nonce: 1,
	}
	return alloc
}

// depositContractCode is the deployed runtime from the pinned execution-specs
// Prague allocation. The other request runtimes come from the Geth dependency.
//
//go:embed contracts/deposit.bin
var depositContractCode []byte

// ExecutionRequestContractAlloc supplies the Prague request contracts for new
// test and private chains. Public networks deploy their contracts on-chain.
func ExecutionRequestContractAlloc() GenesisAlloc {
	const depth = types.DepositContractTreeDepth
	storage := make(map[types.Hash]types.Hash, depth-1)
	next := sha256.Sum256(make([]byte, 64))
	for i := depth + 2; i <= depth*2; i++ {
		var slot types.Hash
		binary.BigEndian.PutUint64(slot[24:], uint64(i))
		storage[slot] = types.Hash(next)
		var pair [64]byte
		copy(pair[:32], next[:])
		copy(pair[32:], next[:])
		next = sha256.Sum256(pair[:])
	}
	return GenesisAlloc{
		types.DepositContractAddress:      {Code: bytes.Clone(depositContractCode), Balance: new(big.Int), Nonce: 1, Storage: storage},
		types.WithdrawalRequestAddress:    {Code: bytes.Clone(params.WithdrawalQueueCode), Balance: new(big.Int), Nonce: 1},
		types.ConsolidationRequestAddress: {Code: bytes.Clone(params.ConsolidationQueueCode), Balance: new(big.Int), Nonce: 1},
	}
}

// Beacon and history stubs accompany the native storage transitions.

func beaconRootStubCode() []byte {
	// Minimal bytecode: PUSH0 PUSH0 SSTORE STOP (stores 0 at slot 0).
	return []byte{0x5F, 0x5F, 0x55, 0x00}
}

func blockHashStubCode() []byte {
	return []byte{0x5F, 0x5F, 0x55, 0x00}
}

// MergeGenesisAlloc creates a genesis allocation with both testnet prefunded
// accounts and system contracts. This is suitable for post-merge test chains.
func MergeGenesisAlloc() GenesisAlloc {
	alloc := TestnetGenesisAlloc()
	systemAlloc := SystemContractAlloc()
	for addr, acct := range systemAlloc {
		alloc[addr] = acct
	}
	return alloc
}

// GenesisAllocJSON represents a JSON-serializable genesis allocation entry.
type GenesisAllocJSON struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
	Nonce   uint64 `json:"nonce,omitempty"`
	Code    string `json:"code,omitempty"`
}

// MarshalGenesisAlloc serializes a genesis allocation to JSON. Accounts are
// serialized in sorted address order for deterministic output.
func MarshalGenesisAlloc(alloc GenesisAlloc) ([]byte, error) {
	// Sort addresses for deterministic output.
	addrs := make([]types.Address, 0, len(alloc))
	for addr := range alloc {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		for k := 0; k < types.AddressLength; k++ {
			if addrs[i][k] != addrs[j][k] {
				return addrs[i][k] < addrs[j][k]
			}
		}
		return false
	})

	entries := make([]GenesisAllocJSON, 0, len(alloc))
	for _, addr := range addrs {
		acct := alloc[addr]
		entry := GenesisAllocJSON{
			Address: addr.Hex(),
			Nonce:   acct.Nonce,
		}
		if acct.Balance != nil {
			entry.Balance = acct.Balance.String()
		} else {
			entry.Balance = "0"
		}
		if len(acct.Code) > 0 {
			entry.Code = types.BytesToHash(crypto.Keccak256(acct.Code)).Hex()
		}
		entries = append(entries, entry)
	}

	return json.Marshal(entries)
}

// AllocAccountCount returns the number of accounts in the genesis allocation.
func AllocAccountCount(alloc GenesisAlloc) int {
	return len(alloc)
}

// AllocHasAccount checks if a specific address is present in the allocation.
func AllocHasAccount(alloc GenesisAlloc, addr types.Address) bool {
	_, ok := alloc[addr]
	return ok
}

// GenesisStateSnapshot captures a snapshot of the genesis state after applying
// allocations. It stores account data in a compact form for verification.
type GenesisStateSnapshot struct {
	Root         types.Hash
	AccountCount int
	TotalBalance *big.Int
	CodeAccounts int
}

// SnapshotGenesisState applies a genesis allocation to a fresh in-memory state
// and returns a snapshot of the resulting state for verification purposes.
func SnapshotGenesisState(alloc GenesisAlloc) GenesisStateSnapshot {
	statedb := state.NewMemoryStateDB()
	ApplyGenesisAlloc(statedb, alloc)

	snap := GenesisStateSnapshot{
		Root:         statedb.GetRoot(),
		AccountCount: len(alloc),
		TotalBalance: new(big.Int),
	}

	for _, acct := range alloc {
		if acct.Balance != nil {
			snap.TotalBalance.Add(snap.TotalBalance, acct.Balance)
		}
		if len(acct.Code) > 0 {
			snap.CodeAccounts++
		}
	}

	return snap
}
