package vdf

import (
	"sync"
	"testing"
)

func newTestChain() *VDFChain {
	return NewVDFChain(NewVDFv2(DefaultVDFv2Config()), 5)
}

func newTestBeacon() *VDFBeacon {
	return NewVDFBeacon(newTestChain(), 2)
}

func TestNewVDFChain(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	chain := NewVDFChain(vdf, 10)
	if chain.itersPerStep != 10 {
		t.Errorf("itersPerStep: want 10, got %d", chain.itersPerStep)
	}
	// Zero iters should be clamped to 1.
	chain2 := NewVDFChain(vdf, 0)
	if chain2.itersPerStep != 1 {
		t.Errorf("itersPerStep should be clamped to 1, got %d", chain2.itersPerStep)
	}
}

func TestEvaluateChain_Basic(t *testing.T) {
	chain := newTestChain()
	proof, err := chain.EvaluateChain([]byte("chain seed"), 3)
	if err != nil {
		t.Fatalf("EvaluateChain: %v", err)
	}
	if proof.ChainLength != 3 || len(proof.Proofs) != 3 {
		t.Errorf("ChainLength/Proofs: want 3, got %d/%d", proof.ChainLength, len(proof.Proofs))
	}
	if len(proof.FinalOutput) == 0 || string(proof.Seed) != "chain seed" {
		t.Fatal("unexpected seed or empty FinalOutput")
	}
}

func TestEvaluateChain_Chaining(t *testing.T) {
	chain := newTestChain()
	proof, _ := chain.EvaluateChain([]byte("chaining test"), 4)
	if !bytesEqual(proof.Proofs[0].Input, proof.Seed) {
		t.Fatal("first link input should equal seed")
	}
	for i := 0; i < 3; i++ {
		if !bytesEqual(proof.Proofs[i].Output, proof.Proofs[i+1].Input) {
			t.Errorf("chain link %d output != link %d input", i, i+1)
		}
	}
	if !bytesEqual(proof.Proofs[3].Output, proof.FinalOutput) {
		t.Fatal("last link output should equal FinalOutput")
	}
}

func TestEvaluateChain_Deterministic(t *testing.T) {
	chain := newTestChain()
	p1, _ := chain.EvaluateChain([]byte("deterministic"), 2)
	p2, _ := chain.EvaluateChain([]byte("deterministic"), 2)
	if !bytesEqual(p1.FinalOutput, p2.FinalOutput) {
		t.Fatal("same seed + chain length should produce same output")
	}
}

func TestEvaluateChain_DifferentInputs(t *testing.T) {
	chain := newTestChain()
	p1, _ := chain.EvaluateChain([]byte("seed A"), 2)
	p2, _ := chain.EvaluateChain([]byte("seed B"), 2)
	p3, _ := chain.EvaluateChain([]byte("seed A"), 3)
	if bytesEqual(p1.FinalOutput, p2.FinalOutput) {
		t.Fatal("different seeds should differ")
	}
	if bytesEqual(p1.FinalOutput, p3.FinalOutput) {
		t.Fatal("different chain lengths should differ")
	}
}

func TestEvaluateChain_Errors(t *testing.T) {
	chain := newTestChain()
	if _, err := chain.EvaluateChain(nil, 2); err != errVDFBeaconNilSeed {
		t.Errorf("nil seed: want errVDFBeaconNilSeed, got %v", err)
	}
	if _, err := chain.EvaluateChain([]byte{}, 2); err != errVDFBeaconEmptySeed {
		t.Errorf("empty seed: want errVDFBeaconEmptySeed, got %v", err)
	}
	if _, err := chain.EvaluateChain([]byte("t"), 0); err != errVDFBeaconZeroChain {
		t.Errorf("zero chain: want errVDFBeaconZeroChain, got %v", err)
	}
	if _, err := chain.EvaluateChain([]byte("t"), MaxChainLength+1); err != errVDFBeaconChainTooLong {
		t.Errorf("long chain: want errVDFBeaconChainTooLong, got %v", err)
	}
}

func TestVerifyChain_Valid(t *testing.T) {
	chain := newTestChain()
	proof, _ := chain.EvaluateChain([]byte("verify me"), 3)
	if !chain.VerifyChain(proof) {
		t.Fatal("valid chain failed verification")
	}
	// Single link.
	proof1, _ := chain.EvaluateChain([]byte("single"), 1)
	if !chain.VerifyChain(proof1) {
		t.Fatal("single-link chain failed verification")
	}
}

