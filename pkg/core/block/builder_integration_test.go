package block_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

func testConfigWithoutAmsterdam() *config.ChainConfig {
	cfg := *config.TestConfig
	cfg.AmsterdamTime = nil
	return &cfg
}

func testConfigAmsterdamWithoutGlamsterdam() *config.ChainConfig {
	cfg := *config.TestConfig
	zero := uint64(0)
	cfg.AmsterdamTime = &zero
	cfg.GlamsterdanTime = nil
	return &cfg
}

// makeGenesisInteg creates a genesis block for integration tests.
func makeGenesisInteg(gasLimit uint64, baseFee *big.Int) *types.Block {
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	calldataGasUsed := uint64(0)
	calldataExcessGas := uint64(0)
	emptyWithdrawalsHash := types.EmptyRootHash
	emptyRoot := types.EmptyRootHash
	emptyRequestsHash := types.EmptyRequestsHash
	header := &types.Header{
		Number:            big.NewInt(0),
		GasLimit:          gasLimit,
		GasUsed:           0,
		Time:              0,
		Difficulty:        new(big.Int),
		BaseFee:           baseFee,
		UncleHash:         block.EmptyUncleHash,
		WithdrawalsHash:   &emptyWithdrawalsHash,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyRoot,
		RequestsHash:      &emptyRequestsHash,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}
	return types.NewBlock(header, &types.Body{Withdrawals: []*types.Withdrawal{}})
}

// makeChainBlocksInteg builds a chain of empty blocks from the given parent using
// the provided state (which is mutated in place).
func makeChainBlocksInteg(parent *types.Block, count int, statedb *state.MemoryStateDB) []*types.Block {
	blocks := make([]*types.Block, count)
	for i := 0; i < count; i++ {
		blocks[i] = makeBlockWithStateInteg(parent, nil, statedb)
		parent = blocks[i]
	}
	return blocks
}

// makeBlockWithStateInteg builds a valid child block and computes the correct header
// fields by executing the transactions against the provided state.
func makeBlockWithStateInteg(parent *types.Block, txs []*types.Transaction, statedb *state.MemoryStateDB) *types.Block {
	parentHeader := parent.Header()
	blobGasUsed := uint64(0)
	var pExcess, pUsed uint64
	if parentHeader.ExcessBlobGas != nil {
		pExcess = *parentHeader.ExcessBlobGas
	}
	if parentHeader.BlobGasUsed != nil {
		pUsed = *parentHeader.BlobGasUsed
	}
	excessBlobGas := gas.CalcExcessBlobGas(pExcess, pUsed)

	calldataGasUsed := uint64(0)
	var pCalldataExcess, pCalldataUsed uint64
	if parentHeader.CalldataExcessGas != nil {
		pCalldataExcess = *parentHeader.CalldataExcessGas
	}
	if parentHeader.CalldataGasUsed != nil {
		pCalldataUsed = *parentHeader.CalldataGasUsed
	}
	calldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, parentHeader.GasLimit)

	emptyWHash := types.EmptyRootHash
	emptyBeaconRoot := types.EmptyRootHash
	emptyRequestsHash := types.EmptyRequestsHash
	header := &types.Header{
		ParentHash:        parent.Hash(),
		Number:            new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:          parentHeader.GasLimit,
		Time:              parentHeader.Time + 12,
		Difficulty:        new(big.Int),
		BaseFee:           gas.CalcBaseFee(parentHeader),
		UncleHash:         block.EmptyUncleHash,
		WithdrawalsHash:   &emptyWHash,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyBeaconRoot,
		RequestsHash:      &emptyRequestsHash,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}

	body := &types.Body{
		Transactions: txs,
		Withdrawals:  []*types.Withdrawal{},
	}

	blk := types.NewBlock(header, body)

	proc := execution.NewStateProcessor(config.TestConfig)
	result, err := proc.ProcessWithBAL(blk, statedb)
	if err == nil {
		var gasUsed uint64
		for _, r := range result.Receipts {
			gasUsed += r.GasUsed
		}
		header.GasUsed = gasUsed

		var cdGasUsed uint64
		for _, tx := range txs {
			cdGasUsed += tx.CalldataGas()
		}
		*header.CalldataGasUsed = cdGasUsed

		if result.BlockAccessList != nil {
			h := result.BlockAccessList.Hash()
			header.BlockAccessListHash = &h
		}

		header.Bloom = types.CreateBloom(result.Receipts)
		header.ReceiptHash = block.DeriveReceiptsRoot(result.Receipts)
		header.Root = statedb.GetRoot()
	}

	header.TxHash = block.DeriveTxsRoot(txs)
	return types.NewBlock(header, body)
}

