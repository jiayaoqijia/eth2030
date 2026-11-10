// BLS12-381 integration adapter for switching between pure-Go and blst backends.
//
// This file provides a BLSBackend interface that abstracts the BLS signature
// verification operations needed by the Ethereum consensus layer. Two backend
// implementations are provided:
//
//   - PureGoBLSBackend: uses the existing pure-Go BLS12-381 implementation
//     from this package (correct but slow, suitable for testing)
//   - BlstBLSBackend: documents the blst CGO-based adapter for production
//     (requires github.com/supranational/blst with build tag "blst")
//
// The active backend can be switched at runtime via SetBLSBackend, which is
// useful for testing. DefaultBLSBackend returns the currently active backend.
//
// Known Ethereum BLS constants and test vectors from the consensus spec are
// included for validation and testing purposes.
//
// Ethereum BLS signature scheme (MinPk variant):
//   - Public keys in G1 (48-byte compressed)
//   - Signatures in G2 (96-byte compressed)
//   - DST: BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_
//   - Hash-to-curve: SHA-256 based expand_message_xmd
package bls

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"
)

// BLS12-381 well-known constants from the Ethereum consensus spec.
var (
	// BLSG1GeneratorCompressed is the compressed form of the BLS12-381 G1
	// generator point (48 bytes). This is the standard generator used across
	// all Ethereum BLS operations.
	//
	// Source: BLS12-381 specification, also used in consensus-specs
	// polynomial-commitments.md G1_POINT_AT_INFINITY reference.
	BLSG1GeneratorCompressed = mustDecodeHex48(
		"97f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb")

	// BLSG2GeneratorCompressed is the compressed form of the BLS12-381 G2
	// generator point (96 bytes).
	//
	// Source: BLS12-381 specification.
	BLSG2GeneratorCompressed = mustDecodeHex96(
		"93e02b6052719f607dacd3a088274f65596bd0d09920b61ab5da61bbdc7f5049334cf11213945d57e5ac7d055d042b7e" +
			"024aa2b2f08f0a91260805272dc51051c6e47ad4fa403b02b4510b647ae3d1770bac0326a805bbefd48056c8c121bdb8")

	// BLSPointAtInfinityG1 is the compressed form of the G1 point at
	// infinity (48 bytes, 0xc0 followed by zeros).
	//
	// Matches G1_POINT_AT_INFINITY in the consensus spec.
	BLSPointAtInfinityG1 = func() [48]byte {
		var b [48]byte
		b[0] = 0xc0
		return b
	}()

	// BLSPointAtInfinityG2 is the compressed form of the G2 point at
	// infinity (96 bytes, 0xc0 followed by zeros).
	BLSPointAtInfinityG2 = func() [96]byte {
		var b [96]byte
		b[0] = 0xc0
		return b
	}()

	// BLSSignatureDST is the domain separation tag used for Ethereum BLS
	// signatures following the "proof of possession" scheme.
	//
	// Source: consensus-specs/specs/phase0/beacon-chain.md
	BLSSignatureDST = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

	// BLSSubgroupOrder is the order r of the BLS12-381 G1/G2 subgroups.
	// r = 0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001
	//
	// This is also known as BLS_MODULUS in the context of KZG scalar fields.
	BLSSubgroupOrder, _ = new(big.Int).SetString(
		"73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
)

// BLS format validation errors.
var (
	ErrBLSInvalidPubkeyLen    = errors.New("bls: pubkey must be 48 bytes")
	ErrBLSInvalidPubkeyFormat = errors.New("bls: invalid compressed G1 format")
	ErrBLSInvalidPubkeyInf    = errors.New("bls: pubkey is point at infinity")
	ErrBLSInvalidSigLen       = errors.New("bls: signature must be 96 bytes")
	ErrBLSInvalidSigFormat    = errors.New("bls: invalid compressed G2 format")
)

// BLSBackend is the interface for BLS12-381 signature verification operations.
// Implementations may use pure-Go arithmetic or optimized native libraries
// such as blst.
type BLSBackend interface {
	// Verify checks a single BLS signature.
	// pubkey: 48-byte compressed G1, msg: arbitrary message, sig: 96-byte compressed G2.
	Verify(pubkey, msg, sig []byte) bool

	// AggregateVerify checks an aggregate signature where each signer signed
	// a different message. pubkeys[i] signed msgs[i], and sig is the aggregate.
	AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool

	// FastAggregateVerify checks an aggregate signature where all signers
	// signed the same message. This is the common case for attestations.
	FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool

	// Name returns a human-readable name for the backend.
	Name() string
}

// activeBLSBackend is the currently selected BLS backend.
var (
	activeBLSMu      sync.RWMutex
	activeBLSBackend BLSBackend = &PureGoBLSBackend{}
)

// DefaultBLSBackend returns the currently active BLS backend.
func DefaultBLSBackend() BLSBackend {
	activeBLSMu.RLock()
	defer activeBLSMu.RUnlock()
	return activeBLSBackend
}

// SetBLSBackend sets the active BLS backend. This is safe for concurrent use.
// Passing nil resets to the default pure-Go backend.
func SetBLSBackend(b BLSBackend) {
	activeBLSMu.Lock()
	defer activeBLSMu.Unlock()
	if b == nil {
		b = &PureGoBLSBackend{}
	}
	activeBLSBackend = b
}

// BLSIntegrationStatus returns the name of the currently active BLS backend.
func BLSIntegrationStatus() string {
	return DefaultBLSBackend().Name()
}

// BLSVerifyWithBackend verifies a BLS signature using the specified backend.
func BLSVerifyWithBackend(backend BLSBackend, pubkey, msg, sig []byte) bool {
	if backend == nil {
		return false
	}
	return backend.Verify(pubkey, msg, sig)
}

// ValidateBLSPubkey validates a 48-byte compressed G1 public key.
// It checks length, compression flag, and that the point is not the identity.
func ValidateBLSPubkey(pubkey []byte) error {
	if len(pubkey) != BLSPubkeySize {
		return ErrBLSInvalidPubkeyLen
	}
	// Compression flag (bit 7 of first byte) must be set.
	if pubkey[0]&0x80 == 0 {
		return ErrBLSInvalidPubkeyFormat
	}
	// Infinity flag (bit 6): if set, this is the point at infinity, which
	// is not a valid public key.
	if pubkey[0]&0x40 != 0 {
		return ErrBLSInvalidPubkeyInf
	}
	// Extract x coordinate (clear flag bits) and check it is less than p.
	buf := make([]byte, BLSPubkeySize)
	copy(buf, pubkey)
	buf[0] &= 0x1F
	x := new(big.Int).SetBytes(buf)
	if x.Cmp(blsP) >= 0 {
		return ErrBLSInvalidPubkeyFormat
	}
	return nil
}

// ValidateBLSSignature validates a 96-byte compressed G2 signature.
// It checks length and the compression flag.
func ValidateBLSSignature(sig []byte) error {
	if len(sig) != BLSSignatureSize {
		return ErrBLSInvalidSigLen
	}
	// Compression flag must be set.
	if sig[0]&0x80 == 0 {
		return ErrBLSInvalidSigFormat
	}
	return nil
}

// --- PureGoBLSBackend ---

// PureGoBLSBackend implements BLSBackend using the pure-Go BLS12-381
// implementation from this package. It delegates to BLSVerify,
// VerifyAggregate, and FastAggregateVerify.
type PureGoBLSBackend struct{}

func (b *PureGoBLSBackend) Name() string { return "pure-go" }

func (b *PureGoBLSBackend) Verify(pubkey, msg, sig []byte) bool {
	if len(pubkey) != BLSPubkeySize || len(sig) != BLSSignatureSize {
		return false
	}
	var pk [BLSPubkeySize]byte
	var s [BLSSignatureSize]byte
	copy(pk[:], pubkey)
	copy(s[:], sig)

	return BLSVerify(pk, msg, s)
}

func (b *PureGoBLSBackend) AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool {
	if len(pubkeys) == 0 || len(pubkeys) != len(msgs) || len(sig) != BLSSignatureSize {
		return false
	}
	pks := make([][BLSPubkeySize]byte, len(pubkeys))
	for i, pk := range pubkeys {
		if len(pk) != BLSPubkeySize {
			return false
		}
		copy(pks[i][:], pk)
	}
	var s [BLSSignatureSize]byte
	copy(s[:], sig)

	return VerifyAggregate(pks, msgs, s)
}

func (b *PureGoBLSBackend) FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool {
	if len(pubkeys) == 0 || len(sig) != BLSSignatureSize {
		return false
	}
	pks := make([][BLSPubkeySize]byte, len(pubkeys))
	for i, pk := range pubkeys {
		if len(pk) != BLSPubkeySize {
			return false
		}
		copy(pks[i][:], pk)
	}
	var s [BLSSignatureSize]byte
	copy(s[:], sig)

	return FastAggregateVerify(pks, msg, s)
}

// --- BlstBLSBackend ---

// BlstBLSBackend is a build-tag-ready adapter for the blst library
// (github.com/supranational/blst). It documents the exact blst API calls
// that would be used in a production deployment.
//
// To enable this backend, build with `-tags blst` and provide an
// implementation that calls the blst Go bindings:
//
//	// Verify: single signature verification
//	func (b *BlstBLSBackend) Verify(pubkey, msg, sig []byte) bool {
//	    pk := new(blst.P1Affine).Uncompress(pubkey)
//	    if pk == nil { return false }
//	    s := new(blst.P2Affine).Uncompress(sig)
//	    if s == nil { return false }
//	    return s.Verify(true, pk, true, msg, BLSSignatureDST)
//	}
//
//	// AggregateVerify: each signer signed a different message
//	func (b *BlstBLSBackend) AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool {
//	    s := new(blst.P2Affine).Uncompress(sig)
//	    if s == nil { return false }
//	    pks := make([]*blst.P1Affine, len(pubkeys))
//	    for i, pk := range pubkeys {
//	        pks[i] = new(blst.P1Affine).Uncompress(pk)
//	        if pks[i] == nil { return false }
//	    }
//	    blstMsgs := make([]blst.Message, len(msgs))
//	    for i, m := range msgs { blstMsgs[i] = m }
//	    return s.AggregateVerify(true, pks, true, blstMsgs, BLSSignatureDST)
//	}
//
//	// FastAggregateVerify: all signers signed the same message
//	func (b *BlstBLSBackend) FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool {
//	    s := new(blst.P2Affine).Uncompress(sig)
//	    if s == nil { return false }
//	    pks := make([]*blst.P1Affine, len(pubkeys))
//	    for i, pk := range pubkeys {
//	        pks[i] = new(blst.P1Affine).Uncompress(pk)
//	        if pks[i] == nil { return false }
//	    }
//	    return s.FastAggregateVerify(true, pks, msg, BLSSignatureDST)
//	}
//
// The BlstBLSBackend struct below is a placeholder that always returns false.
// When blst CGO support is enabled, replace this with the real implementation.
type BlstBLSBackend struct{}

func (b *BlstBLSBackend) Name() string { return "blst" }

func (b *BlstBLSBackend) Verify(pubkey, msg, sig []byte) bool {
	// Placeholder: requires blst CGO build tag.
	return false
}

func (b *BlstBLSBackend) AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool {
	return false
}

func (b *BlstBLSBackend) FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool {
	return false
}

// --- Test vector types ---

// BLSTestVector represents a test case for BLS signature verification.
type BLSTestVector struct {
	Name      string
	SecretKey *big.Int
	Message   []byte
	// Pubkey and Signature are populated by Sign during init.
	Pubkey    [BLSPubkeySize]byte
	Signature [BLSSignatureSize]byte
}

// blsTestVectors contains known test vectors generated using the pure-Go
// BLS implementation with known secret keys. These are used to validate
// that any backend produces consistent results.
//
// The secret keys are small integers for reproducibility. In production,
// secret keys must be cryptographically random.
var blsTestVectors []BLSTestVector

func init() {
	secrets := []struct {
		name   string
		secret int64
		msg    string
	}{
		{"small_secret_hello", 42, "hello"},
		{"medium_secret_world", 1337, "world"},
		{"large_secret_eth2030", 999999, "eth2030 consensus"},
	}
	for _, s := range secrets {
		sk := big.NewInt(s.secret)
		pk := BLSPubkeyFromSecret(sk)
		sig := BLSSign(sk, []byte(s.msg))
		blsTestVectors = append(blsTestVectors, BLSTestVector{
			Name:      s.name,
			SecretKey: sk,
			Message:   []byte(s.msg),
			Pubkey:    pk,
			Signature: sig,
		})
	}
}

// GetBLSTestVectors returns the built-in BLS test vectors.
func GetBLSTestVectors() []BLSTestVector {
	result := make([]BLSTestVector, len(blsTestVectors))
	copy(result, blsTestVectors)
	return result
}

// --- Helpers ---

func mustDecodeHex48(s string) [48]byte {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 48 {
		panic(fmt.Sprintf("invalid hex for 48-byte value: %s", s))
	}
	var out [48]byte
	copy(out[:], b)
	return out
}

func mustDecodeHex96(s string) [96]byte {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 96 {
		panic(fmt.Sprintf("invalid hex for 96-byte value: %s", s))
	}
	var out [96]byte
	copy(out[:], b)
	return out
}
