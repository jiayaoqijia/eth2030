package types

import (
	"testing"

	"golang.org/x/crypto/sha3"
)

func TestBloom9BitPositions(t *testing.T) {
	// Test that bloom9 produces deterministic bit positions for known input.
	// keccak256("test") = 9c22ff5f21f0b81b113e63f7db6da94fedef11b2119b4088b89664fb9a3cb658
	// First 6 bytes: 9c 22 ff 5f 21 f0
	// Pair 0: 0x9c22 & 0x7FF = 0x9c22 & 0x7FF = 0x422 = 1058
	// Pair 1: 0xff5f & 0x7FF = 0x75f = 1887
	// Pair 2: 0x21f0 & 0x7FF = 0x1f0 = 496
	data := []byte("test")
	bits := bloom9(data)

	d := sha3.NewLegacyKeccak256()
	d.Write(data)
	h := d.Sum(nil)
	// Verify our understanding of the hash
	if h[0] != 0x9c || h[1] != 0x22 {
		t.Fatalf("unexpected keccak256 prefix: %x", h[:6])
	}

	expected := [3]uint{
		0x9c22 & 0x7FF, // 1058
		0xff5f & 0x7FF, // 1887
		0x21f0 & 0x7FF, // 496
	}

	for i, got := range bits {
		if got != expected[i] {
			t.Errorf("bloom9 bit[%d]: got %d, want %d", i, got, expected[i])
		}
	}
}

func TestBloom9DifferentInputs(t *testing.T) {
	// Different inputs should (generally) produce different bit positions.
	bits1 := bloom9([]byte("hello"))
	bits2 := bloom9([]byte("world"))

	same := 0
	for i := 0; i < 3; i++ {
		if bits1[i] == bits2[i] {
			same++
		}
	}
	if same == 3 {
		t.Fatal("different inputs produced identical bit positions")
	}
}

func TestBloomAddSetsBits(t *testing.T) {
	var bloom Bloom
	BloomAdd(&bloom, []byte("test"))

	// The bloom should no longer be all zeros.
	allZero := true
	for _, b := range bloom {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("bloom should have bits set after BloomAdd")
	}

	// Count set bits - should be exactly 3 (assuming no collisions in bit positions).
	bits := bloom9([]byte("test"))
	uniqueBits := make(map[uint]bool)
	for _, b := range bits {
		uniqueBits[b] = true
	}

	setBits := 0
	for _, b := range bloom {
		for bit := 0; bit < 8; bit++ {
			if b&(1<<uint(bit)) != 0 {
				setBits++
			}
		}
	}
	if setBits != len(uniqueBits) {
		t.Fatalf("expected %d set bits, got %d", len(uniqueBits), setBits)
	}
}

func TestBloomAddVerifyBitPositions(t *testing.T) {
	var bloom Bloom
	data := []byte("test")
	BloomAdd(&bloom, data)
	bits := bloom9(data)

	// Verify each computed bit is actually set in the bloom.
	for i, bit := range bits {
		byteIdx := BloomLength - 1 - bit/8
		bitIdx := bit % 8
		if bloom[byteIdx]&(1<<bitIdx) == 0 {
			t.Errorf("bit %d (position %d) should be set in bloom", i, bit)
		}
	}
}

func TestBloomContainsPositive(t *testing.T) {
	var bloom Bloom
	items := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		[]byte("ethereum"),
	}

	for _, item := range items {
		BloomAdd(&bloom, item)
	}

	for _, item := range items {
		if !BloomContains(bloom, item) {
			t.Errorf("bloom should contain %q", item)
		}
	}
}

func TestBloomContainsNegative(t *testing.T) {
	var bloom Bloom
	BloomAdd(&bloom, []byte("included"))

	// Test with an item that was not added. There's a small chance of a false
	// positive, but with a 2048-bit filter and only 3 bits set, it's very unlikely.
	if BloomContains(bloom, []byte("not-included-item-xyz-12345")) {
		t.Error("fixed negative input matched the Bloom filter")
	}
}

func TestBloomContainsEmptyBloom(t *testing.T) {
	var bloom Bloom
	if BloomContains(bloom, []byte("anything")) {
		t.Fatal("empty bloom should not contain anything")
	}
}

