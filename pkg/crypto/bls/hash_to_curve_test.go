package bls

import (
	"testing"
)

func TestHashToCurveG1Basic(t *testing.T) {
	dst := []byte("BLS12381G1_XMD:SHA-256_SSWU_RO_")
	msg := []byte("hello world")

	p, err := HashToCurveG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	// Result must be on curve.
	if !p.blsG1IsInfinity() {
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Fatal("HashToCurveG1 produced off-curve point")
		}

		// Result must be in the subgroup (cofactor-cleared).
		if !blsG1InSubgroup(p) {
			t.Fatal("HashToCurveG1 result not in subgroup")
		}
	}
}

func TestHashToCurveG1Deterministic(t *testing.T) {
	dst := []byte("test-suite")
	msg := []byte("deterministic check")

	p1, err := HashToCurveG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := HashToCurveG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	x1, y1 := p1.blsG1ToAffine()
	x2, y2 := p2.blsG1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Fatal("HashToCurveG1 is non-deterministic")
	}
}

func TestHashToCurveG1DifferentMsgs(t *testing.T) {
	dst := []byte("collision-test")
	p1, _ := HashToCurveG1([]byte("msg1"), dst)
	p2, _ := HashToCurveG1([]byte("msg2"), dst)

	x1, y1 := p1.blsG1ToAffine()
	x2, y2 := p2.blsG1ToAffine()
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		t.Fatal("different messages produced same point")
	}
}

func TestHashToCurveG1DifferentDSTs(t *testing.T) {
	msg := []byte("same message")
	p1, _ := HashToCurveG1(msg, []byte("DST-A"))
	p2, _ := HashToCurveG1(msg, []byte("DST-B"))

	x1, y1 := p1.blsG1ToAffine()
	x2, y2 := p2.blsG1ToAffine()
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		t.Fatal("different DSTs produced same point")
	}
}

func TestEncodeToG1(t *testing.T) {
	dst := []byte("BLS12381G1_XMD:SHA-256_SSWU_NU_")
	msg := []byte("encode test")

	p, err := EncodeToG1(msg, dst)
	if err != nil {
		t.Fatal(err)
	}

	if !p.blsG1IsInfinity() {
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Fatal("EncodeToG1 produced off-curve point")
		}
		if !blsG1InSubgroup(p) {
			t.Fatal("EncodeToG1 result not in subgroup")
		}
	}
}

func TestHashToCurveG1DSTTooLong(t *testing.T) {
	longDST := make([]byte, 256)
	_, err := HashToCurveG1([]byte("test"), longDST)
	if err == nil {
		t.Fatal("expected error for DST > 255 bytes")
	}
}

func TestValidateDST(t *testing.T) {
	if err := ValidateDST([]byte("ok")); err != nil {
		t.Fatal("valid DST rejected:", err)
	}
	if err := ValidateDST([]byte{}); err == nil {
		t.Fatal("empty DST accepted")
	}
	if err := ValidateDST(make([]byte, 256)); err == nil {
		t.Fatal("DST > 255 accepted")
	}
	if err := ValidateDST(make([]byte, 255)); err != nil {
		t.Fatal("DST of exactly 255 bytes rejected:", err)
	}
}

func TestHashToCurveG1SubgroupMultiple(t *testing.T) {
	dst := []byte("subgroup-test-suite")
	messages := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, msg := range messages {
		p, err := HashToCurveG1([]byte(msg), dst)
		if err != nil {
			t.Fatalf("HashToCurveG1(%q): %v", msg, err)
		}
		if p.blsG1IsInfinity() {
			continue
		}
		if !blsG1InSubgroup(p) {
			t.Fatalf("HashToCurveG1(%q): result not in subgroup", msg)
		}
	}
}

func TestHashToCurveG1EmptyMessage(t *testing.T) {
	dst := []byte("empty-msg-test")
	p, err := HashToCurveG1([]byte{}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !p.blsG1IsInfinity() {
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Fatal("empty message produced off-curve point")
		}
	}
}
