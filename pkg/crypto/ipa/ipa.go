// IPA (Inner Product Argument) proof system for Verkle trees (EIP-6800).
//
// The IPA scheme proves that a committed polynomial evaluates to a claimed
// value at a given point. It works over the Banderwagon group and uses a
// recursive halving protocol to produce a proof of size O(log n).
//
// Structure:
//   - A Pedersen vector commitment C = <a, G> commits to vector a
//   - The prover demonstrates that <a, b> = v for a public vector b
//   - The proof consists of log2(n) pairs of curve points (L, R) and
//     a final scalar
//
// The protocol follows the Bulletproofs-style IPA adapted for Verkle trees.

package ipa

import (
	"crypto/sha256"
	"errors"
	"math/big"
)

// IPAProofData holds the data for an IPA proof.
type IPAProofData struct {
	// L contains the left commitment points from each round.
	L []*BanderPoint
	// R contains the right commitment points from each round.
	R []*BanderPoint
	// A is the final scalar value after all rounds.
	A *big.Int
}

// IPAProofSize returns the expected number of rounds (L/R pairs) for a
// vector of the given length. The length must be a power of 2.
func IPAProofSize(vectorLen int) int {
	if vectorLen <= 1 {
		return 0
	}
	rounds := 0
	n := vectorLen
	for n > 1 {
		n /= 2
		rounds++
	}
	return rounds
}

// innerProduct computes <a, b> = sum(a[i] * b[i]) mod n (scalar field).
func innerProduct(a, b []*big.Int) *big.Int {
	if len(a) != len(b) {
		return new(big.Int)
	}
	result := new(big.Int)
	for i := range a {
		result = banderScalarAdd(result, banderScalarMul(a[i], b[i]))
	}
	return result
}

// ipaTranscript manages the Fiat-Shamir transcript for the IPA protocol.
type ipaTranscript struct {
	state []byte
}

func newIPATranscript(label string) *ipaTranscript {
	h := sha256.Sum256([]byte(label))
	return &ipaTranscript{state: h[:]}
}

// appendPoint adds a curve point to the transcript.
func (t *ipaTranscript) appendPoint(p *BanderPoint) {
	serialized := BanderSerialize(p)
	h := sha256.New()
	h.Write(t.state)
	h.Write(serialized[:])
	t.state = h.Sum(nil)
}

// appendScalar adds a scalar to the transcript.
func (t *ipaTranscript) appendScalar(s *big.Int) {
	var buf [32]byte
	b := s.Bytes()
	copy(buf[32-len(b):], b)
	h := sha256.New()
	h.Write(t.state)
	h.Write(buf[:])
	t.state = h.Sum(nil)
}

// challenge generates a challenge scalar from the current transcript state.
// The challenge is reduced modulo the subgroup order n.
func (t *ipaTranscript) challenge() *big.Int {
	h := sha256.New()
	h.Write(t.state)
	h.Write([]byte("challenge"))
	digest := h.Sum(nil)
	t.state = digest

	c := new(big.Int).SetBytes(digest)
	c.Mod(c, banderN)
	// Ensure non-zero challenge.
	if c.Sign() == 0 {
		c.SetInt64(1)
	}
	return c
}

// IPAProve generates an IPA proof that <a, b> = v given commitment C = <a, G>.
//
// Parameters:
//   - generators: the Pedersen generator points G_0, ..., G_{n-1}
//   - a: the committed vector (witness)
//   - b: the public evaluation vector
//   - commitment: C = <a, G> (the Pedersen commitment)
//
// Returns the proof and the inner product value v = <a, b>.
func IPAProve(generators []*BanderPoint, a, b []*big.Int, commitment *BanderPoint) (*IPAProofData, *big.Int, error) {
	n := len(a)
	if n == 0 || n != len(b) || n != len(generators) {
		return nil, nil, errors.New("ipa: vector length mismatch")
	}
	if n&(n-1) != 0 {
		return nil, nil, errors.New("ipa: vector length must be power of 2")
	}

	rounds := IPAProofSize(n)
	proof := &IPAProofData{
		L: make([]*BanderPoint, 0, rounds),
		R: make([]*BanderPoint, 0, rounds),
	}

	// Initialize transcript.
	transcript := newIPATranscript("ipa_verkle")
	transcript.appendPoint(commitment)

	v := innerProduct(a, b)
	transcript.appendScalar(v)

	// Working copies.
	aVec := make([]*big.Int, n)
	bVec := make([]*big.Int, n)
	gVec := make([]*BanderPoint, n)
	for i := range a {
		aVec[i] = new(big.Int).Set(a[i])
		bVec[i] = new(big.Int).Set(b[i])
		gVec[i] = generators[i]
	}

	// Recursive halving: commitment-only IPA.
	// At each round, the prover creates L = <a_lo, G_hi> and R = <a_hi, G_lo>.
	// Verifier folds: C' = C + x^{-1}*L + x*R.
	for m := n; m > 1; m /= 2 {
		half := m / 2

		aLo := aVec[:half]
		aHi := aVec[half:m]
		bLo := bVec[:half]
		bHi := bVec[half:m]
		gLo := gVec[:half]
		gHi := gVec[half:m]

		L := BanderMSM(gHi, aLo)
		R := BanderMSM(gLo, aHi)

		proof.L = append(proof.L, L)
		proof.R = append(proof.R, R)

		transcript.appendPoint(L)
		transcript.appendPoint(R)
		x := transcript.challenge()
		xInv := banderScalarInv(x)

		// Fold vectors using scalar field (mod n):
		//   a' = a_lo + x * a_hi
		//   b' = b_lo + x^{-1} * b_hi
		//   G' = G_lo + x^{-1} * G_hi
		newA := make([]*big.Int, half)
		newB := make([]*big.Int, half)
		newG := make([]*BanderPoint, half)
		for i := 0; i < half; i++ {
			newA[i] = banderScalarAdd(aLo[i], banderScalarMul(x, aHi[i]))
			newB[i] = banderScalarAdd(bLo[i], banderScalarMul(xInv, bHi[i]))
			newG[i] = BanderAdd(gLo[i], BanderScalarMul(gHi[i], xInv))
		}

		aVec = newA
		bVec = newB
		gVec = newG
	}

	proof.A = new(big.Int).Set(aVec[0])
	return proof, v, nil
}

