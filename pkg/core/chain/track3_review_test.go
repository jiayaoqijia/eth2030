package chain

import (
	"math/big"
	"testing"

	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

func TestRestoreFailurePreservesDurableHead(t *testing.T) {
	bc, st := testChain(t)
	blocks := makeChainBlocks(bc.Genesis(), 3, st.Copy())
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatal(err)
	}
	want := blocks[2].Hash()
	if err := rawdb.DeleteBody(bc.db, 3, want); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewBlockchain(config.TestConfig, bc.Genesis(), state.NewMemoryStateDB(), bc.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.RestoreSnapPivot(); err == nil {
		t.Fatal("missing tail was not rejected")
	}
	got, err := rawdb.ReadHeadBlockHash(bc.db)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("failed recovery rewrote durable target: got %s want %s", got, want)
	}
}

type reviewTxPool struct{ txs []*types.Transaction }

func (p reviewTxPool) Pending() []*types.Transaction { return p.txs }

func TestNonCanonicalBlockhashExecutionAndReadback(t *testing.T) {
	key, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sender := types.Address(gethcrypto.PubkeyToAddress(key.PublicKey))
	contract := types.BytesToAddress([]byte{0x12, 0x34})
	initial := state.NewMemoryStateDB()
	initial.AddBalance(sender, new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil))
	initial.SetCode(contract, []byte{0x60, 1, 0x40, 0x60, 0, 0x55, 0}) // SSTORE(0, BLOCKHASH(1))
	genesisHeader := makeGenesis(30_000_000, big.NewInt(1)).Header()
	genesisHeader.Root = initial.GetRoot()
	genesis := types.NewBlock(genesisHeader, nil)
	bc, err := NewBlockchain(config.TestConfig, genesis, initial, rawdb.NewMemoryDB())
	if err != nil {
		t.Fatal(err)
	}
	beacon := types.Hash{}
	builder := coreblock.NewBlockBuilder(config.TestConfig, bc, nil)
	first, _, err := builder.BuildBlock(genesis.Header(), &coreblock.BuildBlockAttributes{Timestamp: 12, GasLimit: 30_000_000, BeaconRoot: &beacon, Withdrawals: []*types.Withdrawal{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.InsertBlock(first); err != nil {
		t.Fatal(err)
	}
	alternateHeader := first.Header()
	alternateHeader.Extra = []byte("alternate")
	alternate := types.NewBlock(alternateHeader, first.Body())
	if err := bc.InsertSyncedBlock(alternate); err != nil {
		t.Fatal(err)
	}
	inner := &types.DynamicFeeTx{ChainID: config.TestConfig.ChainID, Nonce: 0, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(100), Gas: 200000, To: &contract, Value: new(big.Int)}
	hash := types.NewTransaction(inner).SigningHash()
	sig, err := gethcrypto.Sign(hash[:], key)
	if err != nil {
		t.Fatal(err)
	}
	inner.R = new(big.Int).SetBytes(sig[:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	inner.V = new(big.Int).SetUint64(uint64(sig[64]))
	tx := types.NewTransaction(inner)
	builder = coreblock.NewBlockBuilder(config.TestConfig, bc, reviewTxPool{[]*types.Transaction{tx}})
	parentState, err := bc.StateAtBlock(alternate)
	if err != nil {
		t.Fatal(err)
	}
	builder.SetState(parentState)
	child, receipts, err := builder.BuildBlock(alternate.Header(), &coreblock.BuildBlockAttributes{Timestamp: 24, GasLimit: 30_000_000, BeaconRoot: &beacon, Withdrawals: []*types.Withdrawal{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0].Status != 1 {
		t.Fatal("contract did not execute")
	}
	if err := bc.InsertSyncedBlock(child); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []types.Hash{child.Hash(), alternate.Hash()} {
		bc.sc.remove(hash)
		bc.memSC.remove(hash)
		bc.EvictBlockFromCache(hash)
	}
	recovered, err := bc.StateAtBlock(child)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.GetState(contract, types.Hash{}) != alternate.Hash() {
		t.Fatal("BLOCKHASH used another branch during build or readback")
	}
	if bc.CurrentBlock().Hash() != first.Hash() {
		t.Fatal("side execution changed canonical head")
	}
}

func TestForkchoiceRewindClearsSafeAndHeaderIndices(t *testing.T) {
	bc, st := testChain(t)
	blocks := makeChainBlocks(bc.Genesis(), 3, st.Copy())
	if _, err := bc.InsertChain(blocks); err != nil {
		t.Fatal(err)
	}
	if err := bc.ApplyForkchoice(blocks[2].Hash(), blocks[2].Hash(), bc.Genesis().Hash()); err != nil {
		t.Fatal(err)
	}
	if err := bc.ApplyForkchoice(blocks[0].Hash(), types.Hash{}, bc.Genesis().Hash()); err != nil {
		t.Fatal(err)
	}
	if bc.CurrentSafeBlock() != nil {
		t.Fatal("zero safe hash retained a non-ancestor safe block")
	}
	if bc.hc.GetHeaderByNumber(3) != nil {
		t.Fatal("header chain still exposes a rewound canonical block")
	}
}

func TestForkchoiceRejectsCorruptCachedState(t *testing.T) {
	bc, st := testChain(t)
	block := makeChainBlocks(bc.Genesis(), 1, st.Copy())[0]
	if err := bc.InsertSyncedBlock(block); err != nil {
		t.Fatal(err)
	}
	wrong := state.NewMemoryStateDB()
	wrong.AddBalance(types.BytesToAddress([]byte{1}), big.NewInt(1))
	bc.sc.remove(block.Hash())
	bc.sc.put(block.Hash(), block.NumberU64(), wrong)
	if err := bc.ApplyForkchoice(block.Hash(), types.Hash{}, types.Hash{}); err == nil {
		t.Fatal("forkchoice published state that does not match head root")
	}
	if bc.CurrentBlock().Hash() != bc.Genesis().Hash() {
		t.Fatal("failed state validation changed head")
	}
}

func TestMinimalBlockCacheMakesProgress(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1))
	bc, err := NewBlockchain(config.TestConfig, genesis, stateDB, rawdb.NewMemoryDB(), BlockchainOpts{BlockCacheSize: 1, StateCacheSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	blocks := makeChainBlocks(genesis, 4, stateDB.Copy())
	for _, block := range blocks {
		if err := bc.InsertBlock(block); err != nil {
			t.Fatal(err)
		}
	}
	if bc.CurrentBlock().Hash() != blocks[3].Hash() {
		t.Fatal("tiny cache lost progress")
	}
}

func TestAncientLookupCannotSubstituteAnotherFork(t *testing.T) {
	bc, st := testChain(t)
	first := makeChainBlocks(bc.Genesis(), 1, st.Copy())[0]
	if err := bc.InsertBlock(first); err != nil {
		t.Fatal(err)
	}
	header := first.Header()
	header.Extra = []byte("side")
	side := types.NewBlock(header, first.Body())
	if err := bc.InsertSyncedBlock(side); err != nil {
		t.Fatal(err)
	}
	ancient, err := rawdb.NewAncientStore(rawdb.AncientStoreConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer ancient.Close()
	for _, block := range []*types.Block{bc.Genesis(), first} {
		encodedHeader, err := block.Header().EncodeRLP()
		if err != nil {
			t.Fatal(err)
		}
		body, err := encodeBlockBody(block.Body())
		if err != nil {
			t.Fatal(err)
		}
		if err := ancient.FreezeBlock(block.NumberU64(), block.Hash(), encodedHeader, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	bc.SetAncientStore(ancient)
	if err := rawdb.DeleteHeaderData(bc.db, side.NumberU64(), side.Hash()); err != nil {
		t.Fatal(err)
	}
	if err := rawdb.DeleteBody(bc.db, side.NumberU64(), side.Hash()); err != nil {
		t.Fatal(err)
	}
	bc.EvictBlockFromCache(side.Hash())
	if got := bc.GetBlock(side.Hash()); got != nil {
		t.Fatalf("requested side hash returned frozen block %s", got.Hash())
	}
}
