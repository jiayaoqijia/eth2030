// send-frame is a CLI tool for sending EIP-8141 frame transactions to an
// Ethereum node. It deploys an APPROVE contract if needed, then constructs
// and submits a type 0x06 frame transaction with VERIFY + SENDER frames.
//
// Usage:
//
//	go run ./cmd/send-frame --rpc http://localhost:8545 --key <hex-private-key>
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

var (
	rpcURL     = flag.String("rpc", "http://localhost:8545", "JSON-RPC URL")
	privateKey = flag.String("key", "", "Hex-encoded private key (without 0x)")
	sender     = flag.String("sender", "", "Sender address (0x...)")
	approveAt  = flag.String("approve-contract", "", "Existing APPROVE contract address (skip deploy)")
	dryRun     = flag.Bool("dry-run", false, "Encode and print but don't send")
)

func main() {
	flag.Parse()

	if *sender == "" {
		fmt.Fprintln(os.Stderr, "Error: --sender is required")
		os.Exit(1)
	}

	senderAddr := types.HexToAddress(*sender)

	// Step 1: Deploy APPROVE contract if needed.
	var approveAddr types.Address
	if *approveAt != "" {
		approveAddr = types.HexToAddress(*approveAt)
		fmt.Printf("Using existing APPROVE contract: %s\n", approveAddr.Hex())
	} else {
		fmt.Println("Step 1: Deploy APPROVE contract (scope 2 = combined exec+payment)...")
		// Runtime: PUSH1 0x02, PUSH1 0x00, PUSH1 0x00, APPROVE(0xaa), STOP
		// = 60 02 60 00 60 00 aa 00
		runtime := mustHex("600260006000aa00")
		// Init code: PUSH1 8, PUSH1 12, PUSH1 0, CODECOPY, PUSH1 8, PUSH1 0, RETURN
		init := mustHex("6008600c60003960086000f3")
		deployCode := append(init, runtime...)

		txHash, err := sendLegacyTx(senderAddr, nil, deployCode, 100000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Deploy failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Deploy tx: %s\n", txHash)

		// Wait for receipt.
		receipt, err := waitForReceipt(txHash, 30*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Receipt failed: %v\n", err)
			os.Exit(1)
		}
		ca, ok := receipt["contractAddress"].(string)
		if !ok || ca == "" || ca == "0x0000000000000000000000000000000000000000" {
			fmt.Fprintln(os.Stderr, "Deploy succeeded but no contract address in receipt")
			os.Exit(1)
		}
		approveAddr = types.HexToAddress(ca)
		fmt.Printf("  APPROVE contract deployed at: %s\n", approveAddr.Hex())

		// Verify code.
		code, _ := rpcCall("eth_getCode", []interface{}{approveAddr.Hex(), "latest"})
		fmt.Printf("  Deployed code: %s\n", code)
	}

	// Step 2: Build and send frame transaction.
	fmt.Println("\nStep 2: Build EIP-8141 frame transaction...")

	chainID, err := getChainID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get chain ID: %v\n", err)
		os.Exit(1)
	}

	nonce, err := getNonce(senderAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get nonce: %v\n", err)
		os.Exit(1)
	}

	// Build frames:
	// Frame 0: VERIFY mode → calls APPROVE contract (scope 2)
	// Frame 1: SENDER mode → simple call to sender (no-op)
	frames := []types.Frame{
		{
			Mode:     types.ModeVerify,
			Target:   &approveAddr,
			GasLimit: 50000,
			Data:     nil, // VERIFY data is elided in sig hash
		},
		{
			Mode:     types.ModeSender,
			Target:   &senderAddr, // call self (no-op)
			GasLimit: 21000,
			Data:     nil,
		},
	}

	baseFee, err := getBaseFee()
	if err != nil {
		baseFee = big.NewInt(1_000_000_000) // fallback 1 Gwei
	}
	maxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	maxPriority := big.NewInt(1_000_000_000) // 1 Gwei tip

	frameTx := &types.FrameTx{
		ChainID:              big.NewInt(int64(chainID)),
		Nonce:                new(big.Int).SetUint64(nonce),
		Sender:               senderAddr,
		Frames:               frames,
		MaxPriorityFeePerGas: maxPriority,
		MaxFeePerGas:         maxFee,
		MaxFeePerBlobGas:     new(big.Int),
		BlobVersionedHashes:  nil,
	}

	// Encode.
	encoded, err := encodeFrameTx(frameTx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Encode failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  Type: 0x06 (EIP-8141 Frame Transaction)\n")
	fmt.Printf("  Sender: %s\n", senderAddr.Hex())
	fmt.Printf("  Chain ID: %d\n", chainID)
	fmt.Printf("  Nonce: %d\n", nonce)
	fmt.Printf("  Frames: %d\n", len(frames))
	fmt.Printf("    Frame 0: VERIFY → %s (gas: %d)\n", approveAddr.Hex(), frames[0].GasLimit)
	fmt.Printf("    Frame 1: SENDER → %s (gas: %d)\n", senderAddr.Hex(), frames[1].GasLimit)
	fmt.Printf("  Max fee: %s wei\n", maxFee.String())
	fmt.Printf("  Raw tx: 0x%s\n", hex.EncodeToString(encoded))
	fmt.Printf("  Size: %d bytes\n", len(encoded))

	if *dryRun {
		fmt.Println("\n[dry-run] Transaction not sent.")
		return
	}

	// Send.
	fmt.Println("\nStep 3: Send frame transaction...")
	rawHex := "0x" + hex.EncodeToString(encoded)
	result, err := rpcCall("eth_sendRawTransaction", []interface{}{rawHex})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Send failed: %v\n", err)
		// Print the full error for debugging.
		fmt.Fprintf(os.Stderr, "Raw tx hex: %s\n", rawHex)
		os.Exit(1)
	}
	fmt.Printf("  Transaction hash: %s\n", result)

	// Wait for receipt.
	fmt.Println("\nStep 4: Wait for receipt...")
	receipt, err := waitForReceipt(result, 60*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Receipt failed: %v\n", err)
		os.Exit(1)
	}

	status, _ := receipt["status"].(string)
	gasUsed, _ := receipt["gasUsed"].(string)
	blockNum, _ := receipt["blockNumber"].(string)

	statusStr := "REVERTED"
	if status == "0x1" {
		statusStr = "SUCCESS"
	}
	fmt.Printf("  Status: %s\n", statusStr)
	fmt.Printf("  Gas used: %s\n", gasUsed)
	fmt.Printf("  Block: %s\n", blockNum)

	if status == "0x1" {
		fmt.Println("\n✓ EIP-8141 frame transaction executed successfully!")
	} else {
		fmt.Println("\n✗ Frame transaction reverted.")
		os.Exit(1)
	}
}

