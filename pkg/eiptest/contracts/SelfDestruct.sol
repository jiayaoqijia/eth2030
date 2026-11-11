// SPDX-License-Identifier: MIT
// SelfDestruct — destructs to the beneficiary with storage write; scenario
// contract for EIP-3529/6780 semantics.
pragma solidity ^0.8.20;

contract SelfDestruct {
    address payable public beneficiary;

    constructor(address payable b) {
        beneficiary = b;
    }

    function destroy() external {
        selfdestruct(beneficiary);
    }

    function touch() external {
        assembly {
            sstore(0, 1)
        }
    }
}