func TestLogsBloom(t *testing.T) {
	addr := HexToAddress("0xdead")
	topic1 := HexToHash("0xaabb")
	topic2 := HexToHash("0xccdd")

	logs := []*Log{
		{
			Address: addr,
			Topics:  []Hash{topic1, topic2},
			Data:    []byte{0x01, 0x02},
		},
	}

	bloom := LogsBloom(logs)

	// Bloom should contain the address and both topics.
	if !BloomContains(bloom, addr.Bytes()) {
		t.Error("bloom should contain log address")
	}
	if !BloomContains(bloom, topic1.Bytes()) {
		t.Error("bloom should contain topic1")
	}
	if !BloomContains(bloom, topic2.Bytes()) {
		t.Error("bloom should contain topic2")
	}

	// Data should NOT be in the bloom (only address and topics are added).
	// Check that some unrelated data is not found.
	unrelated := HexToAddress("0xbeef")
	if BloomContains(bloom, unrelated.Bytes()) {
		t.Error("fixed unrelated address matched the logs Bloom filter")
	}
}

func TestLogsBloomMultipleLogs(t *testing.T) {
	addr1 := HexToAddress("0x1111")
	addr2 := HexToAddress("0x2222")
	topic1 := HexToHash("0xaaaa")
	topic2 := HexToHash("0xbbbb")

	logs := []*Log{
		{Address: addr1, Topics: []Hash{topic1}},
		{Address: addr2, Topics: []Hash{topic2}},
	}

	bloom := LogsBloom(logs)

	if !BloomContains(bloom, addr1.Bytes()) {
		t.Error("bloom should contain addr1")
	}
	if !BloomContains(bloom, addr2.Bytes()) {
		t.Error("bloom should contain addr2")
	}
	if !BloomContains(bloom, topic1.Bytes()) {
		t.Error("bloom should contain topic1")
	}
	if !BloomContains(bloom, topic2.Bytes()) {
		t.Error("bloom should contain topic2")
	}
}

func TestLogsBloomEmpty(t *testing.T) {
	bloom := LogsBloom(nil)
	if bloom != (Bloom{}) {
		t.Fatal("bloom from nil logs should be zero")
	}

	bloom = LogsBloom([]*Log{})
	if bloom != (Bloom{}) {
		t.Fatal("bloom from empty logs should be zero")
	}
}

func TestCreateBloom(t *testing.T) {
	addr1 := HexToAddress("0x1111")
	addr2 := HexToAddress("0x2222")
	topic1 := HexToHash("0xaaaa")
	topic2 := HexToHash("0xbbbb")

	r1 := &Receipt{
		Bloom: LogsBloom([]*Log{{Address: addr1, Topics: []Hash{topic1}}}),
		Logs:  []*Log{{Address: addr1, Topics: []Hash{topic1}}},
	}
	r2 := &Receipt{
		Bloom: LogsBloom([]*Log{{Address: addr2, Topics: []Hash{topic2}}}),
		Logs:  []*Log{{Address: addr2, Topics: []Hash{topic2}}},
	}

	combined := CreateBloom([]*Receipt{r1, r2})

	// The combined bloom should contain all entries from both receipts.
	if !BloomContains(combined, addr1.Bytes()) {
		t.Error("combined bloom should contain addr1")
	}
	if !BloomContains(combined, addr2.Bytes()) {
		t.Error("combined bloom should contain addr2")
	}
	if !BloomContains(combined, topic1.Bytes()) {
		t.Error("combined bloom should contain topic1")
	}
	if !BloomContains(combined, topic2.Bytes()) {
		t.Error("combined bloom should contain topic2")
	}
}

func TestCreateBloomEmpty(t *testing.T) {
	bloom := CreateBloom(nil)
	if bloom != (Bloom{}) {
		t.Fatal("bloom from nil receipts should be zero")
	}

	bloom = CreateBloom([]*Receipt{})
	if bloom != (Bloom{}) {
		t.Fatal("bloom from empty receipts should be zero")
	}
}

func TestCreateBloomMergesCorrectly(t *testing.T) {
	// Verify that CreateBloom is the OR of individual receipt blooms.
	addr := HexToAddress("0xdead")
	topic := HexToHash("0xbeef")

	r1 := &Receipt{
		Bloom: LogsBloom([]*Log{{Address: addr}}),
	}
	r2 := &Receipt{
		Bloom: LogsBloom([]*Log{{Address: addr, Topics: []Hash{topic}}}),
	}

	combined := CreateBloom([]*Receipt{r1, r2})

	// Manually OR the two blooms.
	var expected Bloom
	for i := range expected {
		expected[i] = r1.Bloom[i] | r2.Bloom[i]
	}
	if combined != expected {
		t.Fatal("CreateBloom should be bitwise OR of receipt blooms")
	}
}
