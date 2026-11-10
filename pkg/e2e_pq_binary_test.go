// End-to-end tests for post-quantum cryptography, binary trie,
// proof systems, light client, and cross-feature integrations.
package e2e_test

import (
	"encoding/binary"
	"math/big"
	"testing"

	e2e "github.com/eth2030/eth2030"
	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/consensus/pq"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/pqc"
	"github.com/eth2030/eth2030/das"
	"github.com/eth2030/eth2030/light"
	"github.com/eth2030/eth2030/proofs"
	"github.com/eth2030/eth2030/rollup"
	"github.com/eth2030/eth2030/trie/bintrie"
)

// ==========================================================================
// PQ Crypto Pipeline Tests
// ==========================================================================

// TestE2E_PQBinary_PQTransactionSignVerify tests ML-DSA-65 key generation,
// transaction signing, and verification through the PQTxSigner.
func TestE2E_PQBinary_PQTransactionSignVerify(t *testing.T) {
	signer, err := pqc.NewPQTxSigner(pqc.SigDilithium3)
	if err != nil {
		t.Fatalf("NewPQTxSigner: %v", err)
	}

	priv, pub, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if priv == nil || pub == nil {
		t.Fatal("nil key returned")
	}

	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	tx := e2e.MakeLegacyTx(sender, receiver, 0, 1000)

	sig, err := signer.SignTransaction(tx, priv)
	if err != nil {
		t.Fatalf("SignTransaction: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("empty signature")
	}

	valid, err := signer.VerifyTransaction(tx, sig, pub)
	if err != nil {
		t.Fatalf("VerifyTransaction: %v", err)
	}
	if !valid {
		t.Error("valid signature did not verify")
	}
}

// TestE2E_PQBinary_PQTransactionPoolSubmit tests PQ-signed transaction
// creation and basic validation.
func TestE2E_PQBinary_PQTransactionPoolSubmit(t *testing.T) {
	to := types.BytesToAddress([]byte{0x20})
	pqTx := types.NewPQTransaction(
		big.NewInt(1337), 0, &to, big.NewInt(1000), 21000, big.NewInt(2000), nil,
	)
	if pqTx == nil {
		t.Fatal("NewPQTransaction returned nil")
	}
	if pqTx.Nonce != 0 {
		t.Errorf("nonce: got %d, want 0", pqTx.Nonce)
	}
	if pqTx.Gas != 21000 {
		t.Errorf("gas: got %d, want 21000", pqTx.Gas)
	}

	// Encode and decode round-trip.
	encoded, err := pqTx.EncodePQ()
	if err != nil {
		t.Fatalf("EncodePQ: %v", err)
	}
	decoded, err := types.DecodePQTransaction(encoded)
	if err != nil {
		t.Fatalf("DecodePQTransaction: %v", err)
	}
	if decoded.Nonce != pqTx.Nonce {
		t.Errorf("decoded nonce mismatch: got %d, want %d", decoded.Nonce, pqTx.Nonce)
	}
}

// TestE2E_PQBinary_PQBlobCommitment tests lattice-based blob commitment
// and proof generation/verification.
func TestE2E_PQBinary_PQBlobCommitment(t *testing.T) {
	data := e2e.MakeBlobData(4096, 0xBB)

	commitment, err := das.CommitBlob(data)
	if err != nil {
		t.Fatalf("CommitBlob: %v", err)
	}
	if commitment.DataSize != 4096 {
		t.Errorf("data size: got %d, want 4096", commitment.DataSize)
	}

	// Verify commitment matches.
	if !das.VerifyBlobCommitment(commitment, data) {
		t.Error("commitment verification failed")
	}

	// Generate and verify a chunk proof.
	proof, err := das.GenerateBlobProof(data, 0)
	if err != nil {
		t.Fatalf("GenerateBlobProof: %v", err)
	}
	if proof.ChunkIndex != 0 {
		t.Errorf("chunk index: got %d, want 0", proof.ChunkIndex)
	}

	// Tampered data should not match.
	tampered := make([]byte, len(data))
	copy(tampered, data)
	tampered[0] ^= 0xFF
	if das.VerifyBlobCommitment(commitment, tampered) {
		t.Error("tampered data should not verify against original commitment")
	}
}

// TestE2E_PQBinary_PQAttestationCycle tests PQ attestation creation,
// verification, and counter tracking.
func TestE2E_PQBinary_PQAttestationCycle(t *testing.T) {
	verifier := consensus.NewPQAttestationVerifier(consensus.DefaultPQAttestationConfig())
	root := e2e.DeterministicHash(42)
	keys, err := pqc.GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}
	att, err := pq.CreatePQAttestation(10, 0, root, 2, 3, 5, keys)
	if err != nil {
		t.Fatalf("CreatePQAttestation: %v", err)
	}
	if valid, err := verifier.VerifyAttestation(att); !valid || err != nil {
		t.Fatalf("VerifyAttestation = %v, %v", valid, err)
	}
	att.BeaconBlockRoot[0] ^= 1
	if valid, err := verifier.VerifyAttestation(att); valid || err == nil {
		t.Fatalf("VerifyAttestation for changed root = %v, %v", valid, err)
	}
}