func TestVerifyChain_RejectsInvalid(t *testing.T) {
	chain := newTestChain()
	if chain.VerifyChain(nil) || chain.VerifyChain(&ChainedVDFProof{}) {
		t.Fatal("nil/empty proof should fail")
	}
	proof, _ := chain.EvaluateChain([]byte("reject"), 2)
	// Length mismatch.
	bad := *proof
	bad.ChainLength = 99
	if chain.VerifyChain(&bad) {
		t.Fatal("mismatched chain length should fail")
	}
	// Tampered output.
	proof2, _ := chain.EvaluateChain([]byte("tamper"), 2)
	proof2.FinalOutput[0] ^= 0xFF
	if chain.VerifyChain(proof2) {
		t.Fatal("tampered final output should fail")
	}
	// Tampered seed.
	proof3, _ := chain.EvaluateChain([]byte("tamper seed"), 2)
	proof3.Seed[0] ^= 0xFF
	if chain.VerifyChain(proof3) {
		t.Fatal("tampered seed should fail")
	}
	// Broken chain link.
	proof4, _ := chain.EvaluateChain([]byte("broken"), 3)
	proof4.Proofs[0].Output[0] ^= 0xFF
	if chain.VerifyChain(proof4) {
		t.Fatal("broken chain link should fail")
	}
}

func TestVerifyChain_Cache(t *testing.T) {
	chain := newTestChain()
	if chain.CacheSize() != 0 {
		t.Fatalf("cache should start empty")
	}
	proof, _ := chain.EvaluateChain([]byte("cache"), 2)
	chain.VerifyChain(proof)
	if chain.CacheSize() != 1 {
		t.Errorf("cache should have 1 entry, got %d", chain.CacheSize())
	}
	if !chain.VerifyChain(proof) {
		t.Fatal("cached chain should verify")
	}
	tampered := &proof.Proofs[0]
	tampered.Proof[0] ^= 1
	if chain.VerifyChain(proof) {
		t.Fatal("altered proof reused the verification cache")
	}
	chain.ClearCache()
	if chain.CacheSize() != 0 {
		t.Error("cache should be empty after clear")
	}
}

func TestNewVDFBeacon(t *testing.T) {
	vdf := NewVDFv2(DefaultVDFv2Config())
	chain := NewVDFChain(vdf, 5)
	b := NewVDFBeacon(chain, 3)
	if b.chainLen != 3 {
		t.Errorf("chainLen: want 3, got %d", b.chainLen)
	}
	b2 := NewVDFBeacon(chain, 0)
	if b2.chainLen != 1 {
		t.Error("zero chainLen should be clamped to 1")
	}
	b3 := NewVDFBeacon(chain, MaxChainLength+100)
	if b3.chainLen != MaxChainLength {
		t.Error("excessive chainLen should be clamped")
	}
}

func TestProduceBeaconRandomness_Basic(t *testing.T) {
	beacon := newTestBeacon()
	output, err := beacon.ProduceBeaconRandomness(1, []byte("epoch 1 seed"))
	if err != nil {
		t.Fatalf("ProduceBeaconRandomness: %v", err)
	}
	if output.Epoch != 1 || len(output.Randomness) != 32 || len(output.VDFProof) == 0 {
		t.Fatal("unexpected output fields")
	}
	if output.Timestamp == 0 {
		t.Error("Timestamp should be non-zero")
	}
}

func TestProduceBeaconRandomness_Deterministic(t *testing.T) {
	beacon := newTestBeacon()
	o1, _ := beacon.ProduceBeaconRandomness(1, []byte("same seed"))
	o2, _ := beacon.ProduceBeaconRandomness(1, []byte("same seed"))
	if !bytesEqual(o1.Randomness, o2.Randomness) {
		t.Fatal("same epoch + seed should produce same randomness")
	}
}

func TestProduceBeaconRandomness_DifferentInputs(t *testing.T) {
	beacon := newTestBeacon()
	o1, _ := beacon.ProduceBeaconRandomness(1, []byte("seed"))
	o2, _ := beacon.ProduceBeaconRandomness(2, []byte("seed"))
	o3, _ := beacon.ProduceBeaconRandomness(1, []byte("other"))
	if bytesEqual(o1.Randomness, o2.Randomness) {
		t.Fatal("different epochs should differ")
	}
	if bytesEqual(o1.Randomness, o3.Randomness) {
		t.Fatal("different seeds should differ")
	}
}

