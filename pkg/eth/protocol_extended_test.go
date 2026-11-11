package eth

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/p2p"
)

// --- AllCapabilities ---

func TestAllCapabilities_Count(t *testing.T) {
	caps := AllCapabilities()
	if len(caps) != 5 {
		t.Fatalf("expected 5 capabilities (68-72), got %d", len(caps))
	}
}

func TestAllCapabilities_Versions(t *testing.T) {
	caps := AllCapabilities()
	want := map[uint]bool{
		ETH68:    true,
		ETH69:    true,
		ETH70:    true,
		ETH71:    true,
		ExtETH72: true,
	}
	for _, c := range caps {
		if !want[c.Version] {
			t.Errorf("unexpected version %d in AllCapabilities", c.Version)
		}
		delete(want, c.Version)
	}
	for v := range want {
		t.Errorf("missing version %d in AllCapabilities", v)
	}
}

func TestAllCapabilities_Names(t *testing.T) {
	expected := map[uint]string{
		ETH68:    "eth/68",
		ETH69:    "eth/69",
		ETH70:    "eth/70",
		ETH71:    "eth/71",
		ExtETH72: "eth/72",
	}
	for _, c := range AllCapabilities() {
		if name, ok := expected[c.Version]; ok {
			if c.Name != name {
				t.Errorf("version %d: expected name %q, got %q", c.Version, name, c.Name)
			}
		}
	}
}

func TestAllCapabilities_Lengths(t *testing.T) {
	want := map[uint]uint64{
		ETH68:    17,
		ETH69:    17,
		ETH70:    19,
		ETH71:    21,
		ExtETH72: 23,
	}
	for _, c := range AllCapabilities() {
		if l, ok := want[c.Version]; ok {
			if c.Length != l {
				t.Errorf("version %d: expected length %d, got %d", c.Version, l, c.Length)
			}
		}
	}
}

func TestAllCapabilities_MessageIDsIncreasing(t *testing.T) {
	// Each higher version should have at least as many message IDs.
	caps := AllCapabilities()
	for i := 1; i < len(caps); i++ {
		if len(caps[i].MessageIDs) < len(caps[i-1].MessageIDs) {
			t.Errorf("version %d has fewer message IDs (%d) than version %d (%d)",
				caps[i].Version, len(caps[i].MessageIDs),
				caps[i-1].Version, len(caps[i-1].MessageIDs))
		}
	}
}

// --- CapabilityByVersion ---

func TestCapabilityByVersion_Found(t *testing.T) {
	for _, v := range []uint{ETH68, ETH69, ETH70, ETH71, ExtETH72} {
		cap := CapabilityByVersion(v)
		if cap == nil {
			t.Errorf("expected capability for version %d, got nil", v)
			continue
		}
		if cap.Version != v {
			t.Errorf("expected version %d, got %d", v, cap.Version)
		}
	}
}

func TestCapabilityByVersion_NotFound(t *testing.T) {
	cap := CapabilityByVersion(99)
	if cap != nil {
		t.Errorf("expected nil for unknown version 99, got %+v", cap)
	}
	cap = CapabilityByVersion(0)
	if cap != nil {
		t.Errorf("expected nil for version 0, got %+v", cap)
	}
}

func TestCapabilityByVersion_ReturnsCopy(t *testing.T) {
	c1 := CapabilityByVersion(ETH68)
	c2 := CapabilityByVersion(ETH68)
	if c1 == c2 {
		t.Error("expected distinct pointer on each call")
	}
}

// --- NegotiateCapability ---

func TestNegotiateCapability_HighestCommon(t *testing.T) {
	local := AllCapabilities()
	remote := []ProtocolCapability{
		{Version: ETH68, Name: "eth/68"},
		{Version: ETH70, Name: "eth/70"},
	}

	cap := NegotiateCapability(local, remote)
	if cap == nil {
		t.Fatal("expected negotiated capability, got nil")
	}
	if cap.Version != ETH70 {
		t.Fatalf("expected version %d, got %d", ETH70, cap.Version)
	}
}

func TestNegotiateCapability_NoCommon(t *testing.T) {
	local := []ProtocolCapability{{Version: ETH68}}
	remote := []ProtocolCapability{{Version: ExtETH72}}

	cap := NegotiateCapability(local, remote)
	if cap != nil {
		t.Fatalf("expected nil for no common version, got version %d", cap.Version)
	}
}

func TestNegotiateCapability_ExactMatch(t *testing.T) {
	local := []ProtocolCapability{{Version: ETH70, Name: "eth/70"}}
	remote := []ProtocolCapability{{Version: ETH70, Name: "eth/70"}}

	cap := NegotiateCapability(local, remote)
	if cap == nil {
		t.Fatal("expected negotiated capability, got nil")
	}
	if cap.Version != ETH70 {
		t.Fatalf("expected version %d, got %d", ETH70, cap.Version)
	}
}

