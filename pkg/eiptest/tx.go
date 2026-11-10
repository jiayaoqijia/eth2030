package eiptest

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// Transaction builders for every supported tx envelope type. Builders that
// take an *Account produce fully signed transactions; the Unsigned variants
// produce transactions with a pre-set sender (fast path accepted by the
// state processor, matching the existing E2E convention).

func sign(inner types.TxData, key *ecdsa.PrivateKey) *types.Transaction {
	// Zero sig fields first so the signing hash is canonical (typed encoding
	// excludes V/R/S; legacy paths use the zeroed placeholders).
	setVRS(inner, new(big.Int), new(big.Int), new(big.Int))
	tx := types.NewTransaction(inner)
	sigHash := tx.SigningHash()
	sig, err := crypto.Sign(sigHash[:], key)
	if err != nil {
		panic(err) // framework misuse: invalid key or hash
	}
	v := new(big.Int).SetUint64(uint64(sig[64]))
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	setVRS(inner, v, r, s)
	signed := types.NewTransaction(inner)
	signed.SetSender(crypto.PubkeyToAddress(key.PublicKey))
	return signed
}

func fillVRS(inner types.TxData) {
	setVRS(inner, new(big.Int), new(big.Int), new(big.Int))
}

func setVRS(inner types.TxData, v, r, s *big.Int) {
	switch t := inner.(type) {
	case *types.LegacyTx:
		t.V, t.R, t.S = v, r, s
	case *types.AccessListTx:
		t.V, t.R, t.S = v, r, s
	case *types.DynamicFeeTx:
		t.V, t.R, t.S = v, r, s
	case *types.BlobTx:
		t.V, t.R, t.S = v, r, s
	case *types.SetCodeTx:
		t.V, t.R, t.S = v, r, s
	}
}

// ---------------------------------------------------------------------------
// Type 0x00 — legacy
// ---------------------------------------------------------------------------

