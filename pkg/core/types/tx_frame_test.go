package types

import (
	"fmt"
	"math/big"
	"testing"
)

func TestFrameTxRoundTripDefaultMode(t *testing.T) {
	target := HexToAddress("0xdeadbeef")
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   42,
		Sender:  HexToAddress("0x1111111111111111111111111111111111111111"),
		Frames: []Frame{
			{Mode: ModeDefault, Target: &target, GasLimit: 100000, Data: []byte{0xca, 0xfe}},
		},
		MaxPriorityFeePerGas: big.NewInt(2_000_000_000),
		MaxFeePerGas:         big.NewInt(100_000_000_000),
		MaxFeePerBlobGas:     big.NewInt(0),
		BlobVersionedHashes:  nil,
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	if enc[0] != FrameTxType {
		t.Fatalf("expected type byte 0x%02x, got 0x%02x", FrameTxType, enc[0])
	}

	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.Type() != FrameTxType {
		t.Fatalf("decoded type: expected %d, got %d", FrameTxType, decoded.Type())
	}

	frameTx := decoded.inner.(*FrameTx)
	if frameTx.ChainID.Int64() != 1 {
		t.Fatalf("ChainID mismatch: got %d", frameTx.ChainID.Int64())
	}
	if frameTx.Nonce != 42 {
		t.Fatalf("Nonce mismatch: got %v", frameTx.Nonce)
	}
	if frameTx.Sender != inner.Sender {
		t.Fatalf("Sender mismatch: got %s", frameTx.Sender.Hex())
	}
	if len(frameTx.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frameTx.Frames))
	}
	f := frameTx.Frames[0]
	if f.Mode != ModeDefault {
		t.Fatalf("frame mode: expected %d, got %d", ModeDefault, f.Mode)
	}
	if f.Target == nil || *f.Target != target {
		t.Fatal("frame target mismatch")
	}
	if f.GasLimit != 100000 {
		t.Fatalf("frame gas limit: expected 100000, got %d", f.GasLimit)
	}
}

func TestFrameTxRoundTripVerifyMode(t *testing.T) {
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0x2222222222222222222222222222222222222222"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("signature-data")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}

	frameTx := decoded.inner.(*FrameTx)
	if len(frameTx.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frameTx.Frames))
	}
	if frameTx.Frames[0].Mode != ModeVerify {
		t.Fatalf("expected VERIFY mode, got %d", frameTx.Frames[0].Mode)
	}
	// Nil target should round-trip as nil.
	if frameTx.Frames[0].Target != nil {
		t.Fatal("expected nil target for VERIFY frame")
	}
}

func TestFrameTxRoundTripSenderMode(t *testing.T) {
	target := HexToAddress("0xerc20token")
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   5,
		Sender:  HexToAddress("0x3333333333333333333333333333333333333333"),
		Frames: []Frame{
			{Mode: ModeSender, Target: &target, GasLimit: 200000, Data: []byte{0x01, 0x02, 0x03}},
		},
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(50_000_000_000),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}

	frameTx := decoded.inner.(*FrameTx)
	if frameTx.Frames[0].Mode != ModeSender {
		t.Fatalf("expected SENDER mode, got %d", frameTx.Frames[0].Mode)
	}
}

func TestFrameTxRoundTripMultiFrame(t *testing.T) {
	sender := HexToAddress("0x4444444444444444444444444444444444444444")
	sponsor := HexToAddress("0x5555555555555555555555555555555555555555")
	erc20 := HexToAddress("0x6666666666666666666666666666666666666666")
	callTarget := HexToAddress("0x7777777777777777777777777777777777777777")

	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   10,
		Sender:  sender,
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: ModeVerify, Target: &sponsor, GasLimit: 30000, Data: []byte("sponsor-sig")},
			{Mode: ModeSender, Target: &erc20, GasLimit: 60000, Data: []byte("transfer")},
			{Mode: ModeSender, Target: &callTarget, GasLimit: 100000, Data: []byte("calldata")},
			{Mode: ModeDefault, Target: &sponsor, GasLimit: 40000, Data: []byte("postop")},
		},
		MaxPriorityFeePerGas: big.NewInt(2_000_000_000),
		MaxFeePerGas:         big.NewInt(100_000_000_000),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}

	frameTx := decoded.inner.(*FrameTx)
	if len(frameTx.Frames) != 5 {
		t.Fatalf("expected 5 frames, got %d", len(frameTx.Frames))
	}

	modes := []uint8{ModeVerify, ModeVerify, ModeSender, ModeSender, ModeDefault}
	for i, want := range modes {
		if frameTx.Frames[i].Mode != want {
			t.Fatalf("frame %d: expected mode %d, got %d", i, want, frameTx.Frames[i].Mode)
		}
	}
}

