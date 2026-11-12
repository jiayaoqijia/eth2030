package mpt

import (
	"bytes"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

var (
	// ErrProofInvalid is returned when a Merkle proof is invalid.
	ErrProofInvalid = errors.New("trie: invalid proof")
)

// Prove generates a Merkle proof for the given key. The proof consists of the
// RLP-encoded nodes along the path from the root to the value. The proof can
// be used with VerifyProof to verify that a key-value pair exists in the trie
// with a given root hash.
func (t *Trie) Prove(key []byte) ([][]byte, error) {
	if t.root == nil {
		return nil, ErrNotFound
	}
	// First, hash the trie to make sure all nodes have been hashed.
	t.Hash()

	hexKey := keybytesToHex(key)
	var proof [][]byte
	found := t.prove(t.root, hexKey, 0, &proof)
	if !found {
		return nil, ErrNotFound
	}
	return proof, nil
}

func (t *Trie) prove(n node, key []byte, pos int, proof *[][]byte) bool {
	switch n := n.(type) {
	case nil:
		return false
	case *shortNode:
		// Encode this node for the proof using a collapsed copy.
		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)
		collapsed.Val = collapseForProof(n.Val)
		enc, err := encodeShortNode(collapsed)
		if err != nil {
			return false
		}
		*proof = append(*proof, enc)

		if len(key)-pos < len(n.Key) || !keysEqual(n.Key, key[pos:pos+len(n.Key)]) {
			return false
		}
		return t.prove(n.Val, key, pos+len(n.Key), proof)

	case *fullNode:
		collapsed := collapseFullNodeForProof(n)
		enc, err := encodeFullNode(collapsed)
		if err != nil {
			return false
		}
		*proof = append(*proof, enc)

		if pos >= len(key) {
			// Looking for value at this branch.
			return n.Children[16] != nil
		}
		return t.prove(n.Children[key[pos]], key, pos+1, proof)

	case valueNode:
		return true

	case hashNode:
		return false

	default:
		return false
	}
}

// ProveAbsence generates a Merkle proof of non-existence for the given key.
// The proof contains the RLP-encoded trie nodes along the path until the
// lookup diverges, demonstrating that the key cannot be present. For an empty
// trie, it returns a nil proof which is valid for absence verification.
func (t *Trie) ProveAbsence(key []byte) ([][]byte, error) {
	if t.root == nil {
		// Empty trie: absence is trivially provable with no proof nodes.
		return nil, nil
	}
	// Hash the trie to ensure all nodes have cached hashes.
	t.Hash()

	hexKey := keybytesToHex(key)
	var proof [][]byte
	t.proveAbsence(t.root, hexKey, 0, &proof)
	return proof, nil
}

// proveAbsence collects proof nodes along the path until the key diverges.
func (t *Trie) proveAbsence(n node, key []byte, pos int, proof *[][]byte) {
	switch n := n.(type) {
	case nil:
		return
	case *shortNode:
		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)
		collapsed.Val = collapseForProof(n.Val)
		enc, err := encodeShortNode(collapsed)
		if err != nil {
			return
		}
		*proof = append(*proof, enc)

		// If key doesn't match the short node's key, the path diverges here.
		if len(key)-pos < len(n.Key) || !keysEqual(n.Key, key[pos:pos+len(n.Key)]) {
			return
		}
		// Key matches so far, continue deeper.
		t.proveAbsence(n.Val, key, pos+len(n.Key), proof)

	case *fullNode:
		collapsed := collapseFullNodeForProof(n)
		enc, err := encodeFullNode(collapsed)
		if err != nil {
			return
		}
		*proof = append(*proof, enc)

		if pos >= len(key) {
			// Looking for value at this branch but there is none.
			return
		}
		child := n.Children[key[pos]]
		if child == nil {
			// No child at this nibble -- divergence point.
			return
		}
		t.proveAbsence(child, key, pos+1, proof)

	case valueNode:
		// Reached a value but there's still remaining key -- can't go deeper.
		return

	case hashNode:
		return
	}
}

// collapseForProof creates a collapsed version of a node suitable for inclusion
// in a proof. Child nodes that are large enough get replaced by their hash.
func collapseForProof(n node) node {
	switch n := n.(type) {
	case *shortNode:
		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)
		collapsed.Val = collapseForProof(n.Val)
		enc, err := encodeShortNode(collapsed)
		if err != nil {
			return n
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			return hashNode(hash)
		}
		return collapsed
	case *fullNode:
		collapsed := collapseFullNodeForProof(n)
		enc, err := encodeFullNode(collapsed)
		if err != nil {
			return n
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			return hashNode(hash)
		}
		return collapsed
	default:
		return n
	}
}

// collapseFullNodeForProof collapses all children of a full node for proof inclusion.
func collapseFullNodeForProof(n *fullNode) *fullNode {
	collapsed := n.copy()
	for i := 0; i < 16; i++ {
		if n.Children[i] != nil {
			collapsed.Children[i] = collapseForProof(n.Children[i])
		}
	}
	return collapsed
}

