package geth

import (
	"fmt"
	"math/big"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	eth2030cfg "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/types"
)

// GethBlockProcessor executes ETH2030 blocks using go-ethereum's EVM and
// state transition engine. It produces correct state roots and gas accounting
// for all forks from Frontier through Prague, with optional ETH2030 custom
// precompile injection for Glamsterdam+ forks.
type GethBlockProcessor struct {
	config     *params.ChainConfig
	eth2030Cfg *eth2030cfg.ChainConfig // optional, for custom precompile injection
}

// NewGethBlockProcessor creates a processor for the given chain config.
func NewGethBlockProcessor(config *params.ChainConfig) *GethBlockProcessor {
	return &GethBlockProcessor{config: config}
}

// NewGethBlockProcessorWithEth2028 creates a processor that injects eth2030
// custom precompiles (NTT, NII, field precompiles, Glamsterdam repricing)
// based on the eth2030 fork schedule.
func NewGethBlockProcessorWithEth2028(config *params.ChainConfig, eth2030Cfg *eth2030cfg.ChainConfig) *GethBlockProcessor {
	return &GethBlockProcessor{config: config, eth2030Cfg: eth2030Cfg}
}

// MakeEVM constructs a go-ethereum EVM for the given block context, injecting
// eth2030 custom precompiles when the eth2030 config is present and the
// block timestamp meets the Glamsterdam+ fork threshold.
func (p *GethBlockProcessor) MakeEVM(
	blockCtx gethvm.BlockContext,
	statedb *gethstate.StateDB,
	blockNumber *big.Int,
	timestamp uint64,
) *gethvm.EVM {
	evm := gethvm.NewEVM(blockCtx, statedb, p.config, gethvm.Config{})
	if p.eth2030Cfg != nil {
		forkLevel := Eth2028ForkLevelFromConfig(p.eth2030Cfg, timestamp)
		if forkLevel > ForkLevelPrague {
			rules := p.config.Rules(blockNumber, true, timestamp)
			precompiles := InjectCustomPrecompiles(rules, forkLevel)
			evm.SetPrecompiles(precompiles)
		}
	}
	return evm
}

// ProcessBlock executes all transactions in an eth2030 block against a
// go-ethereum StateDB. Returns eth2030 receipts and the post-state root.
// The caller is responsible for committing the state after processing.
func (p *GethBlockProcessor) ProcessBlock(
	statedb *gethstate.StateDB,
	block *types.Block,
	getHash func(uint64) gethcommon.Hash,
) ([]*types.Receipt, types.Hash, error) {
	header := block.Header()
	blockCtx := MakeBlockContext(header, getHash)

	gasPool := gethcore.NewGasPool(header.GasLimit)

	var (
		receipts          []*types.Receipt
		cumulativeGasUsed uint64
	)

	for i, tx := range block.Transactions() {
		statedb.SetTxContext(ToGethHash(tx.Hash()), i, uint32(i+1))

		receipt, gasUsed, err := p.applyTransaction(statedb, header, tx, &blockCtx, gasPool)
		if err != nil {
			return nil, types.Hash{}, fmt.Errorf("tx %d [%v]: %w", i, tx.Hash(), err)
		}

		cumulativeGasUsed += gasUsed
		receipt.CumulativeGasUsed = cumulativeGasUsed
		receipt.TransactionIndex = uint(i)
		receipt.BlockHash = block.Hash()
		receipt.BlockNumber = new(big.Int).Set(header.Number)

		receipts = append(receipts, receipt)
	}

	// Assign global log indices across all receipts.
	var logIndex uint
	for _, receipt := range receipts {
		for _, log := range receipt.Logs {
			log.Index = logIndex
			logIndex++
		}
	}

	// EIP-4895: process withdrawals after all transactions.
	if p.config.IsShanghai(header.Number, header.Time) {
		for _, w := range block.Withdrawals() {
			if w == nil {
				continue
			}
			addr := ToGethAddress(w.Address)
			amount := new(big.Int).SetUint64(w.Amount)
			amount.Mul(amount, big.NewInt(1_000_000_000)) // Gwei to Wei
			statedb.AddBalance(addr, ToUint256(amount), tracing.BalanceChangeUnspecified)
		}
	}

	// Touch coinbase to ensure it exists in state.
	TouchCoinbase(statedb, blockCtx.Coinbase)

	// Commit state and compute root. Rules carry the EIP-158/161 emptiness
	// semantics that the old Commit(bool, bool) flags used to control.
	rules := p.config.Rules(header.Number, true, header.Time)
	root, err := statedb.Commit(rules, header.Number.Uint64())
	if err != nil {
		return nil, types.Hash{}, fmt.Errorf("commit state: %w", err)
	}

	return receipts, FromGethHash(root), nil
}

