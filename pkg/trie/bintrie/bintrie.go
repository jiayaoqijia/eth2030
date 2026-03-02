// Package bintrie implements the EIP-7864 binary trie for Ethereum state.
//
// The binary trie replaces MPT tries with a SHA-256-based binary
// tree structure that supports efficient stateless proofs. Each key is
// 32 bytes: the first 31 bytes form the "stem" that navigates through
// the internal nodes, and the final byte selects one of 256 leaves in
// a StemNode.
package bintrie

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Key encoding constants matching the go-ethereum reference.
const (
	BasicDataLeafKey        = 0
	CodeHashLeafKey         = 1
	BasicDataCodeSizeOffset = 5
	BasicDataNonceOffset    = 8
	BasicDataBalanceOffset  = 16
)

var (
	zeroHash                      = types.Hash{}
	headerStorageOffset           = new(big.Int).SetUint64(64)
	codeOffset                    = new(big.Int).SetUint64(128)
	codeStorageDelta              = new(big.Int).Sub(codeOffset, headerStorageOffset)
	nodeWidthLog2                 = 8
	mainStorageOffsetLshNodeWidth = new(big.Int).Lsh(big.NewInt(1), 248-uint(nodeWidthLog2))
)

// GetBinaryTreeKey computes the SHA-256 tree key for an address and value key.
// The key is: SHA256(zeroHash[:12] || addr || key[:31] || 0x00), with the
// last byte replaced by key[31].
func GetBinaryTreeKey(addr types.Address, key []byte) []byte {
	hasher := sha256.New()
	hasher.Write(zeroHash[:12])
	hasher.Write(addr[:])
	hasher.Write(key[:31])
	hasher.Write([]byte{0})
	k := hasher.Sum(nil)
	k[31] = key[31]
	return k
}

// GetBinaryTreeKeyWith computes the tree key using a pluggable TrieHasher.
func GetBinaryTreeKeyWith(addr types.Address, key []byte, th TrieHasher) []byte {
	var buf [12 + 20 + 31 + 1]byte
	// zeroHash[:12] is already zero
	copy(buf[12:], addr[:])
	copy(buf[32:], key[:31])
	// buf[63] = 0x00 already
	h := th.Hash(buf[:])
	k := h[:]
	out := make([]byte, 32)
	copy(out, k)
	out[31] = key[31]
	return out
}

// GetBinaryTreeKeyBasicData returns the tree key for an account's basic data.
func GetBinaryTreeKeyBasicData(addr types.Address) []byte {
	var k [32]byte
	k[31] = BasicDataLeafKey
	return GetBinaryTreeKey(addr, k[:])
}

// GetBinaryTreeKeyBasicDataWith returns the tree key using a pluggable hasher.
func GetBinaryTreeKeyBasicDataWith(addr types.Address, th TrieHasher) []byte {
	var k [32]byte
	k[31] = BasicDataLeafKey
	return GetBinaryTreeKeyWith(addr, k[:], th)
}

// GetBinaryTreeKeyCodeHash returns the tree key for an account's code hash.
func GetBinaryTreeKeyCodeHash(addr types.Address) []byte {
	var k [32]byte
	k[31] = CodeHashLeafKey
	return GetBinaryTreeKey(addr, k[:])
}

// GetBinaryTreeKeyCodeHashWith returns the tree key using a pluggable hasher.
func GetBinaryTreeKeyCodeHashWith(addr types.Address, th TrieHasher) []byte {
	var k [32]byte
	k[31] = CodeHashLeafKey
	return GetBinaryTreeKeyWith(addr, k[:], th)
}

