package pqc

// PQSigner defines the interface for post-quantum signature operations.
type PQSigner interface {
	// GenerateKey creates a new key pair.
	GenerateKey() (*PQKeyPair, error)

	// Sign produces a signature over msg using the secret key sk.
	Sign(sk, msg []byte) ([]byte, error)

	// Verify checks that sig is a valid signature of msg under public key pk.
	Verify(pk, msg, sig []byte) bool

	// Algorithm returns the algorithm identifier.
	Algorithm() PQAlgorithm
}

// GetSigner returns a PQSigner for the given algorithm.
func GetSigner(alg PQAlgorithm) PQSigner {
	switch alg {
	case DILITHIUM3:
		return &DilithiumSigner{}
	case FALCON512:
		return &FalconSigner{}
	case SPHINCSSHA256:
		return NewSPHINCSSigner()
	default:
		return nil
	}
}
