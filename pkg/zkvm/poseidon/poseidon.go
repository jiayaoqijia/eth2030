// Package poseidon provides library-backed hashes over the BN254 scalar field.
package poseidon

import (
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/poseidon2"
	idenposeidon "github.com/iden3/go-iden3-crypto/poseidon"
)

// Hash uses the iden3 Poseidon sponge. The field-element count is prepended
// so trailing zero elements are distinguishable from sponge padding.
func Hash(inputs ...*big.Int) (*big.Int, error) {
	field := fr.Modulus()
	values := make([]*big.Int, 1, len(inputs)+1)
	values[0] = new(big.Int).SetUint64(uint64(len(inputs)))
	for _, input := range inputs {
		if input == nil {
			return nil, errors.New("poseidon: nil field element")
		}
		values = append(values, new(big.Int).Mod(input, field))
	}
	return idenposeidon.SpongeHash(values)
}

// Hash2 uses gnark's default BN254 Poseidon2 compression in Merkle-Damgard
// mode, prefixed with the field-element domain and input count.
func Hash2(inputs ...*big.Int) (*big.Int, error) {
	h := poseidon2.NewHasher()
	h.WriteElements(fr.NewElement(0), fr.NewElement(uint64(len(inputs))))
	for _, input := range inputs {
		if input == nil {
			return nil, errors.New("poseidon2: nil field element")
		}
		var value fr.Element
		value.SetBigInt(input)
		h.WriteElement(value)
	}
	digest := h.SumDigest()
	return digest.BigInt(new(big.Int)), nil
}

// Hash2Bytes absorbs the byte domain, byte count, and big-endian 31-byte
// chunks. Each chunk is smaller than the BN254 scalar field modulus.
func Hash2Bytes(data []byte) [32]byte {
	h := poseidon2.NewHasher()
	h.WriteElements(fr.NewElement(1), fr.NewElement(uint64(len(data))))
	for len(data) != 0 {
		size := min(fr.Bytes-1, len(data))
		var value fr.Element
		value.SetBytes(data[:size])
		h.WriteElement(value)
		data = data[size:]
	}
	digest := h.SumDigest()
	return digest.Bytes()
}
