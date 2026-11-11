// Package ipa adapts the Verkle reference library's single polynomial openings.
package ipa

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/crate-crypto/go-ipa/bandersnatch/fr"
	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/crate-crypto/go-ipa/common"
	goipa "github.com/crate-crypto/go-ipa/ipa"
)

const (
	VectorSize      = common.VectorLength
	ProofSize       = 32 * (2*8 + 1)
	transcriptLabel = "eth2030/verkle-ipa/v1"
)

type Scalar = fr.Element

var settings = sync.OnceValues(goipa.NewIPASettings)

// Prove commits to an evaluation-domain vector and opens it at point.
func Prove(values *[VectorSize]Scalar, point Scalar) ([32]byte, []byte, error) {
	if values == nil {
		return [32]byte{}, nil, errors.New("ipa: missing evaluation vector")
	}
	config, err := settings()
	if err != nil {
		return [32]byte{}, nil, err
	}
	commitment := config.Commit(values[:])
	proof, err := goipa.CreateIPAProof(common.NewTranscript(transcriptLabel), config, commitment, values[:], point)
	if err != nil {
		return [32]byte{}, nil, err
	}
	var encoded bytes.Buffer
	if err := proof.Write(&encoded); err != nil {
		return [32]byte{}, nil, err
	}
	return commitment.Bytes(), encoded.Bytes(), nil
}

// Verify checks an opening against the claimed evaluation. The point and value
// use canonical little-endian scalar encodings.
func Verify(commitment, point, value [32]byte, encoded []byte) (bool, error) {
	if len(encoded) != ProofSize {
		return false, fmt.Errorf("ipa: proof length %d, want %d", len(encoded), ProofSize)
	}
	var c banderwagon.Element
	if err := c.SetBytes(commitment[:]); err != nil {
		return false, fmt.Errorf("ipa: commitment: %w", err)
	}
	var z, y Scalar
	if _, err := z.SetBytesLECanonical(point[:]); err != nil {
		return false, fmt.Errorf("ipa: evaluation point: %w", err)
	}
	if _, err := y.SetBytesLECanonical(value[:]); err != nil {
		return false, fmt.Errorf("ipa: claimed value: %w", err)
	}
	var proof goipa.IPAProof
	if err := proof.Read(bytes.NewReader(encoded)); err != nil {
		return false, err
	}
	config, err := settings()
	if err != nil {
		return false, err
	}
	return goipa.CheckIPAProof(common.NewTranscript(transcriptLabel), config, c, proof, z, y)
}
