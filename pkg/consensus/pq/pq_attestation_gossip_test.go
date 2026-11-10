package pq

import (
	"bytes"
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
)

func TestPQAttestationGossipVerifiesAfterDecoding(t *testing.T) {
	keys, err := pqc.GenerateDilithiumKey()
	if err != nil {
		t.Fatal(err)
	}
	att, err := CreatePQAttestation(88, 3, types.Hash{1}, 5, 6, 19, keys)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := EncodePQAttestationForGossip(att)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePQAttestationFromGossip(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ValidatorIndex != 19 || !bytes.Equal(keys.PublicKey, decoded.PQPublicKey) {
		t.Fatal("decoded validator or public key differs")
	}
	verifier := NewPQAttestationVerifier(nil)
	if ok, err := verifier.VerifyAttestation(decoded); err != nil || !ok {
		t.Fatalf("verify gossip attestation = %t, %v", ok, err)
	}

	wire[gossipPublicKeyEnd] ^= 1
	tampered, err := DecodePQAttestationFromGossip(wire)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := verifier.VerifyAttestation(tampered); ok {
		t.Fatal("changed signature verified")
	}
	wire[1] = 1
	if _, err := DecodePQAttestationFromGossip(wire); !errors.Is(err, ErrGossipEncoding) {
		t.Fatalf("old version error = %v", err)
	}
	if _, err := DecodePQAttestationFromGossip(wire[:len(wire)-1]); !errors.Is(err, ErrGossipEncoding) {
		t.Fatalf("truncated packet error = %v", err)
	}
}
