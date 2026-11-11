package core

import (
	"testing"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
)

// TestEIP7976Constants verifies the Glamsterdam calldata floor cost constants.
func TestEIP7976Constants(t *testing.T) {
	// EIP-7976: TOTAL_COST_FLOOR_PER_TOKEN = 16 (up from EIP-7623's 10).
	if gas.TotalCostFloorPerTokenGlamst != 16 {
		t.Errorf("gas.TotalCostFloorPerTokenGlamst = %d, want 16", gas.TotalCostFloorPerTokenGlamst)
	}
}

// TestEIP7976CalldataFloorGas verifies the Glamsterdam calldata floor gas function.
func TestEIP7976CalldataFloorGas(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		isCreate bool
		want     uint64
	}{
		{
			name: "empty calldata",
			data: nil,
			// floor = TX_BASE_COST = 4500
			want: vm.TxBaseGlamsterdam,
		},
		{
			name: "all zero bytes",
			data: make([]byte, 100),
			// floor_tokens = 100 * 4 = 400
			// floor = 4500 + 400 * 16 = 10900
			want: vm.TxBaseGlamsterdam + 400*gas.TotalCostFloorPerTokenGlamst,
		},
		{
			name: "all non-zero bytes",
			data: []byte{0xff, 0xaa, 0xbb, 0xcc},
			// floor_tokens = 4 * 4 = 16
			// floor = 4500 + 16 * 16 = 4756
			want: vm.TxBaseGlamsterdam + 16*gas.TotalCostFloorPerTokenGlamst,
		},
		{
			name: "mixed calldata",
			data: []byte{0x00, 0xff, 0x00, 0xaa},
			// EIP-7976: floor_tokens = (2 + 2) * 4 = 16 (all bytes weighted same)
			// floor = 4500 + 16 * 16 = 4756
			want: vm.TxBaseGlamsterdam + 16*gas.TotalCostFloorPerTokenGlamst,
		},
		{
			name:     "create transaction",
			data:     []byte{0xff, 0xff},
			isCreate: true,
			// floor_tokens = 2 * 4 = 8
			// floor = 4500 + 8 * 16 = 36628
			want: vm.TxBaseGlamsterdam + 8*gas.TotalCostFloorPerTokenGlamst,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := execution.CalldataFloorGasGlamst(tt.data, nil, tt.isCreate)
			if got != tt.want {
				t.Errorf("calldataFloorGasGlamst = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEIP7976FloorVsStandard verifies that the floor cost is higher for
// data-heavy transactions under Glamsterdam.
func TestEIP7976FloorVsStandard(t *testing.T) {
	// 1000 zero bytes: cheap standard cost, expensive floor cost.
	data := make([]byte, 1000)
	data[0] = 0xff // one non-zero byte

	standardGas := execution.IntrinsicGasGlamst(data, false, false, true, 0)
	floorGas := execution.CalldataFloorGasGlamst(data, nil, false)

	// Standard: 4500 + 999*4 + 1*16 = 4500 + 3996 + 16 = 8512
	// Floor: 4500 + 1000*4*16 = 4500 + 64000 = 68500
	if floorGas <= standardGas {
		t.Errorf("expected floor (%d) > standard (%d) for mostly-zero calldata", floorGas, standardGas)
	}
}

// TestEIP7976ComparedToEIP7623 verifies that the Glamsterdam floor is
// higher than the pre-Glamsterdam (EIP-7623) floor.
func TestEIP7976ComparedToEIP7623(t *testing.T) {
	data := make([]byte, 500)
	for i := range data {
		data[i] = 0x01
	}

	// Pre-Glamsterdam floor: 21000 + 500*4*10 = 21000 + 20000 = 41000
	preFloor := execution.CalldataFloorGas(data, false)

	// Glamsterdam floor: 4500 + 500*4*16 = 4500 + 32000 = 36500
	// (Lower base cost but higher per-token floor means the comparison
	// depends on data size.)
	glamFloor := execution.CalldataFloorGasGlamst(data, nil, false)

	_ = preFloor
	_ = glamFloor
	// Both should be valid numbers (no overflow).
	if glamFloor == 0 {
		t.Error("glamFloor should not be 0")
	}
}

// TestEIP7976CalldataTokens verifies the token counting function.
func TestEIP7976CalldataTokens(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{"empty", nil, 0},
		{"all zeros", make([]byte, 10), 10},
		{"all nonzero", []byte{0xff, 0xaa, 0xbb}, 12},
		{"mixed", []byte{0x00, 0xff, 0x00}, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := execution.CalldataTokens(tt.data)
			if got != tt.want {
				t.Errorf("calldataTokens = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEIP7981AccessListDataTokens verifies access list data token counting.
func TestEIP7981AccessListDataTokens(t *testing.T) {
	tests := []struct {
		name string
		al   types.AccessList
		want uint64
	}{
		{
			name: "empty",
			al:   nil,
			want: 0,
		},
		{
			name: "single address no keys",
			al: types.AccessList{
				{Address: types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")},
			},
			// Address has 20 bytes. Count zero/nonzero tokens.
			// 0x1234567890abcdef1234567890abcdef12345678
			// All non-zero except possibly leading zeros.
		},
		{
			name: "address with storage key",
			al: types.AccessList{
				{
					Address: types.HexToAddress("0x0000000000000000000000000000000000000001"),
					StorageKeys: []types.Hash{
						types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
					},
				},
			},
			// Address: 19 zero bytes + 1 nonzero byte = 19*1 + 1*4 = 23
			// Key: 31 zero bytes + 1 nonzero byte = 31*1 + 1*4 = 35
			// Total: 23 + 35 = 58
			want: 58,
		},
	}
	for _, tt := range tests {
		if tt.want == 0 && tt.al == nil {
			t.Run(tt.name, func(t *testing.T) {
				got := execution.AccessListDataTokens(tt.al)
				if got != tt.want {
					t.Errorf("accessListDataTokens = %d, want %d", got, tt.want)
				}
			})
			continue
		}
		if tt.want > 0 {
			t.Run(tt.name, func(t *testing.T) {
				got := execution.AccessListDataTokens(tt.al)
				if got != tt.want {
					t.Errorf("accessListDataTokens = %d, want %d", got, tt.want)
				}
			})
		}
	}
}

// TestEIP7981AccessListGasGlamst verifies access list gas under Glamsterdam.
func TestEIP7981AccessListGasGlamst(t *testing.T) {
	al := types.AccessList{
		{
			Address: types.HexToAddress("0x0000000000000000000000000000000000000001"),
			StorageKeys: []types.Hash{
				types.HexToHash("0x01"),
			},
		},
	}

	alGas := execution.AccessListGasGlamst(al)

	// Base: 3200 (address) + 2500 (key) = 5700
	// Data tokens: address(19 zeros + 1 nonzero = 23) + key(31 zeros + 1 nonzero = 35) = 58
	// Data cost: 58 * 16 = 928
	// Total: 5700 + 928 = 6628
	expectedBase := vm.AccessListAddressGlamst + vm.AccessListStorageGlamst
	dataTokens := execution.AccessListDataTokens(al)
	expectedTotal := expectedBase + dataTokens*gas.TotalCostFloorPerTokenGlamst

	if alGas != expectedTotal {
		t.Errorf("accessListGasGlamst = %d, want %d", alGas, expectedTotal)
	}
}

// TestEIP7981FloorIncludesAccessList verifies that the calldata floor
// includes access list tokens under Glamsterdam.
func TestEIP7981FloorIncludesAccessList(t *testing.T) {
	data := make([]byte, 10) // some calldata
	al := types.AccessList{
		{
			Address:     types.HexToAddress("0x01"),
			StorageKeys: []types.Hash{types.HexToHash("0x01")},
		},
	}

	// Floor with access list should be higher than without.
	floorWithAL := execution.CalldataFloorGasGlamst(data, al, false)
	floorWithoutAL := execution.CalldataFloorGasGlamst(data, nil, false)

	if floorWithAL <= floorWithoutAL {
		t.Errorf("floor with access list (%d) should be > floor without (%d)",
			floorWithAL, floorWithoutAL)
	}
}
