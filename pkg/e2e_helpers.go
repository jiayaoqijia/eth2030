// e2e_helpers.go provides shared utility functions and mock implementations
// for end-to-end roadmap integration tests. This file establishes the base
// package for the pkg/ root directory, enabling external test files to use
// these exported helpers.
package e2e

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"

	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/das"
	"github.com/eth2030/eth2030/epbs"
	"github.com/eth2030/eth2030/proofs"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	RoadmapTestGasLimit = uint64(30_000_000)
	RoadmapTestBaseFee  = 1000
	RoadmapTestGasPrice = 10
	RoadmapTestTxGas    = uint64(21000)
)

// ---------------------------------------------------------------------------
// RoadmapConsensusState bundles SSF engine, validators, and weights.
// ---------------------------------------------------------------------------

type RoadmapConsensusState struct {
	Engine     *consensus.SSFEngine
	Weights    map[uint64]uint64
	TotalStake uint64
}

// NewRoadmapConsensusState creates an SSF engine with n validators each
// having equal stake.
func NewRoadmapConsensusState(n int, stakePerValidator uint64) *RoadmapConsensusState {
	weights := make(map[uint64]uint64, n)
	var total uint64
	for i := 0; i < n; i++ {
		weights[uint64(i)] = stakePerValidator
		total += stakePerValidator
	}
	cfg := consensus.DefaultSSFEngineConfig()
	cfg.TotalStake = total
	engine := consensus.NewSSFEngine(cfg)
	engine.SetValidatorWeights(weights)
	return &RoadmapConsensusState{
		Engine:     engine,
		Weights:    weights,
		TotalStake: total,
	}
}

// VoteForSlot casts attestations from validators 0..count-1 for a slot/root.
func VoteForSlot(engine *consensus.SSFEngine, slot uint64, root types.Hash, count int) error {
	for i := 0; i < count; i++ {
		att := &consensus.SSFAttestation{
			Slot:           slot,
			ValidatorIndex: uint64(i),
			TargetRoot:     root,
		}
		if err := engine.ProcessAttestation(att); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Transaction helpers
// ---------------------------------------------------------------------------

// MakeLegacyTx creates a simple legacy value transfer transaction.
func MakeLegacyTx(sender, receiver types.Address, nonce uint64, value int64) *types.Transaction {
	to := receiver
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(RoadmapTestGasPrice),
		Gas:      RoadmapTestTxGas,
		To:       &to,
		Value:    big.NewInt(value),
	})
	tx.SetSender(sender)
	return tx
}

// MakeDynamicFeeTx creates an EIP-1559 dynamic fee transaction.
func MakeDynamicFeeTx(sender, receiver types.Address, nonce uint64, value, tipCap, feeCap int64) *types.Transaction {
	to := receiver
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1337),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       RoadmapTestTxGas,
		To:        &to,
		Value:     big.NewInt(value),
	})
	tx.SetSender(sender)
	return tx
}

// MakeBlobTx creates an EIP-4844 blob transaction with a single blob hash.
func MakeBlobTx(sender, receiver types.Address, nonce uint64, blobIndex byte) *types.Transaction {
	blobHash := types.Hash{}
	blobHash[0] = 0x01
	blobHash[1] = blobIndex
	tx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1337),
		Nonce:      nonce,
		GasTipCap:  big.NewInt(2000),
		GasFeeCap:  big.NewInt(100000),
		Gas:        RoadmapTestTxGas,
		To:         receiver,
		Value:      big.NewInt(1),
		BlobFeeCap: big.NewInt(100000),
		BlobHashes: []types.Hash{blobHash},
	})
	tx.SetSender(sender)
	return tx
}

// MakeContractTx creates a contract deployment transaction.
func MakeContractTx(sender types.Address, nonce uint64, code []byte) *types.Transaction {
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(RoadmapTestGasPrice),
		Gas:      200_000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     code,
	})
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Header helpers
// ---------------------------------------------------------------------------

// MakeParentHeader creates a genesis-like parent header.
func MakeParentHeader() *types.Header {
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	return &types.Header{
		Number:        big.NewInt(0),
		GasLimit:      RoadmapTestGasLimit,
		GasUsed:       0,
		BaseFee:       big.NewInt(RoadmapTestBaseFee),
		BlobGasUsed:   &blobGasUsed,
		ExcessBlobGas: &excessBlobGas,
	}
}

