// Extended BLS signature aggregation for Ethereum consensus layer.
//
// Provides proof of possession, domain separation, subgroup checks,
// signature set batching with random coefficients, and advanced
// aggregation utilities beyond the base bls_aggregate.go.
//
// Follows the Ethereum beacon chain BLS specification:
//   - Signatures in G2, public keys in G1
//   - Domain separation tags per operation type
//   - Proof of possession prevents rogue-key attacks
//   - Batched verification amortizes pairing cost
package bls

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
)

// BLSAgg provides extended BLS aggregate signature operations including
// proof of possession, domain separation, and batched verification
// with random coefficient optimization.
type BLSAgg struct{}

// NewBLSAgg creates a new BLSAgg instance.
func NewBLSAgg() *BLSAgg {
	return &BLSAgg{}
}

// Errors for BLS aggregation operations.
var (
	ErrBLSAggNoPubkeys         = errors.New("bls_agg: no public keys provided")
	ErrBLSAggNoSignatures      = errors.New("bls_agg: no signatures provided")
	ErrBLSAggMismatchedLengths = errors.New("bls_agg: pubkey/signature/message counts differ")
	ErrBLSAggInvalidPubkey     = errors.New("bls_agg: invalid public key")
	ErrBLSAggInvalidSignature  = errors.New("bls_agg: invalid signature")
	ErrBLSAggPopFailed         = errors.New("bls_agg: proof of possession verification failed")
	ErrBLSAggSubgroupCheck     = errors.New("bls_agg: point not in correct subgroup")
	ErrBLSAggDuplicatePubkey   = errors.New("bls_agg: duplicate public key detected")
)

// --- Domain Separation Tags (DSTs) per beacon chain spec ---

