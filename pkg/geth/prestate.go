package geth

import (
	"math/big"

	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"

	"github.com/eth2030/eth2030/core/types"
)

// PreAccount holds pre-state for a single account (used by EF tests).
type PreAccount struct {
	Balance *big.Int
	Nonce   uint64
	Code    []byte
	Storage map[types.Hash]types.Hash
}

// StateTestState holds the go-ethereum state objects needed for test execution.
type StateTestState struct {
	StateDB *gethstate.StateDB
	TrieDB  *triedb.Database
}

// Close releases the trie database resources.
func (st *StateTestState) Close() {
	if st.TrieDB != nil {
		st.TrieDB.Close()
	}
}

// MakePreState creates a go-ethereum StateDB from a map of pre-state accounts.
// The state is committed to a trie so that state root computation is correct.
func MakePreState(accounts map[string]PreAccount) (*StateTestState, error) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)
	sdb := gethstate.NewDatabase(tdb, nil)
	statedb, err := gethstate.New(gethcommon.Hash{}, sdb)
	if err != nil {
		return nil, err
	}

	for addrHex, acct := range accounts {
		addr := gethcommon.HexToAddress(addrHex)
		statedb.CreateAccount(addr)
		if acct.Balance != nil {
			statedb.AddBalance(addr, ToUint256(acct.Balance), tracing.BalanceChangeUnspecified)
		}
		statedb.SetNonce(addr, acct.Nonce, tracing.NonceChangeUnspecified)
		if len(acct.Code) > 0 {
			statedb.SetCode(addr, acct.Code, tracing.CodeChangeUnspecified)
		}
		for key, val := range acct.Storage {
			statedb.SetState(addr, ToGethHash(key), ToGethHash(val))
		}
	}

	// Commit to trie to produce a clean pre-state root. Zero rules = no
	// EIP-158/161 emptiness handling, matching the old Commit(0, false, false).
	root, err := statedb.Commit(params.Rules{}, 0)
	if err != nil {
		return nil, err
	}
	if err := tdb.Commit(root, false); err != nil {
		return nil, err
	}

	// Re-open state from the committed root.
	statedb, err = gethstate.New(root, sdb)
	if err != nil {
		return nil, err
	}

	return &StateTestState{
		StateDB: statedb,
		TrieDB:  tdb,
	}, nil
}

// TouchCoinbase adds a zero-value balance to the coinbase address.
// This ensures the coinbase exists in state for correct EIP-158 handling.
// go-ethereum's EF test runner does this after every transaction.
func TouchCoinbase(statedb *gethstate.StateDB, coinbase gethcommon.Address) {
	statedb.AddBalance(coinbase, new(uint256.Int), tracing.BalanceChangeUnspecified)
}
