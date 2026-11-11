// SPDX-License-Identifier: CC0-1.0
pragma solidity ^0.8.24;

contract RollupAnchor {
    bytes32 public anchor;

    function record(bytes32 root) external {
        anchor = root;
    }
}
