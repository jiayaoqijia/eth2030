package types

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"testing"
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
	Receipt          string
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

	var requests Requests
	for i, raw := range vectors.Requests {
		requests = append(requests, NewRequest(byte(i), executionVectorBytes(t, raw)))
	}
	root, err := ExecutionRequestsSSZRoot(requests)
	if err != nil || root != payload.RequestsHash {
		t.Fatalf("execution requests root %s, want %s: %v", root.Hex(), payload.RequestsHash.Hex(), err)
	}
	receipt := NewReceipt(ReceiptStatusSuccessful, 21000)
	receipt.Type = DynamicFeeTxType
	receiptWire, err := receipt.EncodeRLP()
	if err != nil || !bytes.Equal(receiptWire, executionVectorBytes(t, vectors.Receipt)) {
		t.Fatalf("receipt envelope %x: %v", receiptWire, err)
	}
	root, err = ExecutionReceiptsSSZRoot([]*Receipt{receipt})
	if err != nil || root != payload.ReceiptsRoot {
		t.Fatalf("receipt root %s, want %s: %v", root.Hex(), payload.ReceiptsRoot.Hex(), err)
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