// GetBinaryTreeKeyStorageSlot returns the tree key for a storage slot.
func GetBinaryTreeKeyStorageSlot(address types.Address, key []byte) []byte {
	var k [32]byte

	// Header storage: key[:31] all zero and key[31] < 64
	if bytes.Equal(key[:31], zeroHash[:31]) && key[31] < 64 {
		k[31] = 64 + key[31]
		return GetBinaryTreeKey(address, k[:])
	}

	// Main storage offset
	k[0] = 1 // 1 << 248
	copy(k[1:], key[:31])
	k[31] = key[31]

	return GetBinaryTreeKey(address, k[:])
}

// GetBinaryTreeKeyStorageSlotWith returns the tree key using a pluggable hasher.
func GetBinaryTreeKeyStorageSlotWith(address types.Address, key []byte, th TrieHasher) []byte {
	var k [32]byte

	if bytes.Equal(key[:31], zeroHash[:31]) && key[31] < 64 {
		k[31] = 64 + key[31]
		return GetBinaryTreeKeyWith(address, k[:], th)
	}

	k[0] = 1
	copy(k[1:], key[:31])
	k[31] = key[31]

	return GetBinaryTreeKeyWith(address, k[:], th)
}

// GetBinaryTreeKeyCodeChunk returns the tree key for a code chunk.
func GetBinaryTreeKeyCodeChunk(address types.Address, chunknr uint64) []byte {
	chunkOffset := new(big.Int).Add(codeOffset, new(big.Int).SetUint64(chunknr))
	var buf [32]byte
	b := chunkOffset.Bytes()
	copy(buf[32-len(b):], b)
	return GetBinaryTreeKey(address, buf[:])
}

// GetBinaryTreeKeyCodeChunkWith returns the tree key using a pluggable hasher.
func GetBinaryTreeKeyCodeChunkWith(address types.Address, chunknr uint64, th TrieHasher) []byte {
	chunkOffset := new(big.Int).Add(codeOffset, new(big.Int).SetUint64(chunknr))
	var buf [32]byte
	b := chunkOffset.Bytes()
	copy(buf[32-len(b):], b)
	return GetBinaryTreeKeyWith(address, buf[:], th)
}

// StorageIndex computes the tree index and sub-index for a storage key.
func StorageIndex(storageKey []byte) (*big.Int, byte) {
	var key big.Int
	key.SetBytes(storageKey)
	if key.Cmp(codeStorageDelta) < 0 {
		key.Add(headerStorageOffset, &key)
		suffix := byte(key.Uint64() & 0xFF)
		return new(big.Int), suffix
	}
	suffix := storageKey[len(storageKey)-1]
	key.Rsh(&key, uint(nodeWidthLog2))
	key.Add(&key, mainStorageOffsetLshNodeWidth)
	return &key, suffix
}

// PUSH opcodes for code chunking
const (
	push1  = byte(0x60)
	push32 = byte(0x7f)
)

// ChunkedCode is a sequence of HashSize-byte chunks of code.
type ChunkedCode []byte

// ChunkifyCode generates the chunked version of EVM bytecode per EIP-7864.
func ChunkifyCode(code []byte) ChunkedCode {
	var (
		chunkOffset = 0
		chunkCount  = len(code) / StemSize
		codeOffset  = 0
	)
	if len(code)%StemSize != 0 {
		chunkCount++
	}
	chunks := make([]byte, chunkCount*HashSize)
	for i := 0; i < chunkCount; i++ {
		end := min(len(code), StemSize*(i+1))
		copy(chunks[i*HashSize+1:], code[StemSize*i:end])

		if chunkOffset > StemSize {
			chunks[i*HashSize] = StemSize
			chunkOffset = 1
			continue
		}
		chunks[HashSize*i] = byte(chunkOffset)
		chunkOffset = 0

		for ; codeOffset < end; codeOffset++ {
			if code[codeOffset] >= push1 && code[codeOffset] <= push32 {
				codeOffset += int(code[codeOffset] - push1 + 1)
				if codeOffset+1 >= StemSize*(i+1) {
					codeOffset++
					chunkOffset = codeOffset - StemSize*(i+1)
					break
				}
			}
		}
	}
	return chunks
}

