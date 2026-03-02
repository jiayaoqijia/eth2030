package bintrie

import (
	"encoding/binary"
	"fmt"
	"testing"
)

var benchHashers = []struct {
	name   string
	hasher TrieHasher
}{
	{"SHA256", SHA256Hasher{}},
	{"BLAKE3", Blake3Hasher{}},
	{"Poseidon2", Poseidon2Hasher{}},
	{"Keccak", KeccakHasher{}},
}

func BenchmarkHashRaw(b *testing.B) {
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}

	for _, bh := range benchHashers {
		b.Run(bh.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = bh.hasher.Hash(data)
			}
		})
	}
}

func BenchmarkHashPair(b *testing.B) {
	var left, right [32]byte
	for i := range left {
		left[i] = byte(i)
	}
	for i := range right {
		right[i] = byte(255 - i)
	}

	for _, bh := range benchHashers {
		b.Run(bh.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = bh.hasher.HashPair(left, right)
			}
		})
	}
}

func BenchmarkStemNodeHash(b *testing.B) {
	// Build a StemNode with 256 values.
	stem := make([]byte, StemSize)
	stem[0] = 0xAA
	var values [StemNodeWidth][]byte
	for i := range StemNodeWidth {
		var v [HashSize]byte
		binary.BigEndian.PutUint64(v[24:], uint64(i+1))
		values[i] = v[:]
	}
	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}

	for _, bh := range benchHashers {
		b.Run(bh.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = node.HashWith(bh.hasher)
			}
		})
	}
}

func BenchmarkTrieRoot(b *testing.B) {
	sizes := []int{10_000, 100_000}

	for _, bh := range benchHashers {
		for _, size := range sizes {
			name := fmt.Sprintf("%s/%dk", bh.name, size/1000)
			b.Run(name, func(b *testing.B) {
				// Build a trie with `size` entries.
				trie := NewWithHasher(bh.hasher)
				for i := 0; i < size; i++ {
					var key, val [32]byte
					binary.BigEndian.PutUint64(key[24:], uint64(i))
					binary.BigEndian.PutUint64(val[24:], uint64(i+1000))
					if err := trie.Put(key[:], val[:]); err != nil {
						b.Fatal(err)
					}
				}

				b.ResetTimer()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = trie.Hash()
				}
			})
		}
	}
}
