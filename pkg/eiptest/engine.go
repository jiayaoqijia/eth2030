package eiptest

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine"
)

// EngineProbe drives the production EngineBackend through the
// forkchoiceUpdated → getPayload → newPayload loop, with transaction
// injection into payload building via the backend's pool hook.
type EngineProbe struct {
	s      *Sim
	backend *engine.EngineBackend
	queue  *slicePool
}

// Engine returns the Engine API probe. The probe owns an independent
// EngineBackend over the same genesis and config; payload building executes
// transactions exactly as production would.
func (s *Sim) Engine() *EngineProbe {
	s.t.Helper()
	statedb, err := s.bc.StateAtBlock(s.genesis)
	if err != nil {
		s.t.Fatalf("eiptest: genesis state for engine probe: %v", err)
	}
	backend := engine.NewEngineBackend(s.cfg, statedb, s.genesis)
	p := &EngineProbe{s: s, backend: backend, queue: &slicePool{}}
	backend.SetTxPool(p.queue)
	return p
}

// Backend exposes the underlying EngineBackend for advanced assertions.
func (p *EngineProbe) Backend() *engine.EngineBackend { return p.backend }

// Submit queues transactions for the next payload build.
func (p *EngineProbe) Submit(txs ...*types.Transaction) {
	p.queue.txs = append(p.queue.txs, txs...)
}

// HeadHash returns the backend's current head hash (genesis before any FCU).
func (p *EngineProbe) HeadHash() types.Hash {
	if h := p.backend.GetHeadHash(); h != (types.Hash{}) {
		return h
	}
	return p.s.genesis.Hash()
}

// HeadBlock returns the backend's stored head block (nil if unknown).
func (p *EngineProbe) HeadBlock() *types.Block {
	return p.backend.GetBlockByHash(p.HeadHash())
}

// FCU submits forkchoiceUpdated targeting the backend's head with payload
// attributes at parent+12s, returning the payload ID.
func (p *EngineProbe) FCU() (engine.PayloadID, error) {
	p.s.t.Helper()
	head := p.HeadHash()
	parent := p.backend.GetBlockByHash(head)
	if parent == nil {
		return engine.PayloadID{}, errUnknownHead
	}
	ts := parent.Header().Time + 12
	attrs := &engine.PayloadAttributesV4{
		PayloadAttributesV3: engine.PayloadAttributesV3{
			PayloadAttributesV2: engine.PayloadAttributesV2{
				PayloadAttributesV1: engine.PayloadAttributesV1{
					Timestamp:             ts,
					SuggestedFeeRecipient: types.Address{0x01},
				},
				Withdrawals: []*engine.Withdrawal{},
			},
		},
	}
	result, err := p.backend.ForkchoiceUpdatedV4(engine.ForkchoiceStateV1{
		HeadBlockHash:      head,
		SafeBlockHash:      head,
		FinalizedBlockHash: head,
	}, attrs)
	if err != nil {
		return engine.PayloadID{}, err
	}
	if result.PayloadID == nil {
		return engine.PayloadID{}, errNoPayloadID
	}
	// NOTE: the queue is drained by GetPayload (after the async builder has
	// consumed it), not here — clearing before the build races the builder.
	return *result.PayloadID, nil
}

// GetPayload retrieves the built payload for the given ID (V6 response with
// blobs bundle when applicable).
func (p *EngineProbe) GetPayload(id engine.PayloadID) (*engine.GetPayloadV6Response, error) {
	resp, err := p.backend.GetPayloadV6ByID(id)
	if err == nil {
		p.queue.txs = nil // builder consumed the queue
	}
	return resp, err
}

// Process submits a payload through newPayload (V5) and returns the status.
func (p *EngineProbe) Process(payload *engine.ExecutionPayloadV5) (engine.PayloadStatusV1, error) {
	return p.backend.ProcessBlockV5(payload, nil, types.Hash{}, nil)
}

// BuildAndProcess drives a full cycle: FCU with the queued txs → getPayload →
// newPayload, returning the payload and the resulting status.
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

type errNoPayloadIDError struct{}

func (errNoPayloadIDError) Error() string { return "eiptest: forkchoiceUpdated returned nil payload id" }

var errNoPayloadID = errNoPayloadIDError{}

type errUnknownHeadError struct{}

func (errUnknownHeadError) Error() string { return "eiptest: engine backend does not know the head block" }

var errUnknownHead = errUnknownHeadError{}

// ExpectPayloadTxCount asserts the payload carries exactly n transactions.
func (p *EngineProbe) ExpectPayloadTxCount(t *testing.T, payload *engine.ExecutionPayloadV5, n int) {
	t.Helper()
	if len(payload.Transactions) != n {
		t.Fatalf("payload txs = %d, want %d", len(payload.Transactions), n)
	}
}

// ExpectStatusValid asserts the payload status is VALID.
func (p *EngineProbe) ExpectStatusValid(t *testing.T, status engine.PayloadStatusV1) {
	t.Helper()
	if status.Status != engine.StatusValid {
		t.Fatalf("payload status = %s (validationError: %v), want VALID", status.Status, status.ValidationError)
	}
}
