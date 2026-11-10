package threshold

import (
	"errors"
	"math/big"
	"testing"
)

func TestRecoverOriginalShareFormat(t *testing.T) {
	// f(x) = 31 + 7x + 9x². The points use non-consecutive indices.
	shares := []Share{{2, big.NewInt(81)}, {4, big.NewInt(203)}, {7, big.NewInt(521)}}
	secret, err := LagrangeInterpolate(shares)
	if err != nil || secret.Cmp(big.NewInt(31)) != 0 {
		t.Fatalf("secret = %v, %v", secret, err)
	}
	shares[2].Index = 2
	if _, err := LagrangeInterpolate(shares); !errors.Is(err, ErrDuplicateShareIndex) {
		t.Fatalf("duplicate index = %v", err)
	}
}

func TestRecoverWrappedPrimeFieldShares(t *testing.T) {
	// f(x) = (q - 3) + 7x crosses the modulus at the first share.
	shares := []Share{{1, big.NewInt(4)}, {3, big.NewInt(18)}}
	secret, err := LagrangeInterpolate(shares)
	want := new(big.Int).Sub(sharePrime, big.NewInt(3))
	if err != nil || secret.Cmp(want) != 0 {
		t.Fatalf("secret = %v, %v; want %v", secret, err, want)
	}
}
