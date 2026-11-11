package eiptest

// Registry maps every EIP in the implementation status list (IFLOW.md
// "EIP Implementation Status") to its integration test surface.
//
// The gate (registry_test.go) enforces two invariants:
//  1. every EIP number parsed from IFLOW.md has a Registry entry;
//  2. every non-exempt entry points to an existing test file in this package.
//
// Tier semantics:
//   - TierA: pure state-transition behavior, fully verifiable in-process.
//   - TierB: cross-component flows (txpool → builder → engine → validator).
//   - TierC: encoding/vector-driven coverage (RLP/SSZ tables).
//   - TierD: multi-node/network behavior — only observable on the devnet
//     feature layer (pkg/devnet/kurtosis); exempt from in-process files.
//   - TierE: consensus-layer (CL) logic with no EL-side chain surface;
//     covered by pkg/consensus unit tests; exempt.
type Tier string

const (
	TierA Tier = "A"
	TierB Tier = "B"
	TierC Tier = "C"
	TierD Tier = "D"
	TierE Tier = "E"
)

// Surface names used in entry coverage lists.
type Surface string

const (
	SurfaceSTF      Surface = "stf"
	SurfacePool     Surface = "pool"
	SurfaceFork     Surface = "fork"
	SurfaceEncoding Surface = "encoding"
	SurfaceRPC      Surface = "rpc"
	SurfaceEngine   Surface = "engine"
	SurfaceDiff     Surface = "diff"
	SurfaceDevnet   Surface = "devnet"
)

// EIPEntry registers one EIP's integration test surface.
type EIPEntry struct {
	Number   int       // EIP number (0 for the PQC aggregate entry)
	Title    string    // short name
	Tier     Tier      // coverage tier
	Surfaces []Surface // surfaces exercised by the test file
	TestFile string    // file in pkg/eiptest (e.g. "eip7702_test.go"); empty iff Exempt set
	Exempt   string    // documented exemption reason (must be non-empty iff TestFile empty)
}

