// SPDX-License-Identifier: CC0-1.0
pragma solidity ^0.8.24;

contract AAEntrypoint {
    uint256 public executions;

    function execute() external {
        executions++;
    }
}
