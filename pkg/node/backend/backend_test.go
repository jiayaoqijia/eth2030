package backend

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/mev"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine/payload"
	"github.com/eth2030/eth2030/proofs"
	"github.com/eth2030/eth2030/rpc"
	"github.com/eth2030/eth2030/txpool"
)

// mockNodeDeps is a mock implementation of NodeDeps for testing.
type mockNodeDeps struct {
	blockchain *chain.Blockchain
	txPool     *txpool.TxPool
	config     *Config
}

func (m *mockNodeDeps) Blockchain() *chain.Blockchain                  { return m.blockchain }
func (m *mockNodeDeps) TxPool() *txpool.TxPool                         { return m.txPool }
func (m *mockNodeDeps) Config() *Config                                { return m.config }
func (m *mockNodeDeps) GasOracle() GasOracleDeps                       { return nil }
func (m *mockNodeDeps) EthHandler() EthHandlerDeps                     { return nil }
func (m *mockNodeDeps) TxJournal() TxJournalDeps                       { return nil }
func (m *mockNodeDeps) MEVConfig() *mev.MEVProtectionConfig            { return nil }
func (m *mockNodeDeps) FCStateManager() FCStateManagerDeps             { return nil }
func (m *mockNodeDeps) StarkFrameProver() proofs.ValidationFrameProver { return nil }
func (m *mockNodeDeps) P2PServer() P2PServerDeps                       { return nil }

func newTestBackendNode(t *testing.T) *mockNodeDeps {
	t.Helper()
	genesis := types.NewBlock(&types.Header{Number: new(big.Int), GasLimit: 30_000_000}, &types.Body{})
	bc, err := chain.NewBlockchain(coreconfig.TestConfig, genesis, state.NewMemoryStateDB(), rawdb.NewMemoryDB())
	if err != nil {
		t.Fatal(err)
	}
	return &mockNodeDeps{blockchain: bc, config: &Config{CacheEnginePayloads: 7}}
}

func TestNewRPCBackend(t *testing.T) {
	node := newTestBackendNode(t)
	engine := NewEngineBackend(node)
	t.Cleanup(engine.Close)
	backend := NewRPCBackend(node, engine)
	header := backend.HeaderByNumber(rpc.LatestBlockNumber)
	if want := node.blockchain.CurrentBlock().Hash(); header == nil || header.Hash() != want {
		t.Fatalf("latest header = %v, want genesis %s", header, want)
	}
}

func TestNewEngineBackend(t *testing.T) {
	node := newTestBackendNode(t)
	backend := NewEngineBackend(node)
	t.Cleanup(backend.Close)
	if got, want := backend.GetHeadHash(), node.blockchain.CurrentBlock().Hash(); got != want {
		t.Fatalf("head = %s, want genesis %s", got, want)
	}
	if backend.maxPayloads != node.config.CacheEnginePayloads {
		t.Fatalf("payload cache limit = %d, want %d", backend.maxPayloads, node.config.CacheEnginePayloads)
	}
}

func TestExtractBlockTips(t *testing.T) {
	// Test with empty transactions
	tips := ExtractBlockTips(nil, big.NewInt(1))
	if tips == nil {
		t.Error("ExtractBlockTips(nil) returned nil")
	}
	if len(tips) != 0 {
		t.Errorf("ExtractBlockTips(nil) = %d tips, want 0", len(tips))
	}

	// Test with nil base fee
	tips = ExtractBlockTips(nil, nil)
	if tips == nil {
		t.Error("ExtractBlockTips with nil baseFee returned nil")
	}
}

func TestEncodeTxsRLPEmpty(t *testing.T) {
	// encodeTxsRLP must return a non-nil empty slice so JSON encodes as []
	// rather than null (Engine API requires [] not null for transactions).
	result := encodeTxsRLP(nil)
	if result == nil {
		t.Error("expected non-nil empty slice for nil txs, got nil (would JSON-encode as null)")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result))
	}
}

func TestGeneratePayloadID(t *testing.T) {
	// Test that generatePayloadID returns a non-zero ID in most cases.
	var parentHash types.Hash
	parentHash[0] = 0x01

	// Create minimal attributes
	attrs := &block.BuildBlockAttributes{
		Timestamp:    12345,
		FeeRecipient: types.Address{0x02},
		Random:       types.Hash{0x03},
	}

	id := generatePayloadID(parentHash, attrs)
	if id == (payload.PayloadID{}) {
		t.Error("generatePayloadID returned zero ID")
	}
}

func TestEngineBackendProcessInclusionListRejectsUnsupportedStorage(t *testing.T) {
	b := &EngineBackend{}
	err := b.ProcessInclusionList(&types.InclusionList{
		Slot:           1,
		ValidatorIndex: 2,
		Transactions:   [][]byte{{0x01}},
	})
	if !errors.Is(err, errInclusionListStorageUnsupported) {
		t.Fatalf("expected %v, got %v", errInclusionListStorageUnsupported, err)
	}
}

// Note: The following tests require a full Node setup and are better suited
// for integration tests. They are kept here as documentation of what should
// be tested:
// - TestBackendChainID
// - TestBackendCurrentHeader
// - TestBackendHeaderByNumber
// - TestBackendBlockByNumber
// - TestBackendSuggestGasPrice
// - TestBackendHeaderByHash
// - TestBackendBlockByHash
// - TestBackendGetTransactionNotFound
// - TestBackendGetReceiptsEmpty
// - TestBackendGetLogsEmpty
// - TestBackendHistoryOldestBlock
// - TestEngineBackendIsCancun
// - TestEngineBackendIsPrague
// - TestEngineBackendIsAmsterdam
// - TestEngineBackendGetHeadTimestamp
// - TestEngineBackendGetPayloadNotFound