// encodeFrameTx produces 0x06 || RLP([chain_id, nonce, sender, frames, fees...])
func encodeFrameTx(tx *types.FrameTx) ([]byte, error) {
	// Encode frames as RLP list of [mode, target, gas_limit, data]
	type rlpFrame struct {
		Mode     uint8
		Target   []byte
		GasLimit uint64
		Data     []byte
	}
	type rlpFrameTx struct {
		ChainID              *big.Int
		Nonce                *big.Int
		Sender               [20]byte
		Frames               []rlpFrame
		MaxPriorityFeePerGas *big.Int
		MaxFeePerGas         *big.Int
		MaxFeePerBlobGas     *big.Int
		BlobVersionedHashes  [][32]byte
	}

	var frames []rlpFrame
	for _, f := range tx.Frames {
		var target []byte
		if f.Target != nil {
			target = f.Target[:]
		}
		frames = append(frames, rlpFrame{
			Mode:     f.Mode,
			Target:   target,
			GasLimit: f.GasLimit,
			Data:     f.Data,
		})
	}

	var hashes [][32]byte
	for _, h := range tx.BlobVersionedHashes {
		hashes = append(hashes, [32]byte(h))
	}
	if hashes == nil {
		hashes = [][32]byte{}
	}

	payload := rlpFrameTx{
		ChainID:              bigOrZero(tx.ChainID),
		Nonce:                bigOrZero(tx.Nonce),
		Sender:               [20]byte(tx.Sender),
		Frames:               frames,
		MaxPriorityFeePerGas: bigOrZero(tx.MaxPriorityFeePerGas),
		MaxFeePerGas:         bigOrZero(tx.MaxFeePerGas),
		MaxFeePerBlobGas:     bigOrZero(tx.MaxFeePerBlobGas),
		BlobVersionedHashes:  hashes,
	}

	enc, err := rlp.EncodeToBytes(payload)
	if err != nil {
		return nil, err
	}

	result := make([]byte, 1+len(enc))
	result[0] = 0x06
	copy(result[1:], enc)
	return result, nil
}

