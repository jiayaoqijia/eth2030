package types

import (
	"bytes"
	"testing"
)

func TestBloomByteLength(t *testing.T) {
	if BloomByteLength != 256 {
		t.Fatalf("BloomByteLength = %d, want 256", BloomByteLength)
	}
	if bloomBitLength != 2048 {
		t.Fatalf("bloomBitLength = %d, want 2048", bloomBitLength)
	}
}

func TestBytesToBloomExact(t *testing.T) {
	data := make([]byte, 256)
	data[0] = 0xAA
	data[255] = 0xBB
	bloom := BytesToBloom(data)
	if bloom[0] != 0xAA {
		t.Errorf("first byte: got 0x%02x, want 0xAA", bloom[0])
	}
	if bloom[255] != 0xBB {
		t.Errorf("last byte: got 0x%02x, want 0xBB", bloom[255])
	}
}

func TestBytesToBloomShort(t *testing.T) {
	// Shorter input should be right-aligned (left-padded with zeros).
	data := []byte{0xFF, 0xEE}
	bloom := BytesToBloom(data)
	// The two bytes should appear at positions 254 and 255.
	if bloom[254] != 0xFF || bloom[255] != 0xEE {
		t.Errorf("short bloom: got %x at end, want ffee", bloom[254:256])
	}
	// Leading bytes should be zero.
	for i := 0; i < 254; i++ {
		if bloom[i] != 0 {
			t.Errorf("byte %d should be zero, got 0x%02x", i, bloom[i])
		}
	}
}

func TestBytesToBloomLong(t *testing.T) {
	// Longer input should be left-truncated to 256 bytes.
	data := make([]byte, 300)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bloom := BytesToBloom(data)
	// The last 256 bytes of data should be the bloom content.
	expected := data[44:] // 300 - 256 = 44
	if !bytes.Equal(bloom[:], expected) {
		t.Error("long bloom does not match expected truncation")
	}
}

func TestBytesToBloomEmpty(t *testing.T) {
	bloom := BytesToBloom(nil)
	if bloom != (Bloom{}) {
		t.Error("BytesToBloom(nil) should be zero bloom")
	}
	bloom = BytesToBloom([]byte{})
	if bloom != (Bloom{}) {
		t.Error("BytesToBloom(empty) should be zero bloom")
	}
}

func TestBloomBytes(t *testing.T) {
	var bloom Bloom
	bloom[0] = 0x12
	bloom[255] = 0x34
	b := bloom.Bytes()
	if len(b) != 256 {
		t.Fatalf("Bytes() length = %d, want 256", len(b))
	}
	if b[0] != 0x12 || b[255] != 0x34 {
		t.Error("Bytes() returned wrong data")
	}
	// Verify it's a copy.
	b[0] = 0xFF
	if bloom[0] == 0xFF {
		t.Error("Bytes() should return a copy, not a reference")
	}
}

func TestBloomSetBytes(t *testing.T) {
	var bloom Bloom
	bloom.SetBytes([]byte{0xAB, 0xCD})
	if bloom[254] != 0xAB || bloom[255] != 0xCD {
		t.Errorf("SetBytes: got %x, want abcd at end", bloom[254:])
	}
	// Set again with different data to verify reset.
	bloom.SetBytes([]byte{0x01})
	if bloom[254] != 0x00 || bloom[255] != 0x01 {
		t.Error("SetBytes should reset previous data")
	}
}

func TestBloomAddMethod(t *testing.T) {
	var bloom Bloom
	bloom.Add([]byte("hello"))
	// Verify it matches the free function.
	var expected Bloom
	BloomAdd(&expected, []byte("hello"))
	if bloom != expected {
		t.Error("Bloom.Add should match BloomAdd")
	}
}

func TestBloomTestMethod(t *testing.T) {
	var bloom Bloom
	bloom.Add([]byte("ethereum"))
	if !bloom.Test([]byte("ethereum")) {
		t.Error("Test should return true for added data")
	}
	// Empty bloom should not test positive.
	var empty Bloom
	if empty.Test([]byte("anything")) {
		t.Error("empty bloom should not test positive")
	}
}

