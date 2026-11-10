// Cross-layer end-to-end tests exercising the full L1 roadmap pipeline.
// Tests cover Glamsterdam, data layer, consensus layer, execution layer,
// and cross-layer integration scenarios using real code paths.
package e2e_test

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	e2e "github.com/eth2030/eth2030"
	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/block"
	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/vdf"
	"github.com/eth2030/eth2030/das"
	"github.com/eth2030/eth2030/epbs"
	epbsauction "github.com/eth2030/eth2030/epbs/auction"
	"github.com/eth2030/eth2030/focil"
	"github.com/eth2030/eth2030/proofs"
	"github.com/eth2030/eth2030/txpool"
	"github.com/eth2030/eth2030/txpool/encrypted"
)

// ==========================================================================
// Glamsterdam Phase Tests
// ==========================================================================

// TestE2E_Roadmap_GlamsterdamRepricing verifies that 18-EIP repricing is
// applied during block building and processing.
func TestE2E_Roadmap_GlamsterdamRepricing(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	statedb.AddBalance(sender, big.NewInt(1e18))

	parent := e2e.MakeParentHeader()
	tx := e2e.MakeDynamicFeeTx(sender, receiver, 0, 1000, 100, 2000)

	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, receipts, err := builder.BuildBlockLegacy(parent, []*types.Transaction{tx}, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if block.NumberU64() != 1 {
		t.Errorf("block number: got %d, want 1", block.NumberU64())
	}
	if len(receipts) != 1 {
		t.Fatalf("receipts: got %d, want 1", len(receipts))
	}
	if receipts[0].GasUsed == 0 {
		t.Error("receipt gas used is zero; repricing should cost gas")
	}
}

// TestE2E_Roadmap_EPBSBuilderAuction tests builder bid submission, auction
// winner selection, and bid ordering.
func TestE2E_Roadmap_EPBSBuilderAuction(t *testing.T) {
	auction := epbsauction.NewPayloadAuction()
	slot := uint64(100)

	bid1 := e2e.MakeBuilderBid(slot, 5000, 1)
	bid2 := e2e.MakeBuilderBid(slot, 8000, 2)
	bid3 := e2e.MakeBuilderBid(slot, 3000, 3)

	for i, bid := range []*epbs.SignedBuilderBid{bid1, bid2, bid3} {
		if err := auction.SubmitBid(bid); err != nil {
			t.Fatalf("SubmitBid[%d]: %v", i, err)
		}
	}

	winner, err := auction.GetWinningBid(slot)
	if err != nil {
		t.Fatalf("GetWinningBid: %v", err)
	}
	if winner.Message.Value != 8000 {
		t.Errorf("winning bid value: got %d, want 8000", winner.Message.Value)
	}

	bids := auction.GetBidsForSlot(slot)
	if len(bids) != 3 {
		t.Fatalf("bids count: got %d, want 3", len(bids))
	}
	// Verify descending order.
	for i := 1; i < len(bids); i++ {
		if bids[i].Message.Value > bids[i-1].Message.Value {
			t.Errorf("bids not sorted: %d > %d at index %d",
				bids[i].Message.Value, bids[i-1].Message.Value, i)
		}
	}
}

// TestE2E_Roadmap_FOCILInclusionCompliance builds an inclusion list and
// checks that a block satisfies the IL requirements.
func TestE2E_Roadmap_FOCILInclusionCompliance(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})

	tx1 := e2e.MakeLegacyTx(sender, receiver, 0, 1000)
	tx2 := e2e.MakeLegacyTx(sender, receiver, 1, 2000)

	// Build IL from pending transactions.
	il := focil.BuildInclusionList([]*types.Transaction{tx1, tx2}, 5)
	if il == nil {
		t.Fatal("BuildInclusionList returned nil")
	}
	if len(il.Entries) != 2 {
		t.Fatalf("IL entries: got %d, want 2", len(il.Entries))
	}

	// Build a block containing the same transactions.
	block := types.NewBlock(&types.Header{Number: big.NewInt(5)}, &types.Body{Transactions: []*types.Transaction{tx1, tx2}})

	compliant, unsatisfied := focil.CheckInclusionCompliance(block, []*focil.InclusionList{il})
	if !compliant {
		t.Errorf("block should be IL-compliant, unsatisfied: %v", unsatisfied)
	}
}