func TestNegotiateCapability_PreferHigher(t *testing.T) {
	local := AllCapabilities()
	remote := AllCapabilities()

	cap := NegotiateCapability(local, remote)
	if cap == nil {
		t.Fatal("expected negotiated capability")
	}
	if cap.Version != ExtETH72 {
		t.Fatalf("expected highest version %d, got %d", ExtETH72, cap.Version)
	}
}

func TestNegotiateCapability_EmptyLocal(t *testing.T) {
	cap := NegotiateCapability(nil, AllCapabilities())
	if cap != nil {
		t.Fatal("expected nil for empty local capabilities")
	}
}

func TestNegotiateCapability_EmptyRemote(t *testing.T) {
	cap := NegotiateCapability(AllCapabilities(), nil)
	if cap != nil {
		t.Fatal("expected nil for empty remote capabilities")
	}
}

// --- IsMessageSupported ---

func TestIsMessageSupported_BaseMessages(t *testing.T) {
	// Status (0x00) is in every version.
	for _, v := range []uint{ETH68, ETH69, ETH70, ETH71, ExtETH72} {
		if !IsMessageSupported(v, MsgStatus) {
			t.Errorf("MsgStatus should be supported in version %d", v)
		}
	}
}

func TestIsMessageSupported_ExtendedMessages(t *testing.T) {
	// Partial receipts only in eth/70+.
	if IsMessageSupported(ETH68, MsgGetPartialReceipts) {
		t.Error("MsgGetPartialReceipts should not be supported in eth/68")
	}
	if IsMessageSupported(ETH69, MsgGetPartialReceipts) {
		t.Error("MsgGetPartialReceipts should not be supported in eth/69")
	}
	if !IsMessageSupported(ETH70, MsgGetPartialReceipts) {
		t.Error("MsgGetPartialReceipts should be supported in eth/70")
	}

	// Block access lists only in eth/71+.
	if IsMessageSupported(ETH70, MsgGetBlockAccessLists) {
		t.Error("MsgGetBlockAccessLists should not be supported in eth/70")
	}
	if !IsMessageSupported(ETH71, MsgGetBlockAccessLists) {
		t.Error("MsgGetBlockAccessLists should be supported in eth/71")
	}

	// Execution witness only in eth/72.
	if IsMessageSupported(ETH71, MsgGetExecutionWitness) {
		t.Error("MsgGetExecutionWitness should not be supported in eth/71")
	}
	if !IsMessageSupported(ExtETH72, MsgGetExecutionWitness) {
		t.Error("MsgGetExecutionWitness should be supported in eth/72")
	}
}

func TestIsMessageSupported_UnknownVersion(t *testing.T) {
	if IsMessageSupported(99, MsgStatus) {
		t.Error("unknown version 99 should not support any message")
	}
}

// --- MessageIDOffset ---

func TestMessageIDOffset_KnownVersion(t *testing.T) {
	// For known versions the function returns baseOffset unchanged.
	offset := MessageIDOffset(ETH68, 100)
	if offset != 100 {
		t.Fatalf("expected 100, got %d", offset)
	}
}

func TestMessageIDOffset_UnknownVersion(t *testing.T) {
	offset := MessageIDOffset(99, 200)
	if offset != 200 {
		t.Fatalf("expected baseOffset 200 for unknown version, got %d", offset)
	}
}

// --- ExtMsgCodeName ---

func TestExtMsgCodeName_KnownCodes(t *testing.T) {
	cases := []struct {
		code uint64
		want string
	}{
		{MsgStatus, "Status"},
		{MsgNewBlockHashes, "NewBlockHashes"},
		{MsgTransactions, "Transactions"},
		{MsgGetBlockHeaders, "GetBlockHeaders"},
		{MsgBlockHeaders, "BlockHeaders"},
		{MsgGetBlockBodies, "GetBlockBodies"},
		{MsgBlockBodies, "BlockBodies"},
		{MsgNewBlock, "NewBlock"},
		{MsgNewPooledTransactionHashes, "NewPooledTransactionHashes"},
		{MsgGetPooledTransactions, "GetPooledTransactions"},
		{MsgPooledTransactions, "PooledTransactions"},
		{MsgGetPartialReceipts, "GetPartialReceipts"},
		{MsgPartialReceipts, "PartialReceipts"},
		{MsgGetBlockAccessLists, "GetBlockAccessLists"},
		{MsgBlockAccessLists, "BlockAccessLists"},
		{MsgGetExecutionWitness, "GetExecutionWitness"},
		{MsgExecutionWitness, "ExecutionWitness"},
	}
	for _, tc := range cases {
		got := ExtMsgCodeName(tc.code)
		if got != tc.want {
			t.Errorf("code 0x%02x: expected %q, got %q", tc.code, tc.want, got)
		}
	}
}

