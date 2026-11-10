package pqc

const (
	SPHINCSPubKeySize = SPHINCSSha256PubKeySize
	SPHINCSSecKeySize = SPHINCSSha256SecKeySize
)

// Historical SPHINCS identifiers carry SLH-DSA-SHA2-128f data.
type SPHINCSPublicKey []byte
type SPHINCSPrivateKey []byte
type SPHINCSSignature []byte

func SPHINCSKeypair() (SPHINCSPublicKey, SPHINCSPrivateKey) {
	key, err := NewSPHINCSSigner().GenerateKey()
	if err != nil {
		return nil, nil
	}
	return key.PublicKey, key.SecretKey
}

func SPHINCSSign(sk SPHINCSPrivateKey, msg []byte) SPHINCSSignature {
	sig, err := NewSPHINCSSigner().Sign(sk, msg)
	if err != nil {
		return nil
	}
	return sig
}

func SPHINCSVerify(pk SPHINCSPublicKey, msg []byte, sig SPHINCSSignature) bool {
	return NewSPHINCSSigner().Verify(pk, msg, sig)
}