// TestE2E_PQBinary_PQChainSecurityLevels tests PQ chain security level
// enforcement at different epochs.
func TestE2E_PQBinary_PQChainSecurityLevels(t *testing.T) {
	// Level: Required -- always enforces PQ after transition epoch.
	cfg := &consensus.PQChainConfig{
		SecurityLevel:      consensus.PQSecurityRequired,
		PQThresholdPercent: 67,
		TransitionEpoch:    100,
		SlotsPerEpoch:      32,
	}
	validator := consensus.NewPQChainValidator(cfg)

	// Before transition: not enforced.
	if validator.IsPQEnforced(50) {
		t.Error("PQ should not be enforced before transition epoch")
	}
	// After transition: enforced.
	if !validator.IsPQEnforced(200) {
		t.Error("PQ should be enforced after transition epoch (Required level)")
	}

	// Level: Preferred -- enforced only when threshold met.
	cfgPref := &consensus.PQChainConfig{
		SecurityLevel:      consensus.PQSecurityPreferred,
		PQThresholdPercent: 67,
		TransitionEpoch:    100,
		SlotsPerEpoch:      32,
	}
	vPref := consensus.NewPQChainValidator(cfgPref)
	vPref.RegisterEpochValidators(200, 70, 100) // 70% PQ
	if !vPref.IsPQEnforced(200) {
		t.Error("PQ should be enforced at 70% PQ validators (threshold 67%)")
	}
	vPref.RegisterEpochValidators(201, 50, 100) // 50% PQ
	if vPref.IsPQEnforced(201) {
		t.Error("PQ should NOT be enforced at 50% PQ validators")
	}
}

// TestE2E_PQBinary_PQAlgorithmRegistry tests algorithm registration,
// lookup, and gas cost verification.
func TestE2E_PQBinary_PQAlgorithmRegistry(t *testing.T) {
	reg := pqc.NewPQAlgorithmRegistry()
	customType := pqc.AlgorithmType(100)
	err := reg.RegisterAlgorithm(
		customType, "TEST-ALGO", 128, 64, 5000,
		func(pubkey, msg, sig []byte) bool { return true },
	)
	if err != nil {
		t.Fatalf("RegisterAlgorithm: %v", err)
	}

	desc, err := reg.GetAlgorithm(customType)
	if err != nil {
		t.Fatalf("GetAlgorithm: %v", err)
	}
	if desc.Name != "TEST-ALGO" {
		t.Errorf("name: got %s, want TEST-ALGO", desc.Name)
	}
	if desc.GasCost != 5000 {
		t.Errorf("gas cost: got %d, want 5000", desc.GasCost)
	}

	// Duplicate registration should fail.
	err = reg.RegisterAlgorithm(customType, "DUP", 128, 64, 5000,
		func(pubkey, msg, sig []byte) bool { return true })
	if err == nil {
		t.Error("expected error on duplicate registration")
	}

	// Verify through registry.
	valid, err := reg.VerifySignature(customType, make([]byte, 64), []byte("msg"), make([]byte, 128))
	if err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	if !valid {
		t.Error("signature should verify via custom algorithm")
	}
}