// mockTxPoolInteg implements block.TxPoolReader for integration testing.
type mockTxPoolInteg struct {
	txs []*types.Transaction
}

func (p *mockTxPoolInteg) Pending() []*types.Transaction {
	return p.txs
}

// TestBuildBlock_Empty tests building an empty block via the new API.
func TestBuildBlock_Empty(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesisInteg(30_000_000, big.NewInt(1000))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// No txpool (nil) means no transactions available.
	builder := block.NewBlockBuilder(config.TestConfig, bc, nil)

	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	blk, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if len(blk.Transactions()) != 0 {
		t.Errorf("expected 0 txs, got %d", len(blk.Transactions()))
	}
	if len(receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(receipts))
	}
	if blk.NumberU64() != 1 {
		t.Errorf("block number = %d, want 1", blk.NumberU64())
	}
	if blk.GasUsed() != 0 {
		t.Errorf("gas used = %d, want 0", blk.GasUsed())
	}
	if blk.Coinbase() != attrs.FeeRecipient {
		t.Errorf("coinbase = %v, want %v", blk.Coinbase(), attrs.FeeRecipient)
	}
}

// TestBuildBlock_WithTransactions tests building a block with transactions from pool.
func TestBuildBlock_WithTransactions(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	genesis := makeGenesisInteg(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Create pending transactions.
	var txs []*types.Transaction
	for i := uint64(0); i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}
	pool := &mockTxPoolInteg{txs: txs}

	builder := block.NewBlockBuilder(config.TestConfig, bc, pool)

	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	blk, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if len(blk.Transactions()) != 3 {
		t.Errorf("tx count = %d, want 3", len(blk.Transactions()))
	}
	if len(receipts) != 3 {
		t.Errorf("receipt count = %d, want 3", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d status = %d, want success", i, r.Status)
		}
	}
	if blk.GasUsed() == 0 {
		t.Error("gas used should be > 0 with transactions")
	}
}

// TestBuildBlock_GasLimitEnforcement tests that the block builder respects gas limits.
func TestBuildBlock_GasLimitEnforcement(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0xaa})
	receiver := types.BytesToAddress([]byte{0xab})
	statedb.AddBalance(sender, big.NewInt(100_000_000))

	genesis := makeGenesisInteg(50000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Create 5 txs each requiring 21000 gas.
	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(1),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}
	pool := &mockTxPoolInteg{txs: txs}

	builder := block.NewBlockBuilder(config.TestConfig, bc, pool)

	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     50000, // Only room for ~2 transactions.
	}

	blk, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// 50000 / 21000 = 2.38, so at most 2 transactions fit.
	if len(blk.Transactions()) > 2 {
		t.Errorf("expected at most 2 txs, got %d", len(blk.Transactions()))
	}
	if len(receipts) != len(blk.Transactions()) {
		t.Errorf("receipt count %d != tx count %d", len(receipts), len(blk.Transactions()))
	}
	if blk.GasUsed() > blk.GasLimit() {
		t.Errorf("gas used %d exceeds gas limit %d", blk.GasUsed(), blk.GasLimit())
	}
}

