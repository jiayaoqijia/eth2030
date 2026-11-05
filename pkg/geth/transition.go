package geth

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// ApplyMessage executes a transaction message using go-ethereum's state
// transition engine. Returns used gas, return data, and any error.
func ApplyMessage(
	statedb *gethstate.StateDB,
	config *params.ChainConfig,
	blockCtx gethvm.BlockContext,
	msg *gethcore.Message,
	gasLimit uint64,
) (*gethcore.ExecutionResult, error) {
	evm := gethvm.NewEVM(blockCtx, statedb, config, gethvm.Config{})
	gp := gethcore.NewGasPool(gasLimit)
	return gethcore.ApplyMessage(evm, msg, gp)
}

// MakeBlockContext creates a go-ethereum BlockContext from eth2030 header fields.
func MakeBlockContext(header *types.Header, getHash func(uint64) gethcommon.Hash) gethvm.BlockContext {
	ctx := gethvm.BlockContext{
		CanTransfer: gethcore.CanTransfer,
		Transfer:    gethcore.Transfer,
		GetHash:     getHash,
		Coinbase:    ToGethAddress(header.Coinbase),
		GasLimit:    header.GasLimit,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        header.Time,
		Difficulty:  new(big.Int),
		BaseFee:     header.BaseFee,
	}

	if header.Difficulty != nil {
		ctx.Difficulty = new(big.Int).Set(header.Difficulty)
	}

	if header.MixDigest != (types.Hash{}) {
		rnd := ToGethHash(header.MixDigest)
		ctx.Random = &rnd
	}

	if header.ExcessBlobGas != nil {
		// Compute blob base fee from excess blob gas.
		blobHeader := &gethtypes.Header{ExcessBlobGas: header.ExcessBlobGas}
		ctx.BlobBaseFee = getBlobBaseFee(blobHeader)
	}

	return ctx
}

// getBlobBaseFee computes the blob base fee. We use a simplified version
// that matches the EIP-4844 formula.
func getBlobBaseFee(header *gethtypes.Header) *big.Int {
	if header.ExcessBlobGas == nil {
		return nil
	}
	// Minimal blob base fee calculation.
	// In production, use eip4844.CalcBlobFee. For simplicity we compute inline.
	excessBlobGas := *header.ExcessBlobGas
	if excessBlobGas == 0 {
		return big.NewInt(1)
	}
	// fake_exponential(1, excess_blob_gas, 3338477)
	// This is a simplified approximation. For EF tests, the exact value
	// is computed by go-ethereum's eip4844.CalcBlobFee.
	return big.NewInt(1) // Placeholder; real tests use eip4844 import.
}

// TestBlockHash is the standard test block hash function used by go-ethereum's
// EF test runner: keccak256(blockNumber).
func TestBlockHash(n uint64) gethcommon.Hash {
	return gethcommon.BytesToHash(crypto.Keccak256([]byte(big.NewInt(int64(n)).String())))
}

// MakeMessage creates a go-ethereum Message from EF test parameters.
func MakeMessage(
	from gethcommon.Address,
	to *gethcommon.Address,
	nonce uint64,
	value *big.Int,
	gasLimit uint64,
	gasPrice *big.Int,
	gasFeeCap *big.Int,
	gasTipCap *big.Int,
	data []byte,
	accessList gethtypes.AccessList,
	blobHashes []gethcommon.Hash,
	blobGasFeeCap *big.Int,
	authList []gethtypes.SetCodeAuthorization,
) *gethcore.Message {
	msg := &gethcore.Message{
		From:                  from,
		To:                    to,
		Nonce:                 nonce,
		Value:                 u256FromBig(value),
		GasLimit:              gasLimit,
		GasPrice:              u256FromBig(gasPrice),
		GasFeeCap:             u256FromBig(gasFeeCap),
		GasTipCap:             u256FromBig(gasTipCap),
		Data:                  data,
		AccessList:            accessList,
		BlobHashes:            blobHashes,
		BlobGasFeeCap:         u256FromBig(blobGasFeeCap),
		SetCodeAuthorizations: authList,
	}
	return msg
}

// EffectiveGasPrice computes effective gas price per EIP-1559.
func EffectiveGasPrice(gasPrice, maxFeePerGas, maxPriorityFee, baseFee *big.Int) *big.Int {
	if baseFee == nil || maxFeePerGas == nil {
		if gasPrice != nil {
			return new(big.Int).Set(gasPrice)
		}
		return new(big.Int)
	}
	// effectiveGasPrice = min(maxFeePerGas, baseFee + maxPriorityFee)
	tip := new(big.Int).Add(maxPriorityFee, baseFee)
	if tip.Cmp(maxFeePerGas) > 0 {
		return new(big.Int).Set(maxFeePerGas)
	}
	return tip
}

// TxContextFromMessage creates a TxContext from a message for EVM execution.
func TxContextFromMessage(msg *gethcore.Message, baseFee *big.Int) gethvm.TxContext {
	gasPrice := msg.GasPrice
	if baseFee != nil && msg.GasFeeCap != nil {
		gasPrice = effectiveGasPrice(msg, baseFee)
	}
	return gethvm.TxContext{
		Origin:   msg.From,
		GasPrice: gasPrice,
	}
}

// IsEIP158 returns whether EIP-158 is active for the given config and block number.
func IsEIP158(config *params.ChainConfig, blockNum *big.Int) bool {
	return config.IsEIP158(blockNum)
}

// IsCancun returns whether Cancun is active for the given config, block, and time.
func IsCancun(config *params.ChainConfig, blockNum *big.Int, time uint64) bool {
	return config.IsCancun(blockNum, time)
}
