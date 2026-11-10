package pqc

// DilithiumSigner is the ML-DSA-65 provider for the historical DILITHIUM3 ID.
type DilithiumSigner struct{}

func (*DilithiumSigner) Algorithm() PQAlgorithm { return DILITHIUM3 }

func (*DilithiumSigner) GenerateKey() (*PQKeyPair, error) {
	key, err := NewMLDSASigner().GenerateKey()
	if err != nil {
		return nil, err
	}
	return &PQKeyPair{Algorithm: DILITHIUM3, PublicKey: key.PublicKey, SecretKey: key.SecretKey}, nil
}

func (*DilithiumSigner) Sign(sk, msg []byte) ([]byte, error) {
	return NewMLDSASigner().Sign(&MLDSAKeyPair{SecretKey: sk}, msg)
}

func (*DilithiumSigner) Verify(pk, msg, sig []byte) bool {
	return NewMLDSASigner().Verify(pk, msg, sig)
}
