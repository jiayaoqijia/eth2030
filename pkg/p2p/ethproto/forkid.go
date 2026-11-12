package ethproto

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"

	"github.com/eth2030/eth2030/core/types"
)

// CalcForkID computes the EIP-2124 fork identifier from a genesis hash, the
// current head block number, and the set of known fork block numbers.
//
// The algorithm:
//  1. Start with CRC32(genesisHash).
//  2. For each fork block that has already been passed (fork <= head),
//     update CRC32 with the fork block number encoded as big-endian uint64.
//  3. The first fork block not yet passed becomes ForkID.Next.
//  4. If all forks have passed, Next is 0.
func CalcForkID(genesisHash types.Hash, head uint64, forkBlocks []uint64) ForkID {
	// Start with CRC32 of the genesis hash.
	hash := crc32.ChecksumIEEE(genesisHash[:])

	// Deduplicate and sort fork blocks, removing 0 (genesis).
	forks := cleanForks(forkBlocks)

	for _, fork := range forks {
		if fork <= head {
			// Fork already passed, fold into the checksum.
			hash = checksumUpdate(hash, fork)
			continue
		}
		// Found the next upcoming fork.
		return ForkID{
			Hash: checksumToBytes(hash),
			Next: fork,
		}
	}
	// All known forks passed.
	return ForkID{
		Hash: checksumToBytes(hash),
		Next: 0,
	}
}

// CalcForkIDWithTime implements EIP-6122: block forks precede timestamp
// forks, and rules active at genesis are not additional checksum events.
func CalcForkIDWithTime(genesis types.Hash, genesisTime, head, headTime uint64, blocks, times []uint64) ForkID {
	blocks = cleanForks(blocks)
	times = cleanTimes(times, genesisTime)
	hash := crc32.ChecksumIEEE(genesis[:])
	for _, fork := range blocks {
		if fork > head {
			return ForkID{Hash: checksumToBytes(hash), Next: fork}
		}
		hash = checksumUpdate(hash, fork)
	}
	for _, fork := range times {
		if fork > headTime {
			return ForkID{Hash: checksumToBytes(hash), Next: fork}
		}
		hash = checksumUpdate(hash, fork)
	}
	return ForkID{Hash: checksumToBytes(hash)}
}
func cleanTimes(times []uint64, genesisTime uint64) []uint64 {
	clean := cleanForks(times)
	out := clean[:0]
	for _, fork := range clean {
		if fork > genesisTime {
			out = append(out, fork)
		}
	}
	return out
}

func ValidateForkID(remote ForkID, genesis types.Hash, genesisTime, head, headTime uint64, blocks, times []uint64) error {
	blocks = cleanForks(blocks)
	times = cleanTimes(times, genesisTime)
	forks := append(append([]uint64{}, blocks...), times...)
	hash := crc32.ChecksumIEEE(genesis[:])
	sums := [][4]byte{checksumToBytes(hash)}
	passed := 0
	for i, fork := range forks {
		limit := head
		if i >= len(blocks) {
			limit = headTime
		}
		if i == passed && fork <= limit {
			passed++
		}
		hash = checksumUpdate(hash, fork)
		sums = append(sums, checksumToBytes(hash))
	}
	for i, sum := range sums {
		if sum != remote.Hash {
			continue
		}
		switch {
		case i < passed:
			if remote.Next != forks[i] {
				return fmt.Errorf("eth: stale fork ID")
			}
			return nil
		case i > passed:
			return nil
		default:
			limit := head
			if passed >= len(blocks) && len(times) > 0 {
				limit = headTime
			}
			if remote.Next > 0 && (remote.Next <= limit || (remote.Next > 1438269973 && remote.Next <= headTime)) {
				return fmt.Errorf("eth: incompatible fork next")
			}
			return nil
		}
	}
	return fmt.Errorf("eth: incompatible fork ID")
}

// checksumUpdate folds a fork block number into the running CRC32 checksum.
func checksumUpdate(hash uint32, fork uint64) uint32 {
	var blob [8]byte
	binary.BigEndian.PutUint64(blob[:], fork)
	return crc32.Update(hash, crc32.IEEETable, blob[:])
}

// checksumToBytes converts a CRC32 checksum to [4]byte big-endian.
func checksumToBytes(hash uint32) [4]byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], hash)
	return b
}

// cleanForks deduplicates and sorts fork block numbers, removing zero values.
func cleanForks(forks []uint64) []uint64 {
	if len(forks) == 0 {
		return nil
	}
	// Copy to avoid mutating the caller's slice.
	cp := make([]uint64, len(forks))
	copy(cp, forks)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })

	// Deduplicate and skip zero.
	result := make([]uint64, 0, len(cp))
	for i, f := range cp {
		if f == 0 {
			continue
		}
		if i > 0 && f == cp[i-1] {
			continue
		}
		result = append(result, f)
	}
	return result
}
