package ipa

import (
	"math/big"
	"sync"
	"testing"
)

func TestIPAIntegrationValidateProofNil(t *testing.T) {
	if err := ValidateIPAProof(nil); err != ErrIPANilProof {
		t.Fatalf("expected ErrIPANilProof, got %v", err)
	}
}

func TestIPAIntegrationValidateProofNilA(t *testing.T) {
	p := &IPAProofData{L: []*BanderPoint{BanderGenerator()}, R: []*BanderPoint{BanderGenerator()}, A: nil}
	if err := ValidateIPAProof(p); err != ErrIPANilA {
		t.Fatalf("expected ErrIPANilA, got %v", err)
	}
}

func TestIPAIntegrationValidateProofLRMismatch(t *testing.T) {
	p := &IPAProofData{L: []*BanderPoint{BanderGenerator()}, R: []*BanderPoint{BanderGenerator(), BanderGenerator()}, A: big.NewInt(42)}
	if err := ValidateIPAProof(p); err != ErrIPALRLengthMismatch {
		t.Fatalf("expected LR mismatch, got %v", err)
	}
}

func TestIPAIntegrationValidateProofNilPoints(t *testing.T) {
	if err := ValidateIPAProof(&IPAProofData{L: []*BanderPoint{nil}, R: []*BanderPoint{BanderGenerator()}, A: big.NewInt(1)}); err == nil {
		t.Fatal("expected error for nil L")
	}
	if err := ValidateIPAProof(&IPAProofData{L: []*BanderPoint{BanderGenerator()}, R: []*BanderPoint{nil}, A: big.NewInt(1)}); err == nil {
		t.Fatal("expected error for nil R")
	}
}

