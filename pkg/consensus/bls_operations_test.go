package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// testForkVersion is a test fork version.
var testForkVersion = [4]byte{0x01, 0x00, 0x00, 0x00}

// testGenesisRoot is a test genesis validators root.
var testGenesisRoot = [32]byte{0xAA, 0xBB, 0xCC, 0xDD}

func TestDomainSeparation(t *testing.T) {
	domain := DomainSeparation(DomainBeaconProposer, testForkVersion, testGenesisRoot)

	// The first 4 bytes should be the domain type.
	if domain[0] != 0x00 || domain[1] != 0x00 || domain[2] != 0x00 || domain[3] != 0x00 {
		t.Fatalf("domain type mismatch: got %x", domain[:4])
	}

	// The remaining 28 bytes should come from the fork data root.
	allZero := true
	for _, b := range domain[4:] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("fork data root portion is all zeros")
	}
}

func TestDomainSeparationDifferentTypes(t *testing.T) {
	d1 := DomainSeparation(DomainBeaconProposer, testForkVersion, testGenesisRoot)
	d2 := DomainSeparation(DomainBeaconAttester, testForkVersion, testGenesisRoot)
	d3 := DomainSeparation(DomainSyncCommittee, testForkVersion, testGenesisRoot)

	if d1 == d2 || d1 == d3 || d2 == d3 {
		t.Fatal("different domain types should produce different domains")
	}
}

func TestDomainSeparationDifferentForks(t *testing.T) {
	fork1 := [4]byte{0x01, 0x00, 0x00, 0x00}
	fork2 := [4]byte{0x02, 0x00, 0x00, 0x00}

	d1 := DomainSeparation(DomainBeaconProposer, fork1, testGenesisRoot)
	d2 := DomainSeparation(DomainBeaconProposer, fork2, testGenesisRoot)

	if d1 == d2 {
		t.Fatal("different fork versions should produce different domains")
	}
}

func TestDomainSeparationDifferentGenesis(t *testing.T) {
	gen1 := [32]byte{0x01}
	gen2 := [32]byte{0x02}

	d1 := DomainSeparation(DomainBeaconProposer, testForkVersion, gen1)
	d2 := DomainSeparation(DomainBeaconProposer, testForkVersion, gen2)

	if d1 == d2 {
		t.Fatal("different genesis roots should produce different domains")
	}
}

func TestComputeSigningRoot(t *testing.T) {
	objectRoot := [32]byte{0x01, 0x02, 0x03}
	domain := [32]byte{0x04, 0x05, 0x06}

	root := ComputeSigningRoot(objectRoot, domain)

	// Should be deterministic.
	root2 := ComputeSigningRoot(objectRoot, domain)
	if root != root2 {
		t.Fatal("signing root is not deterministic")
	}

	// Different object root should give different signing root.
	otherObjectRoot := [32]byte{0x07, 0x08, 0x09}
	root3 := ComputeSigningRoot(otherObjectRoot, domain)
	if root == root3 {
		t.Fatal("different object roots should produce different signing roots")
	}

	// Different domain should give different signing root.
	otherDomain := [32]byte{0x0A, 0x0B, 0x0C}
	root4 := ComputeSigningRoot(objectRoot, otherDomain)
	if root == root4 {
		t.Fatal("different domains should produce different signing roots")
	}
}

func TestHashBeaconBlockHeader(t *testing.T) {
	header := &BeaconBlockHeader{
		Slot:          100,
		ProposerIndex: 42,
		ParentRoot:    [32]byte{0x01},
		StateRoot:     [32]byte{0x02},
		BodyRoot:      [32]byte{0x03},
	}

	root := HashBeaconBlockHeader(header)

	// Should be deterministic.
	root2 := HashBeaconBlockHeader(header)
	if root != root2 {
		t.Fatal("header hash is not deterministic")
	}

	// Different header should give different root.
	header2 := &BeaconBlockHeader{
		Slot:          101,
		ProposerIndex: 42,
		ParentRoot:    [32]byte{0x01},
		StateRoot:     [32]byte{0x02},
		BodyRoot:      [32]byte{0x03},
	}
	root3 := HashBeaconBlockHeader(header2)
	if root == root3 {
		t.Fatal("different headers should produce different roots")
	}

	// Nil header should return zero.
	zeroRoot := HashBeaconBlockHeader(nil)
	if zeroRoot != ([32]byte{}) {
		t.Fatal("nil header should produce zero root")
	}
}

func TestHashAttestationData(t *testing.T) {
	data := &AttestationData{
		Slot:            Slot(100),
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 3, Root: types.Hash{0x02}},
		Target:          Checkpoint{Epoch: 4, Root: types.Hash{0x03}},
	}

	root := HashAttestationData(data)

	// Deterministic.
	root2 := HashAttestationData(data)
	if root != root2 {
		t.Fatal("attestation data hash is not deterministic")
	}

	// Different data should give different root.
	data2 := &AttestationData{
		Slot:            Slot(101),
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 3, Root: types.Hash{0x02}},
		Target:          Checkpoint{Epoch: 4, Root: types.Hash{0x03}},
	}
	root3 := HashAttestationData(data2)
	if root == root3 {
		t.Fatal("different attestation data should produce different roots")
	}

	// Nil data should return zero.
	zeroRoot := HashAttestationData(nil)
	if zeroRoot != ([32]byte{}) {
		t.Fatal("nil data should produce zero root")
	}
}