func TestExtMsgCodeName_UnknownCode(t *testing.T) {
	name := ExtMsgCodeName(0xff)
	if name == "" {
		t.Error("expected non-empty name for unknown code")
	}
	// Should contain hex representation.
	if name == "Status" || name == "Transactions" {
		t.Errorf("expected unknown code name, got %q", name)
	}
}

// --- SupportedVersions ---

func TestSupportedVersions(t *testing.T) {
	versions := SupportedVersions()
	if len(versions) != 5 {
		t.Fatalf("expected 5 supported versions, got %d", len(versions))
	}
	// Verify sorted descending.
	for i := 1; i < len(versions); i++ {
		if versions[i] >= versions[i-1] {
			t.Errorf("versions not sorted descending: [%d]=%d >= [%d]=%d",
				i, versions[i], i-1, versions[i-1])
		}
	}
	// Highest should be ExtETH72.
	if versions[0] != ExtETH72 {
		t.Errorf("expected highest version %d, got %d", ExtETH72, versions[0])
	}
}

// --- BuildStatusMessage ---

// mockBuildChain implements Blockchain for BuildStatusMessage testing.
type mockBuildChain struct {
	current *types.Block
	genesis *types.Block
}

func newMockBuildChain() *mockBuildChain {
	genesis := makeTestBlock(0, types.Hash{1, 2, 3}, nil)
	current := makeTestBlock(5, genesis.Hash(), nil)
	return &mockBuildChain{current: current, genesis: genesis}
}

func (m *mockBuildChain) CurrentBlock() *types.Block             { return m.current }
func (m *mockBuildChain) Genesis() *types.Block                  { return m.genesis }
func (m *mockBuildChain) GetBlock(h types.Hash) *types.Block     { return nil }
func (m *mockBuildChain) GetBlockByNumber(n uint64) *types.Block { return nil }
func (m *mockBuildChain) HasBlock(h types.Hash) bool             { return false }
func (m *mockBuildChain) InsertBlock(b *types.Block) error       { return nil }

func TestBuildStatusMessage(t *testing.T) {
	chain := newMockBuildChain()
	forkID := p2p.ForkID{Hash: [4]byte{0xde, 0xad, 0xbe, 0xef}}

	msg := BuildStatusMessage(68, 1, chain, forkID, 0)
	if msg == nil {
		t.Fatal("expected non-nil StatusMessage")
	}
	if msg.ProtocolVersion != 68 {
		t.Errorf("expected ProtocolVersion 68, got %d", msg.ProtocolVersion)
	}
	if msg.NetworkID != 1 {
		t.Errorf("expected NetworkID 1, got %d", msg.NetworkID)
	}
	if msg.BestHash != chain.CurrentBlock().Hash() {
		t.Errorf("BestHash mismatch")
	}
	if msg.Genesis != chain.Genesis().Hash() {
		t.Errorf("Genesis mismatch")
	}
	if msg.ForkID != forkID {
		t.Errorf("ForkID mismatch")
	}
}

func TestBuildStatusMessage_DifferentVersions(t *testing.T) {
	chain := newMockBuildChain()
	forkID := p2p.ForkID{}

	for _, version := range []uint32{68, 70, 72} {
		msg := BuildStatusMessage(version, 1, chain, forkID, 0)
		if msg == nil {
			t.Fatalf("expected non-nil message for version %d", version)
		}
		if msg.ProtocolVersion != version {
			t.Errorf("version %d: ProtocolVersion mismatch: got %d", version, msg.ProtocolVersion)
		}
	}
}

// --- Message struct sanity checks ---

func TestGetPartialReceiptsMessage(t *testing.T) {
	hash := types.Hash{0xab}
	msg := GetPartialReceiptsMessage{
		BlockHash: hash,
		Indices:   []uint64{0, 1, 2},
	}
	if msg.BlockHash != hash {
		t.Error("BlockHash mismatch")
	}
	if len(msg.Indices) != 3 {
		t.Errorf("expected 3 indices, got %d", len(msg.Indices))
	}
}

func TestPartialReceiptsMessage(t *testing.T) {
	hash := types.Hash{0xcd}
	receipt := &types.Receipt{
		Status:            1,
		CumulativeGasUsed: 21000,
	}
	msg := PartialReceiptsMessage{
		BlockHash: hash,
		Receipts:  []*types.Receipt{receipt},
	}
	if msg.BlockHash != hash {
		t.Error("BlockHash mismatch")
	}
	if len(msg.Receipts) != 1 {
		t.Errorf("expected 1 receipt, got %d", len(msg.Receipts))
	}
}

