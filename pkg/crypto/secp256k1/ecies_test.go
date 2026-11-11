package secp256k1

import (
	"testing"

	"github.com/ethereum/go-ethereum/crypto/ecies"
)

func TestECIESEmptyMessageGethCompatible(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := ECIESEncrypt(&key.PublicKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ecies.ImportECDSA(key).Decrypt(ciphertext, nil, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("Geth ECIES decrypt = %x, %v", got, err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := ECIESDecrypt(key, ciphertext); err == nil {
		t.Fatal("modified empty ciphertext accepted")
	}
}