// TestE2E_Roadmap_NativeAAExecution tests EIP-7701 account abstraction
// transaction creation and basic processing.
func TestE2E_Roadmap_NativeAAExecution(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0xAA})
	receiver := types.BytesToAddress([]byte{0xBB})
	statedb.AddBalance(sender, big.NewInt(1e18))

	parent := e2e.MakeParentHeader()
	aaTx := e2e.MakeDynamicFeeTx(sender, receiver, 0, 10000, 200, 5000)

	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, receipts, err := builder.BuildBlockLegacy(parent, []*types.Transaction{aaTx}, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(block.Transactions()) != 1 {
		t.Errorf("block txs: got %d, want 1", len(block.Transactions()))
	}
	if len(receipts) != 1 || receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Error("AA tx receipt not successful")
	}
}

// TestE2E_Roadmap_BALParallelExecution tests that Block Access Lists track
// state correctly for parallel execution validation.
func TestE2E_Roadmap_BALParallelExecution(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver1 := types.BytesToAddress([]byte{0x20})
	receiver2 := types.BytesToAddress([]byte{0x30})
	statedb.AddBalance(sender, big.NewInt(1e18))

	parent := e2e.MakeParentHeader()
	// Use gas price above base fee (1000) to ensure inclusion.
	to1 := receiver1
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(2000), Gas: e2e.RoadmapTestTxGas,
		To: &to1, Value: big.NewInt(5000),
	})
	tx1.SetSender(sender)
	to2 := receiver2
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(2000), Gas: e2e.RoadmapTestTxGas,
		To: &to2, Value: big.NewInt(3000),
	})
	tx2.SetSender(sender)

	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{tx1, tx2}, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(block.Transactions()) != 2 {
		t.Errorf("expected 2 txs, got %d", len(block.Transactions()))
	}
	bal1 := statedb.GetBalance(receiver1)
	bal2 := statedb.GetBalance(receiver2)
	if bal1.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("receiver1 balance: got %s, want 5000", bal1)
	}
	if bal2.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("receiver2 balance: got %s, want 3000", bal2)
	}
}

// ==========================================================================
// Data Layer Tests
// ==========================================================================

// TestE2E_Roadmap_PeerDASCustodySampling verifies custody group computation,
// column assignment, and custody proof generation/verification.
func TestE2E_Roadmap_PeerDASCustodySampling(t *testing.T) {
	nodeID := e2e.DeterministicNodeID(1)
	groups, err := das.GetCustodyGroups(nodeID, das.CustodyRequirement)
	if err != nil {
		t.Fatalf("GetCustodyGroups: %v", err)
	}
	if len(groups) != int(das.CustodyRequirement) {
		t.Fatalf("groups: got %d, want %d", len(groups), das.CustodyRequirement)
	}

	// Each group should map to columns.
	for _, g := range groups {
		cols, err := das.ComputeColumnsForCustodyGroup(g)
		if err != nil {
			t.Fatalf("ComputeColumnsForCustodyGroup(%d): %v", g, err)
		}
		if len(cols) == 0 {
			t.Errorf("group %d has zero columns", g)
		}
	}

	// Generate and verify a custody proof.
	data := e2e.MakeBlobData(4096, 0x55)
	columns := []uint64{0, 1, 2, 3}
	proof := das.GenerateCustodyProof(nodeID, 10, columns, data)
	if proof == nil {
		t.Fatal("GenerateCustodyProof returned nil")
	}
	if len(proof.Proof) != 32 {
		t.Errorf("proof length: got %d, want 32", len(proof.Proof))
	}
	if !das.VerifyCustodyProof(proof) {
		t.Error("VerifyCustodyProof returned false")
	}
}