func TestBuildBlock_AATxReplayMatchesBuiltStateRoot(t *testing.T) {
	builderState := state.NewMemoryStateDB()
	replayState := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	funds := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	builderState.AddBalance(sender, funds)
	replayState.AddBalance(sender, new(big.Int).Set(funds))

	genesis := makeGenesisInteg(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, builderState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	aaTx := types.NewTransaction(&types.AATx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               sender,
		SenderValidationGas:  50_000,
		SenderExecutionGas:   21_000,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		SenderValidationData: []byte{},
		SenderExecutionData:  []byte{},
	})

	pool := &mockTxPoolInteg{txs: []*types.Transaction{aaTx}}
	builder := block.NewBlockBuilder(config.TestConfig, bc, pool)
	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	blk, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("receipt count = %d, want 1", len(receipts))
	}

	proc := execution.NewStateProcessor(config.TestConfig)
	if _, err := proc.ProcessWithBAL(blk, replayState); err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}

	if blk.Root() != replayState.GetRoot() {
		t.Fatalf("state root mismatch: built=%s replay=%s", blk.Root(), replayState.GetRoot())
	}
	if replayState.GetNonce(sender) == 0 {
		t.Fatalf("replay sender nonce = %d, want AA post-execution nonce increment", replayState.GetNonce(sender))
	}
}

func TestBuildBlock_AATxReplayMatchesBuiltStateRoot_PreAmsterdam(t *testing.T) {
	cfg := testConfigWithoutAmsterdam()
	builderState := state.NewMemoryStateDB()
	replayState := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	funds := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	builderState.AddBalance(sender, funds)
	replayState.AddBalance(sender, new(big.Int).Set(funds))

	genesis := makeGenesisInteg(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(cfg, genesis, builderState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	aaTx := types.NewTransaction(&types.AATx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               sender,
		SenderValidationGas:  50_000,
		SenderExecutionGas:   21_000,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		SenderValidationData: []byte{},
		SenderExecutionData:  []byte{},
	})

	pool := &mockTxPoolInteg{txs: []*types.Transaction{aaTx}}
	builder := block.NewBlockBuilder(cfg, bc, pool)
	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	blk, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("receipt count = %d, want 1", len(receipts))
	}

	proc := execution.NewStateProcessor(cfg)
	if _, err := proc.ProcessWithBAL(blk, replayState); err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}

	if blk.Root() != replayState.GetRoot() {
		t.Fatalf("state root mismatch: built=%s replay=%s", blk.Root(), replayState.GetRoot())
	}
	if replayState.GetNonce(sender) != 1 {
		t.Fatalf("pre-Amsterdam AA sender nonce = %d, want 1", replayState.GetNonce(sender))
	}
}

func TestBuildBlock_AmsterdamWithoutGlamsterdamUsesUserGasForHeader(t *testing.T) {
	cfg := testConfigAmsterdamWithoutGlamsterdam()
	builderState := state.NewMemoryStateDB()
	replayState := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddddd")
	contractAddr := types.HexToAddress("0x000000000000000000000000000000000000c1ea")
	funds := new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
	slotZero := types.Hash{}
	slotValue := types.HexToHash("0x01")
	clearStorageCode := []byte{0x60, 0x00, 0x60, 0x00, 0x55, 0x00}

	for _, db := range []*state.MemoryStateDB{builderState, replayState} {
		db.AddBalance(sender, new(big.Int).Set(funds))
		db.SetCode(contractAddr, clearStorageCode)
		db.SetState(contractAddr, slotZero, slotValue)
	}

	genesis := makeGenesisInteg(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(cfg, genesis, builderState, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   new(big.Int).Set(cfg.ChainID),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(10),
		Gas:       150_000,
		To:        &contractAddr,
		Value:     big.NewInt(0),
	})
	tx.SetSender(sender)

	pool := &mockTxPoolInteg{txs: []*types.Transaction{tx}}
	builder := block.NewBlockBuilder(cfg, bc, pool)
	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	blk, receipts, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("receipt count = %d, want 1", len(receipts))
	}
	if receipts[0].BlockGasUsed <= receipts[0].GasUsed {
		t.Fatalf("expected refund-driven gas split, got blockGasUsed=%d gasUsed=%d", receipts[0].BlockGasUsed, receipts[0].GasUsed)
	}
	if blk.GasUsed() != receipts[0].GasUsed {
		t.Fatalf("header gas used = %d, want post-refund gas %d before Glamsterdam", blk.GasUsed(), receipts[0].GasUsed)
	}

	proc := execution.NewStateProcessor(cfg)
	result, err := proc.ProcessWithBAL(blk, replayState)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}
	if len(result.Receipts) != 1 {
		t.Fatalf("replay receipt count = %d, want 1", len(result.Receipts))
	}
	if result.Receipts[0].GasUsed != blk.GasUsed() {
		t.Fatalf("replay gas used = %d, want header gas %d", result.Receipts[0].GasUsed, blk.GasUsed())
	}
	if result.Receipts[0].BlockGasUsed != receipts[0].BlockGasUsed {
		t.Fatalf("replay block gas used = %d, want %d", result.Receipts[0].BlockGasUsed, receipts[0].BlockGasUsed)
	}
}

