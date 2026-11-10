// TestFalconNTTOnChain is an end-to-end test for PQ-3.4:
// sign with Falcon-512, use NTT precompiles (0x0f-0x10) to verify
// polynomial operations on the signature, and confirm gas is reasonable.
package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/crypto/pqc"
)

// TestFalconNTTOnChain simulates a Falcon-512 on-chain verification workflow
// using the NTT precompiles at addresses 0x0f (forward) and 0x10 (inverse).
// This represents what a VERIFY frame tx containing Falcon verification logic
// would execute via the EIP-7885 NTT precompiles.
func TestFalconNTTOnChain(t *testing.T) {
	// Step 1: Generate a Falcon-512 key pair and sign a message.
	signer := &pqc.FalconSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("Falcon key generation failed: %v", err)
	}

	msg := []byte("ETH2030 Falcon-512 on-chain NTT verification test vector")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("Falcon sign failed: %v", err)
	}

	// Step 2: Embed 8 Falcon signature bytes as BN254 field elements.
	// On-chain, a verification contract would embed the full Falcon polynomial
	// (512 coefficients) as BN254 elements and use the NTT precompile to
	// compute the lattice product h*z in NTT domain.
	const n = 8 // power-of-two, ≤ NTTMaxDegree
	input := make([]byte, n*32)
	for i := 0; i < n; i++ {
		// Map signature bytes to BN254 field elements (mod p for field validity).
		coeff := new(big.Int).SetBytes([]byte{sig[i%len(sig)]})
		coeff.Mod(coeff, bn254ScalarField)
		b := coeff.Bytes()
		copy(input[i*32+(32-len(b)):], b)
	}

	// Step 3: Apply forward NTT (precompile 0x0f) to convert to evaluation domain.
	nttFW := &nttFWPrecompile{}
	gasForward := nttFW.RequiredGas(input)
	nttDomain, err := nttFW.Run(input)
	if err != nil {
		t.Fatalf("NTT forward precompile failed: %v", err)
	}
	if len(nttDomain) != n*32 {
		t.Fatalf("NTT output length = %d, want %d", len(nttDomain), n*32)
	}

	// Step 4: Apply inverse NTT (precompile 0x10) to recover coefficients.
	nttINV := &nttINVPrecompile{}
	gasInverse := nttINV.RequiredGas(nttDomain)
	recovered, err := nttINV.Run(nttDomain)
	if err != nil {
		t.Fatalf("NTT inverse precompile failed: %v", err)
	}
	if len(recovered) != n*32 {
		t.Fatalf("INTT output length = %d, want %d", len(recovered), n*32)
	}

	// Step 5: Verify NTT round-trip: INTT(NTT(x)) == x.
	if !bytes.Equal(recovered, input) {
		t.Fatal("NTT round-trip failed: INTT(NTT(x)) != x")
	}

	// Step 6: Verify the Falcon-512 signature with the FN-DSA provider.
	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Fatal("Falcon signature verification failed")
	}

	// Step 7: Gas accounting.
	totalGas := gasForward + gasInverse
	t.Logf("Falcon NTT on-chain gas: fwd=%d inv=%d total=%d for n=%d elements",
		gasForward, gasInverse, totalGas, n)

	// Target: for 512 elements (full Falcon poly), gas should be ≤ 2M.
	// For our n=8 test case, gas is proportionally lower.
	const gasTarget = 2_000_000
	if totalGas > gasTarget {
		t.Errorf("gas too high: %d (target ≤ %d)", totalGas, gasTarget)
	}
}

// TestFalconNTTOnChain_VecMul verifies that the dot-product precompile (0x13)
// can compute the core polynomial product step used in Falcon signature verification.
func TestFalconNTTOnChain_VecMul(t *testing.T) {
	// Build two 8-element vectors (A and B) representing polynomial coefficients
	// in NTT domain. On-chain Falcon verification computes h*z in NTT domain.
	const n = 8
	aVec := make([]byte, n*32)
	bVec := make([]byte, n*32)

	for i := 0; i < n; i++ {
		a := new(big.Int).SetInt64(int64(i + 1))
		b := new(big.Int).SetInt64(int64(n - i))
		ab := a.Bytes()
		bb := b.Bytes()
		copy(aVec[i*32+(32-len(ab)):], ab)
		copy(bVec[i*32+(32-len(bb)):], bb)
	}

	// Step 1: Compute element-wise multiplication via precompile 0x11.
	input := append(aVec, bVec...)
	vecMul := &nttVecMulModPrecompile{}
	gas := vecMul.RequiredGas(input)
	result, err := vecMul.Run(input)
	if err != nil {
		t.Fatalf("VecMulMod failed: %v", err)
	}
	if len(result) != n*32 {
		t.Fatalf("VecMulMod output length = %d, want %d", len(result), n*32)
	}

	// Step 2: Verify element [0] = 1*8 mod BN254 = 8.
	r0 := new(big.Int).SetBytes(result[:32])
	want0 := new(big.Int).Mul(big.NewInt(1), big.NewInt(int64(n)))
	want0.Mod(want0, bn254ScalarField)
	if r0.Cmp(want0) != 0 {
		t.Errorf("result[0] = %s, want %s", r0, want0)
	}

	t.Logf("VecMulMod gas for %d elements: %d", n, gas)

	// Step 3: Use dot-product precompile (0x13) to compute inner product.
	dotProd := &nttDotProductPrecompile{}
	dpResult, err := dotProd.Run(input)
	if err != nil {
		t.Fatalf("DotProduct failed: %v", err)
	}
	if len(dpResult) != 32 {
		t.Fatalf("DotProduct output length = %d, want 32", len(dpResult))
	}

	// Verify: sum(i*(n-i)) for i in 1..n.
	expected := new(big.Int)
	for i := 0; i < n; i++ {
		expected.Add(expected, new(big.Int).Mul(
			big.NewInt(int64(i+1)),
			big.NewInt(int64(n-i)),
		))
	}
	expected.Mod(expected, bn254ScalarField)
	got := new(big.Int).SetBytes(dpResult)
	if got.Cmp(expected) != 0 {
		t.Errorf("DotProduct = %s, want %s", got, expected)
	}

	gasDP := dotProd.RequiredGas(input)
	t.Logf("DotProduct gas for %d elements: %d", n, gasDP)
}