// UnsignedLegacy builds an unsigned legacy tx with the sender pre-set.
func UnsignedLegacy(sender types.Address, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, gasPrice *big.Int) *types.Transaction {
	inner := &types.LegacyTx{Nonce: nonce, To: to, Value: value, Data: data, Gas: gas, GasPrice: gasPrice}
	fillVRS(inner)
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// Legacy builds a signed EIP-155 legacy tx.
func Legacy(chainID uint64, from *Account, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, gasPrice *big.Int) *types.Transaction {
	inner := &types.LegacyTx{Nonce: nonce, To: to, Value: value, Data: data, Gas: gas, GasPrice: gasPrice}
	fillVRS(inner)
	sigHash := eip155SigningHash(inner, chainID)
	sig, err := crypto.Sign(sigHash[:], from.Key)
	if err != nil {
		panic(err)
	}
	recid := sig[64]
	v := new(big.Int).SetUint64(uint64(recid) + chainID*2 + 35)
	inner.V = v
	inner.R = new(big.Int).SetBytes(sig[0:32])
	inner.S = new(big.Int).SetBytes(sig[32:64])
	signed := types.NewTransaction(inner)
	signed.SetSender(crypto.PubkeyToAddress(from.Key.PublicKey))
	return signed
}

// eip155SigningHash computes Keccak256(RLP([nonce, gasPrice, gas, to, value, data, chainID, 0, 0])).
func eip155SigningHash(tx *types.LegacyTx, chainID uint64) types.Hash {
	toBytes := []byte{}
	if tx.To != nil {
		toBytes = tx.To[:]
	}
	enc := func(v interface{}) []byte {
		b, err := rlp.EncodeToBytes(v)
		if err != nil {
			panic(err)
		}
		return b
	}
	payload := [][]byte{
		enc(tx.Nonce), enc(tx.GasPrice), enc(tx.Gas), enc(toBytes),
		enc(tx.Value), enc(tx.Data), enc(chainID), enc(uint(0)), enc(uint(0)),
	}
	list, err := rlp.EncodeToBytes(payload)
	if err != nil {
		panic(err)
	}
	return keccak256(list)
}

// ---------------------------------------------------------------------------
// Type 0x01 — access list
// ---------------------------------------------------------------------------

// UnsignedAccessList builds an unsigned EIP-2930 tx with the sender pre-set.
func UnsignedAccessList(sender types.Address, chainID *big.Int, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, gasFeeCap, gasTipCap *big.Int, accessList types.AccessList) *types.Transaction {
	inner := &types.AccessListTx{
		ChainID: chainID, Nonce: nonce, To: to, Value: value, Data: data,
		Gas: gas, GasPrice: gasFeeCap, AccessList: accessList,
	}
	fillVRS(inner)
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Type 0x02 — dynamic fee (EIP-1559)
// ---------------------------------------------------------------------------

// UnsignedDynamicFee builds an unsigned EIP-1559 tx with the sender pre-set.
func UnsignedDynamicFee(sender types.Address, chainID *big.Int, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
	inner := &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce, To: to, Value: value, Data: data,
		Gas: gas, GasFeeCap: gasFeeCap, GasTipCap: gasTipCap,
	}
	fillVRS(inner)
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// DynamicFee builds a signed EIP-1559 tx.
func DynamicFee(chainID *big.Int, from *Account, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
	inner := &types.DynamicFeeTx{
		ChainID: chainID, Nonce: nonce, To: to, Value: value, Data: data,
		Gas: gas, GasFeeCap: gasFeeCap, GasTipCap: gasTipCap,
	}
	return sign(inner, from.Key)
}

// ---------------------------------------------------------------------------
// Type 0x03 — blob (EIP-4844)
// ---------------------------------------------------------------------------

// BlobHash builds a well-formed blob versioned hash (0x01 version byte).
func BlobHash(index byte) types.Hash {
	h := types.Hash{}
	h[0] = 0x01
	h[1] = index
	return h
}

// UnsignedBlob builds an unsigned EIP-4844 blob tx with the sender pre-set.
// Hashes must carry the 0x01 version byte (use BlobHash).
func UnsignedBlob(sender types.Address, chainID *big.Int, nonce uint64, to types.Address, value *big.Int, gas uint64, gasFeeCap, gasTipCap, blobFeeCap *big.Int, hashes []types.Hash) *types.Transaction {
	inner := &types.BlobTx{
		ChainID: chainID, Nonce: nonce, To: to, Value: value, Data: nil,
		Gas: gas, GasFeeCap: gasFeeCap, GasTipCap: gasTipCap,
		BlobFeeCap: blobFeeCap, BlobHashes: hashes,
	}
	fillVRS(inner)
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Type 0x04 — set code (EIP-7702)
// ---------------------------------------------------------------------------

// Auth builds a signed EIP-7702 authorization (delegation of authority to
// `delegate`). The signature is over keccak256(0x05 || rlp([chainID, address, nonce])).
func Auth(chainID *big.Int, authority *Account, delegate types.Address, nonce uint64) types.Authorization {
	auth := types.Authorization{ChainID: chainID, Address: delegate, Nonce: nonce}
	list, err := rlp.EncodeToBytes([]interface{}{auth.ChainID, auth.Address, auth.Nonce})
	if err != nil {
		panic(err)
	}
	h := keccak256(append([]byte{0x05}, list...))
	sig, err := crypto.Sign(h[:], authority.Key)
	if err != nil {
		panic(err)
	}
	auth.V = new(big.Int).SetUint64(uint64(sig[64]))
	auth.R = new(big.Int).SetBytes(sig[0:32])
	auth.S = new(big.Int).SetBytes(sig[32:64])
	return auth
}

// UnsignedSetCode builds an unsigned EIP-7702 set-code tx with the sender pre-set.
func UnsignedSetCode(sender types.Address, chainID *big.Int, nonce uint64, to types.Address, gas uint64, gasFeeCap, gasTipCap *big.Int, auths []types.Authorization) *types.Transaction {
	inner := &types.SetCodeTx{
		ChainID: chainID, Nonce: nonce, To: to, Gas: gas,
		GasFeeCap: gasFeeCap, GasTipCap: gasTipCap,
		AuthorizationList: auths, Value: new(big.Int),
	}
	fillVRS(inner)
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// SetCode builds a signed EIP-7702 set-code tx.
func SetCode(chainID *big.Int, from *Account, nonce uint64, to types.Address, gas uint64, gasFeeCap, gasTipCap *big.Int, auths []types.Authorization) *types.Transaction {
	inner := &types.SetCodeTx{
		ChainID: chainID, Nonce: nonce, To: to, Gas: gas,
		GasFeeCap: gasFeeCap, GasTipCap: gasTipCap,
		AuthorizationList: auths, Value: new(big.Int),
	}
	return sign(inner, from.Key)
}

// ---------------------------------------------------------------------------
// Type 0x05 — native AA (EIP-7701)
// ---------------------------------------------------------------------------

// UnsignedAA builds an unsigned EIP-7701 AA tx with the sender pre-set. The
// phase gas limits carry the total gas budget (AABaseCost is added on top).
func UnsignedAA(sender types.Address, chainID *big.Int, nonce uint64, executionData []byte, gas uint64, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
	inner := &types.AATx{
		ChainID: chainID, Nonce: nonce, Sender: sender,
		SenderExecutionData: executionData,
		SenderExecutionGas:  gas,
		MaxFeePerGas:        gasFeeCap, MaxPriorityFeePerGas: gasTipCap,
	}
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Type 0x06 — frame tx (EIP-8141)
// ---------------------------------------------------------------------------

// UnsignedFrame builds an unsigned EIP-8141 frame tx with the sender pre-set.
func UnsignedFrame(sender types.Address, chainID *big.Int, frameNonce *big.Int, gas uint64, gasFeeCap, gasTipCap *big.Int, frames []types.Frame) *types.Transaction {
	inner := &types.FrameTx{
		ChainID: chainID, Nonce: frameNonce, Sender: sender,
		Frames: frames,
		MaxFeePerGas: gasFeeCap, MaxPriorityFeePerGas: gasTipCap,
	}
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Type 0x08 — local tx
// ---------------------------------------------------------------------------

// UnsignedLocal builds an unsigned local tx (type 0x08) with the sender pre-set.
func UnsignedLocal(sender types.Address, chainID *big.Int, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, gasFeeCap, gasTipCap *big.Int) *types.Transaction {
	var toPtr *types.Address
	if to != nil {
		c := *to
		toPtr = &c
	}
	inner := &types.LocalTx{
		ChainID_: chainID, Nonce_: nonce, To_: toPtr, Value_: value, Data_: data,
		Gas_: gas, GasFeeCap_: gasFeeCap, GasTipCap_: gasTipCap,
	}
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Type 0x09 — multidimensional fee tx
// ---------------------------------------------------------------------------

// UnsignedMultiDimFee builds an unsigned multidim fee tx (type 0x09) with the
// sender pre-set. Fee vectors are indexed [execution=0, blob=1, calldata=2].
func UnsignedMultiDimFee(sender types.Address, chainID *big.Int, nonce uint64, to *types.Address, value *big.Int, data []byte, gas uint64, maxFees, priorityFees [3]*big.Int) *types.Transaction {
	inner := &types.MultiDimFeeTx{
		ChainID: chainID, Nonce: nonce, To: to, Value: value, Data: data,
		GasLimit: gas, MaxFeesPerGas: maxFees, PriorityFeesPerGas: priorityFees,
	}
	tx := types.NewTransaction(inner)
	tx.SetSender(sender)
	return tx
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// keccak256 computes the Keccak-256 hash of data using the project's sha3 package.
func keccak256(data []byte) types.Hash {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var out types.Hash
	copy(out[:], h.Sum(nil))
	return out
}

// ToPtr returns a pointer to the given address (helper for tx builders).
func ToPtr(a types.Address) *types.Address { return &a }

// Wei returns wei as *big.Int.
func Wei(n int64) *big.Int { return big.NewInt(n) }

// Uint64ToHash converts a uint64 to a left-padded 32-byte hash (slot encoding).
func Uint64ToHash(v uint64) types.Hash {
	var h types.Hash
	h[31] = byte(v)
	h[30] = byte(v >> 8)
	h[29] = byte(v >> 16)
	h[28] = byte(v >> 24)
	h[27] = byte(v >> 32)
	h[26] = byte(v >> 40)
	h[25] = byte(v >> 48)
	h[24] = byte(v >> 56)
	return h
}
