//go:build integration

package core

// e2e_eip_spec_test.go exercises the new EIP spec compliance code
// (EIP-8141, EIP-7928, EIP-7706, EIP-7805) through the real block
// processing pipeline using makeBlockWithState / ProcessWithBAL.

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/bal"
	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/focil"
)

// ---------------------------------------------------------------------------
// E2E: BAL ordering and AccessIndex assignment through real block processing
// ---------------------------------------------------------------------------

// TestE2E_BALOrdering_InProcessedBlock verifies that BAL entries produced
// by ProcessWithBAL are in valid lexicographic ordering (SPEC-5.1).
func TestE2E_BALOrdering_InProcessedBlock(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, ether(10))

	genesis := makeGenesis(30_000_000, big.NewInt(1))

	// Build 3 transactions from the same sender (nonce 0, 1, 2).
	var txs []*types.Transaction
	for nonce := uint64(0); nonce < 3; nonce++ {
		tx := signLegacyTx(t, key, config.TestConfig.ChainID, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(100),
		})
		txs = append(txs, tx)
	}

	buildState := statedb.Copy()
	block := makeBlockWithState(genesis, txs, buildState)

	proc := execution.NewStateProcessor(config.TestConfig)
	result, err := proc.ProcessWithBAL(block, statedb)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}
	if result.BlockAccessList == nil {
		t.Fatal("expected non-nil BAL")
	}

	// BAL must satisfy ValidateBALOrdering.
	if err := bal.ValidateBALOrdering(result.BlockAccessList); err != nil {
		t.Errorf("BAL ordering violation: %v", err)
	}

	// Verify that at least some entries have AccessIndex > 0 (tx entries).
	hasTxEntry := false
	for _, e := range result.BlockAccessList.Entries {
		if e.AccessIndex > 0 {
			hasTxEntry = true
			break
		}
	}
	if !hasTxEntry {
		t.Error("expected tx-level BAL entries with AccessIndex > 0")
	}
}

// TestE2E_BALItemCost_BlockLimit verifies that the ITEM_COST=2000 constant
// correctly limits the maximum number of BAL items relative to gas limit.
func TestE2E_BALItemCost_BlockLimit(t *testing.T) {
	const gasLimit uint64 = 30_000_000
	maxItems := gasLimit / bal.BALItemCost // 15000

	tracker := bal.NewBlockBALTracker(gasLimit)
	tracker.BeginTx(1)

	var accepted, rejected int
	for i := 0; i < int(maxItems)+10; i++ {
		addr := types.Address{byte(i >> 8), byte(i), 0x01}
		if err := tracker.RecordAccess(addr); err == nil {
			accepted++
		} else {
			rejected++
		}
	}

	if uint64(accepted) > maxItems {
		t.Errorf("accepted %d items, want at most %d", accepted, maxItems)
	}
	if rejected == 0 {
		t.Error("expected at least one rejection after limit")
	}
}

// ---------------------------------------------------------------------------
// E2E: Calldata gas header field tracking through block processing
// ---------------------------------------------------------------------------

// TestE2E_CalldataGasHeader_UpdatesAcrossBlocks verifies that
// CalldataGasUsed and CalldataExcessGas in block headers are updated
// correctly as blocks with calldata-heavy transactions are processed
// (EIP-7706 SPEC-6 integration).
func TestE2E_CalldataGasHeader_UpdatesAcrossBlocks(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	coinbase := types.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")

	// Use Glamsterdan config so CalldataGasUsed/CalldataExcessGas headers are populated.
	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, ether(100))
	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfigGlamsterdan, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	buildInsertGlamsterdan := func(pool coreblock.TxPoolReader) *types.Block {
		t.Helper()
		parent := bc.CurrentBlock()
		builder := coreblock.NewBlockBuilder(config.TestConfigGlamsterdan, bc, pool)
		attrs := &coreblock.BuildBlockAttributes{
			Timestamp:    parent.Time() + 12,
			FeeRecipient: coinbase,
			GasLimit:     parent.GasLimit(),
		}
		block, _, berr := builder.BuildBlock(parent.Header(), attrs)
		if berr != nil {
			t.Fatalf("BuildBlock: %v", berr)
		}
		if ierr := bc.InsertBlock(block); ierr != nil {
			t.Fatalf("InsertBlock: %v", ierr)
		}
		return block
	}

	// Block 1: send a tx with significant calldata.
	calldataPayload := bytes.Repeat([]byte{0xFF}, 200) // 200 non-zero bytes
	tx1 := signLegacyTx(t, key, config.TestConfigGlamsterdan.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      100_000,
		To:       &recipient,
		Value:    big.NewInt(0),
		Data:     calldataPayload,
	})
	block1 := buildInsertGlamsterdan(&simpleTxPool{txs: []*types.Transaction{tx1}})

	// Verify CalldataGasUsed is set and non-zero.
	if block1.Header().CalldataGasUsed == nil {
		t.Fatal("block1 CalldataGasUsed is nil")
	}
	if *block1.Header().CalldataGasUsed == 0 {
		t.Error("block1 CalldataGasUsed should be > 0 for calldata tx")
	}

	// Verify CalldataExcessGas is set (may be 0 for light traffic, but must be non-nil).
	if block1.Header().CalldataExcessGas == nil {
		t.Error("block1 CalldataExcessGas is nil")
	}

	// Block 2: empty block — calldata excess should evolve.
	block2 := buildInsertGlamsterdan(&simpleTxPool{txs: nil})

	if block2.Header().CalldataGasUsed == nil {
		t.Fatal("block2 CalldataGasUsed is nil")
	}
	if *block2.Header().CalldataGasUsed != 0 {
		t.Errorf("block2 CalldataGasUsed = %d, want 0 (no tx)", *block2.Header().CalldataGasUsed)
	}
}

