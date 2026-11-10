// Package threshold reconstructs Ethereum 2030 encrypted-mempool shares.
package threshold

import (
	"errors"
	"math/big"

	"github.com/TNO-MPC/shamir"
)

var (
	ErrInsufficientShares  = errors.New("threshold: insufficient shares for reconstruction")
	ErrDuplicateShareIndex = errors.New("threshold: duplicate share index")
	ErrInvalidShare        = errors.New("threshold: invalid share")
)

// Existing shares use q = 2^255 - 18057, the order of the original subgroup.
var sharePrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(18057))

// Share contains an evaluation of the share polynomial at a one-based index.
type Share struct {
	Index int
	Value *big.Int
}

// LagrangeInterpolate reconstructs f(0) in the original prime field.
func LagrangeInterpolate(shares []Share) (*big.Int, error) {
	if len(shares) == 0 {
		return nil, ErrInsufficientShares
	}
	q := sharePrime
	points := make([]shamir.Share, len(shares))
	seen := make(map[int]bool, len(shares))
	for i, part := range shares {
		if seen[part.Index] {
			return nil, ErrDuplicateShareIndex
		}
		if part.Index < 1 || part.Value == nil {
			return nil, ErrInvalidShare
		}
		seen[part.Index] = true
		points[i] = shamir.Share{
			FieldSize: q,
			Degree:    len(shares) - 1,
			X:         part.Index,
			Y:         new(big.Int).Mod(part.Value, q),
		}
	}
	return shamir.ShareCombine(points)
}
