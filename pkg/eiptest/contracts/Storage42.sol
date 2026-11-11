// SPDX-License-Identifier: MIT
// Storage42 — stores a value at a slot; scenario contract for gas/storage
// integration tests (EIP-2200, EIP-7928, state continuity).
pragma solidity ^0.8.20;

contract Storage42 {
    uint256 public stored;

    constructor(uint256 v) {
        stored = v;
    }

    function set(uint256 v) external {
        stored = v;
    }

    function get() external view returns (uint256) {
        return stored;
    }
}