// VerifyProof verifies a Merkle proof for a given key against a root hash.
// It returns the value if the proof is valid and the key exists, or (nil, nil)
// if the proof validly demonstrates the key's absence.
//
// The proof is a list of RLP-encoded nodes from root to leaf. Each node is
// linked to the next by either a 32-byte Keccak hash reference or by inline
// embedding (when the child's RLP is < 32 bytes).
func VerifyProof(rootHash types.Hash, key []byte, proof [][]byte) ([]byte, error) {
	// An empty proof is valid only for an empty trie (absence proof).
	if len(proof) == 0 {
		if rootHash == emptyRoot {
			return nil, nil
		}
		return nil, ErrProofInvalid
	}

	hexKey := keybytesToHex(key)
	// wantHash tracks the expected hash for the current proof node.
	// For the root, it must match rootHash.
	wantHash := rootHash[:]
	// wantInline tracks an expected inline encoding (when child is < 32 bytes).
	// Only one of wantHash or wantInline should be active at a time.
	var wantInline []byte

	pos := 0
	for i, encoded := range proof {
		// Verify this node matches the expected reference.
		if wantInline != nil {
			// The parent referenced this node via inline embedding.
			if !bytes.Equal(encoded, wantInline) {
				return nil, ErrProofInvalid
			}
			wantInline = nil
		} else {
			nodeHash := crypto.Keccak256(encoded)
			if !bytes.Equal(nodeHash, wantHash) {
				return nil, ErrProofInvalid
			}
		}

		// Decode the RLP list to understand the node structure.
		items, err := decodeRLPList(encoded)
		if err != nil {
			return nil, ErrProofInvalid
		}

		switch len(items) {
		case 2:
			// Short node (leaf or extension).
			compactKey := items[0]
			hexNibbles := compactToHex(compactKey)

			// Check if the key diverges at this short node.
			matchLen := 0
			for matchLen < len(hexNibbles) && pos+matchLen < len(hexKey) {
				if hexNibbles[matchLen] != hexKey[pos+matchLen] {
					break
				}
				matchLen++
			}

			if matchLen < len(hexNibbles) {
				// Key diverges within this node's key.
				// This is the last node; it proves absence.
				if i == len(proof)-1 {
					return nil, nil
				}
				return nil, ErrProofInvalid
			}

			pos += len(hexNibbles)

			if hasTerm(hexNibbles) {
				// Leaf node: the value is items[1].
				if i == len(proof)-1 {
					return items[1], nil
				}
				return nil, ErrProofInvalid
			}

			// Extension node: items[1] is a child reference.
			if i == len(proof)-1 {
				// Proof ends at an extension node with no further nodes.
				// This is not enough to prove anything.
				return nil, ErrProofInvalid
			}
			childRef := items[1]
			if len(childRef) == 32 {
				wantHash = childRef
				wantInline = nil
			} else {
				// Inline child: next proof node must be this exact encoding.
				wantInline = childRef
				wantHash = nil
			}

		case 17:
			// Branch node.
			if pos >= len(hexKey) {
				return nil, ErrProofInvalid
			}
			nibble := hexKey[pos]
			pos++

			if nibble == terminatorByte {
				// Value at this branch.
				val := items[16]
				if len(val) == 0 {
					return nil, nil // absence: no value at branch
				}
				return val, nil
			}

			childRef := items[nibble]
			if len(childRef) == 0 {
				// No child at this nibble -- proves absence.
				if i == len(proof)-1 {
					return nil, nil
				}
				return nil, ErrProofInvalid
			}

			if i == len(proof)-1 {
				// Proof ends at a branch node but child exists and
				// there are no further proof nodes. Invalid proof.
				return nil, ErrProofInvalid
			}

			if len(childRef) == 32 {
				wantHash = childRef
				wantInline = nil
			} else {
				// Inline child: next proof node must match this reference.
				wantInline = childRef
				wantHash = nil
			}

		default:
			return nil, ErrProofInvalid
		}
	}

	return nil, ErrProofInvalid
}

// AccountProof contains the Merkle proof data for a single account, matching
// the response format of the eth_getProof JSON-RPC endpoint (EIP-1186).
type AccountProof struct {
	Address      types.Address  // account address
	AccountProof [][]byte       // list of RLP-encoded trie nodes from state root to account
	Nonce        uint64         // account nonce
	Balance      *big.Int       // account balance in wei
	StorageHash  types.Hash     // root hash of the account's storage trie
	CodeHash     types.Hash     // keccak256 of the account's code
	StorageProof []StorageProof // proofs for requested storage slots
}

// StorageProof contains the proof for a single storage slot.
type StorageProof struct {
	Key   types.Hash // storage slot key
	Value *big.Int   // storage slot value
	Proof [][]byte   // list of RLP-encoded trie nodes from storage root to slot
}