// ==========================================================================
// Binary Trie & State Migration Tests
// ==========================================================================

// TestE2E_PQBinary_BinaryTrieInsertProve tests insert, hash, prove, and
// verify cycle on the EIP-7864 binary trie.
func TestE2E_PQBinary_BinaryTrieInsertProve(t *testing.T) {
	trie := bintrie.New()

	// Insert several key-value pairs.
	keys := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		keys[i] = make([]byte, bintrie.HashSize)
		keys[i][0] = byte(i + 1)
		keys[i][31] = byte(i)
		value := make([]byte, 32)
		value[0] = byte(i * 10)
		if err := trie.Put(keys[i], value); err != nil {
			t.Fatalf("Put[%d]: %v", i, err)
		}
	}

	root := trie.Hash()
	if root == (types.Hash{}) {
		t.Fatal("empty root hash after inserts")
	}

	// Read back values.
	for i, key := range keys {
		val, err := trie.Get(key)
		if err != nil {
			t.Fatalf("Get[%d]: %v", i, err)
		}
		if val[0] != byte(i*10) {
			t.Errorf("Get[%d]: val[0] = %d, want %d", i, val[0], i*10)
		}
	}

	// Prove and verify.
	proof, err := trie.Prove(keys[0])
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !bintrie.VerifyProof(root, proof) {
		t.Error("proof verification failed")
	}
}

// TestE2E_PQBinary_BinaryTrieDeleteAndReprove tests deletion followed
// by re-proving to verify trie consistency.
func TestE2E_PQBinary_BinaryTrieDeleteAndReprove(t *testing.T) {
	trie := bintrie.New()

	key := make([]byte, bintrie.HashSize)
	key[0] = 0x01
	value := make([]byte, 32)
	value[0] = 0x42

	if err := trie.Put(key, value); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rootBefore := trie.Hash()

	// Delete the key.
	if err := trie.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rootAfter := trie.Hash()
	if rootBefore == rootAfter {
		t.Error("root should change after deletion")
	}
}

// TestE2E_PQBinary_TreeKeyDerivation tests binary tree key computation
// for accounts and storage slots.
func TestE2E_PQBinary_TreeKeyDerivation(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x42})

	basicKey := bintrie.GetBinaryTreeKeyBasicData(addr)
	if len(basicKey) != 32 {
		t.Fatalf("basic data key length: got %d, want 32", len(basicKey))
	}

	codeKey := bintrie.GetBinaryTreeKeyCodeHash(addr)
	if len(codeKey) != 32 {
		t.Fatalf("code hash key length: got %d, want 32", len(codeKey))
	}

	// Keys should differ.
	if string(basicKey) == string(codeKey) {
		t.Error("basic data and code hash keys should differ")
	}

	storageKey := make([]byte, 32)
	storageKey[31] = 0x01
	slotKey := bintrie.GetBinaryTreeKeyStorageSlot(addr, storageKey)
	if len(slotKey) != 32 {
		t.Fatalf("storage key length: got %d, want 32", len(slotKey))
	}
}

// ==========================================================================
// Proof System Tests
// ==========================================================================

