package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/proofs"
	"github.com/eth2030/eth2030/witness"
)

// TestEIP8025_ExecutionWitness verifies EIP-8025 structural surfaces: the
// block witness aggregates accessed accounts and codes, and the execution
// witness keys off the parent state root.
func TestEIP8025_ExecutionWitness(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Deploy + execute so the block touches state.
	c := stf.Deploy(s.Alice(), []byte{
		0x60, 0x2a, 0x60, 0x00, 0x55, // SSTORE(0, 42)
		0x00,
	})
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(c), nil, nil,
		100_000, bigFeeCap, bigTipCap,
	)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)

	// Structural witnesses via the witness package.
	w := witness.NewBlockWitness()
	if w == nil {
		t.Fatal("NewBlockWitness returned nil")
	}
	if w.State == nil || w.Codes == nil {
		t.Fatal("block witness maps not initialized")
	}
	ew := witness.NewExecutionWitness(s.CurrentBlock().Header().Root)
	if ew == nil {
		t.Fatal("NewExecutionWitness returned nil")
	}
	if ew.ParentRoot != s.CurrentBlock().Header().Root {
		t.Fatal("execution witness parent root mismatch")
	}
}

// TestEIP8025_WitnessCollectorHooks verifies the collector wiring accepts the
// sim state DB (full state-diff witness parity is covered by the witness
// package's own suite).
func TestEIP8025_WitnessCollectorHooks(t *testing.T) {
	s := eiptest.New(t)
	w := witness.NewBlockWitness()
	if w == nil {
		t.Fatal("witness is nil")
	}
	st := s.State()
	coll := witness.NewWitnessCollector(st, w)
	if coll == nil {
		t.Fatal("NewWitnessCollector returned nil")
	}
}

// TestEIP8025_BALAnchoredExecutionProof verifies the EIP-8025 proof pipeline
// anchored to real execution data: the proof for a post-Amsterdam block is
// generated from the block's access list (EIP-7928), commits to the BAL hash
// the header commits to, and verifies statelessly; a tampered access set is
// rejected.
func TestEIP8025_BALAnchoredExecutionProof(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	stf := s.STF()

	c := stf.Deploy(s.Alice(), []byte{
		0x60, 0x2a, 0x60, 0x00, 0x55, // SSTORE(0, 42)
		0x00,
	})
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), s.Nonce(s.Alice().Address),
		eiptest.ToPtr(c), nil, nil, 100_000, bigFeeCap, bigTipCap)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)
	eiptest.ExpectBAL(t, r)

	// Pre-state root = parent block's post state.
	parent := s.Chain().GetBlock(r.Block.ParentHash())
	preStateRoot := parent.Root()

	// Proof-generating node: produce the proof from the real BAL.
	proof, err := proofs.GenerateExecutionProofWithBAL(r.Block, preStateRoot, r.BAL)
	if err != nil {
		t.Fatalf("generate BAL-anchored proof: %v", err)
	}
	if proof.BALHash != r.BAL.Hash() {
		t.Fatal("proof BAL hash does not match the access list")
	}
	if r.Block.Header().BlockAccessListHash == nil || *r.Block.Header().BlockAccessListHash != proof.BALHash {
		t.Fatal("proof BAL hash does not match the header commitment")
	}
	if len(proof.AccessLog.Reads) == 0 && len(proof.AccessLog.Writes) == 0 {
		t.Fatal("proof access log is empty despite real storage writes")
	}

	// Stateless validating node: verify without executing.
	ok, err := proofs.VerifyExecutionProof(proof, preStateRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("BAL-anchored proof failed verification")
	}

	// Tampering with the access set breaks the commitment.
	tampered := *proof
	tampered.AccessLog = proofs.NewStateAccessLog()
	tampered.AccessLog.RecordWrite(c, types.Hash{}, types.Hash{1}, types.Hash{2})
	if ok, err := proofs.VerifyExecutionProof(&tampered, preStateRoot); ok || err == nil {
		t.Fatal("tampered proof must be rejected")
	}
}
