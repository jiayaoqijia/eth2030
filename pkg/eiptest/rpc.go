package eiptest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/mev"
	nodebackend "github.com/eth2030/eth2030/node/backend"
	"github.com/eth2030/eth2030/proofs"
	rpcserver "github.com/eth2030/eth2030/rpc"
	"github.com/eth2030/eth2030/txpool"
)

// RPCProbe exposes the simulated chain through the production JSON-RPC server
// (node/backend.RPCBackend over httptest), so tests assert real RPC response
// shapes: field presence, hex encoding, null vs empty-array semantics.
type RPCProbe struct {
	client *http.Client
	url    string
}

// rpcHelpStub implements node/backend.NodeDeps with only the core deps wired;
// optional deps return nil (the RPC backend tolerates this).
type rpcHelpStub struct{ sim *Sim }

func (h *rpcHelpStub) Blockchain() *chain.Blockchain { return h.sim.bc }
func (h *rpcHelpStub) TxPool() *txpool.TxPool        { return h.sim.pool }
func (h *rpcHelpStub) Config() *nodebackend.Config   { return &nodebackend.Config{} }
func (h *rpcHelpStub) GasOracle() nodebackend.GasOracleDeps {
	return nil
}
func (h *rpcHelpStub) MEVConfig() *mev.MEVProtectionConfig { return nil }
func (h *rpcHelpStub) FCStateManager() nodebackend.FCStateManagerDeps {
	return nil
}
func (h *rpcHelpStub) StarkFrameProver() proofs.ValidationFrameProver {
	return nil
}
func (h *rpcHelpStub) EthHandler() nodebackend.EthHandlerDeps { return nil }
func (h *rpcHelpStub) TxJournal() nodebackend.TxJournalDeps   { return nil }
func (h *rpcHelpStub) P2PServer() nodebackend.P2PServerDeps   { return nil }

// RPC returns the JSON-RPC probe backed by the production RPC adapter.
func (s *Sim) RPC() *RPCProbe {
	s.t.Helper()
	backend := nodebackend.NewRPCBackend(&rpcHelpStub{sim: s}, nil)
	srv := httptest.NewServer(rpcserver.NewServer(backend).Handler())
	s.t.Cleanup(srv.Close)
	client := srv.Client()
	client.Timeout = 10 * time.Second
	return &RPCProbe{client: client, url: srv.URL}
}

// Call performs a successful JSON-RPC call and returns the raw result.
func (p *RPCProbe) Call(t *testing.T, method string, params []interface{}) json.RawMessage {
	t.Helper()
	result, err := p.CallRaw(method, params)
	if err != nil {
		t.Fatalf("rpc %s: %v", method, err)
	}
	return result
}

// CallRaw performs a JSON-RPC call and exposes errors to negative tests.
func (p *RPCProbe) CallRaw(method string, params []interface{}) (json.RawMessage, *rpcError) {
	return callRPC(p.client, p.url, method, params)
}

func callRPC(client *http.Client, url, method string, params []any) (json.RawMessage, *rpcError) {
	if params == nil {
		params = []any{}
	}
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}{"2.0", 1, method, params}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, &rpcError{Message: err.Error()}
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, &rpcError{Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &rpcError{Message: fmt.Sprintf("HTTP %s", resp.Status)}
	}
	var result struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &rpcError{Message: err.Error()}
	}
	if result.JSONRPC != "2.0" || result.ID != request.ID || (result.Error == nil && result.Result == nil) {
		return nil, &rpcError{Message: "invalid JSON-RPC response envelope"}
	}
	return result.Result, result.Error
}

// rpcError mirrors the JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Value extracts a field of a JSON object result.
func (p *RPCProbe) Value(result json.RawMessage, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(result, &obj); err != nil {
		return nil
	}
	return obj[key]
}

// ExpectField asserts the JSON object at result has key with the raw JSON
// value wantJSON (e.g. `"0x1a"`, `null`, `[]`).
func (p *RPCProbe) ExpectField(t *testing.T, result json.RawMessage, key, wantJSON string) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("result is not an object: %s", result)
	}
	got, ok := obj[key]
	if !ok {
		t.Fatalf("result missing field %q (have %v)", key, obj)
	}
	if wantJSON != "" && string(got) != wantJSON {
		t.Fatalf("field %q = %s, want %s", key, got, wantJSON)
	}
}
