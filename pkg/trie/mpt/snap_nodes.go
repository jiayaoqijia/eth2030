package mpt

import (
	"bytes"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// NodeAtPath returns a consensus RLP node at a snap compact-encoded path.
func (t *Trie) NodeAtPath(compact []byte) ([]byte, error) {
	t.Hash()
	path := compactToHex(compact)
	n := t.root
	for len(path) > 0 {
		switch cur := n.(type) {
		case *shortNode:
			if !bytes.HasPrefix(path, cur.Key) {
				return nil, ErrNotFound
			}
			path = path[len(cur.Key):]
			n = cur.Val
		case *fullNode:
			n = cur.Children[path[0]]
			path = path[1:]
		default:
			return nil, ErrNotFound
		}
	}
	switch cur := n.(type) {
	case *shortNode:
		cp := cur.copy()
		cp.Key = hexToCompact(cur.Key)
		cp.Val = collapseForProof(cur.Val)
		return encodeShortNode(cp)
	case *fullNode:
		return encodeFullNode(collapseFullNodeForProof(cur))
	default:
		return nil, ErrNotFound
	}
}

// MissingNodes walks authenticated child references, including each account's
// storage trie. Paths use snap compact encoding, prefixed by the account hash
// for storage. Existing corrupt nodes are never treated as complete state.
func MissingNodes(root types.Hash, reader NodeReader, limit int) (map[string]types.Hash, error) {
	missing := make(map[string]types.Hash)
	if root == types.EmptyRootHash {
		return missing, nil
	}
	var walk func(node, []byte, []byte, bool) error
	walk = func(n node, path, prefix []byte, accounts bool) error {
		if limit > 0 && len(missing) >= limit {
			return nil
		}
		if len(path) > 65 {
			return fmt.Errorf("snap: trie path too long")
		}
		switch cur := n.(type) {
		case hashNode:
			hash := types.BytesToHash(cur)
			blob, err := reader.Node(hash)
			if err != nil || len(blob) == 0 {
				key := append(append([]byte{}, prefix...), hexToCompact(path)...)
				missing[string(key)] = hash
				return nil
			}
			if crypto.Keccak256Hash(blob) != hash {
				return fmt.Errorf("snap: stored trie node hash mismatch")
			}
			decoded, err := decodeNode(cur, blob)
			if err != nil {
				return err
			}
			return walk(decoded, path, prefix, accounts)
		case *shortNode:
			return walk(cur.Val, append(append([]byte{}, path...), cur.Key...), prefix, accounts)
		case *fullNode:
			for i, child := range cur.Children {
				if child != nil {
					if err := walk(child, append(append([]byte{}, path...), byte(i)), prefix, accounts); err != nil {
						return err
					}
				}
			}
		case valueNode:
			if accounts {
				if len(path) != 65 || path[64] != terminatorByte {
					return fmt.Errorf("snap: malformed account trie key")
				}
				account, err := decodeAccount(cur)
				if err != nil {
					return err
				}
				if account.Root != types.EmptyRootHash {
					key := make([]byte, 32)
					for i := range key {
						key[i] = path[i*2]<<4 | path[i*2+1]
					}
					return walk(hashNode(account.Root[:]), nil, key, false)
				}
			}
		}
		return nil
	}
	err := walk(hashNode(root[:]), nil, nil, true)
	return missing, err
}
