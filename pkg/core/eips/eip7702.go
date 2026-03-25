package eips

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/log"
)

const (
	// EIP-7702 authorization signing magic byte.
	// The authorization hash is: keccak256(0x05 || rlp([chain_id, address, nonce]))
	authMagic = 0x05
)

var (
	ErrAuthChainID    = errors.New("authorization chain ID mismatch")
	ErrAuthNonce      = errors.New("authorization nonce mismatch")
	ErrAuthSignature  = errors.New("authorization signature recovery failed")
	ErrAuthInvalidSig = errors.New("authorization signature values invalid")
)

// ProcessAuthorizations processes EIP-7702 authorization entries for a SetCode
// transaction. For each authorization, it verifies the chain ID, nonce, and
// signature, then sets the signer's code to a delegation designator pointing
// to the authorized address.
//
// Per EIP-7702, invalid authorizations are silently skipped (they do not cause
// the transaction to fail). The function only returns an error for truly
// unrecoverable situations.
func ProcessAuthorizations(statedb state.StateDB, authorizations []types.Authorization, chainID *big.Int) error {
	for i := range authorizations {
		if err := processOneAuthorization(statedb, &authorizations[i], chainID); err != nil {
			continue
		}
	}
	return nil
}

// processOneAuthorization processes a single authorization entry.
func processOneAuthorization(statedb state.StateDB, auth *types.Authorization, chainID *big.Int) error {
	// 1. Verify chain ID: must match the current chain, or be 0 (any-chain wildcard).
	if auth.ChainID != nil && auth.ChainID.Sign() != 0 {
		if chainID == nil || auth.ChainID.Cmp(chainID) != 0 {
			log.Debug("EIP-7702: chain ID mismatch", "auth_chain_id", auth.ChainID, "expected_chain_id", chainID)
			return ErrAuthChainID
		}
	}

	// 2. Recover the signer address from the authorization signature.
	signerAddr, err := RecoverAuthority(auth)
	if err != nil {
		if errors.Is(err, ErrAuthInvalidSig) {
			log.Debug("EIP-7702: invalid signature", "error", err)
			return ErrAuthInvalidSig
		}
		log.Debug("EIP-7702: signature recovery failed", "error", err)
		return fmt.Errorf("%w: %v", ErrAuthSignature, err)
	}

	// 3. Verify the nonce matches the signer's current nonce.
	currentNonce := statedb.GetNonce(signerAddr)
	log.Debug("EIP-7702: processing authorization", "signer", signerAddr, "auth_nonce", auth.Nonce, "current_nonce", currentNonce)
	if auth.Nonce != currentNonce {
		log.Warn("EIP-7702: nonce mismatch", "signer", signerAddr, "auth_nonce", auth.Nonce, "current_nonce", currentNonce)
		return ErrAuthNonce
	}

	// 4. Per EIP-7702: if the authority account is not empty (has balance,
	// nonce, or code), add a gas refund for the difference between
	// PER_EMPTY_ACCOUNT_COST and PER_AUTH_BASE_COST.
	// The intrinsic gas charges PER_EMPTY_ACCOUNT_COST upfront for each auth,
	// so we refund the difference when the account is not actually empty.
	// Note: We use !Empty() instead of Exist() because an account can exist
	// but still be empty (zero nonce, zero balance, empty code).
	if !statedb.Empty(signerAddr) {
		statedb.AddRefund(types.PerEmptyAccountCost - types.PerAuthBaseCost)
	}

	// 5. Set the signer's code to the delegation designator: 0xef0100 || address.
	delegationCode := types.AddressToDelegation(auth.Address)
	statedb.SetCode(signerAddr, delegationCode)

	// 6. Increment the signer's nonce.
	statedb.SetNonce(signerAddr, currentNonce+1)

	log.Info("EIP-7702: delegation set successfully", "signer", signerAddr, "delegate_to", auth.Address)
	return nil
}