// Registry is the authoritative coverage table. Keep sorted by Number.
var Registry = []EIPEntry{
	{150, "call gas all-but-one-64th", TierA, []Surface{SurfaceSTF}, "eip150_test.go", ""},
	{152, "BLAKE2F precompile", TierA, []Surface{SurfaceSTF, SurfaceEncoding}, "eip152_test.go", ""},
	{196, "BN254 add/mul precompiles", TierA, []Surface{SurfaceSTF}, "eip196_test.go", ""},
	{197, "BN254 pairing precompile", TierA, []Surface{SurfaceSTF}, "eip197_test.go", ""},
	{1153, "transient storage", TierA, []Surface{SurfaceSTF}, "eip1153_test.go", ""},
	{1559, "fee market", TierA, []Surface{SurfaceSTF, SurfacePool}, "eip1559_test.go", ""},
	{2200, "SSTORE repricing", TierA, []Surface{SurfaceSTF}, "eip2200_test.go", ""},
	{2537, "BLS12-381 precompiles", TierA, []Surface{SurfaceSTF}, "eip2537_test.go", ""},
	{2718, "typed transaction envelope", TierA, []Surface{SurfaceSTF, SurfaceEncoding}, "eip2718_test.go", ""},
	{2929, "cold/warm access gas", TierA, []Surface{SurfaceSTF}, "eip2929_test.go", ""},
	{2930, "optional access list tx", TierA, []Surface{SurfaceSTF, SurfaceEncoding}, "eip2930_test.go", ""},
	{2935, "block history serve contract", TierA, []Surface{SurfaceSTF}, "eip2935_test.go", ""},
	{3529, "gas refund reduction", TierA, []Surface{SurfaceSTF}, "eip3529_test.go", ""},
	{3540, "EOF container validation", TierA, []Surface{SurfaceSTF}, "eip3540_test.go", ""},
	{4444, "history expiry", TierD, nil, "", "multi-node storage ops; covered by core/rawdb/history tests and devnet blob/history features"},
	{4762, "statelessness gas (witness mode)", TierB, nil, "", "requires witness-mode state; covered by core/vm eip4762_gas tests and pkg/witness suite"},
	{4788, "beacon root system contract", TierA, []Surface{SurfaceSTF}, "eip4788_test.go", ""},
	{4844, "blob transactions + KZG", TierB, []Surface{SurfaceSTF, SurfaceEngine}, "eip4844_test.go", ""},
	{4895, "beacon withdrawals", TierA, []Surface{SurfaceSTF}, "eip4895_test.go", ""},
	{5656, "MCOPY", TierA, []Surface{SurfaceSTF}, "eip5656_test.go", ""},
	{6110, "deposits via logs", TierB, []Surface{SurfaceSTF}, "eip6110_test.go", ""},
	{6404, "SSZ transactions", TierC, []Surface{SurfaceEncoding}, "eip6404_test.go", ""},
	{6780, "selfdestruct restriction", TierA, []Surface{SurfaceSTF}, "eip6780_test.go", ""},
	{7002, "withdrawal requests", TierB, []Surface{SurfaceSTF}, "eip7002_test.go", ""},
	{7069, "EOF ext calls", TierC, nil, "", "covered by core/vm EOF opcode unit tests; chain-level requires EOF container authoring"},
	{7251, "max effective balance (CL)", TierE, nil, "", "consensus-layer logic; covered by consensus/validator tests"},
	{7480, "EOF data section", TierC, nil, "", "covered by core/vm EOF opcode unit tests"},
	{7547, "inclusion list transactions", TierB, []Surface{SurfaceSTF}, "eip7547_test.go", ""},
	{7549, "attestation committee index (CL)", TierE, nil, "", "consensus-layer aggregation; covered by consensus tests"},
	{7594, "PeerDAS sampling", TierD, nil, "", "multi-node sampling; math covered by das/ unit tests, protocol by devnet peerdas-throughput feature"},
	{7620, "EOF create", TierC, nil, "", "covered by core/vm EOF opcode unit tests"},
	{7685, "execution requests", TierB, []Surface{SurfaceSTF}, "eip7685_test.go", ""},
	{7691, "blob throughput schedule", TierA, []Surface{SurfaceFork}, "eip7691_test.go", ""},
	{7698, "EOF create transactions", TierC, []Surface{SurfaceSTF}, "eip7698_test.go", ""},
	{7701, "native account abstraction", TierB, []Surface{SurfaceSTF, SurfacePool}, "eip7701_test.go", ""},
	{7702, "set code transactions", TierA, []Surface{SurfaceSTF, SurfacePool, SurfaceEncoding, SurfaceFork}, "eip7702_test.go", ""},
	{7706, "calldata gas header fields", TierA, []Surface{SurfaceSTF, SurfaceFork}, "eip7706_test.go", ""},
	{7742, "uncoupled blob target/max", TierA, []Surface{SurfaceFork}, "eip7742_test.go", ""},
	{7745, "log index root", TierC, []Surface{SurfaceSTF, SurfaceEncoding}, "eip7745_test.go", ""},
	{7805, "FOCIL inclusion lists", TierB, []Surface{SurfaceSTF, SurfaceEngine}, "eip7805_test.go", ""},
	{7807, "SSZ blocks", TierC, []Surface{SurfaceEncoding}, "eip7807_test.go", ""},
	{7825, "transaction gas cap", TierA, []Surface{SurfacePool, SurfaceSTF}, "eip7825_test.go", ""},
	{7864, "binary tree state", TierC, nil, "", "state commitment format; covered by trie/bintrie suite (17 files) and trie migration tests"},
	{7898, "uncoupled execution payload", TierB, nil, "", "covered by engine/api uncoupled payload tests"},
	{7904, "Glamsterdam repricing", TierA, []Surface{SurfaceSTF}, "eip7904_test.go", ""},
	{7916, "SSZ progressive lists", TierC, []Surface{SurfaceEncoding}, "eip7916_test.go", ""},
	{7918, "blob base fee floor", TierA, []Surface{SurfaceSTF}, "eip7918_test.go", ""},
	{7928, "block access lists", TierB, []Surface{SurfaceSTF, SurfaceEngine}, "eip7928_test.go", ""},
	{7939, "count leading zeros", TierA, []Surface{SurfaceSTF, SurfaceEncoding}, "eip7939_test.go", ""},
	{8024, "DUPN/SWAPN/EXCHANGE", TierA, []Surface{SurfaceSTF}, "eip8024_test.go", ""},
	{8025, "execution witness", TierB, []Surface{SurfaceSTF}, "eip8025_test.go", ""},
	{8070, "sparse blobpool", TierB, []Surface{SurfacePool}, "eip8070_test.go", ""},
	{8077, "announce nonce (ETH/72)", TierD, nil, "", "p2p protocol message; covered by eth/announce_nonce tests and devnet"},
	{8079, "native rollups EXECUTE", TierA, []Surface{SurfaceSTF}, "eip8079_test.go", ""},
	{8141, "frame transactions", TierB, []Surface{SurfaceSTF, SurfacePool}, "eip8141_test.go", ""},
	{7732, "ePBS", TierB, []Surface{SurfaceEngine}, "eip7732_test.go", ""},
	{0, "PQC (Dilithium/ML-DSA/Falcon/SPHINCS+)", TierC, nil, "", "crypto vector coverage in crypto/pqc; tx codec in core/types/pq_transaction tests"},
}

// Lookup returns the registry entry for an EIP number.
func Lookup(number int) (EIPEntry, bool) {
	for _, e := range Registry {
		if e.Number == number {
			return e, true
		}
	}
	return EIPEntry{}, false
}
