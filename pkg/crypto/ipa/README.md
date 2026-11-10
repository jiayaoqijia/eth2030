# Verkle polynomial openings

`Prove` uses crate-crypto/go-ipa to commit to 256 evaluations of a polynomial and produce an opening. `Verify` consumes the 32-byte compressed Banderwagon commitment, canonical little-endian evaluation point and claimed value, and the 544-byte proof. The transcript is domain-separated with `eth2030/verkle-ipa/v1`. Ethereum Verkle state multiproofs have a separate protocol.

The adapter replaces the former variable-length inner-product implementation and its legacy serialization.
