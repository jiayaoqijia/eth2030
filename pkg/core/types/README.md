# core/types — Core Ethereum data types

[← core](../README.md)

## Overview

Package `types` defines the canonical data structures used throughout the ETH2030 client: blocks, headers, transactions (seven types), receipts, logs, withdrawals, and EL requests. It implements RLP encoding for all wire-format types and standalone SSZ codecs for the published EIP-6404 and EIP-7807 drafts.

The package covers a wide range of EIPs including EIP-1559 dynamic fees, EIP-2930 access lists, EIP-4844 blob transactions, EIP-4895 withdrawals, EIP-7685 EL requests, EIP-7702 SetCode (AA), EIP-8141 frame transactions, and PQ (post-quantum) transactions.

## Functionality

### Header

```go
type Header struct {
    ParentHash, UncleHash, Coinbase, Root, TxHash, ReceiptHash Hash
    Bloom Bloom; Difficulty, Number *big.Int
    GasLimit, GasUsed, Time uint64; Extra []byte
    BaseFee *big.Int                // EIP-1559
    WithdrawalsHash *Hash           // EIP-4895
    BlobGasUsed, ExcessBlobGas *uint64  // EIP-4844
    ParentBeaconRoot *Hash          // EIP-4788
    RequestsHash *Hash              // EIP-7685
    BlockAccessListHash *Hash       // EIP-7928
    GasLimitVec, GasUsedVec, ExcessGasVec *[3]uint64  // EIP-7706
    // ... additional Hegotá/I+ fields
}
```

### Block and Body

```go
type Block struct { ... }
type Body struct {
    Transactions []*Transaction
    Uncles       []*Header
    Withdrawals  []*Withdrawal
}
func NewBlock(header *Header, body *Body) *Block
func (b *Block) Header() *Header
func (b *Block) Transactions() []*Transaction
func (b *Block) Hash() Hash
```

### Transactions — seven types

| Constant | Value | Type struct | EIP |
|----------|-------|-------------|-----|
| `LegacyTxType` | 0x00 | `LegacyTx` | — |
| `AccessListTxType` | 0x01 | `AccessListTx` | EIP-2930 |
| `DynamicFeeTxType` | 0x02 | `DynamicFeeTx` | EIP-1559 |
| `BlobTxType` | 0x03 | `BlobTx` | EIP-4844 |
| `SetCodeTxType` | 0x04 | `SetCodeTx` | EIP-7702 |
| `FrameTxType` | 0x06 | `FrameTx` | EIP-8141 |
| `PQTransactionType` | 0x07 | `PQTransaction` | PQC |

```go
type Transaction struct { inner TxData; ... }
type TxData interface { txType() byte; chainID() *big.Int; ... }
func (tx *Transaction) Type() byte
func (tx *Transaction) Hash() Hash
func (tx *Transaction) Gas() uint64
func (tx *Transaction) Value() *big.Int
func (tx *Transaction) Sender() *Address
```

### Receipt

```go
type Receipt struct {
    Type              uint8
    PostState         []byte
    Status            uint64
    CumulativeGasUsed uint64
    Bloom             Bloom
    Logs              []*Log
    TxHash, ContractAddress Hash
    GasUsed uint64
}
```

### Supporting types

- `Bloom` — 2048-bit log bloom filter with `Add` / `Test`
- `Log`, `LogIndex` — event log with EIP-7745 log index
- `Withdrawal` — EIP-4895 beacon chain withdrawal
- `Request` — EIP-7685 EL request (deposits, withdrawals, consolidations)
- `AccessList`, `AccessTuple` — EIP-2929 / EIP-2930
- `Authorization` — EIP-7702 SetCode auth entry
- `TxAssertion` — transaction-level assertions
- `MultidimGas` — EIP-7706 3-dimensional gas vector
- Execution SSZ: `TransactionToSSZ`, `SSZToTransaction`, `TransactionSSZRoot`,
  `TransactionsSSZRoot`, `ExecutionPayloadSSZ` and `ExecutionHeaderSSZ`

## Usage

```go
tx := &types.Transaction{}
// Decode from RLP wire format:
if err := rlp.DecodeBytes(raw, tx); err != nil { ... }

block := types.NewBlock(header, &types.Body{Transactions: txs})
fmt.Println(block.Hash(), block.Number())
```

## Execution SSZ draft versions

The transaction codec follows [EIP-6404](https://eips.ethereum.org/EIPS/eip-6404)
and [EIP-7932](https://eips.ethereum.org/EIPS/eip-7932) at Ethereum EIPs revision
`bd93d240b72702450920eeeadc2ce593626bfd8a`. It uses the published secp256k1
algorithm selector `0x00` and the ten compatible-union payload variants.
Transaction roots use typed field Merkleization. Reconstructing an RLP
transaction preserves its original chain ID, signature parity and identity.
Transaction conversion requires an EIP-7932-compatible signature.

[The published EIP-7807 draft](https://eips.ethereum.org/EIPS/eip-7807)
at that revision defines its own payload container. It commits to existing
transaction, receipt and withdrawal envelope bytes. Use
`NewExecutionPayloadSSZ` with the actual receipts and explicit consensus inputs,
then `MarshalSSZ`, `UnmarshalSSZ` and `HashTreeRoot` on the payload or header
summary. The draft codecs are available independently of fork activation.
The production chain continues to use its configured RLP block and transaction
identities, Engine API formats and RPC rules.

Independent reference vectors live in
[`testdata/execution_ssz`](testdata/execution_ssz/). The generator uses the
schema declarations from the published EIPs and pinned Electra specification
with `ethereum/remerkleable` revision `2f0baeef0082d4278acaef7d822deb7009d7db7e`.
Pass the two EIP Markdown files and
`refs/consensus-specs/specs/electra/beacon-chain.md` to `generate.py` with the
reference checkout on `PYTHONPATH` to reproduce the JSON.
