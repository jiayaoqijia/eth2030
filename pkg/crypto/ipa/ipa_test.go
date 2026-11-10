package ipa

import "testing"

func TestVerkleOpening(t *testing.T) {
	var values [VectorSize]Scalar
	for i := range values {
		values[i].SetUint64(uint64(i*i + 1))
	}
	var point, wrong Scalar
	point.SetUint64(41)
	wrong.SetUint64(42)
	want := values[41].BytesLE()
	commitment, proof, err := Prove(&values, point)
	if err != nil {
		t.Fatal(err)
	}
	if valid, err := Verify(commitment, point.BytesLE(), want, proof); err != nil || !valid {
		t.Fatalf("verify = %t, %v", valid, err)
	}
	if valid, _ := Verify(commitment, wrong.BytesLE(), want, proof); valid {
		t.Fatal("opening accepted at a different evaluation point")
	}
	if valid, _ := Verify(commitment, point.BytesLE(), values[42].BytesLE(), proof); valid {
		t.Fatal("opening accepted with a different claimed value")
	}
	if valid, err := Verify(commitment, point.BytesLE(), want, append(proof, 0)); valid || err == nil {
		t.Fatalf("verify with trailing byte = %t, %v", valid, err)
	}
}