// ---------------------------------------------------------------------------
// ePBS auction helpers
// ---------------------------------------------------------------------------

// MakeBuilderBid creates a signed builder bid for a given slot and value.
func MakeBuilderBid(slot, value, builderIdx uint64) *epbs.SignedBuilderBid {
	parentHash := types.BytesToHash([]byte{0xab, byte(slot)})
	blockHash := types.BytesToHash([]byte{0xcd, byte(slot), byte(value)})
	return &epbs.SignedBuilderBid{
		Message: epbs.BuilderBid{
			ParentBlockHash: parentHash,
			BlockHash:       blockHash,
			Slot:            slot,
			Value:           value,
			BuilderIndex:    epbs.BuilderIndex(builderIdx),
			GasLimit:        RoadmapTestGasLimit,
		},
		Signature: epbs.BLSSignature{0x01, byte(builderIdx)},
	}
}

// ---------------------------------------------------------------------------
// DAS blob helpers
// ---------------------------------------------------------------------------

// MakeBlobData creates deterministic blob data of the given size.
func MakeBlobData(size int, seed byte) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i) ^ seed
	}
	return data
}

// MakeCells creates mock cells from blob data for reconstruction testing.
func MakeCells(data []byte, numCells int) ([]das.Cell, []uint64) {
	cells := make([]das.Cell, numCells)
	indices := make([]uint64, numCells)
	chunkSize := len(data) / numCells
	if chunkSize > das.BytesPerCell {
		chunkSize = das.BytesPerCell
	}
	for i := 0; i < numCells; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		copy(cells[i][:], data[start:end])
		indices[i] = uint64(i)
	}
	return cells, indices
}

// ---------------------------------------------------------------------------
// Proof system helpers
// ---------------------------------------------------------------------------

// RegisterProvers registers n provers with the mandatory proof system.
func RegisterProvers(sys *proofs.MandatoryProofSystem, n int) ([]types.Hash, error) {
	ids := make([]types.Hash, n)
	for i := 0; i < n; i++ {
		h := sha256.Sum256([]byte{byte(i), byte(i >> 8)})
		copy(ids[i][:], h[:])
		if err := sys.RegisterProver(ids[i], []string{"zksnark", "zkstark"}); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// MakeExecutionProof creates a test execution proof for aggregation.
func MakeExecutionProof(blockNum uint64) proofs.ExecutionProof {
	blockHash := DeterministicHash(blockNum)
	stateRoot := DeterministicHash(blockNum + 1000)
	proofData := crypto.Keccak256Hash(blockHash[:], stateRoot[:])
	return proofs.ExecutionProof{
		BlockHash: blockHash,
		StateRoot: stateRoot,
		ProofData: proofData[:],
		ProverID:  "test-prover",
		Type:      proofs.ZKSNARK,
	}
}

// ---------------------------------------------------------------------------
// Deterministic hash helpers
// ---------------------------------------------------------------------------

// DeterministicHash returns a hash derived from a seed.
func DeterministicHash(seed uint64) types.Hash {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seed)
	return crypto.Keccak256Hash(buf[:])
}

// DeterministicNodeID returns a 32-byte node ID from a seed.
func DeterministicNodeID(seed uint64) [32]byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seed)
	return sha256.Sum256(buf[:])
}

// DeterministicAddress returns a types.Address derived from a seed.
func DeterministicAddress(seed byte) types.Address {
	return types.BytesToAddress([]byte{seed, seed ^ 0xff, seed + 1})
}

// ---------------------------------------------------------------------------
// Variable blob config helpers
// ---------------------------------------------------------------------------

// MakeVariableBlobConfig creates a valid variable blob configuration.
func MakeVariableBlobConfig(maxBlobs, targetBlobs, blobSize uint64) *das.BlobConfig {
	return &das.BlobConfig{
		MinBlobsPerBlock:    0,
		MaxBlobsPerBlock:    maxBlobs,
		TargetBlobsPerBlock: targetBlobs,
		BlobSize:            blobSize,
	}
}