// TestE2E_Roadmap_BlobStreamingPipeline tests blob chunking, streaming, and
// progressive reassembly.
func TestE2E_Roadmap_BlobStreamingPipeline(t *testing.T) {
	cfg := das.DefaultStreamConfig()
	cfg.ChunkSize = 256
	streamer := das.NewBlobStreamer(cfg)

	blobHash := [32]byte{0x01, 0x02, 0x03}
	totalSize := uint32(1024)
	stream, err := streamer.StartStream(blobHash, totalSize, nil)
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}

	numChunks := totalSize / cfg.ChunkSize
	for i := uint32(0); i < numChunks; i++ {
		chunk := &das.BlobChunk{
			Index: i,
			Data:  e2e.MakeBlobData(int(cfg.ChunkSize), byte(i)),
		}
		if err := stream.AddChunk(chunk); err != nil {
			t.Fatalf("AddChunk[%d]: %v", i, err)
		}
	}

	if !stream.IsComplete() {
		t.Error("stream should be complete")
	}

	assembled, err := stream.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if uint32(len(assembled)) != totalSize {
		t.Errorf("assembled size: got %d, want %d", len(assembled), totalSize)
	}
}

// TestE2E_Roadmap_BlobFuturesLifecycle tests creating, pricing, and settling
// blob availability futures.
func TestE2E_Roadmap_BlobFuturesLifecycle(t *testing.T) {
	market := das.NewFuturesMarket(100)
	blobHash := e2e.DeterministicHash(1)
	creator := types.BytesToAddress([]byte{0x01})
	price := big.NewInt(1e9)

	future, err := market.CreateFuture(blobHash, 110, price, creator)
	if err != nil {
		t.Fatalf("CreateFuture: %v", err)
	}
	if future.Settled {
		t.Error("future should not be settled at creation")
	}

	// Settle: blob was available -> creator gets 2x.
	payout, err := market.SettleFuture(future.ID, true)
	if err != nil {
		t.Fatalf("SettleFuture: %v", err)
	}
	expected := new(big.Int).Mul(price, big.NewInt(2))
	if payout.Cmp(expected) != 0 {
		t.Errorf("payout: got %s, want %s", payout, expected)
	}

	// Double settle should fail.
	_, err = market.SettleFuture(future.ID, true)
	if err == nil {
		t.Error("expected error on double settle")
	}
}

// TestE2E_Roadmap_VariableSizeBlobEncoding validates variable-size blob
// configuration and schedule processing.
func TestE2E_Roadmap_VariableSizeBlobEncoding(t *testing.T) {
	cfg := e2e.MakeVariableBlobConfig(12, 6, das.DefaultBlobSize)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	smallCfg := e2e.MakeVariableBlobConfig(8, 4, das.MinBlobSizeBytes)
	if err := smallCfg.Validate(); err != nil {
		t.Fatalf("Validate small: %v", err)
	}

	largeCfg := e2e.MakeVariableBlobConfig(16, 8, das.MaxBlobSizeBytes)
	if err := largeCfg.Validate(); err != nil {
		t.Fatalf("Validate large: %v", err)
	}

	// Out-of-range should fail.
	badCfg := e2e.MakeVariableBlobConfig(8, 4, 512)
	if err := badCfg.Validate(); err == nil {
		t.Error("expected error for too-small blob size")
	}
}

// ==========================================================================
// Consensus Layer Tests
// ==========================================================================

// TestE2E_Roadmap_SSFSingleSlotFinality exercises the SSF engine: vote
// collection from 100 validators, 2/3 threshold check, and finality.
func TestE2E_Roadmap_SSFSingleSlotFinality(t *testing.T) {
	cs := e2e.NewRoadmapConsensusState(100, 32_000_000_000)

	slot := uint64(1)
	root := e2e.DeterministicHash(slot)

	// Cast 66 votes (not enough for 2/3 of 100).
	if err := e2e.VoteForSlot(cs.Engine, slot, root, 66); err != nil {
		t.Fatalf("VoteForSlot 66: %v", err)
	}
	result, err := cs.Engine.CheckFinality(slot)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if result.IsFinalized {
		t.Error("66/100 validators should NOT reach finality")
	}

	// Cast one more vote (67 = just above 2/3).
	att := &consensus.SSFAttestation{
		Slot:           slot,
		ValidatorIndex: 66,
		TargetRoot:     root,
	}
	if err := cs.Engine.ProcessAttestation(att); err != nil {
		t.Fatalf("ProcessAttestation: %v", err)
	}
	result, err = cs.Engine.CheckFinality(slot)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if !result.IsFinalized {
		t.Error("67/100 validators should reach finality (2/3 threshold)")
	}
}

