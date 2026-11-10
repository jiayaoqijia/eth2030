package eiptest

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Ether is one ether in wei.
var Ether = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// Gwei is one gwei in wei.
var Gwei = big.NewInt(1_000_000_000)

// Account is a funded simulation account with a real secp256k1 key.
type Account struct {
	Key     *ecdsa.PrivateKey
	Address types.Address
	Name    string
}

// KeyRing holds the standard simulation accounts. Keys are fixed hex values so
// addresses (and therefore genesis allocs) are deterministic across runs.
type KeyRing struct {
	Alice   *Account // primary sender
	Bob     *Account // secondary sender / delegate target
	Charlie *Account
	Dave    *Account
	Erin    *Account
	Faythe  *Account
	Extra   []*Account // additional scratch accounts (OnDemand via s.Account(i))
}

// Fixed test keys (64 hex chars each, secp256k1 scalars). These are throwaway
// framework keys, never used with real funds.
var keyRingHex = []string{
	"1111111111111111111111111111111111111111111111111111111111111111",
	"2222222222222222222222222222222222222222222222222222222222222222",
	"3333333333333333333333333333333333333333333333333333333333333333",
	"4444444444444444444444444444444444444444444444444444444444444444",
	"5555555555555555555555555555555555555555555555555555555555555555",
	"6666666666666666666666666666666666666666666666666666666666666666",
}

var keyRingNames = []string{"alice", "bob", "charlie", "dave", "erin", "faythe"}

func newAccount(name, hexKey string) (*Account, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("eiptest: bad key hex for %s: %w", name, err)
	}
	key := new(ecdsa.PrivateKey)
	key.D = new(big.Int).SetBytes(raw)
	key.PublicKey.Curve = crypto.S256()
	key.PublicKey.X, key.PublicKey.Y = key.PublicKey.Curve.ScalarBaseMult(raw)
	return &Account{Key: key, Address: crypto.PubkeyToAddress(key.PublicKey), Name: name}, nil
}

func newKeyRing() (*KeyRing, error) {
	kr := &KeyRing{}
	accounts := make([]*Account, 0, len(keyRingHex))
	for i, hexKey := range keyRingHex {
		acc, err := newAccount(keyRingNames[i], hexKey)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	kr.Alice, kr.Bob, kr.Charlie, kr.Dave, kr.Erin, kr.Faythe =
		accounts[0], accounts[1], accounts[2], accounts[3], accounts[4], accounts[5]
	return kr, nil
}

// Alice/Bob/Charlie/Dave/Erin/Faythe return the named standard accounts.
func (s *Sim) Alice() *Account   { return s.keyAccounts[0] }
func (s *Sim) Bob() *Account     { return s.keyAccounts[1] }
func (s *Sim) Charlie() *Account { return s.keyAccounts[2] }
func (s *Sim) Dave() *Account    { return s.keyAccounts[3] }
func (s *Sim) Erin() *Account    { return s.keyAccounts[4] }
func (s *Sim) Faythe() *Account  { return s.keyAccounts[5] }

// Account returns the i-th standard account (Alice=0, Bob=1, ...). Index 6+
// generates additional deterministic scratch accounts on demand; scratch
// accounts are NOT genesis-funded, send them funds first.
func (s *Sim) Account(i int) *Account {
	if i < len(s.keyAccounts) {
		return s.keyAccounts[i]
	}
	name := fmt.Sprintf("acct%d", i)
	hexKey := fmt.Sprintf("%064x", i+100)
	acc, err := newAccount(name, hexKey)
	if err != nil {
		panic(err) // deterministic input; unreachable
	}
	for len(s.keyAccounts) < i {
		s.keyAccounts = append(s.keyAccounts, nil)
	}
	s.keyAccounts = append(s.keyAccounts, acc)
	return acc
}