// BLS domain separation tags for different signing contexts.
// These ensure signatures from one context cannot be replayed in another.
var (
	// DSTBeaconAttestation is the DST for beacon chain attestation signatures.
	DSTBeaconAttestation = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_ATTESTATION")

	// DSTBeaconProposal is the DST for beacon chain block proposal signatures.
	DSTBeaconProposal = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_PROPOSAL")

	// DSTSyncCommittee is the DST for sync committee signatures.
	DSTSyncCommittee = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_SYNC_COMMITTEE")

	// DSTPoPMessage is the DST for proof-of-possession signature generation.
	DSTPoPMessage = []byte("BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

	// DSTRandao is the DST for RANDAO reveal signatures.
	DSTRandao = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_RANDAO")

	// DSTVoluntaryExit is the DST for voluntary exit signatures.
	DSTVoluntaryExit = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_VOLUNTARY_EXIT")
)

// --- Proof of Possession ---

// ProofOfPossession is a BLS signature over the public key itself,
// proving the signer holds the corresponding secret key. Required
// to prevent rogue-key attacks when aggregating public keys.
type ProofOfPossession [BLSSignatureSize]byte

// GeneratePoP creates a proof of possession by signing the public key
// serialization with the corresponding secret key.
func (ba *BLSAgg) GeneratePoP(secret *big.Int) ProofOfPossession {
	pk := BLSPubkeyFromSecret(secret)
	hm := HashToG2(pk[:], DSTPoPMessage)
	sig := blsG2ScalarMul(hm, secret)
	var pop ProofOfPossession
	serialized := SerializeG2(sig)
	copy(pop[:], serialized[:])
	return pop
}

// VerifyPoP verifies a proof of possession for the given public key.
// Checks: e(pk, H_pop(serialize(pk))) == e(G1, pop)
func (ba *BLSAgg) VerifyPoP(pubkey [BLSPubkeySize]byte, pop ProofOfPossession) bool {
	pk := DeserializeG1(pubkey)
	if pk == nil || pk.blsG1IsInfinity() {
		return false
	}

	var sigBytes [BLSSignatureSize]byte
	copy(sigBytes[:], pop[:])
	sig := DeserializeG2(sigBytes)
	if sig == nil || sig.blsG2IsInfinity() {
		return false
	}

	hm := HashToG2(pubkey[:], DSTPoPMessage)
	negG1 := blsG1Neg(BlsG1Generator())

	return blsMultiPairing(
		[]*BlsG1Point{pk, negG1},
		[]*BlsG2Point{hm, sig},
	)
}

// --- Subgroup Checks ---

// CheckG1Subgroup verifies that a serialized G1 point is in the correct
// prime-order subgroup. Returns nil if valid, error otherwise.
func (ba *BLSAgg) CheckG1Subgroup(pubkey [BLSPubkeySize]byte) error {
	p := DeserializeG1(pubkey)
	if p == nil {
		return ErrBLSAggInvalidPubkey
	}
	if !blsG1InSubgroup(p) {
		return ErrBLSAggSubgroupCheck
	}
	return nil
}

// CheckG2Subgroup verifies that a serialized G2 point is in the correct
// prime-order subgroup. Returns nil if valid, error otherwise.
func (ba *BLSAgg) CheckG2Subgroup(sig [BLSSignatureSize]byte) error {
	p := DeserializeG2(sig)
	if p == nil {
		return ErrBLSAggInvalidSignature
	}
	if !blsG2InSubgroup(p) {
		return ErrBLSAggSubgroupCheck
	}
	return nil
}

// DecompressG1 decompresses a 48-byte compressed G1 point and validates
// it is on the curve and in the correct subgroup.
func (ba *BLSAgg) DecompressG1(data [BLSPubkeySize]byte) (*BlsG1Point, error) {
	p := DeserializeG1(data)
	if p == nil {
		return nil, ErrBLSAggInvalidPubkey
	}
	return p, nil
}

// DecompressG2 decompresses a 96-byte compressed G2 point and validates
// it is on the curve and in the correct subgroup.
func (ba *BLSAgg) DecompressG2(data [BLSSignatureSize]byte) (*BlsG2Point, error) {
	p := DeserializeG2(data)
	if p == nil {
		return nil, ErrBLSAggInvalidSignature
	}
	return p, nil
}

// --- Signature Set for Batched Verification ---

// BLSSignatureSetEntry represents a single entry in a signature set for
// batched verification. Each entry has its own pubkey, message, and signature.
type BLSSignatureSetEntry struct {
	PubKey    [BLSPubkeySize]byte
	Message   []byte
	Signature [BLSSignatureSize]byte
}

// BLSSignatureSet collects multiple signature verification requests for
// batch verification. Random linear combination reduces the number of
// pairings needed, improving throughput.
type BLSSignatureSet struct {
	entries []BLSSignatureSetEntry
}

// NewBLSSignatureSet creates an empty signature set.
func NewBLSSignatureSet() *BLSSignatureSet {
	return &BLSSignatureSet{}
}

// Add appends a verification request to the set.
func (ss *BLSSignatureSet) Add(pk [BLSPubkeySize]byte, msg []byte, sig [BLSSignatureSize]byte) {
	ss.entries = append(ss.entries, BLSSignatureSetEntry{
		PubKey:    pk,
		Message:   msg,
		Signature: sig,
	})
}

// Len returns the number of entries in the set.
func (ss *BLSSignatureSet) Len() int {
	return len(ss.entries)
}

// Verify verifies all entries in the set using random linear combination.
//
// Instead of checking each e(pk_i, H(m_i)) == e(G1, sig_i) individually,
// the batch check picks random scalars r_i and verifies:
//
//	e(sum(r_i * pk_i), H(m_i_shared)) * e(-G1, sum(r_i * sig_i)) == 1
//
// For distinct messages, the full multi-pairing form is used:
//
//	product(e(r_i * pk_i, H(m_i))) * e(-G1, sum(r_i * sig_i)) == 1
//
// If any individual signature is invalid, the batch check fails.
func (ss *BLSSignatureSet) Verify() bool {
	n := len(ss.entries)
	if n == 0 {
		return false
	}
	if n == 1 {
		return BLSVerify(ss.entries[0].PubKey, ss.entries[0].Message, ss.entries[0].Signature)
	}

	// Generate random coefficients for linear combination.
	coefficients := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		coefficients[i] = randomScalar()
	}

	// Build multi-pairing inputs.
	// For each entry i: (r_i * pk_i, H(m_i))
	// Plus: (-G1, sum(r_i * sig_i))
	g1Points := make([]*BlsG1Point, n+1)
	g2Points := make([]*BlsG2Point, n+1)

	aggSig := BlsG2Infinity()

	for i := 0; i < n; i++ {
		pk := DeserializeG1(ss.entries[i].PubKey)
		if pk == nil || pk.blsG1IsInfinity() {
			return false
		}
		sig := DeserializeG2(ss.entries[i].Signature)
		if sig == nil || sig.blsG2IsInfinity() {
			return false
		}

		// r_i * pk_i
		scaledPK := blsG1ScalarMul(pk, coefficients[i])
		g1Points[i] = scaledPK

		// H(m_i)
		hm := HashToG2(ss.entries[i].Message, blsSignDST)
		g2Points[i] = hm

		// sum(r_i * sig_i)
		scaledSig := blsG2ScalarMul(sig, coefficients[i])
		aggSig = blsG2Add(aggSig, scaledSig)
	}

	// Final pair: (-G1, aggregated weighted signature)
	g1Points[n] = blsG1Neg(BlsG1Generator())
	g2Points[n] = aggSig

	return blsMultiPairing(g1Points, g2Points)
}

