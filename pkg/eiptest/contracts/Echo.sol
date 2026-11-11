// SPDX-License-Identifier: MIT
// Echo — returns its calldata (ABI-encoded selector stripped not required;
// returns the raw input as-is via fallback); scenario contract for
// EIP-2929/2930 access-list warm/cold and EIP-150 call gas tests.
pragma solidity ^0.8.20;

contract Echo {
    fallback(bytes calldata input) external payable returns (bytes memory) {
        return input;
    }
}
