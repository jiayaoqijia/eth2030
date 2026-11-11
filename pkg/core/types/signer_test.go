package types

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"testing"

	"golang.org/x/crypto/sha3"
)

// testKeyToAddress derives the Ethereum address from a key.
func testKeyToAddress(key *ecdsa.PrivateKey) Address {
	pubBytes := testMarshalPub(&key.PublicKey)
	d := sha3.NewLegacyKeccak256()
	d.Write(pubBytes[1:]) // skip 0x04 prefix
	hash := d.Sum(nil)
	return BytesToAddress(hash[12:])
}

// testMarshalPub marshals public key to 65-byte uncompressed format.
func testMarshalPub(pub *ecdsa.PublicKey) []byte {
	ret := make([]byte, 65)
	ret[0] = 0x04
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	copy(ret[1+32-len(xBytes):33], xBytes)
	copy(ret[33+32-len(yBytes):65], yBytes)
	return ret
}

// testSign signs a hash with the private key and returns [R||S||V] (65 bytes).
func testSign(t *testing.T, hash []byte, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	r, s, err := ecdsa.Sign(rand.Reader, key, hash)
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}

	// Normalize s to lower half of curve order.
	halfN := new(big.Int).Div(secp256k1NCopy, big.NewInt(2))
	if s.Cmp(halfN) > 0 {
		s = new(big.Int).Sub(secp256k1NCopy, s)
	}

	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)

	// Determine V by trial recovery.
	expectedPub := testMarshalPub(&key.PublicKey)
	for v := byte(0); v <= 1; v++ {
		sig[64] = v
		recovered, err := recoverPubkey(hash, r, s, v)
		if err != nil {
			continue
		}
		if len(recovered) == len(expectedPub) {
			match := true
			for i := range recovered {
				if recovered[i] != expectedPub[i] {
					match = false
					break
				}
			}
			if match {
				return sig
			}
		}
	}
	sig[64] = 0
	return sig
}

// testGenSecp256k1Key generates a secp256k1 key for tests.
func testGenSecp256k1Key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	curve := testSecp256k1Curve()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

// testSecp256k1Curve returns a secp256k1 elliptic.Curve for test key generation.
func testSecp256k1Curve() elliptic.Curve {
	return &testSecp256k1{
		params: &elliptic.CurveParams{
			P:       gethcrypto.S256().Params().P,
			N:       secp256k1NCopy,
			B:       gethcrypto.S256().Params().B,
			Gx:      gethcrypto.S256().Params().Gx,
			Gy:      gethcrypto.S256().Params().Gy,
			BitSize: 256,
			Name:    "secp256k1",
		},
	}
}

// testSecp256k1 implements elliptic.Curve for secp256k1 in tests.
type testSecp256k1 struct {
	params *elliptic.CurveParams
}

func (c *testSecp256k1) Params() *elliptic.CurveParams { return c.params }

func (c *testSecp256k1) IsOnCurve(x, y *big.Int) bool {
	if x == nil || y == nil || x.Sign() < 0 || y.Sign() < 0 {
		return false
	}
	if x.Cmp(c.params.P) >= 0 || y.Cmp(c.params.P) >= 0 {
		return false
	}
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, c.params.P)
	x3 := new(big.Int).Mul(x, x)
	x3.Mod(x3, c.params.P)
	x3.Mul(x3, x)
	x3.Mod(x3, c.params.P)
	x3.Add(x3, c.params.B)
	x3.Mod(x3, c.params.P)
	return y2.Cmp(x3) == 0
}

func (c *testSecp256k1) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	if x1.Sign() == 0 && y1.Sign() == 0 {
		return new(big.Int).Set(x2), new(big.Int).Set(y2)
	}
	if x2.Sign() == 0 && y2.Sign() == 0 {
		return new(big.Int).Set(x1), new(big.Int).Set(y1)
	}
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		return c.Double(x1, y1)
	}
	if x1.Cmp(x2) == 0 {
		return new(big.Int), new(big.Int)
	}
	p := c.params.P
	dy := new(big.Int).Sub(y2, y1)
	dy.Mod(dy, p)
	dx := new(big.Int).Sub(x2, x1)
	dx.Mod(dx, p)
	dxInv := new(big.Int).ModInverse(dx, p)
	if dxInv == nil {
		return new(big.Int), new(big.Int)
	}
	slope := new(big.Int).Mul(dy, dxInv)
	slope.Mod(slope, p)
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, p)
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)
	return x3, y3
}