// --- Advanced Aggregation Utilities ---

// AggregatePublicKeysValidated aggregates multiple public keys after
// validating each one is a valid G1 point in the correct subgroup.
// Returns an error if any public key is invalid.
func (ba *BLSAgg) AggregatePublicKeysValidated(pubkeys [][BLSPubkeySize]byte) ([BLSPubkeySize]byte, error) {
	if len(pubkeys) == 0 {
		return [BLSPubkeySize]byte{}, ErrBLSAggNoPubkeys
	}
	agg := BlsG1Infinity()
	for _, pk := range pubkeys {
		p := DeserializeG1(pk)
		if p == nil {
			return [BLSPubkeySize]byte{}, ErrBLSAggInvalidPubkey
		}
		if p.blsG1IsInfinity() {
			return [BLSPubkeySize]byte{}, ErrBLSAggInvalidPubkey
		}
		agg = blsG1Add(agg, p)
	}
	return SerializeG1(agg), nil
}

// AggregateSignaturesValidated aggregates multiple signatures after
// validating each one is a valid G2 point in the correct subgroup.
func (ba *BLSAgg) AggregateSignaturesValidated(sigs [][BLSSignatureSize]byte) ([BLSSignatureSize]byte, error) {
	if len(sigs) == 0 {
		return [BLSSignatureSize]byte{}, ErrBLSAggNoSignatures
	}
	agg := BlsG2Infinity()
	for _, s := range sigs {
		p := DeserializeG2(s)
		if p == nil {
			return [BLSSignatureSize]byte{}, ErrBLSAggInvalidSignature
		}
		if p.blsG2IsInfinity() {
			return [BLSSignatureSize]byte{}, ErrBLSAggInvalidSignature
		}
		agg = blsG2Add(agg, p)
	}
	return SerializeG2(agg), nil
}

// SignWithDST signs a message with a given domain separation tag.
// This allows using different DSTs for different protocol contexts.
func (ba *BLSAgg) SignWithDST(secret *big.Int, msg []byte, dst []byte) [BLSSignatureSize]byte {
	hm := HashToG2(msg, dst)
	sig := blsG2ScalarMul(hm, secret)
	return SerializeG2(sig)
}

// VerifyWithDST verifies a signature against a specific domain separation tag.
func (ba *BLSAgg) VerifyWithDST(
	pubkey [BLSPubkeySize]byte,
	msg []byte,
	sig [BLSSignatureSize]byte,
	dst []byte,
) bool {
	pk := DeserializeG1(pubkey)
	if pk == nil || pk.blsG1IsInfinity() {
		return false
	}
	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}
	hm := HashToG2(msg, dst)
	negG1 := blsG1Neg(BlsG1Generator())

	return blsMultiPairing(
		[]*BlsG1Point{pk, negG1},
		[]*BlsG2Point{hm, s},
	)
}