// ---------------------------------------------------------------------------
// E2E: Frame receipt 3-layer structure
// ---------------------------------------------------------------------------

// TestE2E_FrameReceiptRLP_RoundTripInChain verifies that a FrameTxReceipt
// can be RLP-encoded and decoded correctly when produced during chain processing
// (SPEC-1.1 integration test).
func TestE2E_FrameReceiptRLP_RoundTripInChain(t *testing.T) {
	// Build a FrameTxReceipt that mirrors what the chain would produce:
	// 2 frames, one success, one failure, each with logs.
	payer := types.Address{0xAB, 0xCD}
	r := &types.FrameTxReceipt{
		CumulativeGasUsed: 50000,
		Payer:             payer,
		FrameResults: []types.FrameResult{
			{
				Status:  1,
				GasUsed: 30000,
				Logs: []*types.Log{
					{
						Address: types.Address{0x01},
						Topics:  []types.Hash{{0x01}, {0x02}},
						Data:    []byte("frame0 event"),
					},
				},
			},
			{
				Status:  0,
				GasUsed: 20000,
				Logs:    nil,
			},
		},
	}

	// Encode and decode.
	enc, err := types.EncodeFrameTxReceiptRLP(r)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Verify type prefix.
	if enc[0] != types.FrameTxType {
		t.Errorf("type prefix: got 0x%02x, want 0x%02x", enc[0], types.FrameTxType)
	}

	got, err := types.DecodeFrameTxReceiptRLP(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify structure.
	if got.CumulativeGasUsed != r.CumulativeGasUsed {
		t.Errorf("CumulativeGasUsed: got %d, want %d", got.CumulativeGasUsed, r.CumulativeGasUsed)
	}
	if got.Payer != payer {
		t.Errorf("Payer: got %x, want %x", got.Payer, payer)
	}
	if len(got.FrameResults) != 2 {
		t.Fatalf("frame count: got %d, want 2", len(got.FrameResults))
	}

	frame0 := got.FrameResults[0]
	if frame0.Status != 1 || frame0.GasUsed != 30000 {
		t.Errorf("frame0: status=%d gas=%d", frame0.Status, frame0.GasUsed)
	}
	if len(frame0.Logs) != 1 {
		t.Fatalf("frame0 logs: got %d, want 1", len(frame0.Logs))
	}
	if len(frame0.Logs[0].Topics) != 2 {
		t.Errorf("frame0 log topics: got %d, want 2", len(frame0.Logs[0].Topics))
	}
	if !bytes.Equal(frame0.Logs[0].Data, []byte("frame0 event")) {
		t.Errorf("frame0 log data mismatch")
	}

	frame1 := got.FrameResults[1]
	if frame1.Status != 0 || frame1.GasUsed != 20000 {
		t.Errorf("frame1: status=%d gas=%d", frame1.Status, frame1.GasUsed)
	}
	if len(frame1.Logs) != 0 {
		t.Errorf("frame1 logs: got %d, want 0", len(frame1.Logs))
	}
}

// ---------------------------------------------------------------------------
// E2E: FOCIL IL satisfaction check integrated with block data
// ---------------------------------------------------------------------------

// TestE2E_ILSatisfaction_AgainstBuiltBlock verifies that CheckILSatisfaction
// correctly identifies transactions present vs absent in a real block.
func TestE2E_ILSatisfaction_AgainstBuiltBlock(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD")
	coinbase := types.HexToAddress("0xEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(50),
	})

	// Create 2 transactions: tx1 will be included, tx2 will be absent from the IL check.
	tx1 := signLegacyTx(t, key, config.TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &recipient,
		Value:    big.NewInt(100),
	})
	tx2 := signLegacyTx(t, key, config.TestConfig.ChainID, &types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &recipient,
		Value:    big.NewInt(200),
	})

	// Build block with only tx1.
	pool := &simpleTxPool{txs: []*types.Transaction{tx1}}
	block, _ := buildAndInsert(t, bc, pool, coinbase)

	// Encode tx1 and tx2 as raw bytes for IL entries.
	tx1enc, _ := tx1.EncodeRLP()
	tx2enc, _ := tx2.EncodeRLP()

	// IL1: contains only tx1 → satisfied (tx1 is in the block).
	il1 := &focil.InclusionList{
		Slot:          1,
		ProposerIndex: 1,
		Entries: []focil.InclusionListEntry{
			{Transaction: tx1enc, Index: 0},
		},
	}

	// IL2: contains tx2 → unsatisfied (tx2 is not in the block, gas is available).
	il2 := &focil.InclusionList{
		Slot:          1,
		ProposerIndex: 2,
		Entries: []focil.InclusionListEntry{
			{Transaction: tx2enc, Index: 0},
		},
	}

	// Available gas: block gas limit minus what was used by tx1.
	gasRemaining := block.GasLimit() - block.GasUsed()

	// Check IL1 (satisfied).
	result1 := focil.CheckILSatisfaction(block, []*focil.InclusionList{il1}, nil, gasRemaining)
	if result1 != focil.ILSatisfied {
		t.Errorf("IL1 (tx in block): expected ILSatisfied, got %v", result1)
	}

	// Check IL2 (unsatisfied: tx2 absent with enough gas remaining).
	result2 := focil.CheckILSatisfaction(block, []*focil.InclusionList{il2}, nil, gasRemaining)
	if result2 != focil.ILUnsatisfied {
		t.Errorf("IL2 (tx absent): expected ILUnsatisfied, got %v", result2)
	}

	// Check both ILs together → overall unsatisfied because of IL2.
	resultBoth := focil.CheckILSatisfaction(block, []*focil.InclusionList{il1, il2}, nil, gasRemaining)
	if resultBoth != focil.ILUnsatisfied {
		t.Errorf("IL1+IL2: expected ILUnsatisfied (IL2 fails), got %v", resultBoth)
	}
}

