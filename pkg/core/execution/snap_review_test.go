package execution

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/gaspool"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

func TestSnapMultiTransactionOriginalStorage(t *testing.T) {
	sender := types.BytesToAddress([]byte{1})
	contract := types.BytesToAddress([]byte{0x12, 0x34})
	initial := state.NewMemoryStateDB()
	initial.AddBalance(sender, new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil))
	initial.SetCode(contract, []byte{0x60, 0, 0x35, 0x60, 0, 0x55, 0})
	if _, err := initial.Commit(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.ExportStateSnapshot(initial)
	if err != nil {
		t.Fatal(err)
	}
	db := rawdb.NewMemoryDB()
	if err := state.StoreStateSnapshot(db, initial.GetRoot(), snapshot); err != nil {
		t.Fatal(err)
	}
	imported, err := state.OpenSnapState(db, initial.GetRoot())
	if err != nil {
		t.Fatal(err)
	}
	reference := initial.Copy()
	header := &types.Header{Number: big.NewInt(1), Time: 12, GasLimit: 30_000_000, BaseFee: big.NewInt(1)}
	for nonce := uint64(0); nonce < 2; nonce++ {
		data := make([]byte, 32)
		data[31] = byte(nonce + 1)
		tx := types.NewTransaction(&types.LegacyTx{Nonce: nonce, GasPrice: big.NewInt(2), Gas: 200000, To: &contract, Value: new(big.Int), Data: data})
		tx.SetSender(sender)
		want, _, err := ApplyTransaction(config.TestConfig, reference, header, tx, new(gaspool.GasPool).AddGas(header.GasLimit))
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := ApplyTransaction(config.TestConfig, imported, header, tx, new(gaspool.GasPool).AddGas(header.GasLimit))
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != 1 || imported.GetState(contract, types.Hash{}) != types.BytesToHash([]byte{byte(nonce + 1)}) {
			t.Fatal("fixture did not execute the intended SSTORE")
		}
		if got.GasUsed != want.GasUsed {
			t.Fatalf("tx %d used stale original storage: got %d want %d", nonce, got.GasUsed, want.GasUsed)
		}
		if _, err := reference.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if imported.GetRoot() != reference.GetRoot() {
		t.Fatal("multi-transaction state differs")
	}
}