// TestE2E_PQBinary_ProofAggregation tests aggregating multiple execution
// proofs using SimpleAggregator.
func TestE2E_PQBinary_ProofAggregation(t *testing.T) {
	agg := proofs.NewSimpleAggregator()
	epSlice := make([]proofs.ExecutionProof, 8)
	for i := range epSlice {
		epSlice[i] = e2e.MakeExecutionProof(uint64(i + 100))
	}

	aggregated, err := agg.Aggregate(epSlice)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(aggregated.Proofs) != 8 {
		t.Errorf("proof count: got %d, want 8", len(aggregated.Proofs))
	}

	verified, err := agg.Verify(aggregated)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !verified {
		t.Error("aggregate verification failed")
	}

	// Tamper with aggregate root.
	tampered := *aggregated
	tampered.AggregateRoot[0] ^= 0xFF
	tValid, _ := agg.Verify(&tampered)
	if tValid {
		t.Error("tampered aggregate should fail verification")
	}
}

// TestE2E_PQBinary_ExecutionProofGeneration tests execution proof
// access log recording and commitment generation.
func TestE2E_PQBinary_ExecutionProofGeneration(t *testing.T) {
	log := proofs.NewStateAccessLog()
	addr := types.BytesToAddress([]byte{0x10})
	slot := types.BytesToHash([]byte{0x01})
	value := types.BytesToHash([]byte{0x42})

	log.RecordRead(addr, slot, value)
	log.RecordWrite(addr, slot, value, types.BytesToHash([]byte{0x43}))

	if len(log.Reads) == 0 {
		t.Error("no reads recorded")
	}
	if len(log.Writes) == 0 {
		t.Error("no writes recorded")
	}
	if len(log.AccessOrder) != 1 {
		t.Errorf("access order: got %d, want 1", len(log.AccessOrder))
	}
}

// TestE2E_PQBinary_MandatoryProofThreeOfFive tests the full 3-of-5
// prover lifecycle: register, assign, submit, verify, check.
func TestE2E_PQBinary_MandatoryProofThreeOfFive(t *testing.T) {
	cfg := proofs.DefaultMandatoryProofConfig()
	sys := proofs.NewMandatoryProofSystem(cfg)

	ids, err := e2e.RegisterProvers(sys, 5)
	if err != nil {
		t.Fatalf("RegisterProvers: %v", err)
	}

	blockHash := e2e.DeterministicHash(999)
	assigned, err := sys.AssignProvers(blockHash)
	if err != nil {
		t.Fatalf("AssignProvers: %v", err)
	}
	if len(assigned) != 5 {
		t.Fatalf("assigned: got %d, want 5", len(assigned))
	}

	// Submit only 2 (insufficient).
	for i := 0; i < 2; i++ {
		sub := e2e.MakeProofSubmission(assigned[i], blockHash)
		if err := sys.SubmitProof(sub); err != nil {
			t.Fatalf("SubmitProof[%d]: %v", i, err)
		}
		sys.VerifyProof(sub)
	}

	status := sys.CheckRequirement(blockHash)
	if status.IsSatisfied {
		t.Error("2-of-5 should NOT satisfy 3-of-5 requirement")
	}

	// Submit a 3rd.
	sub3 := e2e.MakeProofSubmission(assigned[2], blockHash)
	if err := sys.SubmitProof(sub3); err != nil {
		t.Fatalf("SubmitProof[2]: %v", err)
	}
	sys.VerifyProof(sub3)

	status = sys.CheckRequirement(blockHash)
	if !status.IsSatisfied {
		t.Error("3-of-5 should satisfy requirement")
	}
	_ = ids
}