// NewBinaryNode creates a new empty binary trie root node.
func NewBinaryNode() BinaryNode {
	return Empty{}
}

var errInvalidRootType = errors.New("invalid root type")

// BinaryTrie is the primary trie structure implementing EIP-7864.
type BinaryTrie struct {
	root   BinaryNode
	hasher TrieHasher // pluggable hash function (nil defaults to SHA-256)
}

// New creates a new empty binary trie using SHA-256.
func New() *BinaryTrie {
	return &BinaryTrie{root: NewBinaryNode()}
}

// NewWithHasher creates a new empty binary trie using the specified hasher.
func NewWithHasher(hasher TrieHasher) *BinaryTrie {
	return &BinaryTrie{root: NewBinaryNode(), hasher: hasher}
}

// Hasher returns the trie's configured hasher (SHA-256 if none set).
func (t *BinaryTrie) Hasher() TrieHasher {
	if t.hasher == nil {
		return SHA256Hasher{}
	}
	return t.hasher
}

// Get retrieves a value by its full 32-byte key.
func (t *BinaryTrie) Get(key []byte) ([]byte, error) {
	return t.root.Get(key, nil)
}

// Put inserts or updates a key-value pair.
func (t *BinaryTrie) Put(key, value []byte) error {
	root, err := t.root.Insert(key, value, nil, 0)
	if err != nil {
		return err
	}
	t.root = root
	return nil
}

// Delete removes a key by setting it to zero.
func (t *BinaryTrie) Delete(key []byte) error {
	var zeroVal [HashSize]byte
	root, err := t.root.Insert(key, zeroVal[:], nil, 0)
	if err != nil {
		return err
	}
	t.root = root
	return nil
}

// Hash returns the root hash of the trie using the configured hasher.
// If no hasher was set (via NewWithHasher), defaults to SHA-256.
func (t *BinaryTrie) Hash() types.Hash {
	if t.hasher != nil {
		return t.root.HashWith(t.hasher)
	}
	return t.root.Hash()
}

// HashWith returns the root hash of the trie using the specified hasher.
func (t *BinaryTrie) HashWith(hasher TrieHasher) types.Hash {
	return t.root.HashWith(hasher)
}

// GetAccount returns the account information for the given address.
func (t *BinaryTrie) GetAccount(addr types.Address) (*types.Account, error) {
	var (
		values [][]byte
		err    error
		key    = GetBinaryTreeKey(addr, zero[:])
	)
	switch r := t.root.(type) {
	case *InternalNode:
		values, err = r.GetValuesAtStem(key[:StemSize], nil)
	case *StemNode:
		values = r.Values
	case Empty:
		return nil, nil
	default:
		return nil, errInvalidRootType
	}
	if err != nil {
		return nil, fmt.Errorf("GetAccount (%x) error: %v", addr, err)
	}

	emptyAccount := true
	for i := 0; values != nil && i <= CodeHashLeafKey && emptyAccount; i++ {
		emptyAccount = emptyAccount && values[i] == nil
	}
	if emptyAccount {
		return nil, nil
	}

	// Check for deleted accounts (nonce=0, basic data zero, code hash zero)
	if bytes.Equal(values[BasicDataLeafKey], zero[:]) && len(values) > 10 && len(values[10]) > 0 && bytes.Equal(values[CodeHashLeafKey], zero[:]) {
		return nil, nil
	}

	acc := &types.Account{
		Balance: new(big.Int),
	}
	acc.Nonce = binary.BigEndian.Uint64(values[BasicDataLeafKey][BasicDataNonceOffset:])
	var balance [16]byte
	copy(balance[:], values[BasicDataLeafKey][BasicDataBalanceOffset:])
	acc.Balance.SetBytes(balance[:])
	acc.CodeHash = values[CodeHashLeafKey]

	return acc, nil
}

