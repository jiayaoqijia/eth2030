package eiptest

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine"
	nodebackend "github.com/eth2030/eth2030/node/backend"
)

// EngineProbe drives the node's HTTP Engine API. Payloads are proposed by the
// simulation chain and validated by an independent node from the same genesis.
type EngineProbe struct {
	s        *Sim
	backend  *nodebackend.EngineBackend
	verifier *nodebackend.EngineBackend
	api      *engine.EngineAPI
	proposer *httptest.Server
	receiver *httptest.Server
	builds   map[engine.PayloadID]engine.PayloadAttributesV4
	payloads map[types.Hash]engine.PayloadAttributesV4
	requests map[types.Hash][][]byte
}

func (s *Sim) Engine() *EngineProbe {
	s.t.Helper()
	s.WirePool()
	genesisState, err := s.bc.StateAtBlock(s.genesis)
	if err != nil {
		s.t.Fatal(err)
	}
	bc, err := chain.NewBlockchain(s.cfg, s.genesis, genesisState, rawdb.NewMemoryDB())
	if err != nil {
		s.t.Fatal(err)
	}
	for n := uint64(1); n <= s.CurrentBlock().NumberU64(); n++ {
		if err := bc.InsertBlock(s.Block(n)); err != nil {
			s.t.Fatalf("initialize Engine verifier at block %d: %v", n, err)
		}
	}

	validatorSim := &Sim{cfg: s.cfg, bc: bc, genesis: s.genesis}
	validatorSim.WirePool()
	builder := nodebackend.NewEngineBackend(&rpcHelpStub{sim: s})
	verifier := nodebackend.NewEngineBackend(&rpcHelpStub{sim: validatorSim})
	api := engine.NewEngineAPI(builder)
	p := &EngineProbe{
		s: s, backend: builder, verifier: verifier, api: api,
		proposer: httptest.NewServer(api.Handler()),
		receiver: httptest.NewServer(engine.NewEngineAPI(verifier).Handler()),
		builds:   make(map[engine.PayloadID]engine.PayloadAttributesV4),
		payloads: make(map[types.Hash]engine.PayloadAttributesV4),
		requests: make(map[types.Hash][][]byte),
	}
	for _, server := range []*httptest.Server{p.proposer, p.receiver} {
		server.Client().Timeout = 10 * time.Second
	}
	s.t.Cleanup(func() {
		p.receiver.Close()
		p.proposer.Close()
		verifier.Close()
		builder.Close()
	})
	return p
}

// Backend exposes the proposer backend for node-level assertions.
func (p *EngineProbe) Backend() *nodebackend.EngineBackend { return p.backend }

// API exposes the proposer API for features that have an in-process entry point.
func (p *EngineProbe) API() *engine.EngineAPI { return p.api }

// Call sends a JSON-RPC method to the proposer through the production HTTP handler.
func (p *EngineProbe) Call(method string, result any, params ...any) error {
	return engineCall(p.proposer, method, result, params...)
}

// Submit admits transactions through the same pool used by the node builder.
func (p *EngineProbe) Submit(txs ...*types.Transaction) {
	p.s.t.Helper()
	pool := p.s.Pool()
	for _, tx := range txs {
		pool.ExpectAccepted(p.s.t, tx)
	}
}

// HeadHash returns the last head accepted by the verifier.
func (p *EngineProbe) HeadHash() types.Hash { return p.verifier.GetHeadHash() }

// HeadBlock returns the matching canonical simulation block.
func (p *EngineProbe) HeadBlock() *types.Block { return p.s.bc.GetBlock(p.HeadHash()) }

// Attributes builds defaults for the next Engine payload. Callers may modify
// the timestamp, beacon root, withdrawals, slot and inclusion-list bytes.
func (p *EngineProbe) Attributes() engine.PayloadAttributesV4 {
	return engine.PayloadAttributesV4{
		PayloadAttributesV3: engine.PayloadAttributesV3{
			PayloadAttributesV2: engine.PayloadAttributesV2{
				PayloadAttributesV1: engine.PayloadAttributesV1{
					Timestamp: p.s.Now(), SuggestedFeeRecipient: p.s.beneficiary,
				},
				Withdrawals: []*engine.Withdrawal{},
			},
			ParentBeaconBlockRoot: types.Hash{1},
		},
		SlotNumber: p.HeadBlock().NumberU64() + 1,
	}
}

func (p *EngineProbe) FCU() (engine.PayloadID, error) { return p.FCUWith(p.Attributes()) }

// FCUWith requests a payload through the fork-appropriate Engine API method.
func (p *EngineProbe) FCUWith(attrs engine.PayloadAttributesV4) (engine.PayloadID, error) {
	var result engine.ForkchoiceUpdatedResult
	head := p.HeadHash()
	fc := engine.ForkchoiceStateV1{HeadBlockHash: head}
	var method string
	var wireAttrs any
	if p.s.cfg.IsAmsterdam(attrs.Timestamp) {
		method, wireAttrs = "engine_forkchoiceUpdatedV4", attrs
	} else {
		if len(attrs.InclusionListTransactions) != 0 {
			return engine.PayloadID{}, fmt.Errorf("inclusion list attributes require Amsterdam")
		}
		method, wireAttrs = "engine_forkchoiceUpdatedV3", attrs.PayloadAttributesV3
	}
	if err := engineCall(p.proposer, method, &result, fc, wireAttrs); err != nil {
		return engine.PayloadID{}, err
	}
	if result.PayloadID == nil || result.PayloadStatus.Status != engine.StatusValid {
		return engine.PayloadID{}, fmt.Errorf("forkchoiceUpdated returned status %s without a usable payload ID", result.PayloadStatus.Status)
	}
	p.builds[*result.PayloadID] = attrs
	return *result.PayloadID, nil
}

