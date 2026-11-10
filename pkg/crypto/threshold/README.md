# Encrypted mempool shares

`LagrangeInterpolate` reconstructs the secret at zero from the existing one-based shares over the prime `2^255 - 18057` using TNO Shamir. The encrypted transaction pool hashes the reconstructed field element to derive its decryption key.

The unused standalone Feldman and ElGamal prototype was retired.