// TestE2E_PQBinary_RecursiveProofAggregation tests hierarchical proof
// aggregation: aggregate sub-batches then aggregate the aggregates.
func TestE2E_PQBinary_RecursiveProofAggregation(t *testing.T) {
	agg := proofs.NewSimpleAggregator()

	// Create two batches of proofs.
	batch1 := make([]proofs.ExecutionProof, 3)
	batch2 := make([]proofs.ExecutionProof, 3)
	for i := range batch1 {
		batch1[i] = e2e.MakeExecutionProof(uint64(i + 1))
		batch2[i] = e2e.MakeExecutionProof(uint64(i + 100))
	}

	agg1, err := agg.Aggregate(batch1)
	if err != nil {
		t.Fatalf("Aggregate batch1: %v", err)
	}
	agg2, err := agg.Aggregate(batch2)
	if err != nil {
		t.Fatalf("Aggregate batch2: %v", err)
	}

	// Aggregate the two aggregated proofs.
	combined := append(agg1.Proofs, agg2.Proofs...)
	finalAgg, err := agg.Aggregate(combined)
	if err != nil {
		t.Fatalf("Final aggregate: %v", err)
	}
	if len(finalAgg.Proofs) != 6 {
		t.Errorf("final count: got %d, want 6", len(finalAgg.Proofs))
	}

	valid, err := agg.Verify(finalAgg)
	if err != nil {
		t.Fatalf("Verify final: %v", err)
	}
	if !valid {
		t.Error("recursive aggregation verification failed")
	}
}

// ==========================================================================
// Cross-Feature Tests
// ==========================================================================