func TestVerifyProposerSignature(t *testing.T) {
	secret := big.NewInt(55555)
	pk := crypto.BLSPubkeyFromSecret(secret)

	header := &BeaconBlockHeader{
		Slot:          200,
		ProposerIndex: 7,
		ParentRoot:    [32]byte{0x11},
		StateRoot:     [32]byte{0x22},
		BodyRoot:      [32]byte{0x33},
	}

	// Sign the header.
	domain := DomainSeparation(DomainBeaconProposer, testForkVersion, testGenesisRoot)
	headerRoot := HashBeaconBlockHeader(header)
	sig := SignWithDomain(secret.Bytes(), headerRoot, domain)

	if !VerifyProposerSignature(pk, header, sig, testForkVersion, testGenesisRoot) {
		t.Fatal("valid proposer signature should verify")
	}
}

func TestVerifyProposerSignatureWrongKey(t *testing.T) {
	secret := big.NewInt(55555)
	wrongPK := crypto.BLSPubkeyFromSecret(big.NewInt(66666))

	header := &BeaconBlockHeader{
		Slot:          200,
		ProposerIndex: 7,
		ParentRoot:    [32]byte{0x11},
		StateRoot:     [32]byte{0x22},
		BodyRoot:      [32]byte{0x33},
	}

	domain := DomainSeparation(DomainBeaconProposer, testForkVersion, testGenesisRoot)
	headerRoot := HashBeaconBlockHeader(header)
	sig := SignWithDomain(secret.Bytes(), headerRoot, domain)

	if VerifyProposerSignature(wrongPK, header, sig, testForkVersion, testGenesisRoot) {
		t.Fatal("proposer signature should not verify with wrong key")
	}
}

func TestVerifyProposerSignatureNilHeader(t *testing.T) {
	pk := crypto.BLSPubkeyFromSecret(big.NewInt(1))
	var sig [96]byte

	if VerifyProposerSignature(pk, nil, sig, testForkVersion, testGenesisRoot) {
		t.Fatal("should reject nil header")
	}
}

func TestVerifyAttestationSignature(t *testing.T) {
	secret1 := big.NewInt(10001)
	secret2 := big.NewInt(10002)
	pk1 := crypto.BLSPubkeyFromSecret(secret1)
	pk2 := crypto.BLSPubkeyFromSecret(secret2)

	data := &AttestationData{
		Slot:            Slot(500),
		BeaconBlockRoot: types.Hash{0xAA},
		Source:          Checkpoint{Epoch: 15, Root: types.Hash{0xBB}},
		Target:          Checkpoint{Epoch: 16, Root: types.Hash{0xCC}},
	}

	// Both validators sign the same attestation data.
	domain := DomainSeparation(DomainBeaconAttester, testForkVersion, testGenesisRoot)
	dataRoot := HashAttestationData(data)
	sig1 := SignWithDomain(secret1.Bytes(), dataRoot, domain)
	sig2 := SignWithDomain(secret2.Bytes(), dataRoot, domain)

	aggSig := crypto.AggregateSignatures([][96]byte{sig1, sig2})

	if !VerifyAttestationSignature(
		[][48]byte{pk1, pk2}, data, aggSig, testForkVersion, testGenesisRoot,
	) {
		t.Fatal("valid attestation signature should verify")
	}
}

func TestVerifyAttestationSignatureWrongData(t *testing.T) {
	secret := big.NewInt(10001)
	pk := crypto.BLSPubkeyFromSecret(secret)

	data := &AttestationData{
		Slot:            Slot(500),
		BeaconBlockRoot: types.Hash{0xAA},
		Source:          Checkpoint{Epoch: 15, Root: types.Hash{0xBB}},
		Target:          Checkpoint{Epoch: 16, Root: types.Hash{0xCC}},
	}

	domain := DomainSeparation(DomainBeaconAttester, testForkVersion, testGenesisRoot)
	dataRoot := HashAttestationData(data)
	sig := SignWithDomain(secret.Bytes(), dataRoot, domain)

	// Change the data.
	wrongData := &AttestationData{
		Slot:            Slot(501),
		BeaconBlockRoot: types.Hash{0xAA},
		Source:          Checkpoint{Epoch: 15, Root: types.Hash{0xBB}},
		Target:          Checkpoint{Epoch: 16, Root: types.Hash{0xCC}},
	}

	if VerifyAttestationSignature(
		[][48]byte{pk}, wrongData, sig, testForkVersion, testGenesisRoot,
	) {
		t.Fatal("attestation signature should not verify with wrong data")
	}
}