func TestBloomTestNonMembership(t *testing.T) {
	var bloom Bloom
	bloom.Add([]byte("included"))
	// This specific string should not be a false positive with high probability.
	if bloom.Test([]byte("definitely-not-added-xyz-98765")) {
		t.Error("fixed negative input matched the Bloom filter")
	}
}

func TestBloomOr(t *testing.T) {
	var a, b Bloom
	a.Add([]byte("alpha"))
	b.Add([]byte("beta"))

	// Before OR, a should not contain beta.
	if a.Test([]byte("beta")) {
		t.Log("possible false positive before OR")
	}

	a.Or(b)

	// After OR, a should contain both.
	if !a.Test([]byte("alpha")) {
		t.Error("after OR, bloom should contain alpha")
	}
	if !a.Test([]byte("beta")) {
		t.Error("after OR, bloom should contain beta")
	}
}

func TestBloomOrIdentity(t *testing.T) {
	var bloom Bloom
	bloom.Add([]byte("data"))

	original := bloom
	var empty Bloom
	bloom.Or(empty)

	if bloom != original {
		t.Error("OR with empty bloom should not change the bloom")
	}
}

func TestBloomBitsFunction(t *testing.T) {
	result := bloomBits([]byte("test"))
	// Verify the results match bloom9.
	bits9 := bloom9([]byte("test"))

	for i := 0; i < 3; i++ {
		expectedByteIdx := BloomByteLength - 1 - bits9[i]/8
		expectedBitIdx := bits9[i] % 8
		if result[i][0] != expectedByteIdx {
			t.Errorf("bloomBits[%d] byte index: got %d, want %d", i, result[i][0], expectedByteIdx)
		}
		if result[i][1] != expectedBitIdx {
			t.Errorf("bloomBits[%d] bit index: got %d, want %d", i, result[i][1], expectedBitIdx)
		}
	}
}

func TestBloomBitsConsistency(t *testing.T) {
	// Verify that setting bits via bloomBits produces the same bloom as Add.
	data := []byte("consistency-check")
	var bloom1 Bloom
	bloom1.Add(data)

	var bloom2 Bloom
	bits := bloomBits(data)
	for _, pair := range bits {
		bloom2[pair[0]] |= 1 << pair[1]
	}
	if bloom1 != bloom2 {
		t.Error("bloomBits should produce same result as Add")
	}
}

func TestCreateBloomFromLogsWithMethods(t *testing.T) {
	addr := HexToAddress("0xcafe")
	topic := HexToHash("0xfeed")

	logs := []*Log{
		{Address: addr, Topics: []Hash{topic}},
	}

	bloom := LogsBloom(logs)

	if !bloom.Test(addr.Bytes()) {
		t.Error("bloom should contain log address")
	}
	if !bloom.Test(topic.Bytes()) {
		t.Error("bloom should contain topic")
	}
}

func TestBloomMultipleAdds(t *testing.T) {
	var bloom Bloom
	items := [][]byte{
		[]byte("one"),
		[]byte("two"),
		[]byte("three"),
		[]byte("four"),
		[]byte("five"),
	}
	for _, item := range items {
		bloom.Add(item)
	}
	for _, item := range items {
		if !bloom.Test(item) {
			t.Errorf("bloom should contain %q after adding it", item)
		}
	}
}

func TestBloomOrMultiple(t *testing.T) {
	var b1, b2, b3 Bloom
	b1.Add([]byte("x"))
	b2.Add([]byte("y"))
	b3.Add([]byte("z"))

	var combined Bloom
	combined.Or(b1)
	combined.Or(b2)
	combined.Or(b3)

	if !combined.Test([]byte("x")) || !combined.Test([]byte("y")) || !combined.Test([]byte("z")) {
		t.Error("combined bloom should contain all items after OR")
	}
}