// TestBuildBlock_WithdrawalProcessing tests that withdrawals are applied
// correctly during block building.
func TestBuildBlock_WithdrawalProcessing(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesisInteg(30_000_000, big.NewInt(1000))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	validator1 := types.BytesToAddress([]byte{0xaa})
	validator2 := types.BytesToAddress([]byte{0xbb})

	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 100,
			Address:        validator1,
			Amount:         1_000_000, // 1M Gwei = 1e15 wei = 0.001 ETH
		},
		{
			Index:          1,
			ValidatorIndex: 200,
			Address:        validator2,
			Amount:         2_000_000, // 2M Gwei = 2e15 wei = 0.002 ETH
		},
	}

	builder := block.NewBlockBuilder(config.TestConfig, bc, nil)

	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
		Withdrawals:  withdrawals,
	}

	blk, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// Verify withdrawals are in the block body.
	if blk.Withdrawals() == nil {
		t.Fatal("block should have withdrawals")
	}
	if len(blk.Withdrawals()) != 2 {
		t.Fatalf("withdrawal count = %d, want 2", len(blk.Withdrawals()))
	}

	// Verify withdrawals root is set in header.
	h := blk.Header()
	if h.WithdrawalsHash == nil {
		t.Fatal("WithdrawalsHash should be set")
	}
	if h.WithdrawalsHash.IsZero() {
		t.Error("WithdrawalsHash should not be zero")
	}

	// Verify the hash matches a recomputation.
	expectedHash := block.DeriveWithdrawalsRoot(withdrawals)
	if *h.WithdrawalsHash != expectedHash {
		t.Errorf("WithdrawalsHash mismatch: got %s, want %s",
			h.WithdrawalsHash.Hex(), expectedHash.Hex())
	}
}

// TestBuildBlock_RequestsHash tests that the block builder sets the requests
// hash in the header when Prague is active (EIP-7685).
func TestBuildBlock_RequestsHash(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesisInteg(30_000_000, big.NewInt(1000))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	builder := block.NewBlockBuilder(config.TestConfig, bc, nil)

	attrs := &block.BuildBlockAttributes{
		Timestamp:    12,
		FeeRecipient: types.BytesToAddress([]byte{0xff}),
		GasLimit:     30_000_000,
	}

	blk, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	h := blk.Header()

	// Prague is active in config.TestConfig, so requests hash should be set.
	if h.RequestsHash == nil {
		t.Fatal("RequestsHash should be set for Prague block")
	}
	if h.RequestsHash.IsZero() {
		t.Error("RequestsHash should not be zero")
	}
}