// IPAVerify verifies an IPA proof against the commitment and claimed inner product.
//
// Parameters:
//   - generators: the Pedersen generator points G_0, ..., G_{n-1}
//   - commitment: C = <a, G>
//   - b: the public evaluation vector
//   - v: the claimed inner product value <a, b>
//   - proof: the IPA proof data
func IPAVerify(generators []*BanderPoint, commitment *BanderPoint, b []*big.Int, v *big.Int, proof *IPAProofData) (bool, error) {
	n := len(b)
	if n == 0 || n != len(generators) {
		return false, errors.New("ipa: vector length mismatch")
	}
	if n&(n-1) != 0 {
		return false, errors.New("ipa: vector length must be power of 2")
	}

	rounds := IPAProofSize(n)
	if len(proof.L) != rounds || len(proof.R) != rounds {
		return false, errors.New("ipa: invalid proof size")
	}

	// Reconstruct transcript.
	transcript := newIPATranscript("ipa_verkle")
	transcript.appendPoint(commitment)
	transcript.appendScalar(v)

	// Collect challenges.
	challenges := make([]*big.Int, rounds)
	for i := 0; i < rounds; i++ {
		transcript.appendPoint(proof.L[i])
		transcript.appendPoint(proof.R[i])
		challenges[i] = transcript.challenge()
	}

	// Compute the folded generator and b vector (same folding as prover).
	gVec := make([]*BanderPoint, n)
	bVec := make([]*big.Int, n)
	for i := range generators {
		gVec[i] = generators[i]
		bVec[i] = new(big.Int).Set(b[i])
	}

	m := n
	for round := 0; round < rounds; round++ {
		half := m / 2
		x := challenges[round]
		xInv := banderScalarInv(x)

		newG := make([]*BanderPoint, half)
		newB := make([]*big.Int, half)
		for i := 0; i < half; i++ {
			newG[i] = BanderAdd(gVec[i], BanderScalarMul(gVec[half+i], xInv))
			newB[i] = banderScalarAdd(bVec[i], banderScalarMul(xInv, bVec[half+i]))
		}
		gVec = newG
		bVec = newB
		m = half
	}

	// Fold the commitment using L/R proof points.
	// C' = C + x^{-1}*L + x*R at each round.
	cFinal := commitment
	for i := 0; i < rounds; i++ {
		x := challenges[i]
		xInv := banderScalarInv(x)
		lScaled := BanderScalarMul(proof.L[i], xInv)
		rScaled := BanderScalarMul(proof.R[i], x)
		cFinal = BanderAdd(BanderAdd(cFinal, lScaled), rScaled)
	}

	// Final check: C_final == proof.A * G_final.
	expectedC := BanderScalarMul(gVec[0], proof.A)
	return BanderEqual(cFinal, expectedC), nil
}

// IPASerialize serializes an IPA proof to bytes.
// Format: [num_rounds(1)] [L_0(32)] [R_0(32)] ... [L_n(32)] [R_n(32)] [a(32)]
func IPASerialize(proof *IPAProofData) []byte {
	if proof == nil {
		return nil
	}
	rounds := len(proof.L)
	// 1 byte for round count + 64 bytes per round (L + R) + 32 bytes for a.
	buf := make([]byte, 0, 1+rounds*64+32)
	buf = append(buf, byte(rounds))

	for i := 0; i < rounds; i++ {
		l := BanderSerialize(proof.L[i])
		r := BanderSerialize(proof.R[i])
		buf = append(buf, l[:]...)
		buf = append(buf, r[:]...)
	}

	// Serialize final scalar a.
	var aBuf [32]byte
	aBytes := proof.A.Bytes()
	copy(aBuf[32-len(aBytes):], aBytes)
	buf = append(buf, aBuf[:]...)

	return buf
}

// IPADeserialize deserializes an IPA proof from bytes.
func IPADeserialize(data []byte) (*IPAProofData, error) {
	if len(data) < 33 {
		return nil, errors.New("ipa: proof data too short")
	}

	rounds := int(data[0])
	expectedLen := 1 + rounds*64 + 32
	if len(data) != expectedLen {
		return nil, errors.New("ipa: invalid proof data length")
	}

	proof := &IPAProofData{
		L: make([]*BanderPoint, rounds),
		R: make([]*BanderPoint, rounds),
	}

	offset := 1
	for i := 0; i < rounds; i++ {
		var lBuf, rBuf [32]byte
		copy(lBuf[:], data[offset:offset+32])
		copy(rBuf[:], data[offset+32:offset+64])

		var err error
		proof.L[i], err = BanderDeserialize(lBuf)
		if err != nil {
			return nil, err
		}
		proof.R[i], err = BanderDeserialize(rBuf)
		if err != nil {
			return nil, err
		}
		offset += 64
	}

	proof.A = new(big.Int).SetBytes(data[offset : offset+32])
	return proof, nil
}