func TestIPAIntegrationValidateProofValid(t *testing.T) {
	p := &IPAProofData{L: []*BanderPoint{BanderGenerator()}, R: []*BanderPoint{BanderGenerator()}, A: big.NewInt(42)}
	if err := ValidateIPAProof(p); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestIPAIntegrationBackendNames(t *testing.T) {
	if (&PureGoIPABackend{}).Name() != "pure-go" {
		t.Fatal("wrong name")
	}
}

type testIPABackend struct{ PureGoIPABackend }

func (*testIPABackend) Name() string { return "test-ipa" }

func TestIPAIntegrationDefaultAndSetBackend(t *testing.T) {
	SetIPABackend(nil)
	if DefaultIPABackend().Name() != "pure-go" {
		t.Fatal("default wrong")
	}
	defer SetIPABackend(nil)
	SetIPABackend(&testIPABackend{})
	if DefaultIPABackend().Name() != "test-ipa" {
		t.Fatal("set wrong")
	}
	SetIPABackend(nil)
	if DefaultIPABackend().Name() != "pure-go" {
		t.Fatal("reset wrong")
	}
}

func TestIPAIntegrationStatus(t *testing.T) {
	SetIPABackend(nil)
	if IPAIntegrationStatus() != "pure-go" {
		t.Fatalf("got %s", IPAIntegrationStatus())
	}
}

func TestIPAIntegrationChallengesDeterministic(t *testing.T) {
	g := BanderGenerator()
	v := big.NewInt(42)
	proof := &IPAProofData{
		L: []*BanderPoint{BanderScalarMul(g, big.NewInt(3))},
		R: []*BanderPoint{BanderScalarMul(g, big.NewInt(5))},
		A: big.NewInt(7),
	}
	ch1, _ := GenerateIPAChallenges(g, v, proof)
	ch2, _ := GenerateIPAChallenges(g, v, proof)
	for i := range ch1 {
		if ch1[i].Cmp(ch2[i]) != 0 {
			t.Fatalf("challenge %d differs", i)
		}
	}
}

func TestIPAIntegrationChallengesDifferentInputs(t *testing.T) {
	g := BanderGenerator()
	proof := &IPAProofData{
		L: []*BanderPoint{BanderScalarMul(g, big.NewInt(3))},
		R: []*BanderPoint{BanderScalarMul(g, big.NewInt(5))},
		A: big.NewInt(7),
	}
	ch1, _ := GenerateIPAChallenges(g, big.NewInt(42), proof)
	ch2, _ := GenerateIPAChallenges(g, big.NewInt(99), proof)
	if ch1[0].Cmp(ch2[0]) == 0 {
		t.Fatal("should differ for different v")
	}
}

func TestIPAIntegrationChallengesNilCommitment(t *testing.T) {
	proof := &IPAProofData{L: []*BanderPoint{BanderGenerator()}, R: []*BanderPoint{BanderGenerator()}, A: big.NewInt(7)}
	if _, err := GenerateIPAChallenges(nil, big.NewInt(42), proof); err == nil {
		t.Fatal("expected error")
	}
}

func TestIPAIntegrationFoldScalarIdentity(t *testing.T) {
	if FoldScalar(nil, 0).Cmp(big.NewInt(1)) != 0 {
		t.Fatal("expected 1")
	}
}

func TestIPAIntegrationFoldScalarSingleRound(t *testing.T) {
	x := big.NewInt(7)
	n := BanderN()
	s0 := FoldScalar([]*big.Int{x}, 0)
	s1 := FoldScalar([]*big.Int{x}, 1)
	if s0.Cmp(big.NewInt(1)) != 0 {
		t.Fatal("index 0 should be 1")
	}
	if s1.Cmp(new(big.Int).ModInverse(x, n)) != 0 {
		t.Fatal("index 1 should be x_inv")
	}
}

func TestIPAIntegrationFoldScalarTwoRounds(t *testing.T) {
	x0, x1 := big.NewInt(3), big.NewInt(5)
	n := BanderN()
	challenges := []*big.Int{x0, x1}
	if FoldScalar(challenges, 0).Cmp(big.NewInt(1)) != 0 {
		t.Fatal("index 0 wrong")
	}
	exp3 := new(big.Int).Mul(new(big.Int).ModInverse(x0, n), new(big.Int).ModInverse(x1, n))
	exp3.Mod(exp3, n)
	if FoldScalar(challenges, 3).Cmp(exp3) != 0 {
		t.Fatal("index 3 wrong")
	}
}

func TestIPAIntegrationBVectorInDomain(t *testing.T) {
	b := ComputeBVector(big.NewInt(2), 4)
	if len(b) != 4 || b[2].Cmp(big.NewInt(1)) != 0 {
		t.Fatal("b[2] should be 1")
	}
	for i := range b {
		if i != 2 && b[i].Sign() != 0 {
			t.Fatalf("b[%d] should be 0", i)
		}
	}
}

func TestIPAIntegrationBVectorOutsideDomain(t *testing.T) {
	b := ComputeBVector(big.NewInt(1000), 4)
	for i, v := range b {
		if v.Sign() == 0 {
			t.Fatalf("b[%d] should be non-zero outside domain", i)
		}
	}
}

func TestIPAIntegrationBVectorZero(t *testing.T) {
	b := ComputeBVector(big.NewInt(0), 4)
	if b[0].Cmp(big.NewInt(1)) != 0 {
		t.Fatal("b[0] should be 1 for eval=0")
	}
}

func TestIPAIntegrationProveVerifyRoundtrip(t *testing.T) {
	n := 4
	gens := GenerateIPAGenerators(n)
	a := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)}
	b := ComputeBVector(big.NewInt(1), n)
	v := innerProduct(a, b)
	if v.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("inner product wrong: %v", v)
	}
	commitment := BanderMSM(gens, a)
	proof, cv, err := IPAProve(gens, a, b, commitment)
	if err != nil {
		t.Fatal(err)
	}
	if cv.Cmp(v) != 0 {
		t.Fatal("computed v mismatch")
	}
	ok, err := IPAVerify(gens, commitment, b, v, proof)
	if err != nil || !ok {
		t.Fatalf("verify failed: ok=%v err=%v", ok, err)
	}
}

func TestIPAIntegrationBackendVerifyProof(t *testing.T) {
	backend := &PureGoIPABackend{}
	n := 4
	gens := GenerateIPAGenerators(n)
	a := []*big.Int{big.NewInt(5), big.NewInt(10), big.NewInt(15), big.NewInt(20)}
	b := ComputeBVector(big.NewInt(0), n)
	commitment := BanderMSM(gens, a)
	proof, v, err := IPAProve(gens, a, b, commitment)
	if err != nil {
		t.Fatal(err)
	}
	cb := BanderSerialize(commitment)
	ok, err := backend.VerifyProof(cb[:], proof, []byte{0x00}, v.Bytes())
	if err != nil || !ok {
		t.Fatalf("verify failed: ok=%v err=%v", ok, err)
	}
}