// ProveAccount generates a Merkle proof for an account in the state trie.
// The address is hashed with Keccak-256 to form the trie key (secure trie).
// Returns the account proof including RLP-decoded account fields, or an
// AccountProof with zero-value fields if the account does not exist (with
// a valid absence proof).
func ProveAccount(stateTrie *Trie, addr types.Address) (*AccountProof, error) {
	// In Ethereum's state trie, the key is keccak256(address).
	addrHash := crypto.Keccak256(addr[:])
	result := &AccountProof{
		Address: addr,
		Balance: new(big.Int),
	}

	// Try to prove the key exists.
	proof, err := stateTrie.Prove(addrHash)
	if err == ErrNotFound {
		// Account doesn't exist -- generate absence proof.
		proof, err = stateTrie.ProveAbsence(addrHash)
		if err != nil {
			return nil, err
		}
		result.AccountProof = proof
		result.StorageHash = types.EmptyRootHash
		result.CodeHash = types.EmptyCodeHash
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	result.AccountProof = proof

	// Retrieve and decode the account data from the trie.
	accountRLP, err := stateTrie.Get(addrHash)
	if err != nil {
		return nil, err
	}

	// Decode the RLP-encoded account: [nonce, balance, storageRoot, codeHash].
	account, err := decodeAccount(accountRLP)
	if err != nil {
		return nil, err
	}

	result.Nonce = account.Nonce
	result.Balance = account.Balance
	result.StorageHash = account.Root
	result.CodeHash = types.BytesToHash(account.CodeHash)

	return result, nil
}

// ProveAccountWithStorage generates proofs for an account and a set of its
// storage slots. The storageTrie should be the account's storage trie.
func ProveAccountWithStorage(stateTrie *Trie, addr types.Address, storageTrie *Trie, storageKeys []types.Hash) (*AccountProof, error) {
	result, err := ProveAccount(stateTrie, addr)
	if err != nil {
		return nil, err
	}

	if storageTrie == nil {
		// No storage trie (account doesn't exist or has empty storage).
		for _, key := range storageKeys {
			result.StorageProof = append(result.StorageProof, StorageProof{
				Key:   key,
				Value: new(big.Int),
			})
		}
		return result, nil
	}

	for _, key := range storageKeys {
		sp := StorageProof{
			Key:   key,
			Value: new(big.Int),
		}

		// In Ethereum's storage trie, the key is keccak256(slot).
		slotHash := crypto.Keccak256(key[:])

		proof, err := storageTrie.Prove(slotHash)
		if err == ErrNotFound {
			// Slot doesn't exist -- generate absence proof.
			proof, err = storageTrie.ProveAbsence(slotHash)
			if err != nil {
				return nil, err
			}
			sp.Proof = proof
		} else if err != nil {
			return nil, err
		} else {
			sp.Proof = proof

			// Retrieve the storage value.
			val, getErr := storageTrie.Get(slotHash)
			if getErr == nil && len(val) > 0 {
				var integer []byte
				if err := rlp.DecodeBytes(val, &integer); err != nil {
					return nil, err
				}
				sp.Value = new(big.Int).SetBytes(integer)
			}
		}

		result.StorageProof = append(result.StorageProof, sp)
	}

	return result, nil
}

// decodeAccount decodes an RLP-encoded Ethereum account into a types.Account.
// The encoding is a 4-element RLP list: [nonce, balance, storageRoot, codeHash].
func decodeAccount(data []byte) (*types.Account, error) {
	items, err := decodeRLPList(data)
	if err != nil {
		return nil, err
	}
	if len(items) != 4 {
		return nil, errors.New("trie: invalid account encoding")
	}

	account := &types.Account{
		Balance: new(big.Int),
	}

	// Decode nonce (uint64).
	account.Nonce = decodeBytesAsUint64(items[0])

	// Decode balance (big.Int).
	if len(items[1]) > 0 {
		account.Balance.SetBytes(items[1])
	}

	// Decode storage root (32-byte hash).
	if len(items[2]) == 32 {
		copy(account.Root[:], items[2])
	}

	// Decode code hash (32-byte hash).
	account.CodeHash = make([]byte, len(items[3]))
	copy(account.CodeHash, items[3])

	return account, nil
}

// decodeBytesAsUint64 decodes a big-endian byte sequence as uint64.
func decodeBytesAsUint64(b []byte) uint64 {
	var val uint64
	for _, byt := range b {
		val = val<<8 | uint64(byt)
	}
	return val
}

// EncodeAccount RLP-encodes an Ethereum account as a 4-element list.
// This is useful for inserting accounts into the state trie.
func EncodeAccount(account *types.Account) ([]byte, error) {
	return rlp.EncodeToBytes(struct {
		Nonce    uint64
		Balance  *big.Int
		Root     types.Hash
		CodeHash []byte
	}{
		Nonce:    account.Nonce,
		Balance:  account.Balance,
		Root:     account.Root,
		CodeHash: account.CodeHash,
	})
}

// decodeRLPList is defined in decoder.go; proof.go uses it via package scope.