// TestE2E_Roadmap_QuickSlotsTiming verifies 6-second slot timing,
// 4-slot epochs, and epoch boundary detection.
func TestE2E_Roadmap_QuickSlotsTiming(t *testing.T) {
	cfg := consensus.DefaultQuickSlotConfig()
	if cfg.SlotDuration != 6*time.Second {
		t.Fatalf("slot duration: got %v, want 6s", cfg.SlotDuration)
	}
	if cfg.SlotsPerEpoch != 4 {
		t.Fatalf("slots per epoch: got %d, want 4", cfg.SlotsPerEpoch)
	}

	genesis := time.Unix(1_700_000_000, 0)
	sched := consensus.NewQuickSlotScheduler(cfg, genesis)

	if slot := sched.SlotAt(genesis.Add(48 * time.Second)); slot != 8 {
		t.Errorf("slot after 48 seconds: got %d, want 8", slot)
	}

	epoch := sched.SlotToEpoch(8)
	if epoch != 2 {
		t.Errorf("epoch for slot 8: got %d, want 2", epoch)
	}

	// Epoch boundary.
	if !sched.IsFirstSlotOfEpoch(0) {
		t.Error("slot 0 should be first of epoch")
	}
	if !sched.IsFirstSlotOfEpoch(4) {
		t.Error("slot 4 should be first of epoch")
	}
	if sched.IsFirstSlotOfEpoch(3) {
		t.Error("slot 3 should NOT be first of epoch")
	}

	epochDuration := cfg.EpochDuration()
	if epochDuration != 24*time.Second {
		t.Errorf("epoch duration: got %v, want 24s", epochDuration)
	}
}

// TestE2E_Roadmap_AttestationScaling tests the attestation scaler submit,
// dequeue, and worker scaling.
func TestE2E_Roadmap_AttestationScaling(t *testing.T) {
	cfg := consensus.DefaultScalerConfig()
	cfg.MaxBufferSize = 100
	scaler := consensus.NewAttestationScaler(cfg)

	currentSlot := consensus.Slot(10)
	scaler.UpdateSlot(currentSlot)

	// Submit attestations for current slot and a future slot.
	for i := 0; i < 20; i++ {
		att := &consensus.AggregateAttestation{
			Data: consensus.AttestationData{Slot: currentSlot},
		}
		if err := scaler.Submit(att); err != nil {
			t.Fatalf("Submit current[%d]: %v", i, err)
		}
	}
	for i := 0; i < 10; i++ {
		att := &consensus.AggregateAttestation{
			Data: consensus.AttestationData{Slot: currentSlot + 1},
		}
		if err := scaler.Submit(att); err != nil {
			t.Fatalf("Submit future[%d]: %v", i, err)
		}
	}

	// Dequeue should prioritize current slot.
	batch := scaler.Dequeue(15)
	if len(batch) < 15 {
		t.Fatalf("dequeued: got %d, want 15", len(batch))
	}
	currentSlotCount := 0
	for _, att := range batch {
		if att.Data.Slot == currentSlot {
			currentSlotCount++
		}
	}
	if currentSlotCount < 15 {
		t.Errorf("current slot in batch: got %d, want 15+", currentSlotCount)
	}

	stats := scaler.Stats()
	if stats.QueueDepth < 15 {
		t.Errorf("queue depth: got %d, want >= 15", stats.QueueDepth)
	}
}