// applyTransaction converts an eth2030 transaction to a go-ethereum message
// and executes it using go-ethereum's state transition.
func (p *GethBlockProcessor) applyTransaction(
	statedb *gethstate.StateDB,
	header *types.Header,
	tx *types.Transaction,
	blockCtx *gethvm.BlockContext,
	gasPool *gethcore.GasPool,
) (*types.Receipt, uint64, error) {
	msg := txToGethMessage(tx, header.BaseFee)

	evm := p.MakeEVM(*blockCtx, statedb, header.Number, header.Time)

	snapshot := statedb.Snapshot()
	result, err := gethcore.ApplyMessage(evm, msg, gasPool)
	if err != nil {
		statedb.RevertToSnapshot(snapshot)
		return nil, 0, err
	}

	// Build eth2030 receipt.
	var status uint64
	if result.Failed() {
		status = types.ReceiptStatusFailed
	} else {
		status = types.ReceiptStatusSuccessful
	}

	receipt := types.NewReceipt(status, result.UsedGas)
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas
	receipt.Type = tx.Type()

	// Effective gas price.
	receipt.EffectiveGasPrice = effectiveGasPrice(msg, header.BaseFee).ToBig()

	// Contract creation address.
	if tx.To() == nil {
		contractAddr := FromGethAddress(gethcommon.Address{})
		if result.Err == nil {
			// Compute CREATE address from sender + nonce.
			contractAddr = FromGethAddress(gethcrypto.CreateAddress(msg.From, msg.Nonce))
		}
		receipt.ContractAddress = contractAddr
	}

	// EIP-4844 blob gas.
	if blobGas := tx.BlobGas(); blobGas > 0 {
		receipt.BlobGasUsed = blobGas
		if header.ExcessBlobGas != nil {
			receipt.BlobGasPrice = calcBlobFee(*header.ExcessBlobGas)
		}
	}

	// Collect logs from go-ethereum state and convert.
	gethLogs := statedb.GetLogs(ToGethHash(tx.Hash()), header.Number.Uint64(), ToGethHash(header.Hash()), header.Time)
	receipt.Logs = FromGethLogs(gethLogs)
	receipt.Bloom = types.LogsBloom(receipt.Logs)

	return receipt, result.UsedGas, nil
}

// txToGethMessage converts an eth2030 transaction to a go-ethereum Message.
func txToGethMessage(tx *types.Transaction, baseFee *big.Int) *gethcore.Message {
	var to *gethcommon.Address
	if txTo := tx.To(); txTo != nil {
		addr := ToGethAddress(*txTo)
		to = &addr
	}

	var from gethcommon.Address
	if sender := tx.Sender(); sender != nil {
		from = ToGethAddress(*sender)
	}

	value := tx.Value()
	if value == nil {
		value = new(big.Int)
	}

	// Convert access list.
	accessList := ToGethAccessList(tx.AccessList())

	// Convert blob hashes.
	blobHashes := toGethHashes(tx.BlobHashes())

	// Convert authorization list (EIP-7702).
	authList := toGethAuthList(tx.AuthorizationList())

	msg := &gethcore.Message{
		From:                  from,
		To:                    to,
		Nonce:                 tx.Nonce(),
		Value:                 u256FromBig(value),
		GasLimit:              tx.Gas(),
		Data:                  tx.Data(),
		AccessList:            accessList,
		BlobHashes:            blobHashes,
		BlobGasFeeCap:         u256FromBig(tx.BlobGasFeeCap()),
		SetCodeAuthorizations: authList,
	}

	// Set gas price fields based on tx type.
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType:
		gasPrice := u256FromBig(tx.GasPrice())
		msg.GasPrice = gasPrice
		msg.GasFeeCap = gasPrice
		msg.GasTipCap = gasPrice
	default:
		// EIP-1559+ transactions.
		msg.GasFeeCap = u256FromBig(tx.GasFeeCap())
		msg.GasTipCap = u256FromBig(tx.GasTipCap())
		if baseFee != nil {
			msg.GasPrice = effectiveGasPrice(msg, baseFee)
		} else {
			msg.GasPrice = new(uint256.Int).Set(msg.GasFeeCap)
		}
	}

	return msg
}

