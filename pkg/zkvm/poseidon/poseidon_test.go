package poseidon

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/poseidon2"
	idenposeidon "github.com/iden3/go-iden3-crypto/poseidon"
)

func TestHashMatchesIden3(t *testing.T) {
	want, err := idenposeidon.SpongeHash([]*big.Int{big.NewInt(2), big.NewInt(5), big.NewInt(7)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Hash(big.NewInt(5), big.NewInt(7))
	if err != nil || got.Cmp(want) != 0 {
		t.Fatalf("hash = %v, %v; want %v", got, err, want)
	}
	padded, err := Hash(big.NewInt(5), big.NewInt(7), new(big.Int))
	if err != nil || got.Cmp(padded) == 0 {
		t.Fatalf("hash with trailing zero = %v, %v", padded, err)
	}
}

func TestHash2MatchesGnark(t *testing.T) {
	h := poseidon2.NewHasher()
	h.WriteElements(fr.NewElement(0), fr.NewElement(2), fr.NewElement(5), fr.NewElement(7))
	digest := h.SumDigest()
	want := digest.BigInt(new(big.Int))
	got, err := Hash2(big.NewInt(5), big.NewInt(7))
	if err != nil || got.Cmp(want) != 0 {
		t.Fatalf("hash = %v, %v; want %v", got, err, want)
	}
	if Hash2Bytes([]byte{1}) == Hash2Bytes([]byte{0, 1}) {
		t.Fatal("byte lengths were not bound to the digest")
	}
}
