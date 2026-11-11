package pqc

import (
	"crypto/rand"
	"crypto/sha3"

	"github.com/cloudflare/circl/sign"
	"github.com/cloudflare/circl/sign/slhdsa"
)

const (
	SPHINCSSignerSigSize = SPHINCSSha256SigSize
	SPHINCSSignerPKSize  = SPHINCSSha256PubKeySize
	SPHINCSSignerSKSize  = SPHINCSSha256SecKeySize
)

type SPHINCSSigner struct{}

func NewSPHINCSSigner() *SPHINCSSigner { return &SPHINCSSigner{} }

func (*SPHINCSSigner) Algorithm() PQAlgorithm { return SPHINCSSHA256 }

func (*SPHINCSSigner) GenerateKey() (*PQKeyPair, error) {
	public, private, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_128f)
	if err != nil {
		return nil, err
	}
	return packSLHDSAKey(public, private)
}

func (*SPHINCSSigner) GenerateKeyDeterministic(seed []byte) (*PQKeyPair, error) {
	scheme := slhdsa.SHA2_128f.Scheme()
	public, private := scheme.DeriveKey(sha3.SumSHAKE256(seed, scheme.SeedSize()))
	return packSLHDSAKey(public, private)
}

func packSLHDSAKey(public sign.PublicKey, private sign.PrivateKey) (*PQKeyPair, error) {
	pk, err := public.MarshalBinary()
	if err != nil {
		return nil, err
	}
	sk, err := private.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &PQKeyPair{Algorithm: SPHINCSSHA256, PublicKey: pk, SecretKey: sk}, nil
}

func (*SPHINCSSigner) Sign(sk, msg []byte) ([]byte, error) {
	if len(msg) == 0 {
		return nil, ErrMLDSAEmptyMsg
	}
	private := slhdsa.PrivateKey{ID: slhdsa.SHA2_128f}
	if err := private.UnmarshalBinary(sk); err != nil {
		return nil, err
	}
	return slhdsa.SignDeterministic(&private, slhdsa.NewMessage(msg), nil)
}

func (*SPHINCSSigner) Verify(pk, msg, sig []byte) bool {
	public := slhdsa.PublicKey{ID: slhdsa.SHA2_128f}
	return len(msg) != 0 && len(sig) == SPHINCSSha256SigSize &&
		public.UnmarshalBinary(pk) == nil &&
		slhdsa.Verify(&public, slhdsa.NewMessage(msg), sig, nil)
}

func SPHINCSRandomizedSign(sk SPHINCSPrivateKey, msg []byte) SPHINCSSignature {
	private := slhdsa.PrivateKey{ID: slhdsa.SHA2_128f}
	if private.UnmarshalBinary(sk) != nil {
		return nil
	}
	sig, err := slhdsa.SignRandomized(&private, rand.Reader, slhdsa.NewMessage(msg), nil)
	if err != nil {
		return nil
	}
	return sig
}

func SPHINCSVerifyDetailed(pk SPHINCSPublicKey, msg []byte, sig SPHINCSSignature) (bool, error) {
	if len(pk) != SPHINCSSha256PubKeySize {
		return false, ErrInvalidKeySize
	}
	if len(sig) != SPHINCSSha256SigSize {
		return false, ErrInvalidSigSize
	}
	if !SPHINCSVerify(pk, msg, sig) {
		return false, ErrVerifyFailed
	}
	return true, nil
}
