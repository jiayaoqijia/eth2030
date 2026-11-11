package types

import (
	"bytes"
	"math/big"
	"testing"
)

func TestSSZRoundtripLegacyTx(t *testing.T) {
	to := BytesToAddress([]byte{0xde, 0xad, 0xbe, 0xef})
	tx := NewTransaction(&LegacyTx{
		Nonce:    42,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1e18),
		Data:     []byte{0x01, 0x02, 0x03},
		V:        big.NewInt(28),
		R:        big.NewInt(12345),
		S:        big.NewInt(67890),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertSSZTxEqual(t, tx, decoded)
}

func TestSSZRoundtripLegacyTxContractCreation(t *testing.T) {
	tx := NewTransaction(&LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(20000000000),
		Gas:      100000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     []byte{0x60, 0x80, 0x60, 0x40},
		V:        big.NewInt(27),
		R:        big.NewInt(111111),
		S:        big.NewInt(222222),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.To() != nil {
		t.Error("expected nil To for contract creation")
	}
	assertSSZTxEqual(t, tx, decoded)
}

func TestSSZRoundtripAccessListTx(t *testing.T) {
	to := BytesToAddress([]byte{0xca, 0xfe})
	tx := NewTransaction(&AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    10,
		GasPrice: big.NewInt(5000000000),
		Gas:      50000,
		To:       &to,
		Value:    big.NewInt(100),
		Data:     []byte{0xab, 0xcd},
		AccessList: AccessList{
			{
				Address:     BytesToAddress([]byte{0x01}),
				StorageKeys: []Hash{BytesToHash([]byte{0xff})},
			},
		},
		V: big.NewInt(1),
		R: big.NewInt(99999),
		S: big.NewInt(88888),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}
	// Typed tx should start with type byte.
	if encoded[8] != 5 || encoded[9] != AccessListTxType {
		t.Fatalf("unexpected transaction selector/type: %d/%d", encoded[8], encoded[9])
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertSSZTxEqual(t, tx, decoded)
}

func TestSSZRoundtripDynamicFeeTx(t *testing.T) {
	to := BytesToAddress([]byte{0x11, 0x22})
	tx := NewTransaction(&DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     5,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(2000000000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(500),
		Data:      nil,
		AccessList: AccessList{
			{
				Address:     BytesToAddress([]byte{0xaa}),
				StorageKeys: []Hash{BytesToHash([]byte{0xbb}), BytesToHash([]byte{0xcc})},
			},
		},
		V: big.NewInt(0),
		R: big.NewInt(77777),
		S: big.NewInt(66666),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[8] != 7 || encoded[9] != DynamicFeeTxType {
		t.Fatalf("unexpected transaction selector/type: %d/%d", encoded[8], encoded[9])
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertSSZTxEqual(t, tx, decoded)
}

func TestSSZRoundtripBlobTx(t *testing.T) {
	to := BytesToAddress([]byte{0x33, 0x44})
	tx := NewTransaction(&BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      100,
		GasTipCap:  big.NewInt(1000),
		GasFeeCap:  big.NewInt(2000),
		Gas:        30000,
		To:         to,
		Value:      big.NewInt(0),
		Data:       []byte{0xaa},
		AccessList: nil,
		BlobFeeCap: big.NewInt(3000),
		BlobHashes: []Hash{
			BytesToHash([]byte{0x01, 0x02, 0x03}),
			BytesToHash([]byte{0x04, 0x05, 0x06}),
		},
		V: big.NewInt(1),
		R: big.NewInt(55555),
		S: big.NewInt(44444),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[8] != 9 || encoded[9] != BlobTxType {
		t.Fatalf("unexpected transaction selector/type: %d/%d", encoded[8], encoded[9])
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertSSZTxEqual(t, tx, decoded)

	// Verify blob hashes roundtrip.
	if len(decoded.BlobHashes()) != 2 {
		t.Fatalf("expected 2 blob hashes, got %d", len(decoded.BlobHashes()))
	}
	if decoded.BlobHashes()[0] != tx.BlobHashes()[0] {
		t.Error("blob hash 0 mismatch")
	}
	if decoded.BlobHashes()[1] != tx.BlobHashes()[1] {
		t.Error("blob hash 1 mismatch")
	}
}

func TestSSZRoundtripSetCodeTx(t *testing.T) {
	to := BytesToAddress([]byte{0x55, 0x66})
	tx := NewTransaction(&SetCodeTx{
		ChainID:   big.NewInt(1),
		Nonce:     7,
		GasTipCap: big.NewInt(500),
		GasFeeCap: big.NewInt(1500),
		Gas:       25000,
		To:        to,
		Value:     big.NewInt(0),
		Data:      []byte{0xef, 0x00},
		AccessList: AccessList{
			{
				Address:     BytesToAddress([]byte{0xdd}),
				StorageKeys: nil,
			},
		},
		AuthorizationList: []Authorization{
			{
				ChainID: big.NewInt(1),
				Address: BytesToAddress([]byte{0xaa, 0xbb}),
				Nonce:   3,
				V:       big.NewInt(1),
				R:       big.NewInt(11111),
				S:       big.NewInt(22222),
			},
		},
		V: big.NewInt(0),
		R: big.NewInt(33333),
		S: big.NewInt(44444),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[8] != 10 || encoded[9] != SetCodeTxType {
		t.Fatalf("unexpected transaction selector/type: %d/%d", encoded[8], encoded[9])
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	assertSSZTxEqual(t, tx, decoded)

	// Verify authorization list.
	authList := decoded.AuthorizationList()
	if len(authList) != 1 {
		t.Fatalf("expected 1 auth entry, got %d", len(authList))
	}
	if authList[0].Nonce != 3 {
		t.Errorf("auth nonce: got %d, want 3", authList[0].Nonce)
	}
	if authList[0].ChainID.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("auth chainID: got %v, want 1", authList[0].ChainID)
	}
}

func TestSSZReceiptRoundtrip(t *testing.T) {
	receipt := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Bloom:             Bloom{},
		Logs: []*Log{
			{
				Address: BytesToAddress([]byte{0x01}),
				Topics:  []Hash{BytesToHash([]byte{0xaa}), BytesToHash([]byte{0xbb})},
				Data:    []byte{0x01, 0x02, 0x03},
			},
			{
				Address: BytesToAddress([]byte{0x02}),
				Topics:  nil,
				Data:    []byte{0xff},
			},
		},
	}

	encoded, err := ReceiptToSSZ(receipt)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := SSZToReceipt(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Status != receipt.Status {
		t.Errorf("status: got %d, want %d", decoded.Status, receipt.Status)
	}
	if decoded.CumulativeGasUsed != receipt.CumulativeGasUsed {
		t.Errorf("cumulativeGasUsed: got %d, want %d", decoded.CumulativeGasUsed, receipt.CumulativeGasUsed)
	}
	if len(decoded.Logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(decoded.Logs))
	}
	if decoded.Logs[0].Address != receipt.Logs[0].Address {
		t.Error("log[0] address mismatch")
	}
	if len(decoded.Logs[0].Topics) != 2 {
		t.Errorf("log[0] topics: got %d, want 2", len(decoded.Logs[0].Topics))
	}
	if !bytes.Equal(decoded.Logs[0].Data, receipt.Logs[0].Data) {
		t.Error("log[0] data mismatch")
	}
	if !bytes.Equal(decoded.Logs[1].Data, receipt.Logs[1].Data) {
		t.Error("log[1] data mismatch")
	}
}

func TestSSZHashTreeRootConsistency(t *testing.T) {
	to := BytesToAddress([]byte{0x01})
	tx := NewTransaction(&LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     []byte{0x42},
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(2),
	})

	root1, err := TransactionSSZRoot(tx)
	if err != nil {
		t.Fatal(err)
	}
	root2, err := TransactionSSZRoot(tx)
	if err != nil {
		t.Fatal(err)
	}

	if root1 != root2 {
		t.Error("hash tree root is not deterministic")
	}
	if root1.IsZero() {
		t.Error("hash tree root should not be zero")
	}
}

func TestSSZTransactionsRoot(t *testing.T) {
	to := BytesToAddress([]byte{0x01})
	tx1 := NewTransaction(&LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(100), Gas: 21000,
		To: &to, Value: big.NewInt(0), Data: nil,
		V: big.NewInt(27), R: big.NewInt(1), S: big.NewInt(2),
	})
	tx2 := NewTransaction(&DynamicFeeTx{
		ChainID: big.NewInt(1), Nonce: 2,
		GasTipCap: big.NewInt(10), GasFeeCap: big.NewInt(20),
		Gas: 21000, To: &to, Value: big.NewInt(0), Data: nil,
		V: big.NewInt(0), R: big.NewInt(3), S: big.NewInt(4),
	})

	root, err := TransactionsSSZRoot([]*Transaction{tx1, tx2})
	if err != nil {
		t.Fatal(err)
	}
	if root.IsZero() {
		t.Error("transactions root should not be zero")
	}

	// Different order should produce different root.
	root2, err := TransactionsSSZRoot([]*Transaction{tx2, tx1})
	if err != nil {
		t.Fatal(err)
	}
	if root == root2 {
		t.Error("different order should produce different root")
	}
}

func TestSSZEmptyData(t *testing.T) {
	to := BytesToAddress([]byte{0x01})
	tx := NewTransaction(&LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(0), Gas: 21000,
		To: &to, Value: big.NewInt(0), Data: nil,
		V: big.NewInt(27), R: big.NewInt(1), S: big.NewInt(2),
	})

	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := SSZToTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded.Data()) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(decoded.Data()))
	}
}

// assertSSZTxEqual compares two transactions on key fields.
func assertSSZTxEqual(t *testing.T, a, b *Transaction) {
	t.Helper()
	if a.Type() != b.Type() {
		t.Errorf("type: got %d, want %d", b.Type(), a.Type())
	}
	if a.Nonce() != b.Nonce() {
		t.Errorf("nonce: got %d, want %d", b.Nonce(), a.Nonce())
	}
	if a.Gas() != b.Gas() {
		t.Errorf("gas: got %d, want %d", b.Gas(), a.Gas())
	}
	if a.Value().Cmp(b.Value()) != 0 {
		t.Errorf("value: got %v, want %v", b.Value(), a.Value())
	}
	if !bytes.Equal(a.Data(), b.Data()) {
		t.Errorf("data: got %x, want %x", b.Data(), a.Data())
	}
	if (a.To() == nil) != (b.To() == nil) {
		t.Errorf("to nil mismatch: a=%v b=%v", a.To(), b.To())
	}
	if a.To() != nil && b.To() != nil && *a.To() != *b.To() {
		t.Errorf("to: got %x, want %x", b.To(), a.To())
	}

	av, ar, as := a.RawSignatureValues()
	bv, br, bs := b.RawSignatureValues()
	if av.Cmp(bv) != 0 {
		t.Errorf("v: got %v, want %v", bv, av)
	}
	if ar.Cmp(br) != 0 {
		t.Errorf("r: got %v, want %v", br, ar)
	}
	if as.Cmp(bs) != 0 {
		t.Errorf("s: got %v, want %v", bs, as)
	}

	if a.GasPrice() != nil && b.GasPrice() != nil {
		if a.GasPrice().Cmp(b.GasPrice()) != 0 {
			t.Errorf("gasPrice: got %v, want %v", b.GasPrice(), a.GasPrice())
		}
	}
	if a.GasTipCap() != nil && b.GasTipCap() != nil {
		if a.GasTipCap().Cmp(b.GasTipCap()) != 0 {
			t.Errorf("gasTipCap: got %v, want %v", b.GasTipCap(), a.GasTipCap())
		}
	}
	if a.GasFeeCap() != nil && b.GasFeeCap() != nil {
		if a.GasFeeCap().Cmp(b.GasFeeCap()) != 0 {
			t.Errorf("gasFeeCap: got %v, want %v", b.GasFeeCap(), a.GasFeeCap())
		}
	}
}
