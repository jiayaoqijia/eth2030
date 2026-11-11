package pqc

// NTT supports the separate experimental polynomial API.

var kyberZetas = [128]int16{
	1, 1729, 2580, 3289, 2642, 630, 1897, 848,
	1062, 1919, 193, 797, 2786, 3260, 569, 1746,
	296, 2447, 1339, 1476, 3046, 56, 2240, 1333,
	1426, 2094, 535, 2882, 2393, 2879, 1974, 821,
	289, 331, 3253, 1756, 1197, 2304, 2277, 2055,
	650, 1977, 2513, 632, 2865, 33, 1320, 1915,
	2319, 1435, 807, 452, 1438, 2868, 1534, 2402,
	2647, 2617, 1481, 648, 2474, 3110, 1227, 910,
	17, 2761, 583, 2649, 1637, 723, 2288, 1100,
	1409, 2662, 3281, 233, 756, 2156, 3015, 3050,
	1703, 1651, 2789, 1789, 1847, 952, 1461, 2687,
	939, 2308, 2437, 2388, 733, 2337, 268, 641,
	1584, 2298, 2037, 3220, 375, 2549, 2090, 1645,
	1063, 319, 2773, 757, 2099, 561, 2466, 2594,
	2804, 1092, 403, 1026, 1143, 2150, 2775, 886,
	1722, 1212, 1874, 1029, 2110, 2935, 885, 2154,
}

// bitReverse7 reverses the lower 7 bits of x.
func bitReverse7(x int) int {
	var r int
	for i := 0; i < 7; i++ {
		r = (r << 1) | (x & 1)
		x >>= 1
	}
	return r
}

// NTT performs the Number Theoretic Transform on polynomial a (mod q).
// Returns the NTT-domain representation.
func NTT(a []int16, q int16) []int16 {
	n := len(a)
	out := make([]int16, n)
	copy(out, a)

	k := 1
	for length := n / 2; length >= 2; length /= 2 {
		for start := 0; start < n; start += 2 * length {
			zeta := kyberZetas[k]
			k++
			for j := start; j < start+length; j++ {
				t := mulModQ(zeta, out[j+length], q)
				out[j+length] = modQ16(out[j]-t, q)
				out[j] = modQ16(out[j]+t, q)
			}
		}
	}
	return out
}

// InverseNTT performs the inverse NTT, converting from NTT domain back
// to coefficient domain.
func InverseNTT(a []int16, q int16) []int16 {
	n := len(a)
	out := make([]int16, n)
	copy(out, a)

	k := 127
	for length := 2; length <= n/2; length *= 2 {
		for start := 0; start < n; start += 2 * length {
			zeta := kyberZetas[k]
			k--
			for j := start; j < start+length; j++ {
				t := out[j]
				out[j] = modQ16(t+out[j+length], q)
				out[j+length] = mulModQ(zeta, modQ16(out[j+length]-t, q), q)
			}
		}
	}

	// Scale by (n/2)^(-1) mod q. The NTT butterfly stops at length=2
	// (not length=1), giving 7 levels for n=256, so we divide by n/2.
	nInv := modInverse(int16(n/2), q)
	for i := range out {
		out[i] = mulModQ(out[i], nInv, q)
	}
	return out
}

func modQ16(x, q int16) int16 {
	r := int32(x) % int32(q)
	if r < 0 {
		r += int32(q)
	}
	return int16(r)
}

func mulModQ(a, b, q int16) int16 {
	r := (int32(a) * int32(b)) % int32(q)
	if r < 0 {
		r += int32(q)
	}
	return int16(r)
}

func modInverse(a, q int16) int16 {
	t, newT := int32(0), int32(1)
	r, newR := int32(q), int32(a)
	if newR < 0 {
		newR += int32(q)
	}
	for newR != 0 {
		quotient := r / newR
		t, newT = newT, t-quotient*newT
		r, newR = newR, r-quotient*newR
	}
	if t < 0 {
		t += int32(q)
	}
	return int16(t)
}
