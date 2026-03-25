// Package rpctypes provides JSON-RPC 2.0 protocol types for the Ethereum JSON-RPC API.
package rpctypes

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	"github.com/eth2030/eth2030/core/types"
)

// BlockNumber represents a block number parameter in JSON-RPC.
type BlockNumber int64

const (
	LatestBlockNumber    BlockNumber = -1
	PendingBlockNumber   BlockNumber = -2
	EarliestBlockNumber  BlockNumber = 0
	SafeBlockNumber      BlockNumber = -3
	FinalizedBlockNumber BlockNumber = -4
)

// UnmarshalJSON implements json.Unmarshaler for block number.
func (bn *BlockNumber) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Try as integer.
		var n int64
		if err := json.Unmarshal(data, &n); err != nil {
			return fmt.Errorf("invalid block number: %s", string(data))
		}
		*bn = BlockNumber(n)
		return nil
	}
	switch s {
	case "latest":
		*bn = LatestBlockNumber
	case "pending":
		*bn = PendingBlockNumber
	case "earliest":
		*bn = EarliestBlockNumber
	case "safe":
		*bn = SafeBlockNumber
	case "finalized":
		*bn = FinalizedBlockNumber
	default:
		// Parse hex string.
		n, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return fmt.Errorf("invalid block number: %s", s)
		}
		*bn = BlockNumber(n)
	}
	return nil
}

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string            `json:"jsonrpc"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
	ID      json.RawMessage   `json:"id"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// RPCError is a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return e.Message
}

// Error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603

	// ErrCodeHistoryPruned indicates the requested historical data has been
	// pruned per EIP-4444.
	ErrCodeHistoryPruned = -32000
)

// RPCBlock is the JSON representation of a block.
type RPCBlock struct {
	Number           string   `json:"number"`
	Hash             string   `json:"hash"`
	ParentHash       string   `json:"parentHash"`
	Sha3Uncles       string   `json:"sha3Uncles"`
	Miner            string   `json:"miner"`
	StateRoot        string   `json:"stateRoot"`
	TxRoot           string   `json:"transactionsRoot"`
	ReceiptsRoot     string   `json:"receiptsRoot"`
	LogsBloom        string   `json:"logsBloom"`
	Difficulty       string   `json:"difficulty"`
	GasLimit         string   `json:"gasLimit"`
	GasUsed          string   `json:"gasUsed"`
	Timestamp        string   `json:"timestamp"`
	ExtraData        string   `json:"extraData"`
	MixHash          string   `json:"mixHash"`
	Nonce            string   `json:"nonce"`
	Size             string   `json:"size"`
	BaseFeePerGas    *string  `json:"baseFeePerGas,omitempty"`
	WithdrawalsRoot  *string  `json:"withdrawalsRoot,omitempty"`
	BlobGasUsed      *string  `json:"blobGasUsed,omitempty"`
	ExcessBlobGas    *string  `json:"excessBlobGas,omitempty"`
	ParentBeaconRoot *string  `json:"parentBeaconBlockRoot,omitempty"`
	RequestsHash     *string  `json:"requestsHash,omitempty"`
	Transactions     []string `json:"transactions"` // tx hashes
	Uncles           []string `json:"uncles"`
	// Withdrawals is nil for pre-Shanghai blocks and a (possibly empty) slice
	// for post-Shanghai blocks. Using a pointer preserves the distinction so
	// that post-Shanghai empty blocks emit "withdrawals": [] instead of
	// omitting the field entirely (which breaks EIP-4895 clients).
	Withdrawals *[]*RPCWithdrawal `json:"withdrawals,omitempty"`
}

// RPCAccessTuple is the JSON representation of an EIP-2930 access list entry.
type RPCAccessTuple struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

// RPCAuthorization is the JSON representation of an EIP-7702 authorization entry.
// V is serialized as "yParity" per the Ethereum JSON-RPC spec (EIP-7702).
type RPCAuthorization struct {
	ChainID string `json:"chainId"`
	Address string `json:"address"`
	Nonce   string `json:"nonce"`
	V       string `json:"yParity"`
	R       string `json:"r"`
	S       string `json:"s"`
}