func TestProduceBeaconRandomness_Errors(t *testing.T) {
	beacon := newTestBeacon()
	if _, err := beacon.ProduceBeaconRandomness(0, []byte("s")); err != errVDFBeaconZeroEpoch {
		t.Errorf("zero epoch: got %v", err)
	}
	if _, err := beacon.ProduceBeaconRandomness(1, nil); err != errVDFBeaconNilSeed {
		t.Errorf("nil seed: got %v", err)
	}
	if _, err := beacon.ProduceBeaconRandomness(1, []byte{}); err != errVDFBeaconEmptySeed {
		t.Errorf("empty seed: got %v", err)
	}
}

func TestVerifyBeaconRandomness(t *testing.T) {
	beacon := newTestBeacon()
	seed := []byte("verify beacon")
	output, _ := beacon.ProduceBeaconRandomness(5, seed)
	if !beacon.VerifyBeaconRandomness(output, seed) {
		t.Fatal("valid beacon failed verification")
	}
	// Rejects nil, empty, wrong seed, tampered randomness/proof.
	if beacon.VerifyBeaconRandomness(nil, seed) {
		t.Fatal("nil should fail")
	}
	if beacon.VerifyBeaconRandomness(&BeaconOutput{}, seed) {
		t.Fatal("empty should fail")
	}
	if beacon.VerifyBeaconRandomness(output, []byte("wrong")) {
		t.Fatal("wrong seed should fail")
	}
	bad1, _ := beacon.ProduceBeaconRandomness(1, seed)
	bad1.Randomness[0] ^= 0xFF
	if beacon.VerifyBeaconRandomness(bad1, seed) {
		t.Fatal("tampered randomness should fail")
	}
	bad2, _ := beacon.ProduceBeaconRandomness(2, seed)
	bad2.VDFProof[0] ^= 0xFF
	if beacon.VerifyBeaconRandomness(bad2, seed) {
		t.Fatal("tampered proof should fail")
	}
}

func TestBeaconCaching(t *testing.T) {
	beacon := newTestBeacon()
	if beacon.GetCachedBeacon(1) != nil {
		t.Fatal("no cached beacon initially")
	}
	output, _ := beacon.ProduceBeaconRandomness(1, []byte("cache"))
	cached := beacon.GetCachedBeacon(1)
	if cached == nil || !bytesEqual(cached.Randomness, output.Randomness) {
		t.Fatal("cached output mismatch")
	}
	beacon.ClearBeaconCache()
	if beacon.GetCachedBeacon(1) != nil {
		t.Fatal("cache should be empty after clear")
	}
}

func TestBeaconDomainSeed(t *testing.T) {
	s1 := beaconDomainSeed(1, []byte("seed"))
	s2 := beaconDomainSeed(2, []byte("seed"))
	s3 := beaconDomainSeed(1, []byte("seed"))
	if bytesEqual(s1, s2) {
		t.Fatal("different epochs should differ")
	}
	if !bytesEqual(s1, s3) {
		t.Fatal("same inputs should match")
	}
}

func TestBeaconCompactProof(t *testing.T) {
	chain := newTestChain()
	p1, _ := chain.EvaluateChain([]byte("compact 1"), 2)
	p2, _ := chain.EvaluateChain([]byte("compact 2"), 2)
	cp1, cp2 := beaconCompactProof(p1), beaconCompactProof(p2)
	if len(cp1) != 32 {
		t.Errorf("compact proof: want 32 bytes, got %d", len(cp1))
	}
	if bytesEqual(cp1, cp2) {
		t.Fatal("different chains should differ")
	}
}

func TestVDFChainAndBeacon_Concurrent(t *testing.T) {
	chain := newTestChain()
	beacon := NewVDFBeacon(chain, 2)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seed := []byte{byte(idx), 0xAB}
			proof, err := chain.EvaluateChain(seed, 2)
			if err != nil {
				errs <- err
				return
			}
			if !chain.VerifyChain(proof) {
				errs <- errVDFBeaconMismatch
				return
			}
			errs <- nil
		}(i)
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			epoch := uint64(idx + 1)
			seed := []byte{byte(idx), 0xCD}
			output, err := beacon.ProduceBeaconRandomness(epoch, seed)
			if err != nil {
				errs <- err
				return
			}
			if !beacon.VerifyBeaconRandomness(output, seed) {
				errs <- errVDFBeaconMismatch
				return
			}
			errs <- nil
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent error: %v", err)
		}
	}
}