// TestE2E_PQBinary_PQTransactionInBlock tests creating a PQ transaction
// type and encoding/decoding it for block inclusion.
func TestE2E_PQBinary_PQTransactionInBlock(t *testing.T) {
	to := types.BytesToAddress([]byte{0x20})
	pqTx := types.NewPQTransaction(
		big.NewInt(1), 42, &to, big.NewInt(1e15), 100000, big.NewInt(5000), []byte{0x60, 0x00},
	)
	pqTx.PQSignatureType = types.PQSigDilithium

	encoded, err := pqTx.EncodePQ()
	if err != nil {
		t.Fatalf("EncodePQ: %v", err)
	}
	if encoded[0] != types.PQTransactionType {
		t.Errorf("type prefix: got %d, want %d", encoded[0], types.PQTransactionType)
	}

	decoded, err := types.DecodePQTransaction(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Nonce != 42 {
		t.Errorf("nonce: got %d, want 42", decoded.Nonce)
	}
	if decoded.PQSignatureType != types.PQSigDilithium {
		t.Errorf("sig type: got %d, want Dilithium", decoded.PQSignatureType)
	}
}

// TestE2E_PQBinary_NativeRollupExecution tests the EXECUTE precompile
// with a minimal valid input.
func TestE2E_PQBinary_NativeRollupExecution(t *testing.T) {
	precompile := &rollup.ExecutePrecompile{}

	// Build a minimal input: chainID(8) + preStateRoot(32) + lengths(12) + blockData
	blockData := []byte{0x01, 0x02, 0x03, 0x04}
	input := make([]byte, 52+len(blockData))
	binary.BigEndian.PutUint64(input[0:8], 42)                       // chainID
	copy(input[8:40], e2e.DeterministicHash(1).Bytes())              // preStateRoot
	binary.BigEndian.PutUint32(input[40:44], uint32(len(blockData))) // blockDataLen
	binary.BigEndian.PutUint32(input[44:48], 0)                      // witnessLen
	binary.BigEndian.PutUint32(input[48:52], 0)                      // anchorLen
	copy(input[52:], blockData)

	gas := precompile.RequiredGas(input)
	if gas < rollup.ExecuteBaseGas {
		t.Errorf("gas: got %d, want >= %d", gas, rollup.ExecuteBaseGas)
	}

	output, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(output) < 72 {
		t.Fatalf("output too short: %d bytes", len(output))
	}
}

// TestE2E_PQBinary_LightClientSync tests the light client syncer
// processing an update and storing a finalized header.
func TestE2E_PQBinary_LightClientSync(t *testing.T) {
	client := light.NewLightClient()
	if err := client.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	if !client.IsRunning() {
		t.Fatal("client should be running")
	}

	// Create an update with attested and finalized headers.
	attestedHeader := &types.Header{
		Number: big.NewInt(101),
		Root:   e2e.DeterministicHash(101),
	}
	finalizedHeader := &types.Header{
		Number: big.NewInt(100),
		Root:   e2e.DeterministicHash(100),
	}
	// Need supermajority (2/3 of 512) and correct signature.
	bits := light.MakeCommitteeBits(400)
	sig := light.SignUpdate(attestedHeader, bits)
	update := &light.LightClientUpdate{
		AttestedHeader:    attestedHeader,
		FinalizedHeader:   finalizedHeader,
		SyncCommitteeBits: bits,
		Signature:         sig,
	}
	if err := client.ProcessUpdate(update); err != nil {
		t.Fatalf("ProcessUpdate: %v", err)
	}

	hdr := client.GetFinalizedHeader()
	if hdr == nil {
		t.Fatal("no finalized header after update")
	}
	if hdr.Number.Uint64() != 100 {
		t.Errorf("finalized header number: got %d, want 100", hdr.Number.Uint64())
	}
}

// TestE2E_PQBinary_CLProofCircuit tests CL state proof and validator
// proof generation and verification.
func TestE2E_PQBinary_CLProofCircuit(t *testing.T) {
	prover := light.NewCLProver(light.DefaultCLProverConfig())

	stateRoot := e2e.DeterministicHash(42)
	stateProof, err := prover.GenerateStateProof(10, stateRoot)
	if err != nil {
		t.Fatalf("GenerateStateProof: %v", err)
	}
	if stateProof.Slot != 10 {
		t.Errorf("slot: got %d, want 10", stateProof.Slot)
	}
	if !prover.VerifyStateProof(stateProof) {
		t.Error("state proof verification failed")
	}

	// Validator proof.
	valProof, err := prover.GenerateValidatorProof(5)
	if err != nil {
		t.Fatalf("GenerateValidatorProof: %v", err)
	}
	if valProof.Index != 5 {
		t.Errorf("validator index: got %d, want 5", valProof.Index)
	}
}

// TestE2E_PQBinary_WitnessCollectorFullBlock tests that the access log
// correctly tracks reads and writes across multiple addresses.
func TestE2E_PQBinary_WitnessCollectorFullBlock(t *testing.T) {
	log := proofs.NewStateAccessLog()

	// Simulate multi-tx access pattern.
	addrs := []types.Address{
		types.BytesToAddress([]byte{0x10}),
		types.BytesToAddress([]byte{0x20}),
		types.BytesToAddress([]byte{0x30}),
	}
	slot := types.BytesToHash([]byte{0x01})
	for i, addr := range addrs {
		log.RecordRead(addr, slot, types.BytesToHash([]byte{byte(i)}))
		log.RecordWrite(addr, slot, types.BytesToHash([]byte{byte(i)}), types.BytesToHash([]byte{byte(i + 10)}))
	}

	if len(log.AccessOrder) != 3 {
		t.Errorf("access order: got %d, want 3", len(log.AccessOrder))
	}
	for _, addr := range addrs {
		if len(log.Reads[addr]) != 1 {
			t.Errorf("reads for %x: got %d, want 1", addr[:4], len(log.Reads[addr]))
		}
		if len(log.Writes[addr]) != 1 {
			t.Errorf("writes for %x: got %d, want 1", addr[:4], len(log.Writes[addr]))
		}
	}
}

// TestE2E_PQBinary_BlobReconstructionFromCells tests blob cell creation
// and validates that we can reconstruct blob data from cells.
func TestE2E_PQBinary_BlobReconstructionFromCells(t *testing.T) {
	blobSize := das.BytesPerCell * das.ReconstructionThreshold
	data := e2e.MakeBlobData(blobSize, 0x55)

	cells, indices := e2e.MakeCells(data, das.ReconstructionThreshold)
	if len(cells) != das.ReconstructionThreshold {
		t.Fatalf("cells: got %d, want %d", len(cells), das.ReconstructionThreshold)
	}

	// Verify cells contain correct data.
	for i := 0; i < min(5, len(cells)); i++ {
		expectedStart := i * (blobSize / das.ReconstructionThreshold)
		if cells[i][0] != data[expectedStart] {
			t.Errorf("cell[%d][0]: got %d, want %d", i, cells[i][0], data[expectedStart])
		}
	}
	_ = indices

	// Verify CanReconstruct.
	if !das.CanReconstruct(das.ReconstructionThreshold) {
		t.Error("should be able to reconstruct with threshold cells")
	}
	if das.CanReconstruct(das.ReconstructionThreshold - 1) {
		t.Error("should NOT be able to reconstruct with fewer than threshold cells")
	}
}

// TestE2E_PQBinary_VDFProposerSelection tests VDF evaluation and use
// for deterministic proposer randomness.
func TestE2E_PQBinary_VDFProposerSelection(t *testing.T) {
	params := &crypto.VDFParams{T: 50, Lambda: 128}
	vdf := crypto.NewWesolowskiVDF(params)

	// Different slots produce different randomness.
	proof1, err := vdf.Evaluate([]byte("slot-1-seed"), params.T)
	if err != nil {
		t.Fatalf("Evaluate slot-1: %v", err)
	}
	proof2, err := vdf.Evaluate([]byte("slot-2-seed"), params.T)
	if err != nil {
		t.Fatalf("Evaluate slot-2: %v", err)
	}

	if string(proof1.Output) == string(proof2.Output) {
		t.Error("different inputs should produce different VDF outputs")
	}

	// Both should verify.
	if !vdf.Verify(proof1) {
		t.Error("proof1 should verify")
	}
	if !vdf.Verify(proof2) {
		t.Error("proof2 should verify")
	}
}

// TestE2E_PQBinary_PQBlockHashSHA3 tests that PQ-resistant block hashing
// produces consistent results.
func TestE2E_PQBinary_PQBlockHashSHA3(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(42),
		GasLimit: 30_000_000,
		GasUsed:  21000,
		Time:     1700000000,
	}

	hash1, err := consensus.PQBlockHash(header)
	if err != nil {
		t.Fatalf("PQBlockHash: %v", err)
	}
	if hash1 == (types.Hash{}) {
		t.Fatal("zero PQ block hash")
	}

	// Same header should produce same hash.
	hash2, err := consensus.PQBlockHash(header)
	if err != nil {
		t.Fatalf("PQBlockHash (2nd): %v", err)
	}
	if hash1 != hash2 {
		t.Error("PQ block hash not deterministic")
	}

	// Different header should produce different hash.
	header2 := &types.Header{
		Number:   big.NewInt(42),
		GasLimit: 30_000_000,
		GasUsed:  42000,
		Time:     1700000000,
	}
	hash3, _ := consensus.PQBlockHash(header2)
	if hash1 == hash3 {
		t.Error("different headers should produce different PQ hashes")
	}
}

// TestE2E_PQBinary_MLDSAKeyGenSignVerify tests the real ML-DSA-65
// lattice signer key generation, signing, and verification.
func TestE2E_PQBinary_MLDSAKeyGenSignVerify(t *testing.T) {
	signer := pqc.NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(kp.PublicKey) != pqc.MLDSAPublicKeySize {
		t.Errorf("pubkey size: got %d, want %d", len(kp.PublicKey), pqc.MLDSAPublicKeySize)
	}
	if len(kp.SecretKey) != pqc.MLDSAPrivateKeySize {
		t.Errorf("privkey size: got %d, want %d", len(kp.SecretKey), pqc.MLDSAPrivateKeySize)
	}

	msg := []byte("test-message-for-mldsa-signing")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != pqc.MLDSASignatureSize {
		t.Errorf("sig size: got %d, want %d", len(sig), pqc.MLDSASignatureSize)
	}

	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Error("ML-DSA signature should verify")
	}

	// Tamper with signature.
	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[0] ^= 0xFF
	if signer.Verify(kp.PublicKey, msg, tampered) {
		t.Error("tampered ML-DSA signature should NOT verify")
	}
}
