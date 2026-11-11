package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/state"
)

// TestGetPayloadV6ResponseCarriesGloasFields drives the real backend through
// forkchoiceUpdated (with Gloas-shaped payload attributes) and getPayloadV6,
// asserting the marshaled response exposes blockAccessList and slotNumber —
// the fields lighthouse >= 8.4 requires on Gloas payloads.
func TestGetPayloadV6ResponseCarriesGloasFields(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)
	api := NewEngineAPI(b)

	attrsJSON := `{"timestamp":"0x6aed4ae4","prev_randao":"0x1111111111111111111111111111111111111111111111111111111111111111","suggested_fee_recipient":"0x8943545177806ed17b9f23f0a21ee5948ecaa776","withdrawals":[],"parent_beacon_block_root":"0x2222222222222222222222222222222222222222222222222222222222222222","slot_number":"0x40"}`

	fcuReq := `{"jsonrpc":"2.0","id":1,"method":"engine_forkchoiceUpdatedV5","params":[` +
		`{"headBlockHash":"` + genesis.Hash().Hex() + `","safeBlockHash":"` + genesis.Hash().Hex() + `","finalizedBlockHash":"` + genesis.Hash().Hex() + `"},` +
		attrsJSON + `,null]}`
	fcuResp := api.HandleRequest([]byte(fcuReq))
	var fcu struct {
		Result struct {
			PayloadID string `json:"payloadId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(fcuResp, &fcu); err != nil {
		t.Fatalf("fcu response: %v (%s)", err, fcuResp)
	}
	if fcu.Result.PayloadID == "" {
		t.Fatalf("no payloadId in fcu response: %s", fcuResp)
	}

	// Give the payload build a moment (sync store happens during fcu; async builds lag).
	time.Sleep(500 * time.Millisecond)

	gpReq := `{"jsonrpc":"2.0","id":2,"method":"engine_getPayloadV6","params":["` + fcu.Result.PayloadID + `"]}`
	gpResp := api.HandleRequest([]byte(gpReq))

	s := string(gpResp)
	if !strings.Contains(s, "blockAccessList") {
		t.Fatalf("getPayloadV6 response missing blockAccessList: %s", s[:min(len(s), 600)])
	}
	if !strings.Contains(s, "slotNumber") {
		t.Fatalf("getPayloadV6 response missing slotNumber: %s", s[:min(len(s), 600)])
	}
	if !strings.Contains(s, `"blockAccessList":"0x`) && !strings.Contains(s, `"blockAccessList":""`) {
		t.Fatalf("blockAccessList not a 0x-hex string: %s", s[:min(len(s), 600)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