// RecoverAuthority recovers the signer address from an EIP-7702 authorization.
// It computes keccak256(0x05 || rlp([chain_id, address, nonce])) and uses
// ecrecover to derive the signer's public key.
func RecoverAuthority(auth *types.Authorization) (types.Address, error) {
	// Validate V.
	v := byte(0)
	if auth.V != nil {
		if !auth.V.IsUint64() || auth.V.Uint64() > 1 {
			return types.Address{}, ErrAuthInvalidSig
		}
		v = byte(auth.V.Uint64())
	}

	// Validate R, S.
	if !crypto.ValidateSignatureValues(v, auth.R, auth.S, true) {
		return types.Address{}, ErrAuthInvalidSig
	}

	// Compute the authorization hash.
	authHash := computeAuthorizationHash(auth)

	// Build the 65-byte signature: R (32 bytes) || S (32 bytes) || V (1 byte).
	sig := make([]byte, 65)
	if auth.R != nil {
		rBytes := auth.R.Bytes()
		copy(sig[32-len(rBytes):32], rBytes)
	}
	if auth.S != nil {
		sBytes := auth.S.Bytes()
		copy(sig[64-len(sBytes):64], sBytes)
	}
	sig[64] = v

	pubBytes, err := crypto.Ecrecover(authHash, sig)
	if err != nil {
		return types.Address{}, fmt.Errorf("ecrecover: %w", err)
	}

	// Derive address from the recovered uncompressed public key.
	return types.BytesToAddress(crypto.Keccak256(pubBytes[1:])[12:]), nil
}

// AuthorizationHash returns the EIP-7702 authorization signing hash for the
// given authorization. This is the hash that must be signed to create a valid
// SetCode authorization. Exported for use by callers outside this package.
func AuthorizationHash(auth *types.Authorization) []byte {
	return computeAuthorizationHash(auth)
}

// computeAuthorizationHash computes the EIP-7702 authorization signing hash.
// Delegates to types.computeAuthHash via RecoverAuthority for actual signing.
// This function is kept for test backward compatibility.
func computeAuthorizationHash(auth *types.Authorization) []byte {
	// Re-implement to match the types package implementation exactly.
	chainIDBytes := encodeBigIntRLP(auth.ChainID)
	addressBytes := encodeFixedBytesRLP(auth.Address[:])
	nonceBytes := encodeUint64RLP(auth.Nonce)

	payload := make([]byte, 0, len(chainIDBytes)+len(addressBytes)+len(nonceBytes))
	payload = append(payload, chainIDBytes...)
	payload = append(payload, addressBytes...)
	payload = append(payload, nonceBytes...)

	listData := encodeListHeaderRLP(payload)

	msg := make([]byte, 0, 1+len(listData))
	msg = append(msg, authMagic)
	msg = append(msg, listData...)

	return crypto.Keccak256(msg)
}

// makeDelegationCode creates the delegation designator code: 0xef0100 || address.
func makeDelegationCode(addr types.Address) []byte {
	return types.AddressToDelegation(addr)
}

// IsDelegated checks if the given code starts with the EIP-7702 delegation
// designator prefix (0xef0100).
func IsDelegated(code []byte) bool {
	return types.HasDelegationPrefix(code)
}

// ResolveDelegation extracts the target address from EIP-7702 delegation code.
func ResolveDelegation(code []byte) (types.Address, bool) {
	return types.ParseDelegation(code)
}

// --- Simplified RLP encoding helpers ---
// These implement just enough RLP to encode the EIP-7702 authorization struct.
// Kept for backward compatibility with tests that use computeAuthorizationHash.

func encodeBigIntRLP(i *big.Int) []byte {
	if i == nil || i.Sign() == 0 {
		return []byte{0x80}
	}
	return encodeBytesRLP(i.Bytes())
}

func encodeUint64RLP(n uint64) []byte {
	if n == 0 {
		return []byte{0x80}
	}
	if n < 128 {
		return []byte{byte(n)}
	}
	b := make([]byte, 8)
	b[0] = byte(n >> 56)
	b[1] = byte(n >> 48)
	b[2] = byte(n >> 40)
	b[3] = byte(n >> 32)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return encodeBytesRLP(b)
}

func encodeBytesRLP(b []byte) []byte {
	if len(b) == 1 && b[0] < 128 {
		return b
	}
	if len(b) <= 55 {
		return append([]byte{0x80 + byte(len(b))}, b...)
	}
	lenBytes := encodeLength(uint64(len(b)))
	header := append([]byte{0xb7 + byte(len(lenBytes))}, lenBytes...)
	return append(header, b...)
}

func encodeFixedBytesRLP(b []byte) []byte {
	return encodeBytesRLP(b)
}

func encodeListHeaderRLP(payload []byte) []byte {
	if len(payload) <= 55 {
		return append([]byte{0xc0 + byte(len(payload))}, payload...)
	}
	lenBytes := encodeLength(uint64(len(payload)))
	header := append([]byte{0xf7 + byte(len(lenBytes))}, lenBytes...)
	return append(header, payload...)
}

func encodeLength(n uint64) []byte {
	if n < 256 {
		return []byte{byte(n)}
	}
	b := make([]byte, 8)
	b[0] = byte(n >> 56)
	b[1] = byte(n >> 48)
	b[2] = byte(n >> 40)
	b[3] = byte(n >> 32)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}