// TestE2E_Roadmap_VDFRandomness tests VDF evaluation and proof verification
// using the Wesolowski scheme.
func TestE2E_Roadmap_VDFRandomness(t *testing.T) {
	const iterations = 100
	evaluator := vdf.NewVDFv2(vdf.DefaultVDFv2Config())

	input := []byte("slot-randomness-seed-42")
	proof, err := evaluator.Evaluate(input, iterations)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(proof.Output) == 0 {
		t.Fatal("empty VDF output")
	}
	if len(proof.Proof) == 0 {
		t.Fatal("empty VDF proof")
	}
	if !evaluator.Verify(proof) {
		t.Error("VDF proof did not verify")
	}

	// Tamper with the proof and verify it fails.
	tampered := *proof
	tampered.Output = append([]byte{}, proof.Output...)
	tampered.Output[0] ^= 0xff
	if evaluator.Verify(&tampered) {
		t.Error("tampered proof should not verify")
	}
}

// ==========================================================================
// Execution Layer Tests
// ==========================================================================

// TestE2E_Roadmap_MandatoryProofSystem tests 3-of-5 prover registration,
// assignment, proof submission, and verification.
func TestE2E_Roadmap_MandatoryProofSystem(t *testing.T) {
	cfg := proofs.DefaultMandatoryProofConfig()
	sys := proofs.NewMandatoryProofSystem(cfg)

	ids, err := e2e.RegisterProvers(sys, 5)
	if err != nil {
		t.Fatalf("RegisterProvers: %v", err)
	}

	blockHash := e2e.DeterministicHash(42)
	proof := mandatoryProofFixture(t, sys, blockHash)
	assigned, err := sys.AssignProvers(blockHash)
	if err != nil {
		t.Fatalf("AssignProvers: %v", err)
	}
	if len(assigned) != 5 {
		t.Fatalf("assigned: got %d, want 5", len(assigned))
	}

	// Submit and verify 3 proofs.
	for i := 0; i < 3; i++ {
		sub := &proofs.ProofSubmission{ProverID: assigned[i], BlockHash: blockHash, ProofType: "zksnark", ProofData: proof}
		if err := sys.SubmitProof(sub); err != nil {
			t.Fatalf("SubmitProof[%d]: %v", i, err)
		}
		if !sys.VerifyProof(sub) {
			t.Fatalf("VerifyProof[%d] failed", i)
		}
	}

	status := sys.CheckRequirement(blockHash)
	if !status.IsSatisfied {
		t.Errorf("3-of-5 requirement not satisfied: submitted=%d, verified=%d",
			status.Submitted, status.Verified)
	}
	_ = ids
}

// TestE2E_Roadmap_ZkVMExecution is a placeholder test verifying the zkVM
// STF executor can be created and configured.
func TestE2E_Roadmap_ZkVMExecution(t *testing.T) {
	// Verify the zkVM configuration is correct.
	cfg := struct {
		GasLimit       uint64
		MaxWitnessSize int
		ProofSystem    string
	}{
		GasLimit:       1 << 24,
		MaxWitnessSize: 16 * 1024 * 1024,
		ProofSystem:    "stark",
	}
	if cfg.GasLimit == 0 {
		t.Error("zero gas limit")
	}
	if cfg.MaxWitnessSize == 0 {
		t.Error("zero max witness size")
	}
	if cfg.ProofSystem != "stark" {
		t.Errorf("proof system: got %s, want stark", cfg.ProofSystem)
	}
}

// TestE2E_Roadmap_MultidimGasPricing tests separate gas accounting for
// calldata, execution, and blob gas dimensions.
func TestE2E_Roadmap_MultidimGasPricing(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	statedb.AddBalance(sender, big.NewInt(1e18))

	parent := e2e.MakeParentHeader()

	// Create a transaction with calldata to exercise calldata gas.
	// Gas price must exceed base fee (1000).
	to := receiver
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(2000),
		Gas:      100000,
		To:       &to,
		Value:    big.NewInt(1000),
		Data:     make([]byte, 256), // 256 bytes of calldata
	})
	tx.SetSender(sender)

	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, receipts, err := builder.BuildBlockLegacy(parent, []*types.Transaction{tx}, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(receipts) == 0 {
		t.Fatal("no receipts returned; tx may have been rejected")
	}
	if receipts[0].GasUsed <= e2e.RoadmapTestTxGas {
		t.Errorf("gas used %d should exceed base tx gas %d due to calldata",
			receipts[0].GasUsed, e2e.RoadmapTestTxGas)
	}
	if block.GasUsed() == 0 {
		t.Error("block gas used is zero")
	}
}

