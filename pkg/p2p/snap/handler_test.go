package snap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/big"
	"sort"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// --- Message encoding/decoding roundtrip tests ---

func TestGetAccountRangePacket_Roundtrip(t *testing.T) {
	pkt := GetAccountRangePacket{
		ID:     42,
		Root:   types.HexToHash("0xdeadbeef"),
		Origin: types.Hash{0x01},
		Limit:  types.Hash{0xff},
		Bytes:  500000,
	}

	encoded, err := rlp.EncodeToBytes(pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetAccountRangePacket
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != pkt.ID {
		t.Errorf("ID: want %d, got %d", pkt.ID, decoded.ID)
	}
	if decoded.Root != pkt.Root {
		t.Errorf("Root: want %s, got %s", pkt.Root.Hex(), decoded.Root.Hex())
	}
	if decoded.Origin != pkt.Origin {
		t.Errorf("Origin mismatch")
	}
	if decoded.Limit != pkt.Limit {
		t.Errorf("Limit mismatch")
	}
	if decoded.Bytes != pkt.Bytes {
		t.Errorf("Bytes: want %d, got %d", pkt.Bytes, decoded.Bytes)
	}
}

func TestAccountRangePacket_Roundtrip(t *testing.T) {
	pkt := AccountRangePacket{
		ID: 7,
		Accounts: []AccountData{
			{Hash: types.Hash{0x01}, Body: []byte{0xaa, 0xbb}},
			{Hash: types.Hash{0x02}, Body: []byte{0xcc, 0xdd}},
		},
		Proof: [][]byte{{0x01, 0x02}, {0x03, 0x04}},
	}

	encoded, err := rlp.EncodeToBytes(pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded AccountRangePacket
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != pkt.ID {
		t.Errorf("ID mismatch")
	}
	if len(decoded.Accounts) != len(pkt.Accounts) {
		t.Fatalf("accounts: want %d, got %d", len(pkt.Accounts), len(decoded.Accounts))
	}
	for i, a := range decoded.Accounts {
		if a.Hash != pkt.Accounts[i].Hash {
			t.Errorf("account[%d] hash mismatch", i)
		}
		if !bytes.Equal(a.Body, pkt.Accounts[i].Body) {
			t.Errorf("account[%d] body mismatch", i)
		}
	}
	if len(decoded.Proof) != len(pkt.Proof) {
		t.Fatalf("proof: want %d, got %d", len(pkt.Proof), len(decoded.Proof))
	}
}

func TestGetByteCodesPacket_Roundtrip(t *testing.T) {
	pkt := GetByteCodesPacket{
		ID:     99,
		Hashes: []types.Hash{{0xaa}, {0xbb}, {0xcc}},
		Bytes:  1000000,
	}

	encoded, err := rlp.EncodeToBytes(pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetByteCodesPacket
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != pkt.ID {
		t.Errorf("ID: want %d, got %d", pkt.ID, decoded.ID)
	}
	if len(decoded.Hashes) != len(pkt.Hashes) {
		t.Fatalf("hashes: want %d, got %d", len(pkt.Hashes), len(decoded.Hashes))
	}
	for i := range decoded.Hashes {
		if decoded.Hashes[i] != pkt.Hashes[i] {
			t.Errorf("hash[%d] mismatch", i)
		}
	}
}

func TestByteCodesPacket_Roundtrip(t *testing.T) {
	pkt := ByteCodesPacket{
		ID:    5,
		Codes: [][]byte{{0x60, 0x00}, {0x60, 0x01, 0xfd}},
	}

	encoded, err := rlp.EncodeToBytes(pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded ByteCodesPacket
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != pkt.ID {
		t.Errorf("ID mismatch")
	}
	if len(decoded.Codes) != len(pkt.Codes) {
		t.Fatalf("codes: want %d, got %d", len(pkt.Codes), len(decoded.Codes))
	}
	for i := range decoded.Codes {
		if !bytes.Equal(decoded.Codes[i], pkt.Codes[i]) {
			t.Errorf("code[%d] mismatch", i)
		}
	}
}

func TestGetTrieNodesPacket_Roundtrip(t *testing.T) {
	pkt := GetTrieNodesPacket{
		ID:   17,
		Root: types.Hash{0xde, 0xad},
		Paths: []TrieNodePathSet{
			{[]byte{0x01, 0x02}},
			{[]byte{0x03}, []byte{0x04, 0x05}},
		},
		Bytes: 2000000,
	}

	encoded, err := rlp.EncodeToBytes(pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetTrieNodesPacket
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != pkt.ID {
		t.Errorf("ID mismatch")
	}
	if decoded.Root != pkt.Root {
		t.Errorf("Root mismatch")
	}
	if len(decoded.Paths) != len(pkt.Paths) {
		t.Fatalf("paths: want %d, got %d", len(pkt.Paths), len(decoded.Paths))
	}
}

func TestGetStorageRangesPacket_Roundtrip(t *testing.T) {
	pkt := GetStorageRangesPacket{
		ID:       8,
		Root:     types.Hash{0xab},
		Accounts: []types.Hash{{0x01}, {0x02}},
		Origin:   []byte{0x00},
		Limit:    []byte{0xff},
		Bytes:    100000,
	}

	encoded, err := rlp.EncodeToBytes(pkt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded GetStorageRangesPacket
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != pkt.ID {
		t.Errorf("ID mismatch")
	}
	if len(decoded.Accounts) != len(pkt.Accounts) {
		t.Fatalf("accounts mismatch")
	}
}

// --- Mock state backend for handler tests ---

type mockAccount struct {
	hash types.Hash
	body []byte
}

type mockStorageSlot struct {
	hash types.Hash
	body []byte
}

type mockBackend struct {
	accounts map[types.Hash][]mockAccount // root -> sorted accounts
	storage  map[string][]mockStorageSlot // root+account -> sorted slots
	codes    map[types.Hash][]byte        // code hash -> bytecode
	nodes    map[string][]byte            // root+path -> node data
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		accounts: make(map[types.Hash][]mockAccount),
		storage:  make(map[string][]mockStorageSlot),
		codes:    make(map[types.Hash][]byte),
		nodes:    make(map[string][]byte),
	}
}

func (b *mockBackend) AccountIterator(root, origin types.Hash, fn func(types.Hash, []byte) bool) error {
	accts, ok := b.accounts[root]
	if !ok {
		return ErrMissingRoot
	}
	for _, a := range accts {
		if bytes.Compare(a.hash[:], origin[:]) < 0 {
			continue
		}
		if !fn(a.hash, a.body) {
			break
		}
	}
	return nil
}

func (b *mockBackend) StorageIterator(root, account types.Hash, origin []byte, fn func(types.Hash, []byte) bool) error {
	key := string(root[:]) + string(account[:])
	slots, ok := b.storage[key]
	if !ok {
		return nil // no storage is not an error
	}
	for _, s := range slots {
		if len(origin) > 0 && bytes.Compare(s.hash[:], origin[:]) < 0 {
			continue
		}
		if !fn(s.hash, s.body) {
			break
		}
	}
	return nil
}

func (b *mockBackend) Code(hash types.Hash) ([]byte, error) {
	code, ok := b.codes[hash]
	if !ok {
		return nil, rawdb.ErrNotFound
	}
	return code, nil
}

func (b *mockBackend) TrieNode(root types.Hash, path []byte) ([]byte, error) {
	key := string(root[:]) + string(path)
	data, ok := b.nodes[key]
	if !ok {
		return nil, rawdb.ErrNotFound
	}
	return data, nil
}

func (b *mockBackend) AccountProof(root, hash types.Hash) ([][]byte, error) {
	// Return a simple mock proof.
	return [][]byte{hash[:]}, nil
}

func (b *mockBackend) StorageProof(root, account, slot types.Hash) ([][]byte, error) {
	return [][]byte{slot[:]}, nil
}

// encodeSlimAccount encodes an account in the slim format for the state trie.
func encodeSlimAccount(nonce uint64, balance *big.Int, root, codeHash types.Hash) []byte {
	var buf []byte
	n := make([]byte, 8)
	binary.BigEndian.PutUint64(n, nonce)
	buf = append(buf, n...)
	if balance != nil {
		b := balance.Bytes()
		pad := make([]byte, 32-len(b))
		buf = append(buf, pad...)
		buf = append(buf, b...)
	} else {
		buf = append(buf, make([]byte, 32)...)
	}
	buf = append(buf, root[:]...)
	buf = append(buf, codeHash[:]...)
	return buf
}

func makeTestAccounts(root types.Hash, n int) *mockBackend {
	b := newMockBackend()
	accts := make([]mockAccount, n)
	for i := 0; i < n; i++ {
		hashInput := []byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}
		hash := crypto.Keccak256Hash(hashInput)
		body := encodeSlimAccount(uint64(i), big.NewInt(int64(1000*(i+1))), types.EmptyRootHash, types.EmptyCodeHash)
		accts[i] = mockAccount{hash: hash, body: body}
	}
	sort.Slice(accts, func(i, j int) bool {
		return bytes.Compare(accts[i].hash[:], accts[j].hash[:]) < 0
	})
	b.accounts[root] = accts
	return b
}

// --- Account range iteration tests ---

func TestHandleGetAccountRange_Basic(t *testing.T) {
	root := types.Hash{0x01}
	backend := makeTestAccounts(root, 10)
	handler := NewServerHandler(backend)

	req := &GetAccountRangePacket{
		ID:     1,
		Root:   root,
		Origin: types.Hash{},
		Limit:  maxHash(),
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetAccountRange(req)
	if err != nil {
		t.Fatalf("HandleGetAccountRange: %v", err)
	}

	if resp.ID != 1 {
		t.Errorf("ID: want 1, got %d", resp.ID)
	}
	if len(resp.Accounts) != 10 {
		t.Fatalf("accounts: want 10, got %d", len(resp.Accounts))
	}

	// Verify accounts are in hash-sorted order.
	for i := 1; i < len(resp.Accounts); i++ {
		if bytes.Compare(resp.Accounts[i-1].Hash[:], resp.Accounts[i].Hash[:]) >= 0 {
			t.Fatal("accounts not sorted by hash")
		}
	}
}

func TestHandleGetAccountRange_WithSeekAndLimit(t *testing.T) {
	root := types.Hash{0x01}
	backend := makeTestAccounts(root, 20)
	handler := NewServerHandler(backend)

	allAccts := backend.accounts[root]

	// Request accounts starting from the 5th account's hash.
	origin := allAccts[5].hash
	limit := allAccts[14].hash

	req := &GetAccountRangePacket{
		ID:     2,
		Root:   root,
		Origin: origin,
		Limit:  limit,
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetAccountRange(req)
	if err != nil {
		t.Fatalf("HandleGetAccountRange: %v", err)
	}

	// Should get accounts from index 5 to 14 (inclusive).
	if len(resp.Accounts) != 10 {
		t.Fatalf("accounts: want 10, got %d", len(resp.Accounts))
	}

	// Verify range boundaries.
	if resp.Accounts[0].Hash != origin {
		t.Errorf("first account should be origin")
	}
	if resp.Accounts[len(resp.Accounts)-1].Hash != limit {
		t.Errorf("last account should be limit")
	}
}

func TestHandleGetAccountRange_EmptyResult(t *testing.T) {
	root := types.Hash{0x01}
	backend := makeTestAccounts(root, 5)
	handler := NewServerHandler(backend)

	// Request a range that doesn't overlap any accounts.
	req := &GetAccountRangePacket{
		ID:     3,
		Root:   root,
		Origin: types.Hash{0xff, 0xff, 0xff, 0xff},
		Limit:  maxHash(),
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetAccountRange(req)
	if err != nil {
		t.Fatalf("HandleGetAccountRange: %v", err)
	}

	if len(resp.Accounts) != 0 {
		t.Fatalf("expected empty result, got %d accounts", len(resp.Accounts))
	}
}

func TestHandleGetAccountRange_InvalidRange(t *testing.T) {
	backend := newMockBackend()
	handler := NewServerHandler(backend)

	// Origin > Limit should be rejected.
	req := &GetAccountRangePacket{
		ID:     4,
		Root:   types.Hash{0x01},
		Origin: types.Hash{0xff},
		Limit:  types.Hash{0x01},
		Bytes:  SoftResponseLimit,
	}

	_, err := handler.HandleGetAccountRange(req)
	if !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("expected ErrInvalidRange, got %v", err)
	}
}

// --- Storage range iteration tests ---

func TestHandleGetStorageRanges_Basic(t *testing.T) {
	root := types.Hash{0x01}
	account := types.Hash{0xaa}

	backend := newMockBackend()
	backend.accounts[root] = []mockAccount{{hash: account, body: []byte{0x01}}}

	key := string(root[:]) + string(account[:])
	backend.storage[key] = []mockStorageSlot{
		{hash: types.Hash{0x10}, body: []byte{0xaa}},
		{hash: types.Hash{0x20}, body: []byte{0xbb}},
		{hash: types.Hash{0x30}, body: []byte{0xcc}},
	}

	handler := NewServerHandler(backend)

	req := &GetStorageRangesPacket{
		ID:       1,
		Root:     root,
		Accounts: []types.Hash{account},
		Bytes:    SoftResponseLimit,
	}

	resp, err := handler.HandleGetStorageRanges(req)
	if err != nil {
		t.Fatalf("HandleGetStorageRanges: %v", err)
	}

	if len(resp.Slots) != 1 {
		t.Fatalf("slot sets: want 1, got %d", len(resp.Slots))
	}
	if len(resp.Slots[0]) != 3 {
		t.Fatalf("slots[0]: want 3, got %d", len(resp.Slots[0]))
	}
}

func TestHandleGetStorageRanges_MultipleAccounts(t *testing.T) {
	root := types.Hash{0x01}
	acct1 := types.Hash{0xaa}
	acct2 := types.Hash{0xbb}

	backend := newMockBackend()
	key1 := string(root[:]) + string(acct1[:])
	key2 := string(root[:]) + string(acct2[:])
	backend.storage[key1] = []mockStorageSlot{
		{hash: types.Hash{0x01}, body: []byte{0x11}},
	}
	backend.storage[key2] = []mockStorageSlot{
		{hash: types.Hash{0x02}, body: []byte{0x22}},
		{hash: types.Hash{0x03}, body: []byte{0x33}},
	}

	handler := NewServerHandler(backend)

	req := &GetStorageRangesPacket{
		ID:       2,
		Root:     root,
		Accounts: []types.Hash{acct1, acct2},
		Bytes:    SoftResponseLimit,
	}

	resp, err := handler.HandleGetStorageRanges(req)
	if err != nil {
		t.Fatalf("HandleGetStorageRanges: %v", err)
	}

	if len(resp.Slots) != 2 {
		t.Fatalf("slot sets: want 2, got %d", len(resp.Slots))
	}
	if len(resp.Slots[0]) != 1 {
		t.Fatalf("slots[0]: want 1, got %d", len(resp.Slots[0]))
	}
	if len(resp.Slots[1]) != 2 {
		t.Fatalf("slots[1]: want 2, got %d", len(resp.Slots[1]))
	}
}

// --- Bytecode retrieval tests ---

func TestHandleGetByteCodes_Basic(t *testing.T) {
	code1 := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	code2 := []byte{0x60, 0x40, 0x52}
	hash1 := crypto.Keccak256Hash(code1)
	hash2 := crypto.Keccak256Hash(code2)

	backend := newMockBackend()
	backend.codes[hash1] = code1
	backend.codes[hash2] = code2

	handler := NewServerHandler(backend)

	req := &GetByteCodesPacket{
		ID:     1,
		Hashes: []types.Hash{hash1, hash2},
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetByteCodes(req)
	if err != nil {
		t.Fatalf("HandleGetByteCodes: %v", err)
	}

	if len(resp.Codes) != 2 {
		t.Fatalf("codes: want 2, got %d", len(resp.Codes))
	}
	if !bytes.Equal(resp.Codes[0], code1) {
		t.Errorf("code[0] mismatch")
	}
	if !bytes.Equal(resp.Codes[1], code2) {
		t.Errorf("code[1] mismatch")
	}
}

func TestHandleGetByteCodes_MissingCode(t *testing.T) {
	code := []byte{0x60, 0x00}
	hash := crypto.Keccak256Hash(code)
	missingHash := types.Hash{0xff, 0xfe}

	backend := newMockBackend()
	backend.codes[hash] = code

	handler := NewServerHandler(backend)

	req := &GetByteCodesPacket{
		ID:     2,
		Hashes: []types.Hash{hash, missingHash},
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetByteCodes(req)
	if err != nil {
		t.Fatalf("HandleGetByteCodes: %v", err)
	}

	// Only the found code should be returned.
	if len(resp.Codes) != 1 {
		t.Fatalf("codes: want 1 (missing skipped), got %d", len(resp.Codes))
	}
}

func TestHandleGetByteCodes_Empty(t *testing.T) {
	backend := newMockBackend()
	handler := NewServerHandler(backend)

	req := &GetByteCodesPacket{
		ID:     3,
		Hashes: []types.Hash{{0x01}, {0x02}},
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetByteCodes(req)
	if err != nil {
		t.Fatalf("HandleGetByteCodes: %v", err)
	}

	if len(resp.Codes) != 0 {
		t.Fatalf("expected 0 codes, got %d", len(resp.Codes))
	}
}

// --- Trie node retrieval tests ---

func TestHandleGetTrieNodes_Basic(t *testing.T) {
	root := types.Hash{0x01}
	backend := newMockBackend()
	backend.nodes[string(root[:])+string([]byte{0x01, 0x02})] = []byte{0xde, 0xad}
	backend.nodes[string(root[:])+string([]byte{0x03})] = []byte{0xbe, 0xef}

	handler := NewServerHandler(backend)

	req := &GetTrieNodesPacket{
		ID:   1,
		Root: root,
		Paths: []TrieNodePathSet{
			{[]byte{0x01, 0x02}},
			{[]byte{0x03}},
		},
		Bytes: SoftResponseLimit,
	}

	resp, err := handler.HandleGetTrieNodes(req)
	if err != nil {
		t.Fatalf("HandleGetTrieNodes: %v", err)
	}

	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes: want 2, got %d", len(resp.Nodes))
	}
	if !bytes.Equal(resp.Nodes[0], []byte{0xde, 0xad}) {
		t.Errorf("node[0] mismatch")
	}
	if !bytes.Equal(resp.Nodes[1], []byte{0xbe, 0xef}) {
		t.Errorf("node[1] mismatch")
	}
}

func TestHandleGetTrieNodes_MissingNode(t *testing.T) {
	root := types.Hash{0x01}
	backend := newMockBackend()
	backend.nodes[string(root[:])+string([]byte{0x01})] = []byte{0xaa}

	handler := NewServerHandler(backend)

	req := &GetTrieNodesPacket{
		ID:   2,
		Root: root,
		Paths: []TrieNodePathSet{
			{[]byte{0x01}},
			{[]byte{0x99}}, // missing
		},
		Bytes: SoftResponseLimit,
	}

	resp, err := handler.HandleGetTrieNodes(req)
	if err != nil {
		t.Fatalf("HandleGetTrieNodes: %v", err)
	}

	// snap/1 requires an empty reply if any requested path is unavailable.
	if len(resp.Nodes) != 0 || resp.ID != req.ID {
		t.Fatalf("want correlated empty response, got %+v", resp)
	}
}

// --- Response size limiting tests ---

func TestHandleGetAccountRange_SizeLimit(t *testing.T) {
	root := types.Hash{0x01}
	backend := makeTestAccounts(root, 100)
	handler := NewServerHandler(backend)

	// Request with a very small byte limit to trigger early cutoff.
	req := &GetAccountRangePacket{
		ID:     5,
		Root:   root,
		Origin: types.Hash{},
		Limit:  maxHash(),
		Bytes:  200, // Very small limit.
	}

	resp, err := handler.HandleGetAccountRange(req)
	if err != nil {
		t.Fatalf("HandleGetAccountRange: %v", err)
	}

	// Should get fewer accounts than the total available.
	if len(resp.Accounts) >= 100 {
		t.Fatalf("expected response to be capped, got %d accounts", len(resp.Accounts))
	}
	// Should get at least one account.
	if len(resp.Accounts) == 0 {
		t.Fatal("expected at least 1 account")
	}
}

func TestHandleGetByteCodes_SizeLimit(t *testing.T) {
	backend := newMockBackend()

	// Create many bytecodes.
	var hashes []types.Hash
	for i := 0; i < 50; i++ {
		code := make([]byte, 1000) // 1 KB each
		code[0] = byte(i)
		hash := crypto.Keccak256Hash(code)
		backend.codes[hash] = code
		hashes = append(hashes, hash)
	}

	handler := NewServerHandler(backend)

	req := &GetByteCodesPacket{
		ID:     6,
		Hashes: hashes,
		Bytes:  5000, // Only ~5 KB allowed.
	}

	resp, err := handler.HandleGetByteCodes(req)
	if err != nil {
		t.Fatalf("HandleGetByteCodes: %v", err)
	}

	// Should have been capped well below 50 codes.
	if len(resp.Codes) >= 50 {
		t.Fatalf("expected response to be capped, got %d codes", len(resp.Codes))
	}
	if len(resp.Codes) == 0 {
		t.Fatal("expected at least 1 code")
	}
}

func TestHandleGetTrieNodes_SizeLimit(t *testing.T) {
	root := types.Hash{0x01}
	backend := newMockBackend()

	// Create many large nodes.
	var paths []TrieNodePathSet
	for i := 0; i < 100; i++ {
		path := []byte{byte(i)}
		data := make([]byte, 500) // 500 bytes each
		data[0] = byte(i)
		backend.nodes[string(root[:])+string(path)] = data
		paths = append(paths, TrieNodePathSet{path})
	}

	handler := NewServerHandler(backend)

	req := &GetTrieNodesPacket{
		ID:    7,
		Root:  root,
		Paths: paths,
		Bytes: 2000, // Only ~2 KB allowed.
	}

	resp, err := handler.HandleGetTrieNodes(req)
	if err != nil {
		t.Fatalf("HandleGetTrieNodes: %v", err)
	}

	if len(resp.Nodes) >= 100 {
		t.Fatalf("expected response to be capped, got %d nodes", len(resp.Nodes))
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least 1 node")
	}
}

// --- Protocol constant tests ---

func TestProtocolConstants(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Errorf("protocol version: want 1, got %d", ProtocolVersion)
	}
	if ProtocolName != "snap" {
		t.Errorf("protocol name: want snap, got %s", ProtocolName)
	}
	if SoftResponseLimit >= HardResponseLimit {
		t.Error("soft limit should be less than hard limit")
	}
	if SoftResponseLimit == 0 {
		t.Error("soft limit should be non-zero")
	}

	// Verify message codes are unique and sequential.
	codes := []uint64{
		GetAccountRangeMsg, AccountRangeMsg,
		GetStorageRangesMsg, StorageRangesMsg,
		GetByteCodesMsg, ByteCodesMsg,
		GetTrieNodesMsg, TrieNodesMsg,
	}
	for i := 0; i < len(codes); i++ {
		if codes[i] != uint64(i) {
			t.Errorf("message code[%d]: want %d, got %d", i, i, codes[i])
		}
	}
}

// --- Empty response tests ---

func TestHandleGetAccountRange_UnknownRoot(t *testing.T) {
	backend := newMockBackend()
	handler := NewServerHandler(backend)

	req := &GetAccountRangePacket{
		ID:     10,
		Root:   types.Hash{0xde, 0xad},
		Origin: types.Hash{},
		Limit:  maxHash(),
		Bytes:  SoftResponseLimit,
	}

	resp, err := handler.HandleGetAccountRange(req)
	if err != nil || resp.ID != req.ID || len(resp.Accounts) != 0 || len(resp.Proof) != 0 {
		t.Fatalf("unknown root must return a correlated empty response: %+v %v", resp, err)
	}
}

func TestHandleGetStorageRanges_NoStorage(t *testing.T) {
	backend := newMockBackend()
	handler := NewServerHandler(backend)

	req := &GetStorageRangesPacket{
		ID:       11,
		Root:     types.Hash{0x01},
		Accounts: []types.Hash{{0xaa}},
		Bytes:    SoftResponseLimit,
	}

	resp, err := handler.HandleGetStorageRanges(req)
	if err != nil {
		t.Fatalf("HandleGetStorageRanges: %v", err)
	}

	if len(resp.Slots) != 1 {
		t.Fatalf("slot sets: want 1, got %d", len(resp.Slots))
	}
	if len(resp.Slots[0]) != 0 {
		t.Fatalf("expected empty slots, got %d", len(resp.Slots[0]))
	}
}

// --- Handler interface compliance test ---

func TestServerHandler_ImplementsHandler(t *testing.T) {
	backend := newMockBackend()
	handler := NewServerHandler(backend)

	// Verify the ServerHandler satisfies the Handler interface.
	var _ Handler = handler
}

// maxHash returns the maximum possible hash (all 0xff bytes).
func maxHash() types.Hash {
	var h types.Hash
	for i := range h {
		h[i] = 0xff
	}
	return h
}