// toGethHashes converts eth2030 hashes to go-ethereum hashes.
func toGethHashes(hashes []types.Hash) []gethcommon.Hash {
	if hashes == nil {
		return nil
	}
	result := make([]gethcommon.Hash, len(hashes))
	for i, h := range hashes {
		result[i] = ToGethHash(h)
	}
	return result
}

// toGethAuthList converts eth2030 authorization list to go-ethereum format.
func toGethAuthList(auths []types.Authorization) []gethtypes.SetCodeAuthorization {
	if auths == nil {
		return nil
	}
	result := make([]gethtypes.SetCodeAuthorization, len(auths))
	for i, auth := range auths {
		var chainID uint256.Int
		if auth.ChainID != nil {
			chainID.SetFromBig(auth.ChainID)
		}
		var v uint8
		if auth.V != nil {
			v = uint8(auth.V.Uint64())
		}
		var r, s uint256.Int
		if auth.R != nil {
			r.SetFromBig(auth.R)
		}
		if auth.S != nil {
			s.SetFromBig(auth.S)
		}
		result[i] = gethtypes.SetCodeAuthorization{
			ChainID: chainID,
			Address: ToGethAddress(auth.Address),
			Nonce:   auth.Nonce,
			V:       v,
			R:       r,
			S:       s,
		}
	}
	return result
}

// u256FromBig converts a *big.Int to *uint256.Int, preserving nil.
func u256FromBig(x *big.Int) *uint256.Int {
	if x == nil {
		return nil
	}
	var z uint256.Int
	z.SetFromBig(x)
	return &z
}

// effectiveGasPrice computes the EIP-1559 effective gas price
// (min(baseFee + tipCap, feeCap)) as *uint256.Int.
func effectiveGasPrice(msg *gethcore.Message, baseFee *big.Int) *uint256.Int {
	if baseFee == nil || msg.GasFeeCap == nil {
		if msg.GasPrice != nil {
			return new(uint256.Int).Set(msg.GasPrice)
		}
		return uint256.NewInt(0)
	}
	price := u256FromBig(baseFee)
	if msg.GasTipCap != nil {
		price.Add(price, msg.GasTipCap)
	}
	if price.Gt(msg.GasFeeCap) {
		return new(uint256.Int).Set(msg.GasFeeCap)
	}
	return price
}

// calcBlobFee computes blob base fee from excess blob gas (EIP-4844).
func calcBlobFee(excessBlobGas uint64) *big.Int {
	if excessBlobGas == 0 {
		return big.NewInt(1)
	}
	return fakeExp(big.NewInt(1), new(big.Int).SetUint64(excessBlobGas), big.NewInt(3338477))
}

// fakeExp approximates factor * e^(numerator/denominator).
func fakeExp(factor, numerator, denominator *big.Int) *big.Int {
	i := big.NewInt(1)
	output := new(big.Int)
	accum := new(big.Int).Mul(factor, denominator)
	for accum.Sign() > 0 {
		output.Add(output, accum)
		accum.Mul(accum, numerator)
		accum.Div(accum, new(big.Int).Mul(denominator, i))
		i.Add(i, big.NewInt(1))
	}
	return output.Div(output, denominator)
}