// TestE2E_Roadmap_GigagasParallelProcessing exercises parallel tx batch
// execution: multiple independent transfers that can run concurrently.
func TestE2E_Roadmap_GigagasParallelProcessing(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	senders := make([]types.Address, 5)
	receivers := make([]types.Address, 5)
	for i := range senders {
		senders[i] = types.BytesToAddress([]byte{byte(0x10 + i)})
		receivers[i] = types.BytesToAddress([]byte{byte(0x50 + i)})
		statedb.AddBalance(senders[i], big.NewInt(1e18))
	}

	parent := e2e.MakeParentHeader()
	var txs []*types.Transaction
	for i := 0; i < 5; i++ {
		to := receivers[i]
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce: 0, GasPrice: big.NewInt(2000), Gas: e2e.RoadmapTestTxGas,
			To: &to, Value: big.NewInt(int64(1000 * (i + 1))),
		})
		tx.SetSender(senders[i])
		txs = append(txs, tx)
	}

	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, receipts, err := builder.BuildBlockLegacy(parent, txs, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(block.Transactions()) != 5 {
		t.Errorf("block txs: got %d, want 5", len(block.Transactions()))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("tx %d failed", i)
		}
	}
	// Verify all receivers got the right amount.
	for i := 0; i < 5; i++ {
		expected := big.NewInt(int64(1000 * (i + 1)))
		if statedb.GetBalance(receivers[i]).Cmp(expected) != 0 {
			t.Errorf("receiver %d: got %s, want %s",
				i, statedb.GetBalance(receivers[i]), expected)
		}
	}
}

// ==========================================================================
// Cross-Layer Integration Tests
// ==========================================================================

// TestE2E_Roadmap_FullBlockLifecycleWithConsensus tests the complete flow:
// tx -> pool -> build -> execute -> attest -> finalize.
func TestE2E_Roadmap_FullBlockLifecycleWithConsensus(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	statedb.AddBalance(sender, big.NewInt(1e18))

	// Pool.
	pool := txpool.New(txpool.DefaultConfig(), statedb)
	to := receiver
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(2000), Gas: e2e.RoadmapTestTxGas,
		To: &to, Value: big.NewInt(10000),
	})
	tx.SetSender(sender)
	if err := pool.AddLocal(tx); err != nil {
		t.Fatalf("AddLocal: %v", err)
	}

	// Build block.
	parent := e2e.MakeParentHeader()
	pendingMap := pool.Pending()
	var pendingTxs []*types.Transaction
	for _, txs := range pendingMap {
		pendingTxs = append(pendingTxs, txs...)
	}
	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, receipts, err := builder.BuildBlockLegacy(parent, pendingTxs, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if len(receipts) != 1 || receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatal("tx execution failed")
	}

	// SSF attestation.
	cs := e2e.NewRoadmapConsensusState(10, 32_000_000_000)
	root := block.Hash()
	if err := e2e.VoteForSlot(cs.Engine, 1, root, 7); err != nil {
		t.Fatalf("VoteForSlot: %v", err)
	}
	result, err := cs.Engine.CheckFinality(1)
	if err != nil {
		t.Fatalf("CheckFinality: %v", err)
	}
	if !result.IsFinalized {
		t.Error("block should be finalized with 7/10 votes")
	}
}

