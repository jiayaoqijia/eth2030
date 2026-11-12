package proofs

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// makeTestBlock creates a simple block with n transactions for testing.
func makeTestBlock(n int) *types.Block {
	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 30_000_000,
		GasUsed:  21000 * uint64(n),
		Root:     crypto.Keccak256Hash([]byte("test-state-root")),
	}
	body := &types.Body{
		Transactions: make([]*types.Transaction, n),
	}
	for i := 0; i < n; i++ {
		body.Transactions[i] = types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(1_000_000_000),
			Gas:      21000,
			Value:    big.NewInt(int64(i + 1)),
		})
	}
	return types.NewBlock(header, body)
}

func TestGenerateExecutionProof(t *testing.T) {
	block := makeTestBlock(3)
	stateRoot := crypto.Keccak256Hash([]byte("pre-state"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if proof.BlockHash != block.Hash() {
		t.Fatal("block hash mismatch")
	}
	if proof.BlockNumber != 100 {
		t.Fatalf("expected block 100, got %d", proof.BlockNumber)
	}
	if proof.StateRoot != stateRoot {
		t.Fatal("state root mismatch")
	}
	if proof.PostStateRoot.IsZero() {
		t.Fatal("post-state root should not be zero")
	}
	if len(proof.TxTraces) != 3 {
		t.Fatalf("expected 3 tx traces, got %d", len(proof.TxTraces))
	}
	if proof.AccessLog == nil {
		t.Fatal("access log should not be nil")
	}
	if proof.AccessLog.UniqueAccounts() == 0 {
		t.Fatal("access log should have accounts")
	}
	if proof.Commitment.IsZero() {
		t.Fatal("commitment should not be zero")
	}
	if len(proof.MerkleProofs) == 0 {
		t.Fatal("merkle proofs should not be empty")
	}
}

func TestGenerateExecutionProof_Errors(t *testing.T) {
	t.Run("nil block", func(t *testing.T) {
		_, err := GenerateExecutionProof(nil, crypto.Keccak256Hash([]byte("s")))
		if err != ErrExecProofNilBlock {
			t.Fatalf("expected ErrExecProofNilBlock, got %v", err)
		}
	})

	t.Run("zero state root", func(t *testing.T) {
		block := makeTestBlock(1)
		_, err := GenerateExecutionProof(block, types.Hash{})
		if err != ErrExecProofStateRootNil {
			t.Fatalf("expected ErrExecProofStateRootNil, got %v", err)
		}
	})
}

func TestVerifyExecutionProof(t *testing.T) {
	block := makeTestBlock(2)
	stateRoot := crypto.Keccak256Hash([]byte("verify-state"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	valid, err := VerifyExecutionProof(proof, stateRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !valid {
		t.Fatal("proof should be valid")
	}
}

func TestVerifyExecutionProof_WrongStateRoot(t *testing.T) {
	block := makeTestBlock(2)
	stateRoot := crypto.Keccak256Hash([]byte("original"))
	wrongRoot := crypto.Keccak256Hash([]byte("wrong"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	valid, err := VerifyExecutionProof(proof, wrongRoot)
	if err == nil {
		t.Fatal("expected error for wrong state root")
	}
	if valid {
		t.Fatal("should not be valid with wrong state root")
	}
}

func TestVerifyExecutionProof_Tampered(t *testing.T) {
	block := makeTestBlock(1)
	stateRoot := crypto.Keccak256Hash([]byte("tamper-test"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Tamper with the commitment.
	proof.Commitment[0] ^= 0xff
	valid, err := VerifyExecutionProof(proof, stateRoot)
	if err != ErrExecProofTampered {
		t.Fatalf("expected ErrExecProofTampered, got %v", err)
	}
	if valid {
		t.Fatal("tampered proof should not be valid")
	}
}

func TestVerifyExecutionProof_ErrorCases(t *testing.T) {
	t.Run("nil proof", func(t *testing.T) {
		valid, err := VerifyExecutionProof(nil, crypto.Keccak256Hash([]byte("s")))
		if err != ErrExecProofNilBlock {
			t.Fatalf("expected ErrExecProofNilBlock, got %v", err)
		}
		if valid {
			t.Fatal("nil proof should not be valid")
		}
	})

	t.Run("zero state root", func(t *testing.T) {
		proof := &BlockExecutionProof{TxTraces: []TxTrace{{}}}
		valid, err := VerifyExecutionProof(proof, types.Hash{})
		if err != ErrExecProofStateRootNil {
			t.Fatalf("expected ErrExecProofStateRootNil, got %v", err)
		}
		if valid {
			t.Fatal("should not be valid")
		}
	})

	t.Run("no tx traces", func(t *testing.T) {
		proof := &BlockExecutionProof{}
		valid, err := VerifyExecutionProof(proof, crypto.Keccak256Hash([]byte("s")))
		if err != ErrExecProofNoTxTraces {
			t.Fatalf("expected ErrExecProofNoTxTraces, got %v", err)
		}
		if valid {
			t.Fatal("should not be valid")
		}
	})

	t.Run("empty access log", func(t *testing.T) {
		proof := &BlockExecutionProof{
			TxTraces:  []TxTrace{{}},
			AccessLog: NewStateAccessLog(),
		}
		valid, err := VerifyExecutionProof(proof, crypto.Keccak256Hash([]byte("s")))
		if err != ErrExecProofAccessEmpty {
			t.Fatalf("expected ErrExecProofAccessEmpty, got %v", err)
		}
		if valid {
			t.Fatal("should not be valid")
		}
	})
}

func TestStateAccessLog(t *testing.T) {
	log := NewStateAccessLog()

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	slot := types.BytesToHash([]byte{0x10})
	value := types.BytesToHash([]byte{0x20})

	log.RecordRead(addr1, slot, value)
	log.RecordRead(addr1, types.BytesToHash([]byte{0x11}), types.BytesToHash([]byte{0x21}))
	log.RecordWrite(addr2, slot, types.Hash{}, value)

	if log.TotalReads() != 2 {
		t.Fatalf("expected 2 reads, got %d", log.TotalReads())
	}
	if log.TotalWrites() != 1 {
		t.Fatalf("expected 1 write, got %d", log.TotalWrites())
	}
	if log.UniqueAccounts() != 2 {
		t.Fatalf("expected 2 unique accounts, got %d", log.UniqueAccounts())
	}

	// Digest should be deterministic.
	d1 := log.Digest()
	d2 := log.Digest()
	if d1 != d2 {
		t.Fatal("digest should be deterministic")
	}
	if d1.IsZero() {
		t.Fatal("digest should not be zero")
	}
}

func TestStateAccessLog_DuplicateAddress(t *testing.T) {
	log := NewStateAccessLog()

	addr := types.BytesToAddress([]byte{0x42})
	slot1 := types.BytesToHash([]byte{0x01})
	slot2 := types.BytesToHash([]byte{0x02})
	val := types.BytesToHash([]byte{0x10})

	log.RecordRead(addr, slot1, val)
	log.RecordWrite(addr, slot2, types.Hash{}, val)

	// Address should only appear once in the access order.
	if log.UniqueAccounts() != 1 {
		t.Fatalf("expected 1 unique account, got %d", log.UniqueAccounts())
	}
	if log.TotalReads() != 1 {
		t.Fatalf("expected 1 read, got %d", log.TotalReads())
	}
	if log.TotalWrites() != 1 {
		t.Fatalf("expected 1 write, got %d", log.TotalWrites())
	}
}

func TestProofCompression(t *testing.T) {
	block := makeTestBlock(4)
	stateRoot := crypto.Keccak256Hash([]byte("compress-test"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	pc := NewProofCompression()
	saved := pc.Compress(proof)

	// With 4 transactions and simulated proofs, there may be shared branches.
	t.Logf("bytes saved: %d, shared branches: %d", saved, pc.SharedBranchCount())

	if pc.SharedBranchCount() == 0 {
		t.Fatal("should have at least some shared branches")
	}

	// Decompress and verify round-trip.
	decompressed := pc.DecompressProofs()
	if len(decompressed) != len(proof.MerkleProofs) {
		t.Fatalf("decompressed %d accounts, want %d", len(decompressed), len(proof.MerkleProofs))
	}

	for addr, branches := range proof.MerkleProofs {
		decBranches, ok := decompressed[addr]
		if !ok {
			t.Fatalf("missing account %s in decompressed", addr)
		}
		if len(decBranches) != len(branches) {
			t.Fatalf("branch count mismatch for %s: got %d, want %d", addr, len(decBranches), len(branches))
		}
		for i := range branches {
			if len(branches[i]) != len(decBranches[i]) {
				t.Fatalf("branch %d size mismatch", i)
			}
			for j := range branches[i] {
				if branches[i][j] != decBranches[i][j] {
					t.Fatalf("branch %d byte %d mismatch", i, j)
				}
			}
		}
	}
}

func TestProofCompression_Nil(t *testing.T) {
	pc := NewProofCompression()
	saved := pc.Compress(nil)
	if saved != 0 {
		t.Fatalf("nil proof should save 0 bytes, got %d", saved)
	}

	saved = pc.Compress(&BlockExecutionProof{})
	if saved != 0 {
		t.Fatalf("empty proof should save 0 bytes, got %d", saved)
	}
}

func TestTxTrace_Fields(t *testing.T) {
	block := makeTestBlock(1)
	stateRoot := crypto.Keccak256Hash([]byte("trace-test"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	trace := proof.TxTraces[0]
	if trace.TxIndex != 0 {
		t.Fatalf("expected tx index 0, got %d", trace.TxIndex)
	}
	if !trace.Success {
		t.Fatal("tx should be successful")
	}
	if trace.GasUsed == 0 {
		t.Fatal("gas used should not be zero")
	}
	if len(trace.StateReads) == 0 {
		t.Fatal("should have state reads")
	}
	if len(trace.StateWrites) == 0 {
		t.Fatal("should have state writes")
	}
}

func TestVerifyExecutionProof_TamperedMerkleProof(t *testing.T) {
	block := makeTestBlock(1)
	stateRoot := crypto.Keccak256Hash([]byte("merkle-tamper"))

	proof, err := GenerateExecutionProof(block, stateRoot)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Tamper with a merkle proof branch.
	for addr := range proof.MerkleProofs {
		if len(proof.MerkleProofs[addr]) > 0 {
			proof.MerkleProofs[addr][0][0] ^= 0xff
		}
		break
	}

	// Recompute commitment so it doesn't fail on commitment check first.
	proof.Commitment = computeExecutionCommitment(
		proof.BlockHash, stateRoot, proof.PostStateRoot, types.Hash{}, proof.AccessLog,
	)

	valid, err := VerifyExecutionProof(proof, stateRoot)
	if err != ErrExecProofTampered {
		t.Fatalf("expected ErrExecProofTampered, got %v", err)
	}
	if valid {
		t.Fatal("should not be valid with tampered merkle proof")
	}
}

func TestDeriveAddress(t *testing.T) {
	h := crypto.Keccak256Hash([]byte("test-hash"))
	addr0 := deriveAddress(h, 0)
	addr1 := deriveAddress(h, 1)

	if addr0 == addr1 {
		t.Fatal("different salts should produce different addresses")
	}
	if addr0.IsZero() || addr1.IsZero() {
		t.Fatal("derived addresses should not be zero")
	}
}

func TestSortAccesses(t *testing.T) {
	accesses := []StateAccess{
		{Slot: types.BytesToHash([]byte{3})},
		{Slot: types.BytesToHash([]byte{1})},
		{Slot: types.BytesToHash([]byte{2})},
	}

	sortAccesses(accesses)

	for i := 0; i < len(accesses)-1; i++ {
		for k := 0; k < types.HashLength; k++ {
			if accesses[i].Slot[k] > accesses[i+1].Slot[k] {
				t.Fatalf("accesses not sorted: %v > %v", accesses[i].Slot, accesses[i+1].Slot)
			}
			if accesses[i].Slot[k] < accesses[i+1].Slot[k] {
				break
			}
		}
	}
}

func TestGenerateAndVerify_Deterministic(t *testing.T) {
	block := makeTestBlock(2)
	stateRoot := crypto.Keccak256Hash([]byte("determinism"))

	proof1, _ := GenerateExecutionProof(block, stateRoot)
	proof2, _ := GenerateExecutionProof(block, stateRoot)

	if proof1.Commitment != proof2.Commitment {
		t.Fatal("proofs from same inputs should have same commitment")
	}
	if proof1.PostStateRoot != proof2.PostStateRoot {
		t.Fatal("proofs from same inputs should have same post-state")
	}
}
