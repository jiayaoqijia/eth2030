package crypto

// ipa_compat.go provides backward-compatible re-exports from crypto/ipa.
// Existing code that imports "github.com/eth2030/eth2030/crypto" and uses
// IPA/Banderwagon types continues to work unchanged. New code should import
// "github.com/eth2030/eth2030/crypto/ipa" directly.
//
// Note: CommitmentTree remains in the root crypto package because
// shielded_circuit.go uses its unexported helpers.

import (
	"math/big"

	"github.com/eth2030/eth2030/crypto/ipa"
)

// IPA/Banderwagon type aliases.
type (
	BanderPoint          = ipa.BanderPoint
	IPAProofData         = ipa.IPAProofData
	IPABackend           = ipa.IPABackend
	IPAIntegrationConfig = ipa.IPAIntegrationConfig
	PureGoIPABackend     = ipa.PureGoIPABackend
)

// IPA/Banderwagon constants.
const (
	NumPedersenGenerators = ipa.NumPedersenGenerators
	DefaultVectorSize     = ipa.DefaultVectorSize
	DefaultNumRounds      = ipa.DefaultNumRounds
	BanderwagonFieldSize  = ipa.BanderwagonFieldSize
)

// IPA integration error re-exports.
var (
	ErrIPANilProof          = ipa.ErrIPANilProof
	ErrIPANilA              = ipa.ErrIPANilA
	ErrIPALRLengthMismatch  = ipa.ErrIPALRLengthMismatch
	ErrIPAInvalidRoundCount = ipa.ErrIPAInvalidRoundCount
	ErrIPANilLPoint         = ipa.ErrIPANilLPoint
	ErrIPANilRPoint         = ipa.ErrIPANilRPoint
	ErrIPAInvalidVectorSize = ipa.ErrIPAInvalidVectorSize
	ErrIPABackendNil        = ipa.ErrIPABackendNil
	ErrIPAEmptyCommitment   = ipa.ErrIPAEmptyCommitment
	ErrIPAEmptyEvalPoint    = ipa.ErrIPAEmptyEvalPoint
)

// Banderwagon function wrappers.

func BanderIdentity() *BanderPoint                         { return ipa.BanderIdentity() }
func BanderGenerator() *BanderPoint                        { return ipa.BanderGenerator() }
func BanderFromAffine(x, y *big.Int) (*BanderPoint, error) { return ipa.BanderFromAffine(x, y) }
func BanderAdd(p1, p2 *BanderPoint) *BanderPoint           { return ipa.BanderAdd(p1, p2) }
func BanderDouble(p *BanderPoint) *BanderPoint             { return ipa.BanderDouble(p) }
func BanderNeg(p *BanderPoint) *BanderPoint                { return ipa.BanderNeg(p) }
func BanderScalarMul(p *BanderPoint, k *big.Int) *BanderPoint {
	return ipa.BanderScalarMul(p, k)
}
func BanderMSM(points []*BanderPoint, scalars []*big.Int) *BanderPoint {
	return ipa.BanderMSM(points, scalars)
}
func BanderEqual(p1, p2 *BanderPoint) bool { return ipa.BanderEqual(p1, p2) }
func BanderSerialize(p *BanderPoint) [32]byte {
	return ipa.BanderSerialize(p)
}
func BanderDeserialize(data [32]byte) (*BanderPoint, error) {
	return ipa.BanderDeserialize(data)
}
func BanderMapToField(p *BanderPoint) *big.Int { return ipa.BanderMapToField(p) }
func BanderMapToBytes(p *BanderPoint) [32]byte { return ipa.BanderMapToBytes(p) }
func BanderFr() *big.Int                       { return ipa.BanderFr() }
func BanderN() *big.Int                        { return ipa.BanderN() }

func GeneratePedersenGenerators() [NumPedersenGenerators]*BanderPoint {
	return ipa.GeneratePedersenGenerators()
}
func PedersenCommit(values []*big.Int) *BanderPoint  { return ipa.PedersenCommit(values) }
func PedersenCommitBytes(values []*big.Int) [32]byte { return ipa.PedersenCommitBytes(values) }

// IPA function wrappers.

func IPAProofSize(vectorLen int) int { return ipa.IPAProofSize(vectorLen) }
func IPAProve(generators []*BanderPoint, a, b []*big.Int, commitment *BanderPoint) (*IPAProofData, *big.Int, error) {
	return ipa.IPAProve(generators, a, b, commitment)
}
func IPAVerify(generators []*BanderPoint, commitment *BanderPoint, b []*big.Int, v *big.Int, proof *IPAProofData) (bool, error) {
	return ipa.IPAVerify(generators, commitment, b, v, proof)
}
func IPASerialize(proof *IPAProofData) []byte           { return ipa.IPASerialize(proof) }
func IPADeserialize(data []byte) (*IPAProofData, error) { return ipa.IPADeserialize(data) }

// IPA integration function wrappers.

func DefaultIPAIntegrationConfig() *IPAIntegrationConfig { return ipa.DefaultIPAIntegrationConfig() }
func ValidateIPAIntegrationConfig(cfg *IPAIntegrationConfig) error {
	return ipa.ValidateIPAIntegrationConfig(cfg)
}
func DefaultIPABackend() IPABackend              { return ipa.DefaultIPABackend() }
func SetIPABackend(b IPABackend)                 { ipa.SetIPABackend(b) }
func IPAIntegrationStatus() string               { return ipa.IPAIntegrationStatus() }
func ValidateIPAProof(proof *IPAProofData) error { return ipa.ValidateIPAProof(proof) }
func ValidateIPAProofForConfig(proof *IPAProofData, cfg *IPAIntegrationConfig) error {
	return ipa.ValidateIPAProofForConfig(proof, cfg)
}
func GenerateIPAGenerators(n int) []*BanderPoint { return ipa.GenerateIPAGenerators(n) }
func ComputeBVector(evalPoint *big.Int, vectorSize int) []*big.Int {
	return ipa.ComputeBVector(evalPoint, vectorSize)
}
func GenerateIPAChallenges(commitment *BanderPoint, v *big.Int, proof *IPAProofData) ([]*big.Int, error) {
	return ipa.GenerateIPAChallenges(commitment, v, proof)
}
func FoldScalar(challenges []*big.Int, index int) *big.Int { return ipa.FoldScalar(challenges, index) }
func DefaultIPAProofSize() int                             { return ipa.DefaultIPAProofSize() }
