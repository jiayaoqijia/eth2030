package node

import (
	"crypto/ecdsa"
	"os"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// The data-directory lock is held before this runs. RLPx identity persists
// across restarts; an invalid existing key fails rather than changing identity.
func loadNodeKey(path string) (*ecdsa.PrivateKey, error) {
	key, err := gethcrypto.LoadECDSA(path)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key, err = gethcrypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	if err := gethcrypto.SaveECDSA(path, key); err != nil {
		return nil, err
	}
	return key, nil
}
