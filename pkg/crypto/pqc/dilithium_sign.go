package pqc

const (
	DSign3PubKeyBytes = MLDSAPublicKeySize
	DSign3SecKeyBytes = MLDSAPrivateKeySize
	DSign3SigBytes    = MLDSASignatureSize
)

type DSign3PublicKey []byte
type DSign3PrivateKey []byte
type DSign3Signature []byte

func DilithiumKeypair() (DSign3PublicKey, DSign3PrivateKey) {
	keys, err := (&DilithiumSigner{}).GenerateKey()
	if err != nil {
		return nil, nil
	}
	return keys.PublicKey, keys.SecretKey
}

func DilithiumSign(sk DSign3PrivateKey, msg []byte) DSign3Signature {
	sig, err := (&DilithiumSigner{}).Sign(sk, msg)
	if err != nil {
		return nil
	}
	return sig
}

func DilithiumVerify(pk DSign3PublicKey, msg []byte, sig DSign3Signature) bool {
	return (&DilithiumSigner{}).Verify(pk, msg, sig)
}

func DilithiumBatchVerify(pks []DSign3PublicKey, msgs [][]byte, sigs []DSign3Signature) bool {
	if len(pks) != len(msgs) || len(pks) != len(sigs) || len(pks) == 0 {
		return false
	}
	for i := range pks {
		if !DilithiumVerify(pks[i], msgs[i], sigs[i]) {
			return false
		}
	}
	return true
}