func TestGetBlockAccessListsMessage(t *testing.T) {
	h1 := types.Hash{0x01}
	h2 := types.Hash{0x02}
	msg := GetBlockAccessListsMessage{Hashes: []types.Hash{h1, h2}}
	if len(msg.Hashes) != 2 {
		t.Errorf("expected 2 hashes, got %d", len(msg.Hashes))
	}
}

func TestBlockAccessListsMessage(t *testing.T) {
	hash := types.Hash{0xef}
	msg := BlockAccessListsMessage{
		Hashes:      []types.Hash{hash},
		AccessLists: [][]byte{{0xc1, 0xaa}},
	}
	if len(msg.Hashes) != 1 || msg.Hashes[0] != hash {
		t.Error("Hash mismatch")
	}
	if len(msg.AccessLists) != 1 {
		t.Errorf("expected 1 RLP-encoded access list, got %d", len(msg.AccessLists))
	}
}

func TestGetExecutionWitnessMessage(t *testing.T) {
	hash := types.Hash{0xaa}
	msg := GetExecutionWitnessMessage{BlockHash: hash}
	if msg.BlockHash != hash {
		t.Error("BlockHash mismatch")
	}
}

func TestExecutionWitnessMessage(t *testing.T) {
	hash := types.Hash{0xbb}
	data := []byte{1, 2, 3, 4}
	msg := ExecutionWitnessMessage{BlockHash: hash, WitnessData: data}
	if msg.BlockHash != hash {
		t.Error("BlockHash mismatch")
	}
	if len(msg.WitnessData) != 4 {
		t.Errorf("expected 4 bytes witness data, got %d", len(msg.WitnessData))
	}
}

func TestExtStatusInfo(t *testing.T) {
	info := ExtStatusInfo{
		StatusInfo: StatusInfo{
			ProtocolVersion: 69,
			NetworkID:       1,
			TD:              big.NewInt(0),
			Head:            types.Hash{0x01},
			Genesis:         types.Hash{0x02},
		},
		PostMerge: true,
	}
	if !info.PostMerge {
		t.Error("expected PostMerge true")
	}
	if info.ProtocolVersion != 69 {
		t.Errorf("expected ProtocolVersion 69, got %d", info.ProtocolVersion)
	}
}

// --- Constant values ---

func TestProtocolConstants(t *testing.T) {
	if ETH69 != 69 {
		t.Errorf("ETH69 should be 69, got %d", ETH69)
	}
	if ExtETH72 != 72 {
		t.Errorf("ExtETH72 should be 72, got %d", ExtETH72)
	}
	if MsgGetPartialReceipts != 0x0d {
		t.Errorf("MsgGetPartialReceipts should be 0x0d, got 0x%02x", MsgGetPartialReceipts)
	}
	if MsgPartialReceipts != 0x0e {
		t.Errorf("MsgPartialReceipts should be 0x0e, got 0x%02x", MsgPartialReceipts)
	}
	if MsgGetBlockAccessLists != 0x0f {
		t.Errorf("MsgGetBlockAccessLists should be 0x0f, got 0x%02x", MsgGetBlockAccessLists)
	}
	if MsgBlockAccessLists != 0x10 {
		t.Errorf("MsgBlockAccessLists should be 0x10, got 0x%02x", MsgBlockAccessLists)
	}
	if MsgGetExecutionWitness != 0x11 {
		t.Errorf("MsgGetExecutionWitness should be 0x11, got 0x%02x", MsgGetExecutionWitness)
	}
	if MsgExecutionWitness != 0x12 {
		t.Errorf("MsgExecutionWitness should be 0x12, got 0x%02x", MsgExecutionWitness)
	}
}

func TestLimitConstants(t *testing.T) {
	if MaxReceiptsServe != 256 {
		t.Errorf("MaxReceiptsServe should be 256, got %d", MaxReceiptsServe)
	}
	if MaxExecutionWitness != 1<<20 {
		t.Errorf("MaxExecutionWitness should be 1 MiB, got %d", MaxExecutionWitness)
	}
	if MaxMessageSize != 10*1024*1024 {
		t.Errorf("MaxMessageSize should be 10 MiB, got %d", MaxMessageSize)
	}
}

func TestErrorVariables(t *testing.T) {
	if ErrUnsupportedMessage == nil {
		t.Error("ErrUnsupportedMessage should not be nil")
	}
	if ErrVersionNegotiation == nil {
		t.Error("ErrVersionNegotiation should not be nil")
	}
	if ErrMessageTooLarge == nil {
		t.Error("ErrMessageTooLarge should not be nil")
	}
	if ErrInvalidMessageCode == nil {
		t.Error("ErrInvalidMessageCode should not be nil")
	}
}
