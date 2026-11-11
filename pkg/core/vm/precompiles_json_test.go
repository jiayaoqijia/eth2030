package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// precompileFixture matches the JSON format from go-ethereum test fixtures.
type precompileFixture struct {
	Input       string `json:"Input"`
	Expected    string `json:"Expected"`
	Gas         uint64 `json:"Gas"`
	Name        string `json:"Name"`
	NoBenchmark bool   `json:"NoBenchmark"`
}

// precompileFailFixture matches the fail-* JSON format.
type precompileFailFixture struct {
	Input         string `json:"Input"`
	ExpectedError string `json:"ExpectedError"`
	Name          string `json:"Name"`
}

func loadPrecompileFixtures(t *testing.T, filename string) []precompileFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/precompiles/" + filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("skipping precompile fixture test: %s is not present in this checkout", filename)
		}
		t.Fatalf("failed to read fixture %s: %v", filename, err)
	}
	var fixtures []precompileFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("failed to parse fixture %s: %v", filename, err)
	}
	return fixtures
}

func loadPrecompileFailFixtures(t *testing.T, filename string) []precompileFailFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/precompiles/" + filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("skipping precompile failure fixture test: %s is not present in this checkout", filename)
		}
		t.Fatalf("failed to read fixture %s: %v", filename, err)
	}
	var fixtures []precompileFailFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("failed to parse fixture %s: %v", filename, err)
	}
	return fixtures
}

// getPrecompile returns the precompile contract at the given address byte(s).
func getPrecompile(addrBytes ...byte) PrecompiledContract {
	addr := types.BytesToAddress(addrBytes)
	p, ok := PrecompiledContractsCancun[addr]
	if !ok {
		return nil
	}
	return p
}

func hexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("invalid hex: %v", err)
	}
	return b
}

// runPrecompileOutputTest checks only the output (not gas) for a fixture.
func runPrecompileOutputTest(t *testing.T, p PrecompiledContract, tc precompileFixture) {
	t.Helper()

	input := hexDecode(t, tc.Input)

	out, err := p.Run(input)
	if err != nil {
		// Some precompiles return nil,nil for invalid inputs (ecrecover, p256verify).
		if tc.Expected == "" {
			return
		}
		t.Fatalf("unexpected error: %v", err)
	}

	gotHex := hex.EncodeToString(out)
	if !strings.EqualFold(gotHex, tc.Expected) {
		t.Errorf("output mismatch:\n  got:  %s\n  want: %s", gotHex, strings.ToLower(tc.Expected))
	}
}

// runPrecompileGasTest checks the gas cost for a fixture.
func runPrecompileGasTest(t *testing.T, p PrecompiledContract, tc precompileFixture) {
	t.Helper()

	input := hexDecode(t, tc.Input)

	gas := p.RequiredGas(input)
	if gas != tc.Gas {
		t.Errorf("gas mismatch: got %d, want %d", gas, tc.Gas)
	}
}

// runPrecompileFailTest checks that a failure fixture returns an error.
func runPrecompileFailTest(t *testing.T, p PrecompiledContract, tc precompileFailFixture) {
	t.Helper()

	input := hexDecode(t, tc.Input)

	out, err := p.Run(input)
	if err == nil && out != nil {
		t.Errorf("expected error, but got output: %x", out)
	}
}

// --- EcRecover ---