func (c *testSecp256k1) Double(x1, y1 *big.Int) (*big.Int, *big.Int) {
	if y1.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}
	p := c.params.P
	x1sq := new(big.Int).Mul(x1, x1)
	x1sq.Mod(x1sq, p)
	num := new(big.Int).Mul(big.NewInt(3), x1sq)
	num.Mod(num, p)
	den := new(big.Int).Mul(big.NewInt(2), y1)
	den.Mod(den, p)
	denInv := new(big.Int).ModInverse(den, p)
	if denInv == nil {
		return new(big.Int), new(big.Int)
	}
	slope := new(big.Int).Mul(num, denInv)
	slope.Mod(slope, p)
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, new(big.Int).Mul(big.NewInt(2), x1))
	x3.Mod(x3, p)
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)
	return x3, y3
}

func (c *testSecp256k1) ScalarMult(bx, by *big.Int, k []byte) (*big.Int, *big.Int) {
	scalar := new(big.Int).SetBytes(k)
	scalar.Mod(scalar, c.params.N)
	if scalar.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}
	rx, ry := new(big.Int), new(big.Int)
	px, py := new(big.Int).Set(bx), new(big.Int).Set(by)
	for i := scalar.BitLen() - 1; i >= 0; i-- {
		rx, ry = c.Double(rx, ry)
		if scalar.Bit(i) == 1 {
			rx, ry = c.Add(rx, ry, px, py)
		}
	}
	return rx, ry
}

func (c *testSecp256k1) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	return c.ScalarMult(c.params.Gx, c.params.Gy, k)
}

// --- Actual tests ---

func TestEIP155SignerChainID(t *testing.T) {
	s := NewEIP155Signer(1)
	if s.ChainID() != 1 {
		t.Errorf("ChainID() = %d, want 1", s.ChainID())
	}
	s2 := NewEIP155Signer(1337)
	if s2.ChainID() != 1337 {
		t.Errorf("ChainID() = %d, want 1337", s2.ChainID())
	}
}

func TestLondonSignerChainID(t *testing.T) {
	s := NewLondonSigner(42)
	if s.ChainID() != 42 {
		t.Errorf("ChainID() = %d, want 42", s.ChainID())
	}
}

func TestLatestSignerReturnsLondon(t *testing.T) {
	s := LatestSigner(1)
	_, ok := s.(LondonSigner)
	if !ok {
		t.Error("LatestSigner should return LondonSigner")
	}
	if s.ChainID() != 1 {
		t.Errorf("ChainID() = %d, want 1", s.ChainID())
	}
}

func TestMakeSignerLegacy(t *testing.T) {
	s := MakeSigner(1, LegacyTxType)
	_, ok := s.(EIP155Signer)
	if !ok {
		t.Error("MakeSigner for legacy should return EIP155Signer")
	}
}

func TestMakeSignerDynamic(t *testing.T) {
	s := MakeSigner(1, DynamicFeeTxType)
	_, ok := s.(LondonSigner)
	if !ok {
		t.Error("MakeSigner for DynamicFee should return LondonSigner")
	}
}

func TestSignatureValuesValid(t *testing.T) {
	s := NewLondonSigner(1)
	sig := make([]byte, 65)
	sig[0] = 0x01
	sig[32] = 0x02
	sig[64] = 0

	r, sv, v, err := s.SignatureValues(sig)
	if err != nil {
		t.Fatalf("SignatureValues error: %v", err)
	}
	if r.Sign() <= 0 || sv.Sign() <= 0 {
		t.Error("r and s should be positive")
	}
	if v != 0 {
		t.Errorf("v = %d, want 0", v)
	}
}

func TestSignatureValuesInvalidLength(t *testing.T) {
	s := NewLondonSigner(1)
	_, _, _, err := s.SignatureValues(make([]byte, 64))
	if err == nil {
		t.Error("expected error for 64-byte sig")
	}
	_, _, _, err = s.SignatureValues(make([]byte, 66))
	if err == nil {
		t.Error("expected error for 66-byte sig")
	}
}

func TestSignatureValuesInvalidV(t *testing.T) {
	s := NewEIP155Signer(1)
	sig := make([]byte, 65)
	sig[0] = 0x01
	sig[32] = 0x02
	sig[64] = 2
	_, _, _, err := s.SignatureValues(sig)
	if err == nil {
		t.Error("expected error for v > 1")
	}
}