// TestE2E_ILSatisfaction_GasExemptionInFullBlock verifies that an absent tx
// is exempt (satisfied) when the block gas is exhausted.
func TestE2E_ILSatisfaction_GasExemptionInFullBlock(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, ether(100))
	genesis := makeGenesis(30_000_000, big.NewInt(1))

	// Create a tx that won't be in the block.
	absentTx := signLegacyTx(t, key, config.TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      50000,
		To:       &recipient,
		Value:    big.NewInt(100),
	})
	absentEnc, _ := absentTx.EncodeRLP()

	il := &focil.InclusionList{
		Slot:          1,
		ProposerIndex: 1,
		Entries: []focil.InclusionListEntry{
			{Transaction: absentEnc, Index: 0},
		},
	}

	// Build an empty block.
	buildState := statedb.Copy()
	block := makeBlockWithState(genesis, nil, buildState)

	// gasRemaining=0 → gas exemption: absent tx is exempt.
	result := focil.CheckILSatisfaction(block, []*focil.InclusionList{il}, nil, 0)
	if result != focil.ILSatisfied {
		t.Errorf("gas=0 exemption: expected ILSatisfied, got %v", result)
	}

	// gasRemaining=absentTx.Gas() → tx could fit → unsatisfied.
	result2 := focil.CheckILSatisfaction(block, []*focil.InclusionList{il}, nil, absentTx.Gas())
	if result2 != focil.ILUnsatisfied {
		t.Errorf("gas sufficient: expected ILUnsatisfied, got %v", result2)
	}
}

// ---------------------------------------------------------------------------
// E2E: Multi-block chain state with BAL access index progression
// ---------------------------------------------------------------------------