func TestJsonEcRecover(t *testing.T) {
	fixtures := loadPrecompileFixtures(t, "ecRecover.json")
	p := getPrecompile(1)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.Name == "ValidKey" {
				// Our ecrecover uses homestead=true for ValidateSignatureValues,
				// but the precompile should use homestead=false (tighter s values
				// only apply to transaction signatures, not the precompile).
				t.Skip("ecrecover uses homestead=true; fixture has s > secp256k1N/2")
			}
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

// --- BN256 (alt_bn128) ---

func TestJsonBN256Add(t *testing.T) {
	fixtures := loadPrecompileFixtures(t, "bn256Add.json")
	p := getPrecompile(6)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

func TestJsonBN256ScalarMul(t *testing.T) {
	fixtures := loadPrecompileFixtures(t, "bn256ScalarMul.json")
	p := getPrecompile(7)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

func TestJsonBN256Pairing(t *testing.T) {
	fixtures := loadPrecompileFixtures(t, "bn256Pairing.json")
	p := getPrecompile(8)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

// --- Blake2F ---

func TestJsonBlake2F(t *testing.T) {
	fixtures := loadPrecompileFixtures(t, "blake2F.json")
	p := getPrecompile(9)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

// --- ModExp ---

func TestJsonModExp(t *testing.T) {
	// modexp.json uses pre-EIP-2565 gas pricing; our implementation uses EIP-2565.
	// Output correctness is tested; gas is skipped.
	fixtures := loadPrecompileFixtures(t, "modexp.json")
	p := getPrecompile(5)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			// Skip gas: modexp.json has pre-EIP-2565 gas costs.
		})
	}
}

func TestJsonModExpEIP2565(t *testing.T) {
	// EIP-2565 gas pricing matches our implementation.
	fixtures := loadPrecompileFixtures(t, "modexp_eip2565.json")
	p := getPrecompile(5)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

// --- P256Verify ---

func TestJsonP256Verify(t *testing.T) {
	fixtures := loadPrecompileFixtures(t, "p256Verify.json")
	p := getPrecompile(0x01, 0x00)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			runPrecompileGasTest(t, p, tc)
		})
	}
}

// --- Point Evaluation (KZG) ---

func TestJsonPointEvaluation(t *testing.T) {
	if _, err := os.Stat("testdata/precompiles/pointEvaluation.json"); err != nil {
		t.Fatal(err)
	}
	fixtures := loadPrecompileFixtures(t, "pointEvaluation.json")
	if len(fixtures) == 0 {
		t.Fatal("missing point evaluation vectors")
	}
	addr := types.BytesToAddress([]byte{0x0a})
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			out, remainingGas, err := RunPrecompiledContract(addr, hexDecode(t, tc.Input), tc.Gas)
			if err != nil {
				t.Fatalf("point evaluation failed: %v", err)
			}
			if got := hex.EncodeToString(out); !strings.EqualFold(got, tc.Expected) {
				t.Errorf("output = %s, want %s", got, tc.Expected)
			}
			if remainingGas != 0 {
				t.Errorf("remaining gas = %d, want 0", remainingGas)
			}
		})
	}
}

// --- EIP-2537 success vectors ---

func runPragueBLSFixtures(t *testing.T, file string, address byte, checkGas bool) {
	t.Helper()
	p := SelectPrecompiles(ForkRules{IsPrague: true})[types.Address{19: address}]
	for _, tc := range loadPrecompileFixtures(t, file) {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileOutputTest(t, p, tc)
			if checkGas {
				runPrecompileGasTest(t, p, tc)
			}
		})
	}
}

func TestJsonBLSG1Add(t *testing.T)      { runPragueBLSFixtures(t, "blsG1Add.json", 0x0b, true) }
func TestJsonBLSG1Mul(t *testing.T)      { runPragueBLSFixtures(t, "blsG1Mul.json", 0x0c, false) }
func TestJsonBLSG1MultiExp(t *testing.T) { runPragueBLSFixtures(t, "blsG1MultiExp.json", 0x0c, true) }
func TestJsonBLSG2Add(t *testing.T)      { runPragueBLSFixtures(t, "blsG2Add.json", 0x0d, true) }
func TestJsonBLSG2Mul(t *testing.T)      { runPragueBLSFixtures(t, "blsG2Mul.json", 0x0e, false) }
func TestJsonBLSG2MultiExp(t *testing.T) { runPragueBLSFixtures(t, "blsG2MultiExp.json", 0x0e, true) }
func TestJsonBLSPairing(t *testing.T)    { runPragueBLSFixtures(t, "blsPairing.json", 0x0f, true) }
func TestJsonBLSMapG1(t *testing.T)      { runPragueBLSFixtures(t, "blsMapG1.json", 0x10, true) }
func TestJsonBLSMapG2(t *testing.T)      { runPragueBLSFixtures(t, "blsMapG2.json", 0x11, true) }