func TestSignatureValuesZeroR(t *testing.T) {
	s := NewLondonSigner(1)
	sig := make([]byte, 65)
	sig[32] = 0x01
	sig[64] = 0
	_, _, _, err := s.SignatureValues(sig)
	if err == nil {
		t.Error("expected error for r = 0")
	}
}

func TestEIP155SignerHash(t *testing.T) {
	s := NewEIP155Signer(1)
	to := HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	tx := NewTransaction(&LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
		V:        big.NewInt(37),
		R:        new(big.Int),
		S:        new(big.Int),
	})
	h := s.Hash(tx)
	if h.IsZero() {
		t.Error("signing hash should not be zero")
	}
	h2 := s.Hash(tx)
	if h != h2 {
		t.Error("signing hash should be deterministic")
	}
}

func TestLondonSignerHashDynamicFee(t *testing.T) {
	s := NewLondonSigner(1)
	to := HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	tx := NewTransaction(&DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     5,
		GasTipCap: big.NewInt(2000000000),
		GasFeeCap: big.NewInt(30000000000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1000000),
	})
	h := s.Hash(tx)
	if h.IsZero() {
		t.Error("London signing hash should not be zero")
	}

	legacyTx := NewTransaction(&LegacyTx{
		Nonce:    5,
		GasPrice: big.NewInt(30000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000000),
		V:        big.NewInt(37),
		R:        new(big.Int),
		S:        new(big.Int),
	})
	legacyHash := s.Hash(legacyTx)
	if h == legacyHash {
		t.Error("dynamic fee tx hash should differ from legacy tx hash")
	}
}

func TestEIP155SignerHashNotSupportedType(t *testing.T) {
	s := NewEIP155Signer(1)
	to := HexToAddress("0xdead")
	tx := NewTransaction(&DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	h := s.Hash(tx)
	if !h.IsZero() {
		t.Error("EIP155Signer hash for DynamicFeeTx should be zero")
	}
}

func TestLondonSignerSenderLegacy(t *testing.T) {
	key := testGenSecp256k1Key(t)
	expectedAddr := testKeyToAddress(key)

	chainID := uint64(1)
	to := HexToAddress("0xdead")

	inner := &LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
		V:        big.NewInt(37),
		R:        new(big.Int),
		S:        new(big.Int),
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	recoveryID := sig[64]

	v := new(big.Int).Add(
		new(big.Int).Add(
			new(big.Int).Mul(big.NewInt(int64(chainID)), big.NewInt(2)),
			big.NewInt(35),
		),
		new(big.Int).SetUint64(uint64(recoveryID)),
	)
	inner.V = v
	inner.R = r
	inner.S = s
	signedTx := NewTransaction(inner)

	signer := NewLondonSigner(chainID)
	recovered, err := signer.Sender(signedTx)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered.Hex(), expectedAddr.Hex())
	}
}

func TestLondonSignerSenderDynamicFee(t *testing.T) {
	key := testGenSecp256k1Key(t)
	expectedAddr := testKeyToAddress(key)

	chainID := uint64(1337)
	to := HexToAddress("0xbeef")

	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(int64(chainID)),
		Nonce:     42,
		GasTipCap: big.NewInt(2000000000),
		GasFeeCap: big.NewInt(30000000000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1000000),
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	inner.R = new(big.Int).SetBytes(sig[0:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	inner.V = new(big.Int).SetUint64(uint64(sig[64]))
	signedTx := NewTransaction(inner)

	signer := NewLondonSigner(chainID)
	recovered, err := signer.Sender(signedTx)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered.Hex(), expectedAddr.Hex())
	}
}

func TestEIP155SignerSender(t *testing.T) {
	key := testGenSecp256k1Key(t)
	expectedAddr := testKeyToAddress(key)

	chainID := uint64(1)
	to := HexToAddress("0xdead")

	inner := &LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(100),
		V:        big.NewInt(37),
		R:        new(big.Int),
		S:        new(big.Int),
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	recoveryID := sig[64]

	v := new(big.Int).Add(
		new(big.Int).Add(
			new(big.Int).Mul(big.NewInt(int64(chainID)), big.NewInt(2)),
			big.NewInt(35),
		),
		new(big.Int).SetUint64(uint64(recoveryID)),
	)
	inner.V = v
	inner.R = r
	inner.S = s
	signedTx := NewTransaction(inner)

	signer := NewEIP155Signer(chainID)
	recovered, err := signer.Sender(signedTx)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered.Hex(), expectedAddr.Hex())
	}
}