func TestIPAIntegrationBackendCreateProof(t *testing.T) {
	backend := &PureGoIPABackend{}
	values := [][]byte{{1}, {2}, {3}, {4}}
	proof, err := backend.CreateProof(values, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.L) != 2 {
		t.Fatalf("expected 2 rounds, got %d", len(proof.L))
	}
}

func TestIPAIntegrationBackendErrors(t *testing.T) {
	b := &PureGoIPABackend{}
	if _, err := b.CreateProof(nil, []byte{1}); err == nil {
		t.Fatal("empty values")
	}
	if _, err := b.CreateProof([][]byte{{1}, {2}, {3}, {4}}, nil); err == nil {
		t.Fatal("empty eval")
	}
	if _, err := b.CreateProof([][]byte{{1}, {2}, {3}}, []byte{1}); err == nil {
		t.Fatal("non-pow2")
	}
	if _, err := b.VerifyProof(nil, &IPAProofData{L: []*BanderPoint{BanderGenerator()}, R: []*BanderPoint{BanderGenerator()}, A: big.NewInt(1)}, []byte{1}, []byte{1}); err == nil {
		t.Fatal("empty commit")
	}
	if _, err := b.VerifyProof([]byte{1}, nil, []byte{1}, []byte{1}); err == nil {
		t.Fatal("nil proof")
	}
}

func TestIPAIntegrationConfigValidation(t *testing.T) {
	if err := ValidateIPAIntegrationConfig(DefaultIPAIntegrationConfig()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIPAIntegrationConfig(nil); err == nil {
		t.Fatal("nil")
	}
	if err := ValidateIPAIntegrationConfig(&IPAIntegrationConfig{VectorSize: 3, NumRounds: 2}); err == nil {
		t.Fatal("non-pow2")
	}
	if err := ValidateIPAIntegrationConfig(&IPAIntegrationConfig{VectorSize: 256, NumRounds: 7}); err == nil {
		t.Fatal("wrong rounds")
	}
}

func TestIPAIntegrationValidateProofForConfig(t *testing.T) {
	cfg := &IPAIntegrationConfig{VectorSize: 4, NumRounds: 2}
	g := BanderGenerator()
	good := &IPAProofData{L: []*BanderPoint{g, g}, R: []*BanderPoint{g, g}, A: big.NewInt(1)}
	if err := ValidateIPAProofForConfig(good, cfg); err != nil {
		t.Fatal(err)
	}
	bad := &IPAProofData{L: []*BanderPoint{g}, R: []*BanderPoint{g}, A: big.NewInt(1)}
	if err := ValidateIPAProofForConfig(bad, cfg); err == nil {
		t.Fatal("wrong rounds")
	}
}

func TestIPAIntegrationConcurrentVerification(t *testing.T) {
	n := 4
	gens := GenerateIPAGenerators(n)
	a := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)}
	b := ComputeBVector(big.NewInt(0), n)
	com := BanderMSM(gens, a)
	proof, v, _ := IPAProve(gens, a, b, com)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := IPAVerify(gens, com, b, v, proof)
			if err != nil || !ok {
				t.Errorf("concurrent verify failed")
			}
		}()
	}
	wg.Wait()
}

func TestIPAIntegrationConstants(t *testing.T) {
	if DefaultVectorSize != 256 || DefaultNumRounds != 8 || BanderwagonFieldSize != 32 || DefaultIPAProofSize() != 8 {
		t.Fatal("constants wrong")
	}
}

func TestIPAIntegrationGenerateGenerators(t *testing.T) {
	gens := GenerateIPAGenerators(8)
	if len(gens) != 8 {
		t.Fatal("wrong count")
	}
	for i := 0; i < len(gens); i++ {
		for j := i + 1; j < len(gens); j++ {
			if BanderEqual(gens[i], gens[j]) {
				t.Fatalf("generators %d and %d equal", i, j)
			}
		}
	}
}
