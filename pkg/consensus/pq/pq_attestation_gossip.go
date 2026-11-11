package pq

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// Version 2 uses an intact ML-DSA-65 public key and signature after the metadata.
const (
	gossipVersion      = uint16(2)
	gossipHeaderSize   = 2 + 8 + 8 + 32 + 8 + 8 + 8
	gossipPublicKeyEnd = gossipHeaderSize + types.DilithiumPubKeySize
	gossipMessageSize  = gossipPublicKeyEnd + types.DilithiumSigSize
)

var ErrGossipEncoding = errors.New("pq attestation gossip: invalid ML-DSA-65 encoding")

// EncodePQAttestationForGossip encodes the version, metadata, and complete PQ
// signature material. It copies the key and signature into the returned packet.
func EncodePQAttestationForGossip(att *PQAttestation) ([]byte, error) {
	if att == nil || len(att.PQPublicKey) != types.DilithiumPubKeySize ||
		len(att.PQSignature) != types.DilithiumSigSize {
		return nil, ErrGossipEncoding
	}
	out := make([]byte, gossipMessageSize)
	binary.BigEndian.PutUint16(out[:2], gossipVersion)
	binary.BigEndian.PutUint64(out[2:10], att.Slot)
	binary.BigEndian.PutUint64(out[10:18], att.CommitteeIndex)
	copy(out[18:50], att.BeaconBlockRoot[:])
	binary.BigEndian.PutUint64(out[50:58], att.SourceEpoch)
	binary.BigEndian.PutUint64(out[58:66], att.TargetEpoch)
	binary.BigEndian.PutUint64(out[66:74], att.ValidatorIndex)
	copy(out[gossipHeaderSize:gossipPublicKeyEnd], att.PQPublicKey)
	copy(out[gossipPublicKeyEnd:], att.PQSignature)
	return out, nil
}

// DecodePQAttestationFromGossip decodes a version 2 packet.
func DecodePQAttestationFromGossip(data []byte) (*PQAttestation, error) {
	if len(data) != gossipMessageSize || binary.BigEndian.Uint16(data[:2]) != gossipVersion {
		return nil, ErrGossipEncoding
	}
	return &PQAttestation{
		Slot:            binary.BigEndian.Uint64(data[2:10]),
		CommitteeIndex:  binary.BigEndian.Uint64(data[10:18]),
		BeaconBlockRoot: types.Hash(data[18:50]),
		SourceEpoch:     binary.BigEndian.Uint64(data[50:58]),
		TargetEpoch:     binary.BigEndian.Uint64(data[58:66]),
		ValidatorIndex:  binary.BigEndian.Uint64(data[66:74]),
		PQPublicKey:     bytes.Clone(data[gossipHeaderSize:gossipPublicKeyEnd]),
		PQSignature:     bytes.Clone(data[gossipPublicKeyEnd:]),
	}, nil
}