func TestFrameTxRoundTripWithBlobs(t *testing.T) {
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: ModeSender, Target: nil, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(1_000_000),
		BlobVersionedHashes:  []Hash{HexToHash("0x01"), HexToHash("0x02")},
	}
	tx := NewTransaction(inner)

	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}

	frameTx := decoded.inner.(*FrameTx)
	if frameTx.MaxFeePerBlobGas.Int64() != 1_000_000 {
		t.Fatalf("MaxFeePerBlobGas mismatch: got %d", frameTx.MaxFeePerBlobGas.Int64())
	}
	if len(frameTx.BlobVersionedHashes) != 2 {
		t.Fatalf("expected 2 blob hashes, got %d", len(frameTx.BlobVersionedHashes))
	}
}

func TestComputeFrameSigHashVerifyElision(t *testing.T) {
	sender := HexToAddress("0x1111111111111111111111111111111111111111")
	target := HexToAddress("0x2222222222222222222222222222222222222222")

	tx := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  sender,
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("signature-A")},
			{Mode: ModeSender, Target: &target, GasLimit: 100000, Data: []byte("calldata")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	h1 := ComputeFrameSigHash(tx)
	if h1.IsZero() {
		t.Fatal("sig hash should not be zero")
	}

	// Change the VERIFY frame data -- sig hash should remain the same.
	tx2 := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  sender,
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("completely-different-signature-B")},
			{Mode: ModeSender, Target: &target, GasLimit: 100000, Data: []byte("calldata")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	h2 := ComputeFrameSigHash(tx2)
	if h1 != h2 {
		t.Fatalf("sig hash should be same when only VERIFY data differs:\n  h1=%s\n  h2=%s", h1.Hex(), h2.Hex())
	}

	// Change the SENDER frame data -- sig hash should differ.
	tx3 := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  sender,
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("signature-A")},
			{Mode: ModeSender, Target: &target, GasLimit: 100000, Data: []byte("different-calldata")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	h3 := ComputeFrameSigHash(tx3)
	if h1 == h3 {
		t.Fatal("sig hash should differ when non-VERIFY frame data changes")
	}
}

func TestComputeFrameSigHashConsistency(t *testing.T) {
	sender := HexToAddress("0xaaaa")
	tx := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   5,
		Sender:  sender,
		Frames: []Frame{
			{Mode: ModeDefault, Target: nil, GasLimit: 50000, Data: []byte("data")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	h1 := ComputeFrameSigHash(tx)
	h2 := ComputeFrameSigHash(tx)
	if h1 != h2 {
		t.Fatal("sig hash should be deterministic")
	}
	if h1.IsZero() {
		t.Fatal("sig hash should not be zero")
	}
}

func TestValidateFrameTxValid(t *testing.T) {
	target := HexToAddress("0xbeef")
	tx := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	if err := ValidateFrameTx(tx); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidateFrameTxEmptyFrames(t *testing.T) {
	tx := &FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               HexToAddress("0xaaaa"),
		Frames:               nil,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	if err := ValidateFrameTx(tx); err == nil {
		t.Fatal("expected error for empty frames")
	}
}

func TestValidateFrameTxTooManyFrames(t *testing.T) {
	frames := make([]Frame, MaxFrames+1)
	for i := range frames {
		frames[i] = Frame{Mode: ModeDefault, GasLimit: 100}
	}
	tx := &FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               HexToAddress("0xaaaa"),
		Frames:               frames,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	if err := ValidateFrameTx(tx); err == nil {
		t.Fatal("expected error for too many frames")
	}
}

func TestValidateFrameTxInvalidMode(t *testing.T) {
	tx := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: 5, GasLimit: 100, Data: nil},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	if err := ValidateFrameTx(tx); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestValidateFrameTxBlobFeeWithoutBlobs(t *testing.T) {
	tx := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeDefault, GasLimit: 100},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(999), // non-zero without blobs
		BlobVersionedHashes:  nil,
	}
	if err := ValidateFrameTx(tx); err == nil {
		t.Fatal("expected error for non-zero blob fee without blobs")
	}
}

func TestCalcFrameTxGas(t *testing.T) {
	tx := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: ModeSender, Target: nil, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	gas := CalcFrameTxGas(tx)
	// Should be at least intrinsic + sum of frame gas limits.
	minGas := FrameTxIntrinsicCost + 50000 + 100000
	if gas < minGas {
		t.Fatalf("gas %d should be >= %d (intrinsic + frame limits)", gas, minGas)
	}
	// Calldata cost should add something.
	if gas == minGas {
		t.Fatal("expected calldata cost to add to gas")
	}
}

func TestFrameTxHashConsistency(t *testing.T) {
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	tx := NewTransaction(inner)

	h1 := tx.Hash()
	h2 := tx.Hash()
	if h1 != h2 {
		t.Fatal("Hash() should return consistent results")
	}
	if h1.IsZero() {
		t.Fatal("hash should not be zero")
	}

	// Reconstruct from RLP and verify hash matches.
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	decoded, err := DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.Hash() != h1 {
		t.Fatal("decoded transaction should produce the same hash")
	}
}

func TestFrameTxCopy(t *testing.T) {
	target := HexToAddress("0xbeef")
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   7,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: &target, GasLimit: 50000, Data: []byte("sig")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
		BlobVersionedHashes:  []Hash{HexToHash("0x01")},
	}
	cpy := inner.copy().(*FrameTx)

	// Mutate original, verify copy is independent.
	inner.Frames[0].Data = []byte("mutated")
	inner.ChainID.SetInt64(9999)

	if cpy.Nonce != 7 {
		t.Fatal("copy nonce should be independent")
	}
	if string(cpy.Frames[0].Data) != "sig" {
		t.Fatal("copy frame data should be independent")
	}
	if cpy.ChainID.Int64() != 1 {
		t.Fatal("copy chain ID should be independent")
	}
}