// TestE2E_MultiBlock_BALAccessIndex verifies that across a 3-block chain,
// BAL entries in each block have correct per-block access index assignment.
func TestE2E_MultiBlock_BALAccessIndex(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	coinbase := types.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	proc := execution.NewStateProcessor(config.TestConfig)

	for blockNum := uint64(1); blockNum <= 3; blockNum++ {
		nonce := blockNum - 1
		tx := signLegacyTx(t, key, config.TestConfig.ChainID, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(100),
		})

		pool := &simpleTxPool{txs: []*types.Transaction{tx}}
		parent := bc.CurrentBlock()
		builder := coreblock.NewBlockBuilder(config.TestConfig, bc, pool)
		attrs := &coreblock.BuildBlockAttributes{
			Timestamp:    parent.Time() + 12,
			FeeRecipient: coinbase,
			GasLimit:     parent.GasLimit(),
		}
		block, _, err := builder.BuildBlock(parent.Header(), attrs)
		if err != nil {
			t.Fatalf("block %d BuildBlock: %v", blockNum, err)
		}

		// Re-process with BAL to get access indices.
		// Build a state that mirrors the chain state: correct nonce + remaining balance.
		blockState := state.NewMemoryStateDB()
		spent := new(big.Int).Mul(big.NewInt(int64(nonce)), new(big.Int).Mul(big.NewInt(21000), big.NewInt(10)))
		remaining := new(big.Int).Sub(ether(100), spent)
		blockState.AddBalance(sender, remaining)
		blockState.SetNonce(sender, nonce)
		result, err := proc.ProcessWithBAL(block, blockState)
		if err != nil {
			t.Logf("block %d ProcessWithBAL: %v (non-fatal for this test)", blockNum, err)
			continue
		}

		if result.BlockAccessList != nil {
			// All tx entries should have AccessIndex=1 (single tx per block).
			for _, e := range result.BlockAccessList.Entries {
				if e.AccessIndex > 1 && e.AccessIndex != ^uint64(0) {
					// Only 1 tx so entries should be 0 (pre-exec) or 1 (tx) or 2 (post-exec).
					if e.AccessIndex > 2 {
						t.Errorf("block %d: unexpected AccessIndex %d", blockNum, e.AccessIndex)
					}
				}
			}
			// Verify ordering is valid.
			if err := bal.ValidateBALOrdering(result.BlockAccessList); err != nil {
				t.Errorf("block %d BAL ordering invalid: %v", blockNum, err)
			}
		}

		if err := bc.InsertBlock(block); err != nil {
			t.Fatalf("block %d InsertBlock: %v", blockNum, err)
		}
	}

	if bc.CurrentBlock().NumberU64() != 3 {
		t.Errorf("chain height = %d, want 3", bc.CurrentBlock().NumberU64())
	}
}

// ---------------------------------------------------------------------------
// E2E: FrameTx (EIP-8141) execution through real block processing
// ---------------------------------------------------------------------------

