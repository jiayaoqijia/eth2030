package crypto

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	gethkeystore "github.com/ethereum/go-ethereum/accounts/keystore"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

// KeystoreConfig holds configuration for the keystore.
type KeystoreConfig struct {
	ScryptN int // CPU/memory cost parameter (default: 262144)
	ScryptR int // block size parameter (default: 8)
	ScryptP int // parallelization parameter (default: 1)
	KeyDir  string
}

// DefaultKeystoreConfig returns a KeystoreConfig with standard defaults.
func DefaultKeystoreConfig() KeystoreConfig {
	return KeystoreConfig{
		ScryptN: 262144,
		ScryptR: 8,
		ScryptP: 1,
		KeyDir:  "keystore",
	}
}

// EncryptedKey holds the encrypted key material and associated metadata.
type EncryptedKey struct {
	Address    types.Address
	ID         string // UUID v4
	Version    int    // always 3
	CipherText []byte
	IV         []byte
	Salt       []byte
	MAC        []byte
}

// Keystore manages encrypted private keys (thread-safe).
type Keystore struct {
	mu     sync.RWMutex
	config KeystoreConfig
	keys   map[types.Address]string
}

// NewKeystore creates a new Keystore with the given configuration.
// Zero-valued config fields are replaced with defaults.
func NewKeystore(config KeystoreConfig) *Keystore {
	if config.ScryptN == 0 {
		config.ScryptN = 262144
	}
	if config.ScryptR == 0 {
		config.ScryptR = 8
	}
	if config.ScryptP == 0 {
		config.ScryptP = 1
	}
	if config.KeyDir == "" {
		config.KeyDir = "keystore"
	}
	return &Keystore{
		config: config,
		keys:   make(map[types.Address]string),
	}
}

// StoreKey encrypts a private key with the given passphrase and stores it.
// The privateKey must be a 32-byte secp256k1 private key.
func (ks *Keystore) StoreKey(privateKey []byte, passphrase string) (*EncryptedKey, error) {
	key, encoded, err := ks.encryptKey(privateKey, passphrase)
	if err != nil {
		return nil, err
	}
	ks.mu.Lock()
	ks.keys[key.Address] = encoded
	ks.mu.Unlock()
	return key, nil
}

func (ks *Keystore) encryptKey(privateKey []byte, passphrase string) (*EncryptedKey, string, error) {
	priv, err := gethcrypto.ToECDSA(privateKey)
	if err != nil {
		return nil, "", err
	}
	if ks.config.ScryptR != 8 {
		return nil, "", errors.New("keystore: Web3 Secret Storage requires scrypt r=8")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, "", err
	}
	address := gethcrypto.PubkeyToAddress(priv.PublicKey)
	encoded, err := gethkeystore.EncryptKey(
		&gethkeystore.Key{Id: id, Address: address, PrivateKey: priv},
		passphrase, ks.config.ScryptN, ks.config.ScryptP,
	)
	if err != nil {
		return nil, "", err
	}
	var data struct {
		Crypto gethkeystore.CryptoJSON `json:"crypto"`
	}
	if err := json.Unmarshal(encoded, &data); err != nil {
		return nil, "", err
	}
	salt, ok := data.Crypto.KDFParams["salt"].(string)
	if !ok {
		return nil, "", errors.New("keystore: missing salt in encrypted key")
	}
	metadata := &EncryptedKey{Address: types.Address(address), ID: id.String(), Version: 3}
	for _, field := range []struct {
		src  string
		dest *[]byte
	}{
		{data.Crypto.CipherText, &metadata.CipherText},
		{data.Crypto.CipherParams.IV, &metadata.IV},
		{salt, &metadata.Salt},
		{data.Crypto.MAC, &metadata.MAC},
	} {
		*field.dest, err = hex.DecodeString(field.src)
		if err != nil {
			return nil, "", err
		}
	}
	return metadata, string(encoded), nil
}

// LoadKey decrypts and returns the private key for the given address.
func (ks *Keystore) LoadKey(address types.Address, passphrase string) ([]byte, error) {
	ks.mu.RLock()
	encoded, ok := ks.keys[address]
	ks.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("keystore: key not found for address %s", address.Hex())
	}
	key, err := gethkeystore.DecryptKey([]byte(encoded), passphrase)
	if err != nil {
		return nil, err
	}
	return gethcrypto.FromECDSA(key.PrivateKey), nil
}

// HasKey returns true if a key exists for the given address.
func (ks *Keystore) HasKey(address types.Address) bool {
	ks.mu.RLock()
	_, ok := ks.keys[address]
	ks.mu.RUnlock()
	return ok
}

// ListAddresses returns all addresses stored in the keystore.
func (ks *Keystore) ListAddresses() []types.Address {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	addrs := make([]types.Address, 0, len(ks.keys))
	for addr := range ks.keys {
		addrs = append(addrs, addr)
	}
	return addrs
}

// DeleteKey removes the key for the given address.
func (ks *Keystore) DeleteKey(address types.Address) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if _, ok := ks.keys[address]; !ok {
		return fmt.Errorf("keystore: key not found for address %s", address.Hex())
	}
	delete(ks.keys, address)
	return nil
}

// ChangePassphrase re-encrypts the key under a new passphrase.
func (ks *Keystore) ChangePassphrase(address types.Address, oldPass, newPass string) error {
	ks.mu.RLock()
	encoded, ok := ks.keys[address]
	ks.mu.RUnlock()
	if !ok {
		return fmt.Errorf("keystore: key not found for address %s", address.Hex())
	}
	key, err := gethkeystore.DecryptKey([]byte(encoded), oldPass)
	if err != nil {
		return err
	}
	_, replacement, err := ks.encryptKey(gethcrypto.FromECDSA(key.PrivateKey), newPass)
	if err != nil {
		return err
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.keys[address] != encoded {
		return errors.New("keystore: key changed during passphrase update")
	}
	ks.keys[address] = replacement
	return nil
}

// DeriveAddress derives the Ethereum address from a raw secp256k1 private key.
func DeriveAddress(privateKey []byte) types.Address {
	priv, err := gethcrypto.ToECDSA(privateKey)
	if err != nil {
		return types.Address{}
	}
	return types.Address(gethcrypto.PubkeyToAddress(priv.PublicKey))
}