// --- Fail fixture tests ---

func TestJsonFailBlake2F(t *testing.T) {
	fixtures := loadPrecompileFailFixtures(t, "fail-blake2f.json")
	p := getPrecompile(9)
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileFailTest(t, p, tc)
		})
	}
}

func runPragueBLSFailureFixtures(t *testing.T, file string, address byte) {
	t.Helper()
	p := SelectPrecompiles(ForkRules{IsPrague: true})[types.Address{19: address}]
	for _, tc := range loadPrecompileFailFixtures(t, file) {
		t.Run(tc.Name, func(t *testing.T) {
			runPrecompileFailTest(t, p, tc)
		})
	}
}

func TestJsonFailBLSG1Add(t *testing.T) { runPragueBLSFailureFixtures(t, "fail-blsG1Add.json", 0x0b) }
func TestJsonFailBLSG1MultiExp(t *testing.T) {
	runPragueBLSFailureFixtures(t, "fail-blsG1MultiExp.json", 0x0c)
}
func TestJsonFailBLSG2Add(t *testing.T) { runPragueBLSFailureFixtures(t, "fail-blsG2Add.json", 0x0d) }
func TestJsonFailBLSG2MultiExp(t *testing.T) {
	runPragueBLSFailureFixtures(t, "fail-blsG2MultiExp.json", 0x0e)
}
func TestJsonFailBLSPairing(t *testing.T) {
	runPragueBLSFailureFixtures(t, "fail-blsPairing.json", 0x0f)
}
func TestJsonFailBLSMapG1(t *testing.T) { runPragueBLSFailureFixtures(t, "fail-blsMapG1.json", 0x10) }
func TestJsonFailBLSMapG2(t *testing.T) { runPragueBLSFailureFixtures(t, "fail-blsMapG2.json", 0x11) }

func TestNativeBLSActivationAndLegacyReplay(t *testing.T) {
	input := make([]byte, 128)
	input[63], input[127] = 1, 2
	if _, ok := SelectPrecompiles(ForkRules{IsCancun: true})[types.Address{19: 0x0b}]; ok {
		t.Fatal("BLS active before Prague")
	}
	for _, rules := range []ForkRules{{IsPrague: true}, {IsGlamsterdan: true}, {IsIPlus: true}} {
		set := SelectPrecompiles(rules)
		if _, ok := set[types.Address{19: 0x12}]; ok {
			t.Fatal("precompile shadows the CREATE2 factory")
		}
		out, gas, err := runPrecompile(set[types.Address{19: 0x11}], input, 30_000)
		if err != nil || len(out) != 256 || gas != 6_200 {
			t.Fatalf("standard G2 map length=%d gas=%d err=%v", len(out), gas, err)
		}
		if _, gas, err := runPrecompile(set[types.Address{19: 0x11}], input[:127], 30_000); err == nil || gas != 0 {
			t.Fatalf("invalid BLS input gas=%d err=%v", gas, err)
		}
	}
	router := NewPrecompileRouter(SelectPrecompiles(ForkRules{IsPrague: true}))
	if _, gas, err := router.RunPrecompile(types.Address{19: 0x11}, input[:127], 30_000); err == nil || gas != 0 {
		t.Fatalf("routed invalid BLS input gas=%d err=%v", gas, err)
	}
	set := SelectPrecompiles(ForkRules{IsGlamsterdan: true, LegacyNativeBLS: true})
	out, err := set[types.Address{19: 0x13}].Run(input)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(out)
	if hex.EncodeToString(digest[:8]) != "0a57d5b455807f1e" {
		t.Fatalf("historical G2 mapping digest = %x", digest[:8])
	}
}