// TestReorg_Simple tests a basic chain reorganization.
func TestReorg_Simple(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesisInteg(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Build original chain: genesis -> A1 -> A2
	aBlocks := makeChainBlocksInteg(genesis, 2, statedb.Copy())
	a1, a2 := aBlocks[0], aBlocks[1]
	if err := bc.InsertBlock(a1); err != nil {
		t.Fatalf("insert A1: %v", err)
	}
	if err := bc.InsertBlock(a2); err != nil {
		t.Fatalf("insert A2: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 2 {
		t.Fatalf("head = %d, want 2", bc.CurrentBlock().NumberU64())
	}

	// Build fork chain: genesis -> B1 -> B2 -> B3 (longer)
	bState := statedb.Copy()
	b1Header := &types.Header{
		ParentHash: genesis.Hash(),
		Number:     big.NewInt(1),
		GasLimit:   genesis.GasLimit(),
		Time:       genesis.Time() + 6, // different timestamp -> different hash
		Difficulty: new(big.Int),
		BaseFee:    gas.CalcBaseFee(genesis.Header()),
		UncleHash:  block.EmptyUncleHash,
	}
	emptyWHash := types.EmptyRootHash
	b1Header.WithdrawalsHash = &emptyWHash
	zeroBlobGas := uint64(0)
	var pExcess, pUsed uint64
	if genesis.Header().ExcessBlobGas != nil {
		pExcess = *genesis.Header().ExcessBlobGas
	}
	if genesis.Header().BlobGasUsed != nil {
		pUsed = *genesis.Header().BlobGasUsed
	}
	b1ExcessBlobGas := gas.CalcExcessBlobGas(pExcess, pUsed)
	b1Header.BlobGasUsed = &zeroBlobGas
	b1Header.ExcessBlobGas = &b1ExcessBlobGas
	b1BeaconRoot := types.EmptyRootHash
	b1Header.ParentBeaconRoot = &b1BeaconRoot
	b1RequestsHash := types.EmptyRequestsHash
	b1Header.RequestsHash = &b1RequestsHash
	b1CalldataGasUsed := uint64(0)
	var pCalldataExcess, pCalldataUsed uint64
	if genesis.Header().CalldataExcessGas != nil {
		pCalldataExcess = *genesis.Header().CalldataExcessGas
	}
	if genesis.Header().CalldataGasUsed != nil {
		pCalldataUsed = *genesis.Header().CalldataGasUsed
	}
	b1CalldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, genesis.Header().GasLimit)
	b1Header.CalldataGasUsed = &b1CalldataGasUsed
	b1Header.CalldataExcessGas = &b1CalldataExcessGas
	b1Body := &types.Body{Withdrawals: []*types.Withdrawal{}}
	b1Block := types.NewBlock(b1Header, b1Body)

	proc := execution.NewStateProcessor(config.TestConfig)
	result, procErr := proc.ProcessWithBAL(b1Block, bState)
	if procErr == nil {
		b1Header.GasUsed = 0
		if result.BlockAccessList != nil {
			h := result.BlockAccessList.Hash()
			b1Header.BlockAccessListHash = &h
		}
		b1Header.Bloom = types.CreateBloom(result.Receipts)
		b1Header.ReceiptHash = block.DeriveReceiptsRoot(result.Receipts)
		b1Header.Root = bState.GetRoot()
	}
	b1Header.TxHash = block.DeriveTxsRoot(nil)
	b1 := types.NewBlock(b1Header, b1Body)

	bRest := makeChainBlocksInteg(b1, 2, bState)
	b2, b3 := bRest[0], bRest[1]

	if err := bc.InsertBlock(b1); err != nil {
		t.Logf("b1 insert (side chain): %v", err)
	}
	if err := bc.InsertBlock(b2); err != nil {
		t.Logf("b2 insert (side chain): %v", err)
	}
	if err := bc.InsertBlock(b3); err != nil {
		t.Logf("b3 insert: %v", err)
	}

	err = bc.Reorg(b3)
	if err != nil {
		t.Fatalf("Reorg: %v", err)
	}

	if bc.CurrentBlock().NumberU64() != 3 {
		t.Errorf("head = %d, want 3", bc.CurrentBlock().NumberU64())
	}
	if bc.CurrentBlock().Hash() != b3.Hash() {
		t.Errorf("head hash mismatch after reorg")
	}

	if got := bc.GetBlockByNumber(1); got == nil || got.Hash() != b1.Hash() {
		t.Errorf("canonical block 1 should be B1")
	}
	if got := bc.GetBlockByNumber(2); got == nil || got.Hash() != b2.Hash() {
		t.Errorf("canonical block 2 should be B2")
	}
	if got := bc.GetBlockByNumber(3); got == nil || got.Hash() != b3.Hash() {
		t.Errorf("canonical block 3 should be B3")
	}
}
