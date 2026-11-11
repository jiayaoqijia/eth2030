package eftest

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	gethtracing "github.com/ethereum/go-ethereum/core/tracing"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	gethparams "github.com/ethereum/go-ethereum/params"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/geth"
)

// GethRunResult holds the outcome of running a single subtest via go-ethereum.
type GethRunResult struct {
	Name         string
	Fork         string
	Index        int
	Passed       bool
	ExpectedRoot types.Hash
	GotRoot      types.Hash
	ExpectedLogs types.Hash
	GotLogs      types.Hash
	Error        error
}

// Note: StateTest.RunWithGeth is not provided because the old stTransaction
// struct lacks AccessLists, BlobVersionedHashes, and AuthorizationList fields.
// Use GethStateTest (via LoadGethTests) for full EF test execution.

// calcBlobFee computes blob base fee. We use the eip4844 package if available,
// otherwise a minimal inline implementation.
func calcBlobFee(config *gethparams.ChainConfig, header *gethtypes.Header) *big.Int {
	if header.ExcessBlobGas == nil {
		return big.NewInt(1)
	}
	// Import from consensus/misc/eip4844 for correct calculation.
	// For now, use a minimal fake_exponential implementation.
	return fakeExponential(big.NewInt(1), new(big.Int).SetUint64(*header.ExcessBlobGas), big.NewInt(3338477))
}

// fakeExponential implements the EIP-4844 fake exponential function.
func fakeExponential(factor, numerator, denominator *big.Int) *big.Int {
	i := big.NewInt(1)
	output := new(big.Int)
	numeratorAccum := new(big.Int).Mul(factor, denominator)
	for numeratorAccum.Sign() > 0 {
		output.Add(output, numeratorAccum)
		numeratorAccum.Mul(numeratorAccum, numerator)
		numeratorAccum.Div(numeratorAccum, new(big.Int).Mul(denominator, i))
		i.Add(i, big.NewInt(1))
	}
	return output.Div(output, denominator)
}

// parseStorage converts string→string storage map to Hash→Hash.
func parseStorage(m map[string]string) map[types.Hash]types.Hash {
	if m == nil {
		return nil
	}
	result := make(map[types.Hash]types.Hash, len(m))
	for k, v := range m {
		result[hexToHash(k)] = hexToHash(v)
	}
	return result
}

// parseGethAccessList converts the EF test JSON access list format to go-ethereum's.
func parseGethAccessList(entries []stAccessListEntry) gethtypes.AccessList {
	result := make(gethtypes.AccessList, len(entries))
	for i, entry := range entries {
		keys := make([]gethcommon.Hash, len(entry.StorageKeys))
		for j, k := range entry.StorageKeys {
			keys[j] = gethcommon.HexToHash(k)
		}
		result[i] = gethtypes.AccessTuple{
			Address:     gethcommon.HexToAddress(entry.Address),
			StorageKeys: keys,
		}
	}
	return result
}

// hexToBigIntNilable returns nil for empty strings.
func hexToBigIntNilable(s string) *big.Int {
	if s == "" {
		return nil
	}
	return hexToBigInt(s)
}

// gethRlpHash computes the keccak256 of the RLP-encoded value.
func gethRlpHash(x interface{}) gethcommon.Hash {
	hw := gethcrypto.NewKeccakState()
	gethrlp.Encode(hw, x)
	var h gethcommon.Hash
	hw.Read(h[:])
	return h
}

// LoadGethStateTests loads and parses EF test JSON files for use with RunWithGeth.
// This is the same as LoadStateTests but returns the same type.
func LoadGethStateTests(path string) (map[string]*StateTest, error) {
	return LoadStateTests(path)
}

// stAccessListEntry represents a single access list entry in the JSON.
type stAccessListEntry struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

// stAuthorizationEntry represents a single EIP-7702 authorization in the JSON.
type stAuthorizationEntry struct {
	ChainID string `json:"chainId"`
	Address string `json:"address"`
	Nonce   string `json:"nonce"`
	V       string `json:"v"`
	R       string `json:"r"`
	S       string `json:"s"`
}