// RPCFrame is the JSON representation of a single frame in a FrameTx (EIP-8141).
type RPCFrame struct {
	Mode     string  `json:"mode"`
	Target   *string `json:"target,omitempty"`
	GasLimit string  `json:"gasLimit"`
	Data     string  `json:"data"`
}

// RPCTransaction is the JSON representation of a transaction.
type RPCTransaction struct {
	Hash        string  `json:"hash"`
	Nonce       string  `json:"nonce"`
	BlockHash   *string `json:"blockHash"`
	BlockNumber *string `json:"blockNumber"`
	// BlockTimestamp is a Geth extension: the timestamp of the block containing the tx.
	BlockTimestamp   *string `json:"blockTimestamp,omitempty"`
	TransactionIndex *string `json:"transactionIndex"`
	From             string  `json:"from"`
	To               *string `json:"to"`
	Value            string  `json:"value"`
	Gas              string  `json:"gas"`
	GasPrice         string  `json:"gasPrice"`
	Input            string  `json:"input"`
	Type             string  `json:"type"`
	V                string  `json:"v"`
	R                string  `json:"r"`
	S                string  `json:"s"`
	// YParity mirrors V for EIP-2718 typed txs (type ≥ 1).
	YParity *string `json:"yParity,omitempty"`
	// EIP-2930 / EIP-1559 / EIP-4844 / EIP-7702 fields (omitted for legacy txs).
	ChainID              *string `json:"chainId,omitempty"`
	MaxFeePerGas         *string `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas *string `json:"maxPriorityFeePerGas,omitempty"`
	// AccessList uses a pointer so a non-nil empty slice encodes as [] (not omitted).
	AccessList          *[]RPCAccessTuple  `json:"accessList,omitempty"`
	MaxFeePerBlobGas    *string            `json:"maxFeePerBlobGas,omitempty"`
	BlobVersionedHashes []string           `json:"blobVersionedHashes,omitempty"`
	AuthorizationList   []RPCAuthorization `json:"authorizationList,omitempty"`
	// EIP-8141 FrameTx fields (type 0x06).
	Frames []RPCFrame `json:"frames,omitempty"`
}

// RPCReceipt is the JSON representation of a transaction receipt.
type RPCReceipt struct {
	TransactionHash   string    `json:"transactionHash"`
	TransactionIndex  string    `json:"transactionIndex"`
	BlockHash         string    `json:"blockHash"`
	BlockNumber       string    `json:"blockNumber"`
	From              string    `json:"from"`
	To                *string   `json:"to"`
	GasUsed           string    `json:"gasUsed"`
	CumulativeGasUsed string    `json:"cumulativeGasUsed"`
	ContractAddress   *string   `json:"contractAddress"`
	Logs              []*RPCLog `json:"logs"`
	Status            string    `json:"status"`
	LogsBloom         string    `json:"logsBloom"`
	Type              string    `json:"type"`
	EffectiveGasPrice string    `json:"effectiveGasPrice"`

	// EIP-4844 blob transaction fields (only present for blob txs).
	BlobGasUsed  *string `json:"blobGasUsed,omitempty"`
	BlobGasPrice *string `json:"blobGasPrice,omitempty"`

	// EIP-8141 Frame transaction fields (only present for FrameTx type 0x06).
	// Payer is the address that paid for the gas (repurposed ContractAddress for FrameTx).
	Payer        *string          `json:"payer,omitempty"`
	FrameResults *[]RPCFrameResult `json:"frameResults,omitempty"`
}

// RPCFrameResult represents a single frame's execution result in a FrameTx receipt.
type RPCFrameResult struct {
	Status  string    `json:"status"`
	GasUsed string    `json:"gasUsed"`
	Logs    []*RPCLog `json:"logs,omitempty"`
}

// RPCLog is the JSON representation of a contract log event.
type RPCLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	BlockHash        string   `json:"blockHash"`
	// BlockTimestamp is a Geth extension: the timestamp of the block.
	BlockTimestamp *string `json:"blockTimestamp,omitempty"`
	LogIndex       string  `json:"logIndex"`
	Removed        bool    `json:"removed"`
}

// CallArgs represents the arguments for eth_call and eth_estimateGas.
type CallArgs struct {
	From     *string `json:"from"`
	To       *string `json:"to"`
	Gas      *string `json:"gas"`
	GasPrice *string `json:"gasPrice"`
	Value    *string `json:"value"`
	Data     *string `json:"data"`
	Input    *string `json:"input"`
}

// GetData returns the call input data, preferring "input" over "data".
func (args *CallArgs) GetData() []byte {
	if args.Input != nil {
		return FromHexBytes(*args.Input)
	}
	if args.Data != nil {
		return FromHexBytes(*args.Data)
	}
	return nil
}

// FilterCriteria contains parameters for log filtering.
type FilterCriteria struct {
	FromBlock *BlockNumber `json:"fromBlock"`
	ToBlock   *BlockNumber `json:"toBlock"`
	Addresses []string     `json:"address"`
	Topics    [][]string   `json:"topics"`
}

// RPCBlockWithTxs is the JSON representation of a block with full transaction objects.
type RPCBlockWithTxs struct {
	Number           string            `json:"number"`
	Hash             string            `json:"hash"`
	ParentHash       string            `json:"parentHash"`
	Sha3Uncles       string            `json:"sha3Uncles"`
	Miner            string            `json:"miner"`
	StateRoot        string            `json:"stateRoot"`
	TxRoot           string            `json:"transactionsRoot"`
	ReceiptsRoot     string            `json:"receiptsRoot"`
	LogsBloom        string            `json:"logsBloom"`
	Difficulty       string            `json:"difficulty"`
	GasLimit         string            `json:"gasLimit"`
	GasUsed          string            `json:"gasUsed"`
	Timestamp        string            `json:"timestamp"`
	ExtraData        string            `json:"extraData"`
	MixHash          string            `json:"mixHash"`
	Nonce            string            `json:"nonce"`
	Size             string            `json:"size"`
	BaseFeePerGas    *string           `json:"baseFeePerGas,omitempty"`
	WithdrawalsRoot  *string           `json:"withdrawalsRoot,omitempty"`
	BlobGasUsed      *string           `json:"blobGasUsed,omitempty"`
	ExcessBlobGas    *string           `json:"excessBlobGas,omitempty"`
	ParentBeaconRoot *string           `json:"parentBeaconBlockRoot,omitempty"`
	RequestsHash     *string           `json:"requestsHash,omitempty"`
	Transactions     []*RPCTransaction `json:"transactions"`
	Uncles           []string          `json:"uncles"`
	// Withdrawals is nil for pre-Shanghai blocks and a (possibly empty) slice
	// for post-Shanghai blocks (see RPCBlock.Withdrawals).
	Withdrawals *[]*RPCWithdrawal `json:"withdrawals,omitempty"`
}

// RPCWithdrawal is the JSON representation of a beacon-chain withdrawal.
type RPCWithdrawal struct {
	Index          string `json:"index"`
	ValidatorIndex string `json:"validatorIndex"`
	Address        string `json:"address"`
	Amount         string `json:"amount"`
}

// EncodeUint64 encodes a uint64 as a hex string with 0x prefix.
func EncodeUint64(n uint64) string {
	return "0x" + strconv.FormatUint(n, 16)
}

// EncodeBigInt encodes a big.Int as a hex string with 0x prefix.
func EncodeBigInt(n *big.Int) string {
	if n == nil {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

// EncodeHash encodes a hash as a 0x-prefixed 32-byte hex string.
func EncodeHash(h types.Hash) string {
	return "0x" + fmt.Sprintf("%064x", h[:])
}

// EncodeAddress encodes an address as a 0x-prefixed 20-byte hex string.
func EncodeAddress(a types.Address) string {
	return "0x" + fmt.Sprintf("%040x", a[:])
}

// EncodeBytes encodes a byte slice as a 0x-prefixed hex string.
func EncodeBytes(b []byte) string {
	if len(b) == 0 {
		return "0x"
	}
	return "0x" + fmt.Sprintf("%x", b)
}

// EncodeBloom encodes a bloom filter as a 0x-prefixed hex string.
func EncodeBloom(b types.Bloom) string {
	return fmt.Sprintf("0x%0512x", b[:])
}

// FromHexBytes decodes a hex string (with optional 0x prefix) into bytes.
func FromHexBytes(s string) []byte {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	if len(s) == 0 {
		return nil
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(b); i++ {
		b[i] = Unhex(s[2*i])<<4 | Unhex(s[2*i+1])
	}
	return b
}

// Unhex converts a single hex character to its numeric value.
func Unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// ParseHexUint64 parses a hex string (with optional 0x prefix) as uint64.
func ParseHexUint64(s string) uint64 {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	n, _ := strconv.ParseUint(s, 16, 64)
	return n
}

// ParseHexBigInt parses a hex string (with optional 0x prefix) as *big.Int.
func ParseHexBigInt(s string) *big.Int {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	n := new(big.Int)
	n.SetString(s, 16)
	return n
}

// FormatUncleHashes returns uncle hashes as hex strings (empty post-merge).
func FormatUncleHashes(uncles []*types.Header) []string {
	if len(uncles) == 0 {
		return []string{}
	}
	hashes := make([]string, len(uncles))
	for i, u := range uncles {
		hashes[i] = EncodeHash(u.Hash())
	}
	return hashes
}

// FormatAccessList converts an AccessList to its JSON-RPC representation.
func FormatAccessList(al types.AccessList) []RPCAccessTuple {
	result := make([]RPCAccessTuple, len(al))
	for i, entry := range al {
		keys := make([]string, len(entry.StorageKeys))
		for j, k := range entry.StorageKeys {
			keys[j] = EncodeHash(k)
		}
		result[i] = RPCAccessTuple{
			Address:     EncodeAddress(entry.Address),
			StorageKeys: keys,
		}
	}
	return result
}

// FormatAuthorizationList converts an AuthorizationList to its JSON-RPC representation.
func FormatAuthorizationList(auths []types.Authorization) []RPCAuthorization {
	result := make([]RPCAuthorization, len(auths))
	for i, auth := range auths {
		av, ar, as_ := auth.V, auth.R, auth.S
		authEntry := RPCAuthorization{
			Address: EncodeAddress(auth.Address),
			Nonce:   EncodeUint64(auth.Nonce),
		}
		if auth.ChainID != nil {
			authEntry.ChainID = EncodeBigInt(auth.ChainID)
		} else {
			authEntry.ChainID = "0x0"
		}
		if av != nil {
			authEntry.V = EncodeBigInt(av)
		} else {
			authEntry.V = "0x0"
		}
		if ar != nil {
			authEntry.R = EncodeBigInt(ar)
		} else {
			authEntry.R = "0x0"
		}
		if as_ != nil {
			authEntry.S = EncodeBigInt(as_)
		} else {
			authEntry.S = "0x0"
		}
		result[i] = authEntry
	}
	return result
}

// FormatBlock converts a block to its JSON-RPC representation.
// If fullTx is true, returns full transaction objects; otherwise returns tx hashes.
func FormatBlock(block *types.Block, fullTx bool) interface{} {
	header := block.Header()
	if !fullTx {
		rb := FormatHeader(header)
		// Populate tx hashes from block body.
		txs := block.Transactions()
		rb.Transactions = make([]string, len(txs))
		for i, tx := range txs {
			rb.Transactions[i] = EncodeHash(tx.Hash())
		}
		rb.Uncles = FormatUncleHashes(block.Uncles())
		// Withdrawals: always present (even empty) for post-Shanghai blocks.
		if header.WithdrawalsHash != nil {
			ws := block.Withdrawals()
			wList := make([]*RPCWithdrawal, len(ws))
			for i, w := range ws {
				wList[i] = &RPCWithdrawal{
					Index:          EncodeUint64(w.Index),
					ValidatorIndex: EncodeUint64(w.ValidatorIndex),
					Address:        EncodeAddress(w.Address),
					Amount:         EncodeUint64(w.Amount),
				}
			}
			rb.Withdrawals = &wList
		}
		return rb
	}

	difficulty := "0x0"
	if header.Difficulty != nil {
		difficulty = EncodeBigInt(header.Difficulty)
	}
	result := &RPCBlockWithTxs{
		Number:       EncodeUint64(header.Number.Uint64()),
		Hash:         EncodeHash(header.Hash()),
		ParentHash:   EncodeHash(header.ParentHash),
		Sha3Uncles:   EncodeHash(header.UncleHash),
		Miner:        EncodeAddress(header.Coinbase),
		StateRoot:    EncodeHash(header.Root),
		TxRoot:       EncodeHash(header.TxHash),
		ReceiptsRoot: EncodeHash(header.ReceiptHash),
		LogsBloom:    EncodeBloom(header.Bloom),
		Difficulty:   difficulty,
		GasLimit:     EncodeUint64(header.GasLimit),
		GasUsed:      EncodeUint64(header.GasUsed),
		Timestamp:    EncodeUint64(header.Time),
		ExtraData:    EncodeBytes(header.Extra),
		MixHash:      EncodeHash(header.MixDigest),
		Nonce:        fmt.Sprintf("0x%016x", header.Nonce),
		Size:         EncodeUint64(header.Size()),
		Uncles:       FormatUncleHashes(block.Uncles()),
	}
	if header.BaseFee != nil {
		s := EncodeBigInt(header.BaseFee)
		result.BaseFeePerGas = &s
	}
	if header.WithdrawalsHash != nil {
		s := EncodeHash(*header.WithdrawalsHash)
		result.WithdrawalsRoot = &s
	}
	if header.BlobGasUsed != nil {
		s := EncodeUint64(*header.BlobGasUsed)
		result.BlobGasUsed = &s
	}
	if header.ExcessBlobGas != nil {
		s := EncodeUint64(*header.ExcessBlobGas)
		result.ExcessBlobGas = &s
	}
	if header.ParentBeaconRoot != nil {
		s := EncodeHash(*header.ParentBeaconRoot)
		result.ParentBeaconRoot = &s
	}
	if header.RequestsHash != nil {
		s := EncodeHash(*header.RequestsHash)
		result.RequestsHash = &s
	}

	txs := block.Transactions()
	result.Transactions = make([]*RPCTransaction, len(txs))
	blockHash := block.Hash()
	blockNum := block.NumberU64()
	blockTS := header.Time
	for i, tx := range txs {
		idx := uint64(i)
		result.Transactions[i] = FormatTransaction(tx, &blockHash, &blockNum, &idx, blockTS, header.BaseFee)
	}

	// Withdrawals: always present (even empty) for post-Shanghai blocks.
	if header.WithdrawalsHash != nil {
		ws := block.Withdrawals()
		wList := make([]*RPCWithdrawal, len(ws))
		for i, w := range ws {
			wList[i] = &RPCWithdrawal{
				Index:          EncodeUint64(w.Index),
				ValidatorIndex: EncodeUint64(w.ValidatorIndex),
				Address:        EncodeAddress(w.Address),
				Amount:         EncodeUint64(w.Amount),
			}
		}
		result.Withdrawals = &wList
	}

	return result
}

// FormatHeader converts a header to JSON-RPC representation.
func FormatHeader(h *types.Header) *RPCBlock {
	difficulty := "0x0"
	if h.Difficulty != nil {
		difficulty = EncodeBigInt(h.Difficulty)
	}
	block := &RPCBlock{
		Number:       EncodeUint64(h.Number.Uint64()),
		Hash:         EncodeHash(h.Hash()),
		ParentHash:   EncodeHash(h.ParentHash),
		Sha3Uncles:   EncodeHash(h.UncleHash),
		Miner:        EncodeAddress(h.Coinbase),
		StateRoot:    EncodeHash(h.Root),
		TxRoot:       EncodeHash(h.TxHash),
		ReceiptsRoot: EncodeHash(h.ReceiptHash),
		LogsBloom:    EncodeBloom(h.Bloom),
		Difficulty:   difficulty,
		GasLimit:     EncodeUint64(h.GasLimit),
		GasUsed:      EncodeUint64(h.GasUsed),
		Timestamp:    EncodeUint64(h.Time),
		ExtraData:    EncodeBytes(h.Extra),
		MixHash:      EncodeHash(h.MixDigest),
		Nonce:        fmt.Sprintf("0x%016x", h.Nonce),
		Size:         EncodeUint64(h.Size()),
		Transactions: []string{},
		Uncles:       []string{},
	}
	if h.BaseFee != nil {
		s := EncodeBigInt(h.BaseFee)
		block.BaseFeePerGas = &s
	}
	if h.WithdrawalsHash != nil {
		s := EncodeHash(*h.WithdrawalsHash)
		block.WithdrawalsRoot = &s
	}
	if h.BlobGasUsed != nil {
		s := EncodeUint64(*h.BlobGasUsed)
		block.BlobGasUsed = &s
	}
	if h.ExcessBlobGas != nil {
		s := EncodeUint64(*h.ExcessBlobGas)
		block.ExcessBlobGas = &s
	}
	if h.ParentBeaconRoot != nil {
		s := EncodeHash(*h.ParentBeaconRoot)
		block.ParentBeaconRoot = &s
	}
	if h.RequestsHash != nil {
		s := EncodeHash(*h.RequestsHash)
		block.RequestsHash = &s
	}
	return block
}

// FormatTransaction converts a transaction to its JSON-RPC representation.
// blockTimestamp is the timestamp of the containing block (0 if unknown/pending).
// baseFee is the block base fee per gas; used to compute the effective gasPrice
// for EIP-1559 txs (type ≥ 2). Pass nil for pending txs or pre-London blocks.
func FormatTransaction(tx *types.Transaction, blockHash *types.Hash, blockNumber *uint64, index *uint64, blockTimestamp uint64, baseFee *big.Int) *RPCTransaction {
	// For EIP-1559+ txs, effective gasPrice = min(feeCap, baseFee + tip).
	gasPrice := tx.GasPrice()
	txType := tx.Type()
	if txType >= types.DynamicFeeTxType && baseFee != nil {
		tip := tx.GasTipCap()
		if tip == nil {
			tip = new(big.Int)
		}
		effective := new(big.Int).Add(baseFee, tip)
		feeCap := tx.GasFeeCap()
		if feeCap != nil && effective.Cmp(feeCap) > 0 {
			effective = feeCap
		}
		gasPrice = effective
	}

	rpcTx := &RPCTransaction{
		Hash:     EncodeHash(tx.Hash()),
		Nonce:    EncodeUint64(tx.Nonce()),
		Value:    EncodeBigInt(tx.Value()),
		Gas:      EncodeUint64(tx.Gas()),
		GasPrice: EncodeBigInt(gasPrice),
		Input:    EncodeBytes(tx.Data()),
		Type:     EncodeUint64(uint64(txType)),
	}

	// For FrameTx (type 0x06), use FrameSender() since the sender is in the FrameTx struct.
	// For other transaction types, use the cached Sender().
	if txType == types.FrameTxType {
		frameSender := tx.FrameSender()
		if !frameSender.IsZero() {
			rpcTx.From = EncodeAddress(frameSender)
		}
	} else if sender := tx.Sender(); sender != nil {
		rpcTx.From = EncodeAddress(*sender)
	}

	if tx.To() != nil {
		to := EncodeAddress(*tx.To())
		rpcTx.To = &to
	}

	if blockHash != nil {
		bh := EncodeHash(*blockHash)
		rpcTx.BlockHash = &bh
	}
	if blockNumber != nil {
		bn := EncodeUint64(*blockNumber)
		rpcTx.BlockNumber = &bn
	}
	if blockTimestamp > 0 {
		ts := EncodeUint64(blockTimestamp)
		rpcTx.BlockTimestamp = &ts
	}
	if index != nil {
		idx := EncodeUint64(*index)
		rpcTx.TransactionIndex = &idx
	}

	// V, R, S from signature.
	v, r, s := tx.RawSignatureValues()
	if v != nil {
		rpcTx.V = EncodeBigInt(v)
	} else {
		rpcTx.V = "0x0"
	}
	if r != nil {
		rpcTx.R = EncodeBigInt(r)
	} else {
		rpcTx.R = "0x0"
	}
	if s != nil {
		rpcTx.S = EncodeBigInt(s)
	} else {
		rpcTx.S = "0x0"
	}

	// yParity mirrors V for typed txs (EIP-2718, type ≥ 1).
	if txType >= types.AccessListTxType {
		yp := rpcTx.V
		rpcTx.YParity = &yp
	}

	// EIP-2930+: chainId and accessList (types 1, 2, 3, 4).
	// Use *[]RPCAccessTuple so an empty access list encodes as [] (not omitted).
	if txType >= types.AccessListTxType {
		chainID := tx.ChainId()
		if chainID != nil {
			cid := EncodeBigInt(chainID)
			rpcTx.ChainID = &cid
		}
		al := FormatAccessList(tx.AccessList())
		rpcTx.AccessList = &al
	}

	// EIP-1559+: maxFeePerGas and maxPriorityFeePerGas (types 2, 3, 4).
	if txType >= types.DynamicFeeTxType {
		mfpg := EncodeBigInt(tx.GasFeeCap())
		rpcTx.MaxFeePerGas = &mfpg
		mpfpg := EncodeBigInt(tx.GasTipCap())
		rpcTx.MaxPriorityFeePerGas = &mpfpg
	}

	// EIP-4844: blob fields (type 3).
	if txType == types.BlobTxType {
		if blobFeeCap := tx.BlobGasFeeCap(); blobFeeCap != nil {
			mfpbg := EncodeBigInt(blobFeeCap)
			rpcTx.MaxFeePerBlobGas = &mfpbg
		}
		blobHashes := tx.BlobHashes()
		rpcTx.BlobVersionedHashes = make([]string, len(blobHashes))
		for i, h := range blobHashes {
			rpcTx.BlobVersionedHashes[i] = EncodeHash(h)
		}
	}

	// EIP-7702: authorization list (type 4).
	if txType == types.SetCodeTxType {
		rpcTx.AuthorizationList = FormatAuthorizationList(tx.AuthorizationList())
	}

	// EIP-8141: FrameTx frames (type 6).
	if txType == types.FrameTxType {
		frames := tx.Frames()
		if frames != nil {
			rpcTx.Frames = make([]RPCFrame, len(frames))
			for i, f := range frames {
				rpcFrame := RPCFrame{
					Mode:     EncodeUint64(uint64(f.Mode)),
					GasLimit: EncodeUint64(f.GasLimit),
					Data:     EncodeBytes(f.Data),
				}
				if f.Target != nil {
					target := EncodeAddress(*f.Target)
					rpcFrame.Target = &target
				}
				rpcTx.Frames[i] = rpcFrame
			}
		}
	}

	return rpcTx
}

// FormatReceipt converts a receipt to its JSON-RPC representation.
// blockTimestamp is the timestamp of the containing block; if non-zero it is
// propagated to each log's BlockTimestamp so FormatLog can include the field.
func FormatReceipt(receipt *types.Receipt, tx *types.Transaction, blockTimestamp uint64) *RPCReceipt {
	rpcReceipt := &RPCReceipt{
		TransactionHash:   EncodeHash(receipt.TxHash),
		TransactionIndex:  EncodeUint64(uint64(receipt.TransactionIndex)),
		BlockHash:         EncodeHash(receipt.BlockHash),
		BlockNumber:       EncodeBigInt(receipt.BlockNumber),
		GasUsed:           EncodeUint64(receipt.GasUsed),
		CumulativeGasUsed: EncodeUint64(receipt.CumulativeGasUsed),
		Status:            EncodeUint64(receipt.Status),
		LogsBloom:         EncodeBloom(receipt.Bloom),
		Type:              EncodeUint64(uint64(receipt.Type)),
	}

	// EffectiveGasPrice
	if receipt.EffectiveGasPrice != nil {
		rpcReceipt.EffectiveGasPrice = EncodeBigInt(receipt.EffectiveGasPrice)
	} else {
		rpcReceipt.EffectiveGasPrice = "0x0"
	}

	// From and To
	if tx != nil {
		// For FrameTx (type 0x06), use FrameSender() since the sender is in the FrameTx struct.
		if tx.Type() == types.FrameTxType {
			frameSender := tx.FrameSender()
			if !frameSender.IsZero() {
				rpcReceipt.From = EncodeAddress(frameSender)
			}
		} else if sender := tx.Sender(); sender != nil {
			rpcReceipt.From = EncodeAddress(*sender)
		}
		if tx.To() != nil {
			to := EncodeAddress(*tx.To())
			rpcReceipt.To = &to
		}
	}

	// Contract address (only if contract creation)
	if !receipt.ContractAddress.IsZero() {
		ca := EncodeAddress(receipt.ContractAddress)
		rpcReceipt.ContractAddress = &ca
	}

	// Logs — copy each log to set BlockTimestamp (Geth extension).
	rpcReceipt.Logs = make([]*RPCLog, len(receipt.Logs))
	for i, log := range receipt.Logs {
		if blockTimestamp > 0 && log.BlockTimestamp == 0 {
			logCopy := *log
			logCopy.BlockTimestamp = blockTimestamp
			rpcReceipt.Logs[i] = FormatLog(&logCopy)
		} else {
			rpcReceipt.Logs[i] = FormatLog(log)
		}
	}
	if rpcReceipt.Logs == nil {
		rpcReceipt.Logs = []*RPCLog{}
	}

	// EIP-4844 blob transaction fields
	if receipt.BlobGasUsed > 0 {
		bgu := EncodeUint64(receipt.BlobGasUsed)
		rpcReceipt.BlobGasUsed = &bgu
	}
	if receipt.BlobGasPrice != nil {
		bgp := EncodeBigInt(receipt.BlobGasPrice)
		rpcReceipt.BlobGasPrice = &bgp
	}

	// EIP-8141 Frame transaction fields (type 0x06).
	// For FrameTx, ContractAddress is repurposed to hold the payer address.
	if receipt.Type == types.FrameTxType && !receipt.ContractAddress.IsZero() {
		payer := EncodeAddress(receipt.ContractAddress)
		rpcReceipt.Payer = &payer
		// Note: FrameResults are not stored in the standard Receipt.
		// To populate FrameResults, the caller would need to provide
		// the original FrameTxReceipt separately.
	}

	return rpcReceipt
}

// NewSuccessResponse creates a JSON-RPC 2.0 success response.
// result may be nil, in which case it is serialized as JSON null.
func NewSuccessResponse(id json.RawMessage, result interface{}) *Response {
	if result == nil {
		result = json.RawMessage("null")
	}
	return &Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

// NewErrorResponse creates a JSON-RPC 2.0 error response.
func NewErrorResponse(id json.RawMessage, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: message},
		ID:      id,
	}
}

// FormatLog converts a log to its JSON-RPC representation.
// If log.BlockTimestamp > 0 it is included as the Geth-extension blockTimestamp field.
func FormatLog(log *types.Log) *RPCLog {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = EncodeHash(topic)
	}
	rpcLog := &RPCLog{
		Address:          EncodeAddress(log.Address),
		Topics:           topics,
		Data:             EncodeBytes(log.Data),
		BlockNumber:      EncodeUint64(log.BlockNumber),
		TransactionHash:  EncodeHash(log.TxHash),
		TransactionIndex: EncodeUint64(uint64(log.TxIndex)),
		BlockHash:        EncodeHash(log.BlockHash),
		LogIndex:         EncodeUint64(uint64(log.Index)),
		Removed:          log.Removed,
	}
	if log.BlockTimestamp > 0 {
		ts := EncodeUint64(log.BlockTimestamp)
		rpcLog.BlockTimestamp = &ts
	}
	return rpcLog
}
