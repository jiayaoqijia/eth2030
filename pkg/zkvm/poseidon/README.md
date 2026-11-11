# BN254 Poseidon hashes

- `Hash` absorbs the field-element count and the reduced field elements with the iden3 Poseidon sponge.
- `Hash2` uses the default gnark-crypto Poseidon2 field hasher. It absorbs a domain tag, the field-element count, and the reduced field elements.
- `Hash2Bytes` absorbs a separate domain tag, the byte count, and consecutive big-endian chunks of at most 31 bytes.

Digests produced by the previous project-local permutations require regeneration. The circuit builder computes the hash witness with `Hash` and records it as an oracle value in the current local constraint format.