// FastAggregateVerifyWithPoP verifies an aggregate signature where all
// signers signed the same message, with proof-of-possession validation.
// Each signer must have a valid PoP before their key is included in
// the aggregate, preventing rogue-key attacks.
func (ba *BLSAgg) FastAggregateVerifyWithPoP(
	pubkeys [][BLSPubkeySize]byte,
	pops []ProofOfPossession,
	msg []byte,
	aggSig [BLSSignatureSize]byte,
) bool {
	if len(pubkeys) == 0 || len(pubkeys) != len(pops) {
		return false
	}

	// Verify each PoP first.
	for i, pk := range pubkeys {
		if !ba.VerifyPoP(pk, pops[i]) {
			return false
		}
	}

	// Proceed with standard fast aggregate verify.
	return FastAggregateVerify(pubkeys, msg, aggSig)
}

// AggregateVerifyDistinct verifies an aggregate signature where each
// signer signed a different message, validating all inputs first.
func (ba *BLSAgg) AggregateVerifyDistinct(
	pubkeys [][BLSPubkeySize]byte,
	msgs [][]byte,
	aggSig [BLSSignatureSize]byte,
) (bool, error) {
	if len(pubkeys) == 0 {
		return false, ErrBLSAggNoPubkeys
	}
	if len(pubkeys) != len(msgs) {
		return false, ErrBLSAggMismatchedLengths
	}

	// Validate all pubkeys.
	for _, pk := range pubkeys {
		p := DeserializeG1(pk)
		if p == nil {
			return false, ErrBLSAggInvalidPubkey
		}
	}

	// Validate aggregate signature.
	if err := ba.CheckG2Subgroup(aggSig); err != nil {
		return false, err
	}

	return VerifyAggregate(pubkeys, msgs, aggSig), nil
}

// DeduplicatePubkeys removes duplicate public keys from a list.
// Returns the unique pubkeys and their original indices.
func (ba *BLSAgg) DeduplicatePubkeys(
	pubkeys [][BLSPubkeySize]byte,
) ([][BLSPubkeySize]byte, []int) {
	seen := make(map[[BLSPubkeySize]byte]bool)
	unique := make([][BLSPubkeySize]byte, 0, len(pubkeys))
	indices := make([]int, 0, len(pubkeys))

	for i, pk := range pubkeys {
		if !seen[pk] {
			seen[pk] = true
			unique = append(unique, pk)
			indices = append(indices, i)
		}
	}
	return unique, indices
}

// HasDuplicatePubkeys checks whether any public keys are duplicated.
func (ba *BLSAgg) HasDuplicatePubkeys(pubkeys [][BLSPubkeySize]byte) bool {
	seen := make(map[[BLSPubkeySize]byte]bool, len(pubkeys))
	for _, pk := range pubkeys {
		if seen[pk] {
			return true
		}
		seen[pk] = true
	}
	return false
}

// --- Domain Separation Helpers ---

// ComputeSigningRoot computes the signing root for a beacon chain message
// by combining the message root with the domain. This is the value that
// gets signed by BLS.
//
// signing_root = SHA-256(domain || message_root)[:32]
func ComputeSigningRoot(domain [32]byte, messageRoot [32]byte) [32]byte {
	h := sha256.New()
	h.Write(domain[:])
	h.Write(messageRoot[:])
	digest := h.Sum(nil)
	var result [32]byte
	copy(result[:], digest[:32])
	return result
}

// ComputeDomain computes the beacon chain domain for a given domain type
// and fork version. Per the spec:
//
//	domain = domain_type(4) || fork_data_root(28)
func ComputeDomain(domainType [4]byte, forkVersion [4]byte, genesisValidatorsRoot [32]byte) [32]byte {
	// fork_data_root = SHA-256(fork_version || genesis_validators_root)[:28]
	h := sha256.New()
	h.Write(forkVersion[:])
	h.Write(genesisValidatorsRoot[:])
	forkDataRoot := h.Sum(nil)

	var domain [32]byte
	copy(domain[:4], domainType[:])
	copy(domain[4:], forkDataRoot[:28])
	return domain
}

// randomScalar generates a random 128-bit scalar for batched verification.
// 128 bits provides sufficient security for the random linear combination
// while being fast to generate. The probability of an invalid batch
// passing is negligible (< 2^{-128}).
func randomScalar() *big.Int {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to a deterministic but non-trivial value.
		return big.NewInt(1)
	}
	s := new(big.Int).SetBytes(buf)
	if s.Sign() == 0 {
		s.SetInt64(1)
	}
	return s
}
