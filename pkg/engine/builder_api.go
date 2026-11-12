package engine

import (
	"encoding/json"
	"fmt"
)

// GetBuilderByIndex returns the registered builder at the given index.
func (api *EngineAPI) GetBuilderByIndex(index BuilderIndex) (*Builder, error) {
	if api.builderRegistry == nil {
		return nil, ErrBuilderNotFound
	}
	return api.builderRegistry.GetBuilderByIndex(index)
}

// SetBidObserver installs a callback invoked with every accepted builder
// bid, letting the node feed its ePBS auction subsystem (EIP-7732).
func (api *EngineAPI) SetBidObserver(fn func(*SignedExecutionPayloadBid) error) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.bidObserver = fn
}

// SubmitBuilderBidV1 processes a builder bid submission.
// The bid contains a commitment to an execution payload (by block hash)
// along with a value the builder is willing to pay the proposer.
func (api *EngineAPI) SubmitBuilderBidV1(signed SignedExecutionPayloadBid) error {
	if api.builderRegistry == nil {
		return ErrBuilderNotFound
	}
	if err := api.builderRegistry.SubmitBid(&signed); err != nil {
		return err
	}
	api.mu.Lock()
	observer := api.bidObserver
	api.mu.Unlock()
	if observer != nil {
		return observer(&signed)
	}
	return nil
}

// GetBuilderBidsV1 returns the bids for a given slot, ordered by value.
// If no bids exist for the slot, an empty list is returned.
func (api *EngineAPI) GetBuilderBidsV1(slot uint64) []*SignedExecutionPayloadBid {
	if api.builderRegistry == nil {
		return nil
	}
	return api.builderRegistry.GetBidsForSlot(slot)
}

// GetBestBuilderBidV1 returns the highest-value bid for a given slot.
func (api *EngineAPI) GetBestBuilderBidV1(slot uint64) (*SignedExecutionPayloadBid, error) {
	if api.builderRegistry == nil {
		return nil, ErrNoBidsAvailable
	}
	return api.builderRegistry.GetBestBid(slot)
}

// RegisterBuilderV1 registers a new builder in the registry.
func (api *EngineAPI) RegisterBuilderV1(signed SignedBuilderRegistrationV1) (*Builder, error) {
	if api.builderRegistry == nil {
		return nil, ErrBuilderNotFound
	}
	return api.builderRegistry.RegisterBuilder(&signed.Message, MinBuilderStake)
}

// handleSubmitBuilderBidV1 is the JSON-RPC handler for engine_submitBuilderBidV1.
func (api *EngineAPI) handleSubmitBuilderBidV1(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var signed SignedExecutionPayloadBid
	if err := json.Unmarshal(params[0], &signed); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid signed bid: %v", err),
		}
	}

	if err := api.SubmitBuilderBidV1(signed); err != nil {
		return nil, engineErrorToRPC(err)
	}
	return true, nil
}

// handleGetBuilderBidsV1 is the JSON-RPC handler for engine_getBuilderBidsV1.
func (api *EngineAPI) handleGetBuilderBidsV1(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var slot uint64
	if err := json.Unmarshal(params[0], &slot); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid slot: %v", err),
		}
	}

	bids := api.GetBuilderBidsV1(slot)
	if bids == nil {
		bids = make([]*SignedExecutionPayloadBid, 0)
	}
	return bids, nil
}
