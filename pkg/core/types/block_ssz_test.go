package types

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/eth2030/eth2030/ssz"
)

type executionSSZVector struct {
	SSZ  string
	Root string
}

type executionSSZVectors struct {
	Transactions []struct {
		Name string
		RLP  string
		executionSSZVector
	}
	TransactionsRoot string `json:"transactions_root"`
	Payload          executionSSZVector
	Summary          executionSSZVector
	Requests         []string
	Receipts         []string
}

func loadExecutionSSZVectors(t *testing.T) executionSSZVectors {
	t.Helper()
	data, err := os.ReadFile("testdata/execution_ssz/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors executionSSZVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	return vectors
}

func executionVectorBytes(t *testing.T, text string) []byte {
	t.Helper()
	value, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestEIP6404ReferenceVectors(t *testing.T) {
	vectors := loadExecutionSSZVectors(t)
	var transactions []*Transaction
	for i, vector := range vectors.Transactions {
		t.Run(vector.Name, func(t *testing.T) {
			raw := executionVectorBytes(t, vector.RLP)
			tx, err := DecodeTxRLP(raw)
			if err != nil {
				t.Fatal(err)
			}
			want := executionVectorBytes(t, vector.SSZ)
			wire, err := TransactionToSSZ(tx)
			if err != nil || !bytes.Equal(wire, want) {
				t.Fatalf("serialized %x, want %x: %v", wire, want, err)
			}
			decoded, err := SSZToTransaction(want)
			if err != nil {
				t.Fatal(err)
			}
			reconstructed, err := decoded.EncodeRLP()
			if err != nil || !bytes.Equal(reconstructed, raw) ||
				tx.Hash() != decoded.Hash() || tx.SigningHash() != decoded.SigningHash() {
				t.Fatalf("reconstructed RLP %x, want %x: %v", reconstructed, raw, err)
			}
			root, err := TransactionSSZRoot(decoded)
			if err != nil || root != HexToHash(vector.Root) {
				t.Fatalf("transaction root %s, want %s: %v", root.Hex(), vector.Root, err)
			}
			if i == 2 {
				if sender, err := NewEIP155Signer(1).Sender(decoded); err != nil ||
					sender != HexToAddress("0x9d8a62f656a8d1615c1294fd71e9cfb3e4855a4f") {
					t.Fatalf("published EIP-155 sender %s: %v", sender.Hex(), err)
				}
			}
			transactions = append(transactions, decoded)
		})
	}
	if len(transactions) != 10 {
		t.Fatalf("decoded %d variants, want 10", len(transactions))
	}
	root, err := TransactionsSSZRoot(transactions)
	if err != nil || root != HexToHash(vectors.TransactionsRoot) {
		t.Fatalf("transaction list root %s, want %s: %v", root.Hex(), vectors.TransactionsRoot, err)
	}
}

func TestEIP6404RejectsNoncanonicalContainer(t *testing.T) {
	vectors := loadExecutionSSZVectors(t)
	wire := executionVectorBytes(t, vectors.Transactions[2].SSZ)
	cases := map[string]func([]byte){
		"offset before fixed fields": func(w []byte) { binary.LittleEndian.PutUint32(w[:4], 7) },
		"unknown selector":           func(w []byte) { w[8] = 0 },
		"wrong transaction type":     func(w []byte) { w[9] = 1 },
		"obsolete algorithm":         func(w []byte) { w[len(w)-66] = 0xff },
		"invalid parity":             func(w []byte) { w[len(w)-1] = 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := bytes.Clone(wire)
			mutate(changed)
			if _, err := SSZToTransaction(changed); err == nil {
				t.Fatal("accepted noncanonical transaction")
			}
		})
	}
}

func TestEIP6404RejectsHighSSignature(t *testing.T) {
	vector := loadExecutionSSZVectors(t).Transactions[2]
	tx, err := DecodeTxRLP(executionVectorBytes(t, vector.RLP))
	if err != nil {
		t.Fatal(err)
	}
	// The alternate EIP-155 signature recovers the same key with the
	// opposite parity and a high S. EIP-7932 requires the low-S form.
	legacy := tx.Inner().copy().(*LegacyTx)
	legacy.S.Sub(secp256k1NCopy, legacy.S)
	legacy.V.Add(legacy.V, big.NewInt(1))
	if _, err := TransactionToSSZ(NewTransaction(legacy)); !errors.Is(err, ErrSSZTransactionSignature) {
		t.Fatalf("encode high-S transaction: got %v, want signature error", err)
	}

	wire := executionVectorBytes(t, vector.SSZ)
	offset := int(binary.LittleEndian.Uint32(wire[4:8]))
	legacy.S.FillBytes(wire[offset+33 : offset+65])
	wire[offset+65] ^= 1
	if _, err := SSZToTransaction(wire); !errors.Is(err, ErrSSZTransactionSignature) {
		t.Fatalf("decode high-S transaction: got %v, want signature error", err)
	}
}

func TestEIP6404RejectsPaddingBeforePayload(t *testing.T) {
	vector := loadExecutionSSZVectors(t).Transactions[2]
	wire := executionVectorBytes(t, vector.SSZ)
	// Preserve both fields and shift both offsets so that only the
	// unreferenced byte between the fixed section and payload is invalid.
	padded := append(bytes.Clone(wire[:8]), 0)
	padded = append(padded, wire[8:]...)
	for _, start := range []int{0, 4} {
		binary.LittleEndian.PutUint32(padded[start:], binary.LittleEndian.Uint32(wire[start:])+1)
	}
	if _, err := SSZToTransaction(padded); !errors.Is(err, ssz.ErrOffset) {
		t.Fatalf("decode padded transaction: got %v, want offset error", err)
	}
}

func referenceExecutionRequests(t *testing.T, vectors executionSSZVectors) Requests {
	t.Helper()
	if len(vectors.Requests) != 3 {
		t.Fatalf("fixture has %d request types, want 3", len(vectors.Requests))
	}
	requests := make(Requests, len(vectors.Requests))
	for i, raw := range vectors.Requests {
		requests[i] = NewRequest(byte(i), executionVectorBytes(t, raw))
	}
	return requests
}

func referenceExecutionReceipts(t *testing.T, vectors executionSSZVectors) []*Receipt {
	t.Helper()
	txTypes := []byte{DynamicFeeTxType, BlobTxType}
	if len(vectors.Receipts) != len(txTypes) {
		t.Fatalf("fixture has %d receipts, want %d", len(vectors.Receipts), len(txTypes))
	}
	receipts := make([]*Receipt, len(txTypes))
	for i, txType := range txTypes {
		receipt := NewReceipt(ReceiptStatusSuccessful, uint64(i+1)*21_000)
		receipt.Type = txType
		wire, err := receipt.EncodeRLP()
		if err != nil || !bytes.Equal(wire, executionVectorBytes(t, vectors.Receipts[i])) {
			t.Fatalf("receipt %d envelope %x: %v", i, wire, err)
		}
		receipts[i] = receipt
	}
	return receipts
}

func TestEIP7807ReferenceVectors(t *testing.T) {
	vectors := loadExecutionSSZVectors(t)
	wire := executionVectorBytes(t, vectors.Payload.SSZ)
	var payload ExecutionPayloadSSZ
	if err := payload.UnmarshalSSZ(wire); err != nil {
		t.Fatal(err)
	}
	if payload.SlotNumber != 42 || payload.GasUsed.Blob != 131072 ||
		payload.BaseFeesPerGas.Blob.Cmp(new(big.Int).Lsh(big.NewInt(1), 100)) != 0 ||
		payload.ParentBeaconBlockRoot != BytesToHash(bytes.Repeat([]byte{0x14}, 32)) ||
		len(payload.Transactions) != 2 || len(payload.Withdrawals) != 1 {
		t.Fatalf("decoded payload fields differ from fixture: %+v", payload)
	}
	if raw := executionVectorBytes(t, vectors.Transactions[6].RLP); !bytes.Equal(raw, payload.Transactions[0]) {
		t.Fatal("block transaction lost its EIP-2718 bytes")
	}
	got, err := payload.MarshalSSZ()
	if err != nil || !bytes.Equal(got, wire) {
		t.Fatalf("payload encoding differs from reference: %v", err)
	}
	summary := payload.Header()
	got, err = summary.MarshalSSZ()
	if err != nil || !bytes.Equal(got, executionVectorBytes(t, vectors.Summary.SSZ)) {
		t.Fatalf("summary encoding differs from reference: %v", err)
	}
	var restored ExecutionHeaderSSZ
	if err := restored.UnmarshalSSZ(got); err != nil {
		t.Fatal(err)
	}
	headerRoot, err := restored.HashTreeRoot()
	if err != nil || headerRoot != HexToHash(vectors.Summary.Root) {
		t.Fatalf("summary root %s: %v", headerRoot.Hex(), err)
	}
	payloadRoot, err := payload.HashTreeRoot()
	if err != nil || payloadRoot != HexToHash(vectors.Payload.Root) || payloadRoot != headerRoot {
		t.Fatalf("payload root %s, summary root %s: %v", payloadRoot.Hex(), headerRoot.Hex(), err)
	}

	root, err := ExecutionRequestsSSZRoot(referenceExecutionRequests(t, vectors))
	if err != nil || root != payload.RequestsHash {
		t.Fatalf("execution requests root %s, want %s: %v", root.Hex(), payload.RequestsHash.Hex(), err)
	}
	root, err = ExecutionReceiptsSSZRoot(referenceExecutionReceipts(t, vectors))
	if err != nil || root != payload.ReceiptsRoot {
		t.Fatalf("receipt root %s, want %s: %v", root.Hex(), payload.ReceiptsRoot.Hex(), err)
	}
}

func TestEIP7807ProjectionMatchesReference(t *testing.T) {
	vectors := loadExecutionSSZVectors(t)
	wire := executionVectorBytes(t, vectors.Payload.SSZ)
	var reference ExecutionPayloadSSZ
	if err := reference.UnmarshalSSZ(wire); err != nil {
		t.Fatal(err)
	}
	txs := make([]*Transaction, len(reference.Transactions))
	for i, raw := range reference.Transactions {
		tx, err := DecodeTxRLP(raw)
		if err != nil {
			t.Fatal(err)
		}
		txs[i] = tx
	}
	withdrawals := make([]*Withdrawal, len(reference.Withdrawals))
	for i, raw := range reference.Withdrawals {
		w, err := DecodeWithdrawal(raw)
		if err != nil {
			t.Fatal(err)
		}
		withdrawals[i] = w
	}

	// Supply the legacy block fields and execution inputs separately. Its
	// RLP commitments are deliberately distinct from the SSZ fixture roots.
	legacyRequestsHash := Hash{1}
	block := NewBlock(&Header{
		ParentHash: reference.ParentHash, Coinbase: reference.Miner, Root: reference.StateRoot,
		ReceiptHash: Hash{2}, RequestsHash: &legacyRequestsHash,
		Number: new(big.Int).SetUint64(reference.Number), GasLimit: reference.GasLimits.Regular,
		GasUsed: reference.GasUsed.Regular, Time: reference.Timestamp, Extra: reference.ExtraData,
		MixDigest: reference.MixHash, BaseFee: reference.BaseFeesPerGas.Regular,
		BlobGasUsed: &reference.GasUsed.Blob, ExcessBlobGas: &reference.ExcessGas.Blob,
		ParentBeaconRoot: &reference.ParentBeaconBlockRoot,
	}, &Body{Transactions: txs, Withdrawals: withdrawals})
	payload, err := NewExecutionPayloadSSZ(block, referenceExecutionReceipts(t, vectors), ExecutionSSZContext{
		BlobGasLimit: reference.GasLimits.Blob, BlobBaseFee: reference.BaseFeesPerGas.Blob,
		ExcessRegularGas: reference.ExcessGas.Regular, Requests: referenceExecutionRequests(t, vectors),
		BlockAccessList: reference.BlockAccessList, SlotNumber: reference.SlotNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.RequestsHash != reference.RequestsHash || payload.ReceiptsRoot != reference.ReceiptsRoot {
		t.Fatalf("projected requests root %s, receipts root %s", payload.RequestsHash.Hex(), payload.ReceiptsRoot.Hex())
	}
	if len(payload.Withdrawals) != len(withdrawals) || payload.GasLimits != reference.GasLimits {
		t.Fatalf("projected %d withdrawals and gas limits %+v", len(payload.Withdrawals), payload.GasLimits)
	}
	got, err := payload.MarshalSSZ()
	if err != nil || !bytes.Equal(got, wire) {
		t.Fatalf("projected payload encoding differs from reference: %v", err)
	}
	root, err := payload.HashTreeRoot()
	if err != nil || root != HexToHash(vectors.Payload.Root) {
		t.Fatalf("projected payload root %s: %v", root.Hex(), err)
	}
}

func TestEIP7807WithdrawalRequestLimit(t *testing.T) {
	vectors := loadExecutionSSZVectors(t)
	data := referenceExecutionRequests(t, vectors)[WithdrawalRequestType].Data
	for _, count := range []int{16, 17} {
		request := NewRequest(WithdrawalRequestType, bytes.Repeat(data, count))
		if _, err := ExecutionRequestsSSZRoot(Requests{request}); (err != nil) != (count == 17) {
			t.Fatalf("%d withdrawal requests: %v", count, err)
		}
	}
}

func TestEIP7807RejectsNoncanonicalOffsets(t *testing.T) {
	vectors := loadExecutionSSZVectors(t)
	wire := executionVectorBytes(t, vectors.Payload.SSZ)
	changed := bytes.Clone(wire)
	binary.LittleEndian.PutUint32(changed[84:], 0) // transactions offset
	var payload ExecutionPayloadSSZ
	if err := payload.UnmarshalSSZ(changed); err == nil {
		t.Fatal("accepted payload with a noncanonical transactions offset")
	}
	if err := payload.UnmarshalSSZ(wire); err != nil {
		t.Fatal(err)
	}
	payload.ExtraData = make([]byte, 33)
	if _, err := payload.MarshalSSZ(); err == nil {
		t.Fatal("accepted oversized extra data")
	}
}