// UpdateAccount updates the account information for the given address.
func (t *BinaryTrie) UpdateAccount(addr types.Address, acc *types.Account, codeLen int) error {
	var (
		err       error
		basicData [HashSize]byte
		values    = make([][]byte, StemNodeWidth)
		stem      = GetBinaryTreeKey(addr, zero[:])
	)
	binary.BigEndian.PutUint32(basicData[BasicDataCodeSizeOffset-1:], uint32(codeLen))
	binary.BigEndian.PutUint64(basicData[BasicDataNonceOffset:], acc.Nonce)

	balanceBytes := acc.Balance.Bytes()
	if len(balanceBytes) > 16 {
		balanceBytes = balanceBytes[16:]
	}
	copy(basicData[HashSize-len(balanceBytes):], balanceBytes[:])
	values[BasicDataLeafKey] = basicData[:]
	values[CodeHashLeafKey] = acc.CodeHash[:]

	t.root, err = t.root.InsertValuesAtStem(stem, values, nil, 0)
	return err
}

// GetStorage returns the value for a storage slot.
func (t *BinaryTrie) GetStorage(addr types.Address, key []byte) ([]byte, error) {
	return t.root.Get(GetBinaryTreeKeyStorageSlot(addr, key), nil)
}

// UpdateStorage sets a storage slot value.
func (t *BinaryTrie) UpdateStorage(address types.Address, key, value []byte) error {
	k := GetBinaryTreeKeyStorageSlot(address, key)
	var v [HashSize]byte
	if len(value) >= HashSize {
		copy(v[:], value[:HashSize])
	} else {
		copy(v[HashSize-len(value):], value[:])
	}
	root, err := t.root.Insert(k, v[:], nil, 0)
	if err != nil {
		return fmt.Errorf("UpdateStorage (%x) error: %w", address, err)
	}
	t.root = root
	return nil
}

// DeleteStorage removes a storage slot value by zeroing it.
func (t *BinaryTrie) DeleteStorage(addr types.Address, key []byte) error {
	k := GetBinaryTreeKeyStorageSlot(addr, key)
	var zeroVal [HashSize]byte
	root, err := t.root.Insert(k, zeroVal[:], nil, 0)
	if err != nil {
		return fmt.Errorf("DeleteStorage (%x) error: %w", addr, err)
	}
	t.root = root
	return nil
}

// UpdateContractCode updates the contract code into the trie.
func (t *BinaryTrie) UpdateContractCode(addr types.Address, code []byte) error {
	var (
		chunks = ChunkifyCode(code)
		values [][]byte
		key    []byte
		err    error
	)
	for i, chunknr := 0, uint64(0); i < len(chunks); i, chunknr = i+HashSize, chunknr+1 {
		groupOffset := (chunknr + 128) % StemNodeWidth
		if groupOffset == 0 || chunknr == 0 {
			values = make([][]byte, StemNodeWidth)
			var offset [HashSize]byte
			binary.LittleEndian.PutUint64(offset[24:], chunknr+128)
			key = GetBinaryTreeKey(addr, offset[:])
		}
		values[groupOffset] = chunks[i : i+HashSize]

		if groupOffset == StemNodeWidth-1 || len(chunks)-i <= HashSize {
			err = t.UpdateStem(key[:StemSize], values)
			if err != nil {
				return fmt.Errorf("UpdateContractCode (addr=%x) error: %w", addr[:], err)
			}
		}
	}
	return nil
}

// UpdateStem updates the values for a given stem key.
func (t *BinaryTrie) UpdateStem(key []byte, values [][]byte) error {
	var err error
	t.root, err = t.root.InsertValuesAtStem(key, values, nil, 0)
	return err
}

// Copy creates a deep copy of the trie.
func (t *BinaryTrie) Copy() *BinaryTrie {
	return &BinaryTrie{root: t.root.Copy(), hasher: t.hasher}
}

// Root returns the root node (for testing and iteration).
func (t *BinaryTrie) Root() BinaryNode {
	return t.root
}
