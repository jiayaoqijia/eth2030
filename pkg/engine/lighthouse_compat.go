package engine

// Compatibility layer for sigp/lighthouse >= 8.4 Engine API shapes.
//
// Upstream diverged from the fork-era conventions this engine was built
// against (feat/fix-native-aa CL, superseded 2026-11):
//   - forkchoiceUpdatedV4/V5 carry a third `custodyColumns` bitarray param
//     (Fulu/PeerDAS custody set, may be null; lighthouse #9729 era).
//   - Payload attributes use snake_case field names with hex-encoded
//     integers (slot_number, withdrawals[].validator_index/amount, ...) while
//     the fork-era CL sent camelCase with decimal integers.
//   - PayloadAttributesV5 adds inclusion_list_transactions (EIP-7916
//     progressive list serialized as a single hex string).
//
// The helpers here parse upstream shapes into the internal payload attributes
// so both CL generations can drive the same backend.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	gethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine/payload"
)

// withdrawalFromLighthouse converts a lighthouse-shaped withdrawal.
func withdrawalFromLighthouse(w lighthouseWithdrawal, i int) (*payload.Withdrawal, error) {
	out := &payload.Withdrawal{Address: types.Address(gethcommon.HexToAddress(w.Address))}
	if w.Index != nil {
		idx, err := parseHexU64(*w.Index)
		if err != nil {
			return nil, fmt.Errorf("withdrawal %d: invalid index: %w", i, err)
		}
		out.Index = idx
	}
	if w.ValidatorIndex != nil {
		vidx, err := parseHexU64(*w.ValidatorIndex)
		if err != nil {
			return nil, fmt.Errorf("withdrawal %d: invalid validator_index: %w", i, err)
		}
		out.ValidatorIndex = vidx
	}
	if w.Amount != nil {
		amt, err := parseHexU64(*w.Amount)
		if err != nil {
			return nil, fmt.Errorf("withdrawal %d: invalid amount: %w", i, err)
		}
		out.Amount = amt
	}
	return out, nil
}

// lighthouseWithdrawal mirrors lighthouse JsonWithdrawal: snake_case fields,
// hex-encoded integers.
type lighthouseWithdrawal struct {
	Index          *string `json:"index"`
	ValidatorIndex *string `json:"validator_index"`
	Address        string  `json:"address"`
	Amount         *string `json:"amount"`
}

// lighthousePayloadAttrs mirrors lighthouse JsonPayloadAttributesV4/V5.
// All integers are hex-encoded (serde_utils::u64_hex_be) upstream.
type lighthousePayloadAttrs struct {
	Timestamp                 *string                `json:"timestamp"`
	PrevRandao                string                 `json:"prev_randao"`
	SuggestedFeeRecipient     string                 `json:"suggested_fee_recipient"`
	Withdrawals               []lighthouseWithdrawal `json:"withdrawals"`
	ParentBeaconBlockRoot     string                 `json:"parent_beacon_block_root"`
	SlotNumber                *string                `json:"slot_number"`
	TargetGasLimit            *string                `json:"target_gas_limit"`
	InclusionListTransactions json.RawMessage        `json:"inclusion_list_transactions"`
}

// parseHexU64 decodes a lighthouse hex-encoded u64 ("0x1f").
func parseHexU64(s string) (uint64, error) {
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if s == "" {
		return 0, fmt.Errorf("empty hex u64")
	}
	return strconv.ParseUint(s, 16, 64)
}

// parsePayloadAttributes converts one JSON payload-attributes param into the
// internal attributes struct. Two wire shapes are recognized:
//   - lighthouse (upstream): snake_case keys, hex integers, progressive IL list
//   - fork-era CL: camelCase keys, decimal integers (internal struct as-is)
//
// A nil input (JSON null) yields nil attributes.
func parsePayloadAttributes(raw []byte) (*payload.PayloadAttributesV4, error) {
	if raw == nil || string(raw) == "null" {
		return nil, nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("invalid payload attributes object: %w", err)
	}

	if _, ok := probe["prev_randao"]; ok {
		return parseLighthousePayloadAttrs(raw)
	}

	// Fork-era shape: unmarshal directly into the internal struct.
	var attrs payload.PayloadAttributesV4
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, fmt.Errorf("invalid payload attributes: %w", err)
	}
	return &attrs, nil
}

// parseLighthousePayloadAttrs converts lighthouse-shaped attributes into the
// internal struct.
func parseLighthousePayloadAttrs(raw []byte) (*payload.PayloadAttributesV4, error) {
	var la lighthousePayloadAttrs
	if err := json.Unmarshal(raw, &la); err != nil {
		return nil, fmt.Errorf("invalid lighthouse payload attributes: %w", err)
	}

	attrs := &payload.PayloadAttributesV4{}
	// go.mod targets go1.26 which forbids multi-level promoted-field struct
	// literals (a go1.27 feature), so assign through the embedding chain.
	if la.Timestamp != nil {
		ts, err := parseHexU64(*la.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %w", err)
		}
		attrs.Timestamp = ts
	}
	attrs.PayloadAttributesV2.PayloadAttributesV1.PrevRandao = types.Hash(gethcommon.HexToHash(la.PrevRandao))
	attrs.PayloadAttributesV2.PayloadAttributesV1.SuggestedFeeRecipient = types.Address(gethcommon.HexToAddress(la.SuggestedFeeRecipient))
	attrs.ParentBeaconBlockRoot = types.Hash(gethcommon.HexToHash(la.ParentBeaconBlockRoot))

	for i, w := range la.Withdrawals {
		out, err := withdrawalFromLighthouse(w, i)
		if err != nil {
			return nil, err
		}
		attrs.Withdrawals = append(attrs.Withdrawals, out)
	}

	if la.SlotNumber != nil {
		slot, err := parseHexU64(*la.SlotNumber)
		if err != nil {
			return nil, fmt.Errorf("invalid slot_number: %w", err)
		}
		attrs.SlotNumber = slot
	}

	if len(la.InclusionListTransactions) > 0 && string(la.InclusionListTransactions) != "null" {
		// TODO(engine): decode lighthouse's EIP-7916 progressive list hex
		// (prog_list_of_hex_prog_var_list) into [][]byte and populate
		// InclusionListTransactions. Until then payloads are built without
		// IL transactions; IL tx delivery is exercised via engine_getInclusionListV1.
		attrs.InclusionListTransactions = nil
	}

	return attrs, nil
}

// validateCustodyColumns checks the optional third forkchoiceUpdatedV4/V5
// parameter (lighthouse #9729: Fulu PeerDAS custody set as a hex bitarray, or
// null). The EL treats null as a no-op; a non-null set is accepted but not yet
// consumed.
// TODO(engine): thread the custody set into data-column serving decisions.
func validateCustodyColumns(raw []byte) error {
	if raw == nil || string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("custodyColumns must be a hex bitarray string or null")
	}
	b := strings.TrimPrefix(strings.ToLower(s), "0x")
	if _, err := hex.DecodeString(b); err != nil {
		return fmt.Errorf("invalid custodyColumns bitarray: %w", err)
	}
	return nil
}