// Extended stTransaction fields needed for geth runner.
// We parse these from the raw JSON since the existing stTransaction struct
// doesn't have all fields.
type gethStTransaction struct {
	Data                 []string               `json:"data"`
	GasLimit             []string               `json:"gasLimit"`
	Value                []string               `json:"value"`
	GasPrice             string                 `json:"gasPrice"`
	Nonce                string                 `json:"nonce"`
	To                   string                 `json:"to"`
	Sender               string                 `json:"sender"`
	SecretKey            string                 `json:"secretKey"`
	MaxFeePerGas         string                 `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string                 `json:"maxPriorityFeePerGas"`
	AccessLists          [][]stAccessListEntry  `json:"accessLists"`
	BlobVersionedHashes  []string               `json:"blobVersionedHashes"`
	MaxFeePerBlobGas     string                 `json:"maxFeePerBlobGas"`
	AuthorizationList    []stAuthorizationEntry `json:"authorizationList"`
}

// gethStJSON extends stJSON with the full transaction format.
type gethStJSON struct {
	Env  stEnv                    `json:"env"`
	Pre  map[string]stAccount     `json:"pre"`
	Tx   gethStTransaction        `json:"transaction"`
	Post map[string][]stPostState `json:"post"`
}

// GethStateTest wraps a parsed state test with the full transaction format.
type GethStateTest struct {
	Name string
	json gethStJSON
}

// Subtests returns all fork/index permutations.
func (st *GethStateTest) Subtests() []StateSubtest {
	var subs []StateSubtest
	for fork, posts := range st.json.Post {
		for i := range posts {
			subs = append(subs, StateSubtest{Fork: fork, Index: i})
		}
	}
	return subs
}

// RunWithGeth executes a single subtest using go-ethereum's execution engine.
func (st *GethStateTest) RunWithGeth(subtest StateSubtest) *GethRunResult {
	result := &GethRunResult{
		Name:  st.Name,
		Fork:  subtest.Fork,
		Index: subtest.Index,
	}

	posts, ok := st.json.Post[subtest.Fork]
	if !ok || subtest.Index >= len(posts) {
		result.Error = fmt.Errorf("no post state for fork %s index %d", subtest.Fork, subtest.Index)
		return result
	}
	post := posts[subtest.Index]
	result.ExpectedRoot = hexToHash(post.Hash)
	result.ExpectedLogs = hexToHash(post.Logs)

	config, err := geth.EFTestChainConfig(subtest.Fork)
	if err != nil {
		result.Error = err
		return result
	}

	// Build pre-state.
	preAccounts := make(map[string]geth.PreAccount)
	for addrHex, acct := range st.json.Pre {
		preAccounts[addrHex] = geth.PreAccount{
			Balance: hexToBigInt(acct.Balance),
			Nonce:   hexToUint64(acct.Nonce),
			Code:    hexToBytes(acct.Code),
			Storage: parseStorage(acct.Storage),
		}
	}

	stState, err := geth.MakePreState(preAccounts)
	if err != nil {
		result.Error = fmt.Errorf("make pre-state: %w", err)
		return result
	}
	defer stState.Close()

	// Build message.
	msg, err := st.buildGethMessage(post, config)
	if err != nil {
		if post.ExpectException != "" {
			result.Passed = true
			return result
		}
		result.Error = fmt.Errorf("build message: %w", err)
		return result
	}

	// Build block context and execute via processor (injects custom precompiles when active).
	blockCtx := st.buildGethBlockContext(config)
	evmBlockNum := new(big.Int).SetUint64(hexToUint64(st.json.Env.CurrentNumber))
	evmTimestamp := hexToUint64(st.json.Env.CurrentTimestamp)
	processor := geth.NewGethBlockProcessorWithEth2028(config, nil)
	evm := processor.MakeEVM(blockCtx, stState.StateDB, evmBlockNum, evmTimestamp)
	gasPool := gethcore.NewGasPool(hexToUint64(st.json.Env.CurrentGasLimit))

	_, applyErr := gethcore.ApplyMessage(evm, msg, gasPool)

	if post.ExpectException != "" {
		if applyErr != nil {
			result.Passed = true
		} else {
			result.Error = fmt.Errorf("expected exception %q but tx succeeded", post.ExpectException)
		}
		return result
	}

	if applyErr != nil {
		result.Error = fmt.Errorf("unexpected transaction error: %w", applyErr)
		return result
	}

	// Touch coinbase.
	coinbase := gethcommon.HexToAddress(st.json.Env.CurrentCoinbase)
	stState.StateDB.AddBalance(coinbase, new(uint256.Int), gethtracing.BalanceChangeUnspecified)

	// Commit.
	blockNum := hexToUint64(st.json.Env.CurrentNumber)
	rules := config.Rules(new(big.Int).SetUint64(blockNum), true, hexToUint64(st.json.Env.CurrentTimestamp))
	root, err := stState.StateDB.Commit(rules, blockNum)
	if err != nil {
		result.Error = fmt.Errorf("commit state: %w", err)
		return result
	}
	result.GotRoot = geth.FromGethHash(root)

	// Logs hash.
	logsHash := gethRlpHash(stState.StateDB.Logs())
	result.GotLogs = geth.FromGethHash(logsHash)

	// Compare.
	if result.ExpectedRoot != (types.Hash{}) {
		result.Passed = (result.GotRoot == result.ExpectedRoot)
		if !result.Passed {
			result.Error = fmt.Errorf("state root mismatch: expected %s, got %s",
				result.ExpectedRoot.Hex(), result.GotRoot.Hex())
		}
	} else {
		result.Passed = true
	}

	if result.Passed && result.ExpectedLogs != (types.Hash{}) {
		if result.GotLogs != result.ExpectedLogs {
			result.Passed = false
			result.Error = fmt.Errorf("logs hash mismatch: expected %s, got %s",
				result.ExpectedLogs.Hex(), result.GotLogs.Hex())
		}
	}

	return result
}

// buildGethMessage creates a go-ethereum Message (same pattern for GethStateTest).
func (st *GethStateTest) buildGethMessage(post stPostState, config *gethparams.ChainConfig) (*gethcore.Message, error) {
	tx := st.json.Tx
	indexes := post.Indexes

	var from gethcommon.Address
	if tx.Sender != "" {
		from = gethcommon.HexToAddress(tx.Sender)
	} else if tx.SecretKey != "" {
		keyBytes := hexToBytes(tx.SecretKey)
		key, err := gethcrypto.ToECDSA(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %v", err)
		}
		from = gethcrypto.PubkeyToAddress(key.PublicKey)
	}

	var to *gethcommon.Address
	if tx.To != "" {
		addr := gethcommon.HexToAddress(tx.To)
		to = &addr
	}

	if indexes.Data >= len(tx.Data) {
		return nil, fmt.Errorf("data index out of bounds")
	}
	if indexes.Gas >= len(tx.GasLimit) {
		return nil, fmt.Errorf("gas index out of bounds")
	}
	if indexes.Value >= len(tx.Value) {
		return nil, fmt.Errorf("value index out of bounds")
	}

	data, _ := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(tx.Data[indexes.Data], "0x"), "0X"))
	gasLimit := hexToUint64(tx.GasLimit[indexes.Gas])
	value := new(big.Int)
	if vhex := tx.Value[indexes.Value]; vhex != "0x" && vhex != "" {
		value = hexToBigInt(vhex)
	}

	var accessList gethtypes.AccessList
	if tx.AccessLists != nil && indexes.Data < len(tx.AccessLists) && tx.AccessLists[indexes.Data] != nil {
		accessList = parseGethAccessList(tx.AccessLists[indexes.Data])
	}

	gasPrice := hexToBigIntNilable(tx.GasPrice)
	maxFee := hexToBigIntNilable(tx.MaxFeePerGas)
	maxPriority := hexToBigIntNilable(tx.MaxPriorityFeePerGas)

	var baseFee *big.Int
	if config.IsLondon(new(big.Int)) {
		baseFee = hexToBigIntNilable(st.json.Env.CurrentBaseFee)
		if baseFee == nil {
			baseFee = big.NewInt(0x0a)
		}
	}

	if baseFee != nil {
		if maxFee == nil {
			maxFee = gasPrice
		}
		if maxFee == nil {
			maxFee = new(big.Int)
		}
		if maxPriority == nil {
			maxPriority = maxFee
		}
		gasPrice = new(big.Int).Add(maxPriority, baseFee)
		if gasPrice.Cmp(maxFee) > 0 {
			gasPrice = maxFee
		}
	}
	if gasPrice == nil {
		return nil, fmt.Errorf("no gas price provided")
	}

	var blobHashes []gethcommon.Hash
	for _, h := range tx.BlobVersionedHashes {
		blobHashes = append(blobHashes, gethcommon.HexToHash(h))
	}
	var blobFeeCap *big.Int
	if tx.MaxFeePerBlobGas != "" {
		blobFeeCap = hexToBigInt(tx.MaxFeePerBlobGas)
	}

	var authList []gethtypes.SetCodeAuthorization
	for _, auth := range tx.AuthorizationList {
		authList = append(authList, gethtypes.SetCodeAuthorization{
			ChainID: *uint256.MustFromBig(hexToBigInt(auth.ChainID)),
			Address: gethcommon.HexToAddress(auth.Address),
			Nonce:   hexToUint64(auth.Nonce),
			V:       uint8(hexToUint64(auth.V)),
			R:       *uint256.MustFromBig(hexToBigInt(auth.R)),
			S:       *uint256.MustFromBig(hexToBigInt(auth.S)),
		})
	}

	return &gethcore.Message{
		From:                  from,
		To:                    to,
		Nonce:                 hexToUint64(tx.Nonce),
		Value:                 u256OrNil(value),
		GasLimit:              gasLimit,
		GasPrice:              u256OrNil(gasPrice),
		GasFeeCap:             u256OrNil(maxFee),
		GasTipCap:             u256OrNil(maxPriority),
		Data:                  data,
		AccessList:            accessList,
		BlobHashes:            blobHashes,
		BlobGasFeeCap:         u256OrNil(blobFeeCap),
		SetCodeAuthorizations: authList,
	}, nil
}

// u256OrNil converts a *big.Int to *uint256.Int, preserving nil.
func u256OrNil(x *big.Int) *uint256.Int {
	if x == nil {
		return nil
	}
	return uint256.MustFromBig(x)
}

// buildGethBlockContext creates a go-ethereum BlockContext (same for GethStateTest).
func (st *GethStateTest) buildGethBlockContext(config *gethparams.ChainConfig) gethvm.BlockContext {
	env := st.json.Env
	ctx := gethvm.BlockContext{
		CanTransfer: gethcore.CanTransfer,
		Transfer:    gethcore.Transfer,
		GetHash:     geth.TestBlockHash,
		Coinbase:    gethcommon.HexToAddress(env.CurrentCoinbase),
		GasLimit:    hexToUint64(env.CurrentGasLimit),
		BlockNumber: hexToBigInt(env.CurrentNumber),
		Time:        hexToUint64(env.CurrentTimestamp),
		Difficulty:  new(big.Int),
	}
	if env.CurrentDifficulty != "" {
		ctx.Difficulty = hexToBigInt(env.CurrentDifficulty)
	}
	if config.IsLondon(new(big.Int)) {
		baseFee := hexToBigIntNilable(env.CurrentBaseFee)
		if baseFee == nil {
			baseFee = big.NewInt(0x0a)
		}
		ctx.BaseFee = baseFee
	}
	if config.IsLondon(new(big.Int)) && env.CurrentRandom != "" {
		rnd := gethcommon.HexToHash(env.CurrentRandom)
		ctx.Random = &rnd
		ctx.Difficulty = big.NewInt(0)
	}
	if config.IsCancun(new(big.Int), ctx.Time) && env.ExcessBlobGas != "" {
		ebg := hexToUint64(env.ExcessBlobGas)
		header := &gethtypes.Header{Time: ctx.Time, ExcessBlobGas: &ebg}
		ctx.BlobBaseFee = calcBlobFee(config, header)
	}
	return ctx
}

// LoadGethTests loads state tests with full transaction parsing (access lists, etc.).
func LoadGethTests(path string) (map[string]*GethStateTest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state test file: %w", err)
	}

	var raw map[string]gethStJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse state test JSON: %w", err)
	}

	tests := make(map[string]*GethStateTest, len(raw))
	for name, j := range raw {
		tests[name] = &GethStateTest{Name: name, json: j}
	}
	return tests, nil
}

// Ensure unused import for ecdsa doesn't cause build errors.
var _ *ecdsa.PrivateKey

// Ensure unused import for gethstate doesn't cause build errors.
var _ *gethstate.StateDB