func bigOrZero(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return v
}

// --- RPC helpers ---

func rpcCall(method string, params interface{}) (string, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	data, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "POST", *rpcURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Result interface{} `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("RPC response parse failed: %w (body: %s)", err, string(respBody))
	}
	if result.Error != nil {
		return "", fmt.Errorf("RPC error %d: %s", result.Error.Code, result.Error.Message)
	}
	if s, ok := result.Result.(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("unexpected RPC result type: %T", result.Result)
}

func sendLegacyTx(from types.Address, to *types.Address, data []byte, gasLimit uint64) (string, error) {
	var toHex interface{}
	if to != nil {
		toHex = to.Hex()
	}
	params := []interface{}{
		map[string]interface{}{
			"from": from.Hex(),
			"to":   toHex,
			"data": "0x" + hex.EncodeToString(data),
			"gas":  fmt.Sprintf("0x%x", gasLimit),
		},
	}
	return rpcCall("eth_sendTransaction", params)
}

func getChainID() (uint64, error) {
	result, err := rpcCall("eth_chainId", []interface{}{})
	if err != nil {
		return 0, err
	}
	result = strings.TrimPrefix(result, "0x")
	var id big.Int
	id.SetString(result, 16)
	return id.Uint64(), nil
}

func getNonce(addr types.Address) (uint64, error) {
	result, err := rpcCall("eth_getTransactionCount", []interface{}{addr.Hex(), "latest"})
	if err != nil {
		return 0, err
	}
	result = strings.TrimPrefix(result, "0x")
	var n big.Int
	n.SetString(result, 16)
	return n.Uint64(), nil
}

func getBaseFee() (*big.Int, error) {
	result, err := rpcCall("eth_getBlockByNumber", []interface{}{"latest", false})
	if err != nil {
		return nil, err
	}
	// result is not a string for this call, re-parse.
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_getBlockByNumber",
		"params":  []interface{}{"latest", false},
		"id":      1,
	}
	data, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", *rpcURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var block struct {
		Result struct {
			BaseFeePerGas string `json:"baseFeePerGas"`
		} `json:"result"`
	}
	json.Unmarshal(respBody, &block)
	_ = result
	bf := new(big.Int)
	bf.SetString(strings.TrimPrefix(block.Result.BaseFeePerGas, "0x"), 16)
	return bf, nil
}

func waitForReceipt(txHash string, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "eth_getTransactionReceipt",
			"params":  []interface{}{txHash},
			"id":      1,
		}
		data, _ := json.Marshal(body)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "POST", *rpcURL, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			Result map[string]interface{} `json:"result"`
		}
		json.Unmarshal(respBody, &result)
		if result.Result != nil {
			return result.Result, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("timeout waiting for receipt")
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
