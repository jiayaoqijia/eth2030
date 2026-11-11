package pqc

import "github.com/pornin/go-fn-dsa/fndsa"

// FalconSigner uses the public FN-DSA-512 provider for the historical FALCON512 ID.
type FalconSigner struct{}

func (*FalconSigner) Algorithm() PQAlgorithm { return FALCON512 }

func (*FalconSigner) GenerateKey() (*PQKeyPair, error) {
	secret, public, err := fndsa.KeyGen(9, nil)
	if err != nil {
		return nil, err
	}
	return &PQKeyPair{Algorithm: FALCON512, PublicKey: public, SecretKey: secret}, nil
}

func (*FalconSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(sk) != Falcon512SecKeySize {
		return nil, ErrInvalidKeySize
	}
	return fndsa.Sign(nil, sk, nil, 0, msg)
}

func (*FalconSigner) Verify(pk, msg, sig []byte) bool {
	return len(msg) != 0 && len(pk) == Falcon512PubKeySize && len(sig) == Falcon512SigSize &&
		fndsa.Verify(pk, nil, 0, msg, sig)
}