// GetPayload retrieves the built payload and stores its validation attributes.
func (p *EngineProbe) GetPayload(id engine.PayloadID) (*engine.GetPayloadV6Response, error) {
	attrs, ok := p.builds[id]
	if !ok {
		return nil, fmt.Errorf("unknown payload ID %x", id)
	}
	var result engine.GetPayloadV6Response
	if p.s.cfg.IsAmsterdam(attrs.Timestamp) {
		if err := engineCall(p.proposer, "engine_getPayloadV6", &result, id); err != nil {
			return nil, err
		}
	} else {
		var v4 engine.GetPayloadV4Response
		if err := engineCall(p.proposer, "engine_getPayloadV4", &v4, id); err != nil {
			return nil, err
		}
		if v4.ExecutionPayload == nil {
			return nil, fmt.Errorf("engine_getPayloadV4 returned no payload")
		}
		result.ExecutionPayload = &engine.ExecutionPayloadV5{}
		result.ExecutionPayload.ExecutionPayloadV3 = *v4.ExecutionPayload
		result.BlockValue, result.ExecutionRequests = v4.BlockValue, v4.ExecutionRequests
	}
	if result.ExecutionPayload == nil {
		return nil, fmt.Errorf("Engine returned no payload")
	}
	p.payloads[result.ExecutionPayload.BlockHash] = attrs
	p.requests[result.ExecutionPayload.BlockHash] = result.ExecutionRequests
	return &result, nil
}

// Process validates a payload on the independent receiver.
func (p *EngineProbe) Process(pl *engine.ExecutionPayloadV5) (engine.PayloadStatusV1, error) {
	attrs, ok := p.payloads[pl.BlockHash]
	if !ok {
		return engine.PayloadStatusV1{}, fmt.Errorf("no validation attributes for payload %s", pl.BlockHash.Hex())
	}
	return p.ProcessWith(pl, attrs.ParentBeaconBlockRoot, p.requests[pl.BlockHash])
}

// ProcessWith validates a supplied payload with explicit consensus inputs.
func (p *EngineProbe) ProcessWith(pl *engine.ExecutionPayloadV5, beaconRoot types.Hash, requests [][]byte) (engine.PayloadStatusV1, error) {
	var status engine.PayloadStatusV1
	method, payload := "engine_newPayloadV4", any(pl.ExecutionPayloadV3)
	if p.s.cfg.IsAmsterdam(pl.Timestamp) {
		method, payload = "engine_newPayloadV5", pl
	}
	hashes := make([]types.Hash, 0)
	for _, raw := range pl.Transactions {
		tx, err := types.DecodeTxRLP(raw)
		if err != nil {
			return status, err
		}
		hashes = append(hashes, tx.BlobHashes()...)
	}
	encodedRequests := make([]string, len(requests))
	for i, request := range requests {
		encodedRequests[i] = Hex(request)
	}
	if err := engineCall(p.receiver, method, &status, payload, hashes, beaconRoot, encodedRequests); err != nil {
		return status, err
	}
	if status.Status == engine.StatusValid {
		if status.LatestValidHash == nil || *status.LatestValidHash != pl.BlockHash {
			return status, fmt.Errorf("Engine accepted %s with latestValidHash %v and head %s", pl.BlockHash.Hex(), status.LatestValidHash, p.HeadHash().Hex())
		}
		for _, server := range []*httptest.Server{p.receiver, p.proposer} {
			var selected engine.ForkchoiceUpdatedResult
			fc := engine.ForkchoiceStateV1{HeadBlockHash: pl.BlockHash}
			if err := engineCall(server, "engine_forkchoiceUpdatedV3", &selected, fc, nil); err != nil {
				return status, err
			}
			if selected.PayloadStatus.Status != engine.StatusValid {
				return status, fmt.Errorf("forkchoice did not select validated payload")
			}
		}
		if p.HeadHash() != pl.BlockHash {
			return status, fmt.Errorf("forkchoice head mismatch")
		}
		p.s.now = pl.Timestamp
		p.s.syncPool(p.HeadBlock())
	}
	return status, nil
}

func engineCall(server *httptest.Server, method string, result any, args ...any) error {
	raw, err := callRPC(server.Client(), server.URL, method, args)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("%s response: %w", method, err)
	}
	return nil
}

// BuildAndProcess drives a complete build and validation cycle.
func (p *EngineProbe) BuildAndProcess() (*engine.ExecutionPayloadV5, engine.PayloadStatusV1, error) {
	p.s.t.Helper()
	id, err := p.FCU()
	if err != nil {
		return nil, engine.PayloadStatusV1{}, err
	}
	resp, err := p.GetPayload(id)
	if err != nil {
		return nil, engine.PayloadStatusV1{}, err
	}
	status, err := p.Process(resp.ExecutionPayload)
	return resp.ExecutionPayload, status, err
}

func (p *EngineProbe) ExpectPayloadTxCount(t *testing.T, payload *engine.ExecutionPayloadV5, n int) {
	t.Helper()
	if len(payload.Transactions) != n {
		t.Fatalf("payload txs = %d, want %d", len(payload.Transactions), n)
	}
}

func (p *EngineProbe) ExpectStatusValid(t *testing.T, status engine.PayloadStatusV1) {
	t.Helper()
	if status.Status != engine.StatusValid {
		detail := ""
		if status.ValidationError != nil {
			detail = *status.ValidationError
		}
		t.Fatalf("payload status = %s, want VALID: %s", status.Status, detail)
	}
}
