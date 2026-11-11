# Class-group verifiable delay functions

`Evaluate` derives a domain-separated seed and uses Harmony's class-group Wesolowski provider. `EncodeSolution` concatenates the serialized output and proof. `VerifySolution` verifies both against the seed and the expected iteration count. The adapter uses a 1024-bit discriminant and a 260-byte solution.

The experimental epoch reveal and encrypted-mempool timer use this format. `VDFv2`, `VDFChain`, and `VDFBeacon` provide batch evaluation, sequential chains, and application-level randomness. Batch digests bind the complete list of individual results and verification checks each result.
