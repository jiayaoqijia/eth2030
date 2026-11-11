// SPDX-License-Identifier: CC0-1.0
pragma solidity ^0.8.24;

contract DelegateCaller {
    function setThrough(address target, uint256 value) external {
        (bool ok,) = target.delegatecall(abi.encodeWithSignature("set(uint256)", value));
        require(ok);
    }
}