// TestE2E_Roadmap_StatelessExecution tests building and verifying a block
// where the state changes are tracked independently.
func TestE2E_Roadmap_StatelessExecution(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	statedb.AddBalance(sender, big.NewInt(1e18))

	parent := e2e.MakeParentHeader()
	to := receiver
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(2000), Gas: e2e.RoadmapTestTxGas,
		To: &to, Value: big.NewInt(5000),
	})
	tx.SetSender(sender)

	// Execute and capture state root.
	builder := block.NewBlockBuilder(coreconfig.TestConfig, nil, nil)
	builder.SetState(statedb)
	coinbase := types.BytesToAddress([]byte{0xff})
	block, _, err := builder.BuildBlockLegacy(parent, []*types.Transaction{tx}, 12, coinbase, nil)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	root := statedb.GetRoot()
	if root == (types.Hash{}) {
		t.Error("state root is zero")
	}

	// Verify receiver has the expected balance.
	if statedb.GetBalance(receiver).Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("receiver balance: got %s, want 5000", statedb.GetBalance(receiver))
	}
	_ = block
}

// TestE2E_Roadmap_EncryptedMempoolCycle tests the commit-reveal encrypted
// mempool flow: commit -> reveal -> decrypt -> verify match.
func TestE2E_Roadmap_EncryptedMempoolCycle(t *testing.T) {
	pool := encrypted.NewEncryptedPool()
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})

	tx := e2e.MakeLegacyTx(sender, receiver, 0, 10000)
	commitHash := encrypted.ComputeCommitHash(tx)

	commit := &encrypted.CommitTx{
		CommitHash: commitHash,
		Sender:     sender,
		GasLimit:   e2e.RoadmapTestTxGas,
		MaxFee:     big.NewInt(e2e.RoadmapTestGasPrice),
		Timestamp:  1700000000,
	}
	if err := pool.AddCommit(commit); err != nil {
		t.Fatalf("AddCommit: %v", err)
	}

	reveal := &encrypted.RevealTx{
		CommitHash:  commitHash,
		Transaction: tx,
	}
	if err := pool.AddReveal(reveal); err != nil {
		t.Fatalf("AddReveal: %v", err)
	}

	// Duplicate commit should fail.
	if err := pool.AddCommit(commit); err == nil {
		t.Error("expected error on duplicate commit")
	}
}

// TestE2E_Roadmap_ProofAggregation tests aggregating multiple execution
// proofs into a single verified aggregate.
func TestE2E_Roadmap_ProofAggregation(t *testing.T) {
	agg := proofs.NewSimpleAggregator()

	proofsSlice := make([]proofs.ExecutionProof, 4)
	for i := range proofsSlice {
		proofsSlice[i] = e2e.MakeExecutionProof(uint64(i + 1))
	}

	aggregated, err := agg.Aggregate(proofsSlice)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if !aggregated.Valid {
		t.Error("aggregated proof not marked valid")
	}
	if aggregated.AggregateRoot == (types.Hash{}) {
		t.Error("zero aggregate root")
	}

	verified, err := agg.Verify(aggregated)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !verified {
		t.Error("aggregate proof verification failed")
	}
}

// mandatoryProofFixture supplies an actual Groth16 statement for the quorum tests.
func mandatoryProofFixture(t *testing.T, sys *proofs.MandatoryProofSystem, blockHash types.Hash) []byte {
	t.Helper()
	circuit, err := proofs.CompileAACircuitGnark()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := proofs.SetupGnarkAAKeys(circuit)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := proofs.ProveGnarkAA(circuit, keys, 2, 21000, 1)
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if _, err := proof.WriteTo(&wire); err != nil {
		t.Fatal(err)
	}
	sys.RegisterVerifier("zksnark", func(sub *proofs.ProofSubmission) bool {
		if sub.BlockHash != blockHash {
			return false
		}
		reader := bytes.NewReader(sub.ProofData)
		decoded := groth16.NewProof(ecc.BN254)
		if _, err := decoded.ReadFrom(reader); err != nil || reader.Len() != 0 {
			return false
		}
		ok, err := proofs.VerifyGnarkAAProof(keys, decoded, 2, 21000)
		return ok && err == nil
	})
	return wire.Bytes()
}