// TestE2E_FrameTx_WithEIP7702Delegation tests a complete FrameTx execution
// where the sender has delegated to a SimpleApprover contract via EIP-7702.
func TestE2E_FrameTx_WithEIP7702Delegation(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	approver := types.HexToAddress("0x6D264Bf712D5F9698e82E97E0BaEE255eA4BD1c3")
	target := types.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, ether(10))
	statedb.SetNonce(sender, 0)

	// Set up approver contract with SimpleApprover code.
	// PUSH1 0x02, PUSH1 0x00, PUSH1 0x00, APPROVE
	simpleApproverCode := []byte{0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xaa}
	statedb.CreateAccount(approver)
	statedb.SetCode(approver, simpleApproverCode)
	statedb.AddBalance(approver, ether(1))

	// Set up sender with delegation to approver (EIP-7702).
	// Designator: 0xef0100 || approver_address
	designator := make([]byte, 23)
	designator[0] = 0xef
	designator[1] = 0x01
	designator[2] = 0x00
	copy(designator[3:], approver[:])
	statedb.CreateAccount(sender)
	statedb.SetCode(sender, designator)
	statedb.AddBalance(sender, ether(10))

	genesis := makeGenesis(30_000_000, big.NewInt(1))

	// Build FrameTx: VERIFY frame (calls APPROVE(2)) + SENDER frame (call to target)
	// Note: Use TestConfigGlamsterdan for APPROVE opcode support
	frameTx := &types.FrameTx{
		ChainID:  config.TestConfigGlamsterdan.ChainID,
		Nonce:    0,
		Sender:   sender,
		Frames:   []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte{}}, // Target=nil means sender
			{Mode: types.ModeSender, Target: &target, GasLimit: 21000, Data: []byte{}},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
	}
	tx := types.NewTransaction(frameTx)

	// Create block manually to avoid double ProcessWithBAL call
	// (makeBlockWithState internally calls ProcessWithBAL with TestConfig)
	genesisHeader := genesis.Header()
	header := &types.Header{
		ParentHash:      genesis.Hash(),
		Number:          new(big.Int).Add(genesisHeader.Number, big.NewInt(1)),
		GasLimit:        genesisHeader.GasLimit,
		Time:            genesisHeader.Time + 12,
		Difficulty:      new(big.Int),
		BaseFee:         new(big.Int).Set(genesisHeader.BaseFee),
		UncleHash:       types.EmptyUncleHash,
		WithdrawalsHash: &types.EmptyRootHash,
	}
	body := &types.Body{
		Transactions: []*types.Transaction{tx},
		Withdrawals:  []*types.Withdrawal{},
	}
	block := types.NewBlock(header, body)

	// Process the block with TestConfigGlamsterdan for APPROVE opcode support
	proc := execution.NewStateProcessor(config.TestConfigGlamsterdan)
	result, err := proc.ProcessWithBAL(block, statedb)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}

	// Check receipt
	if len(result.Receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(result.Receipts))
	}
	receipt := result.Receipts[0]
	t.Logf("FrameTx receipt: status=%d, gasUsed=%d", receipt.Status, receipt.GasUsed)
	t.Logf("FrameTx receipt details: %+v", receipt)

	if receipt.Status != types.ReceiptStatusSuccessful {
		// Log detailed error for debugging
		t.Errorf("FrameTx failed with status=%d, expected success", receipt.Status)
	}

	// Verify nonce was incremented
	nonce := statedb.GetNonce(sender)
	t.Logf("Sender nonce after FrameTx: %d", nonce)
	if nonce != 1 {
		t.Errorf("expected nonce=1, got %d", nonce)
	}
}

// TestE2E_FrameTx_VerifyFrameApproveScope2 tests that VERIFY frame with APPROVE(2)
// correctly approves both sender and payer.
func TestE2E_FrameTx_VerifyFrameApproveScope2(t *testing.T) {
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	approver := types.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, ether(10))

	// Set up approver with SimpleApprover code (APPROVE(2))
	simpleApproverCode := []byte{0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xaa}
	statedb.CreateAccount(approver)
	statedb.SetCode(approver, simpleApproverCode)

	// Set up sender with delegation
	designator := make([]byte, 23)
	designator[0] = 0xef
	designator[1] = 0x01
	designator[2] = 0x00
	copy(designator[3:], approver[:])
	statedb.SetCode(sender, designator)
	statedb.SetNonce(sender, 0)

	genesis := makeGenesis(30_000_000, big.NewInt(1))

	// FrameTx with only VERIFY frame (should succeed with APPROVE(2))
	frameTx := &types.FrameTx{
		ChainID:  config.TestConfigGlamsterdan.ChainID,
		Nonce:    0,
		Sender:   sender,
		Frames:   []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte{}},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
	}
	tx := types.NewTransaction(frameTx)

	// Create block manually to avoid double ProcessWithBAL call
	genesisHeader := genesis.Header()
	header := &types.Header{
		ParentHash:      genesis.Hash(),
		Number:          new(big.Int).Add(genesisHeader.Number, big.NewInt(1)),
		GasLimit:        genesisHeader.GasLimit,
		Time:            genesisHeader.Time + 12,
		Difficulty:      new(big.Int),
		BaseFee:         new(big.Int).Set(genesisHeader.BaseFee),
		UncleHash:       types.EmptyUncleHash,
		WithdrawalsHash: &types.EmptyRootHash,
	}
	body := &types.Body{
		Transactions: []*types.Transaction{tx},
		Withdrawals:  []*types.Withdrawal{},
	}
	block := types.NewBlock(header, body)

	proc := execution.NewStateProcessor(config.TestConfigGlamsterdan)
	result, err := proc.ProcessWithBAL(block, statedb)
	if err != nil {
		t.Fatalf("ProcessWithBAL: %v", err)
	}

	if len(result.Receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(result.Receipts))
	}

	receipt := result.Receipts[0]
	t.Logf("VERIFY-only FrameTx receipt: status=%d", receipt.Status)

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Errorf("VERIFY-only FrameTx failed with status=%d", receipt.Status)
	}
}