func TestVerifyAttestationSignatureEmptyPubkeys(t *testing.T) {
	var sig [96]byte
	data := &AttestationData{Slot: 1}

	if VerifyAttestationSignature(nil, data, sig, testForkVersion, testGenesisRoot) {
		t.Fatal("should reject empty pubkeys")
	}
}

func TestVerifyAttestationSignatureNilData(t *testing.T) {
	pk := crypto.BLSPubkeyFromSecret(big.NewInt(1))
	var sig [96]byte

	if VerifyAttestationSignature([][48]byte{pk}, nil, sig, testForkVersion, testGenesisRoot) {
		t.Fatal("should reject nil data")
	}
}

func TestVerifySyncCommitteeBLSSignature(t *testing.T) {
	secret1 := big.NewInt(20001)
	secret2 := big.NewInt(20002)
	pk1 := crypto.BLSPubkeyFromSecret(secret1)
	pk2 := crypto.BLSPubkeyFromSecret(secret2)

	blockRoot := types.Hash{0xDD, 0xEE, 0xFF}

	// Both committee members sign the block root.
	domain := DomainSeparation(DomainSyncCommittee, testForkVersion, testGenesisRoot)
	var objectRoot [32]byte
	copy(objectRoot[:], blockRoot[:])
	sig1 := SignWithDomain(secret1.Bytes(), objectRoot, domain)
	sig2 := SignWithDomain(secret2.Bytes(), objectRoot, domain)

	aggSig := crypto.AggregateSignatures([][96]byte{sig1, sig2})

	if !VerifySyncCommitteeBLSSignature(
		[][48]byte{pk1, pk2}, blockRoot, aggSig, testForkVersion, testGenesisRoot,
	) {
		t.Fatal("valid sync committee signature should verify")
	}
}

func TestVerifySyncCommitteeBLSSignatureWrongRoot(t *testing.T) {
	secret := big.NewInt(20001)
	pk := crypto.BLSPubkeyFromSecret(secret)

	blockRoot := types.Hash{0xDD}
	domain := DomainSeparation(DomainSyncCommittee, testForkVersion, testGenesisRoot)
	var objectRoot [32]byte
	copy(objectRoot[:], blockRoot[:])
	sig := SignWithDomain(secret.Bytes(), objectRoot, domain)

	wrongRoot := types.Hash{0xEE}
	if VerifySyncCommitteeBLSSignature(
		[][48]byte{pk}, wrongRoot, sig, testForkVersion, testGenesisRoot,
	) {
		t.Fatal("sync committee signature should not verify with wrong block root")
	}
}

func TestVerifySyncCommitteeBLSSignatureEmptyPubkeys(t *testing.T) {
	var sig [96]byte
	blockRoot := types.Hash{0x01}

	if VerifySyncCommitteeBLSSignature(nil, blockRoot, sig, testForkVersion, testGenesisRoot) {
		t.Fatal("should reject empty pubkeys")
	}
}

func TestSignWithDomain(t *testing.T) {
	secret := big.NewInt(42)
	objectRoot := [32]byte{0x01}
	domain := [32]byte{0x02}

	sig := SignWithDomain(secret.Bytes(), objectRoot, domain)

	// Verify deterministic.
	sig2 := SignWithDomain(secret.Bytes(), objectRoot, domain)
	if sig != sig2 {
		t.Fatal("SignWithDomain is not deterministic")
	}

	// Should not be all zeros.
	allZero := true
	for _, b := range sig {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("signature is all zeros")
	}
}

func TestDomainConstants(t *testing.T) {
	// Verify domain types match the spec values.
	if DomainBeaconProposer != [4]byte{0x00, 0x00, 0x00, 0x00} {
		t.Fatal("DomainBeaconProposer mismatch")
	}
	if DomainBeaconAttester != [4]byte{0x01, 0x00, 0x00, 0x00} {
		t.Fatal("DomainBeaconAttester mismatch")
	}
	if DomainRandao != [4]byte{0x02, 0x00, 0x00, 0x00} {
		t.Fatal("DomainRandao mismatch")
	}
	if DomainDeposit != [4]byte{0x03, 0x00, 0x00, 0x00} {
		t.Fatal("DomainDeposit mismatch")
	}
	if DomainVoluntaryExit != [4]byte{0x04, 0x00, 0x00, 0x00} {
		t.Fatal("DomainVoluntaryExit mismatch")
	}
	if DomainSyncCommittee != [4]byte{0x07, 0x00, 0x00, 0x00} {
		t.Fatal("DomainSyncCommittee mismatch")
	}
}

func TestHashCheckpointDeterministic(t *testing.T) {
	cp := Checkpoint{Epoch: 10, Root: types.Hash{0x01}}
	h1 := hashCheckpoint(cp)
	h2 := hashCheckpoint(cp)
	if h1 != h2 {
		t.Fatal("hashCheckpoint is not deterministic")
	}
}