func TestFrameTxTxDataInterface(t *testing.T) {
	inner := &FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                5,
		Sender:               HexToAddress("0xaaaa"),
		Frames:               []Frame{{Mode: ModeDefault, GasLimit: 100}},
		MaxPriorityFeePerGas: big.NewInt(2),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	// Verify it implements TxData.
	var _ TxData = inner

	if inner.txType() != FrameTxType {
		t.Fatalf("txType: expected 0x%02x, got 0x%02x", FrameTxType, inner.txType())
	}
	if inner.chainID().Int64() != 1 {
		t.Fatal("chainID mismatch")
	}
	if inner.nonce() != 5 {
		t.Fatal("nonce mismatch")
	}
	if inner.gasTipCap().Int64() != 2 {
		t.Fatal("gasTipCap mismatch")
	}
	if inner.gasFeeCap().Int64() != 10 {
		t.Fatal("gasFeeCap mismatch")
	}
	if inner.value().Sign() != 0 {
		t.Fatal("value should be zero for frame tx")
	}
	if inner.to() != nil {
		t.Fatal("to() should be nil for frame tx")
	}
	if inner.accessList() != nil {
		t.Fatal("accessList should be nil for frame tx")
	}
}

func TestFrameTxNilNonce(t *testing.T) {
	// Nonce is now uint64, so zero is the default.
	tx := &FrameTx{Nonce: 0}

	if got := tx.nonce(); got != 0 {
		t.Fatalf("zero nonce should return 0: got %d", got)
	}
}

func TestFrameTxSigningHashMatchesSigHash(t *testing.T) {
	inner := &FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  HexToAddress("0xaaaa"),
		Frames: []Frame{
			{Mode: ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: ModeSender, Target: nil, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}
	tx := NewTransaction(inner)

	// The Transaction.SigningHash() should delegate to ComputeFrameSigHash.
	sigHash := tx.SigningHash()
	directHash := ComputeFrameSigHash(inner)
	if sigHash != directHash {
		t.Fatalf("SigningHash and ComputeFrameSigHash should match:\n  signing=%s\n  direct=%s",
			sigHash.Hex(), directHash.Hex())
	}
}

func TestTransactionFramesMethod(t *testing.T) {
	sender := HexToAddress("0x1234567890123456789012345678901234567890")
	target := HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	
	inner := &FrameTx{
		ChainID: big.NewInt(3151908),
		Nonce:   1,
		Sender:  sender,
		Frames: []Frame{
			{Mode: ModeVerify, GasLimit: 50000, Data: []byte{0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xaa}},
			{Mode: ModeSender, Target: &target, GasLimit: 21000, Data: []byte{}},
		},
	}
	
	tx := NewTransaction(inner)
	
	// Test tx.Type()
	if tx.Type() != FrameTxType {
		t.Fatalf("expected type 0x%02x, got 0x%02x", FrameTxType, tx.Type())
	}
	
	// Test tx.Frames()
	frames := tx.Frames()
	fmt.Printf("DEBUG Test: frames count = %d\n", len(frames))
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	
	// Test tx.FrameSender()
	fs := tx.FrameSender()
	fmt.Printf("DEBUG Test: frameSender = %s\n", fs.Hex())
	if fs != sender {
		t.Fatalf("expected sender %s, got %s", sender.Hex(), fs.Hex())
	}
}