func TestLondonSignerSenderAccessList(t *testing.T) {
	key := testGenSecp256k1Key(t)
	expectedAddr := testKeyToAddress(key)

	chainID := uint64(1)
	to := HexToAddress("0xaaaa")

	inner := &AccessListTx{
		ChainID:  big.NewInt(int64(chainID)),
		Nonce:    10,
		GasPrice: big.NewInt(1000000000),
		Gas:      25000,
		To:       &to,
		Value:    big.NewInt(500),
		AccessList: AccessList{
			{Address: to, StorageKeys: []Hash{{0x01}}},
		},
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	inner.R = new(big.Int).SetBytes(sig[0:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	inner.V = new(big.Int).SetUint64(uint64(sig[64]))
	signedTx := NewTransaction(inner)

	signer := NewLondonSigner(chainID)
	recovered, err := signer.Sender(signedTx)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered.Hex(), expectedAddr.Hex())
	}
}

func TestLondonSignerWrongChainID(t *testing.T) {
	key := testGenSecp256k1Key(t)
	to := HexToAddress("0xdead")

	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	inner.R = new(big.Int).SetBytes(sig[0:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	inner.V = new(big.Int).SetUint64(uint64(sig[64]))
	signedTx := NewTransaction(inner)

	signer := NewLondonSigner(42)
	_, err := signer.Sender(signedTx)
	if err == nil {
		t.Error("expected chain ID mismatch error")
	}
}

func TestEIP155SenderNotSupportedType(t *testing.T) {
	to := HexToAddress("0xdead")
	tx := NewTransaction(&DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})

	signer := NewEIP155Signer(1)
	_, err := signer.Sender(tx)
	if err == nil {
		t.Error("EIP155Signer should not support DynamicFeeTx")
	}
}

func TestRecoverPlainInvalidV(t *testing.T) {
	h := HexToHash("0xabcd")
	r := big.NewInt(1)
	s := big.NewInt(2)
	_, err := RecoverPlain(h, r, s, 2)
	if err == nil {
		t.Error("expected error for v > 1")
	}
}

func TestRecoverPlainZeroRS(t *testing.T) {
	h := HexToHash("0xabcd")
	_, err := RecoverPlain(h, big.NewInt(0), big.NewInt(1), 0)
	if err == nil {
		t.Error("expected error for r = 0")
	}
	_, err = RecoverPlain(h, big.NewInt(1), big.NewInt(0), 0)
	if err == nil {
		t.Error("expected error for s = 0")
	}
}

func TestLondonSignerSenderBlobTx(t *testing.T) {
	key := testGenSecp256k1Key(t)
	expectedAddr := testKeyToAddress(key)
	chainID := uint64(1)
	to := HexToAddress("0xbeef")

	inner := &BlobTx{
		ChainID:    big.NewInt(int64(chainID)),
		Nonce:      0,
		GasTipCap:  big.NewInt(2000000000),
		GasFeeCap:  big.NewInt(30000000000),
		Gas:        21000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1000),
		BlobHashes: []Hash{{0x01}},
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	inner.R = new(big.Int).SetBytes(sig[0:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	inner.V = new(big.Int).SetUint64(uint64(sig[64]))
	signedTx := NewTransaction(inner)

	signer := NewLondonSigner(chainID)
	recovered, err := signer.Sender(signedTx)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered.Hex(), expectedAddr.Hex())
	}
}

func TestLondonSignerSenderSetCodeTx(t *testing.T) {
	key := testGenSecp256k1Key(t)
	expectedAddr := testKeyToAddress(key)
	chainID := uint64(1)
	to := HexToAddress("0xbeef")

	inner := &SetCodeTx{
		ChainID:   big.NewInt(int64(chainID)),
		Nonce:     0,
		GasTipCap: big.NewInt(2000000000),
		GasFeeCap: big.NewInt(30000000000),
		Gas:       50000,
		To:        to,
		Value:     big.NewInt(0),
		AuthorizationList: []Authorization{
			{ChainID: big.NewInt(1), Address: to, Nonce: 0},
		},
	}
	tx := NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig := testSign(t, sigHash[:], key)
	inner.R = new(big.Int).SetBytes(sig[0:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	inner.V = new(big.Int).SetUint64(uint64(sig[64]))
	signedTx := NewTransaction(inner)

	signer := NewLondonSigner(chainID)
	recovered, err := signer.Sender(signedTx)
	if err != nil {
		t.Fatalf("Sender error: %v", err)
	}
	if recovered != expectedAddr {
		t.Errorf("recovered %s, want %s", recovered.Hex(), expectedAddr.Hex())
	}
}
