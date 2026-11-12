# trie/stack — Stack-based sequential Merkle Patricia Trie builder

Computes Merkle Patricia Trie roots from key-value pairs inserted in strictly sorted order, for transaction trie and receipt trie root calculation.

[← trie](../README.md)

## Overview

`StackTrie` accepts keys sequentially, but retains the trie nodes in memory until finalization. It is not a bounded-memory streaming trie builder.

Keys must be inserted in ascending lexicographic order (enforced by comparing nibble-encoded keys). Inserting an out-of-order key returns `ErrStackTrieOutOfOrder`. Calling `Hash()` or `Commit()` finalises the trie; any subsequent `Update` call returns `ErrStackTrieFinalized`.

`StackTrieBuilder` (`stack_trie_builder.go`) wraps `StackTrie` to provide a convenient interface for building transaction and receipt tries from sorted items.

An optional `NodeWriter` interface can be passed to `NewStackTrie` to persist intermediate nodes to storage during `Commit`.

## Functionality

### Types

| Type | Purpose |
|------|---------|
| `StackTrie` | Sequential MPT builder retaining nodes until finalization |
| `NodeWriter` | Interface: `WriteNode(path, hash, data)` for node persistence |

### Key Functions

- `NewStackTrie(writer)` / `Update(key, value)` / `Hash()` / `Commit()`
- `ErrStackTrieOutOfOrder` / `ErrStackTrieFinalized`

## Usage

```go
st := stack.NewStackTrie(nil)
// Keys must be inserted in sorted order (e.g. RLP-encoded indices 0, 1, 2 ...)
for i, tx := range sortedTxs {
    key := rlp.EncodeUint(uint64(i))
    val, _ := rlp.EncodeToBytes(tx)
    st.Update(key, val)
}
txRoot := st.Hash()
```
