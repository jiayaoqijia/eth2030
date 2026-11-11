"""Amsterdam state-vector smoke test shared by EELS and Geth."""

import pytest
from execution_testing import Account, Alloc, Environment, Op, StateTestFiller, Transaction


@pytest.mark.valid_from("Amsterdam")
def test_amsterdam_calldatasize(state_test: StateTestFiller, pre: Alloc) -> None:
    """Read calldata size, persist it, and assert the post-state."""
    sender = pre.fund_eoa()
    target = pre.deploy_contract(Op.SSTORE(0, Op.CALLDATASIZE) + Op.STOP)
    state_test(
        env=Environment(base_fee_per_gas=0),
        pre=pre,
        tx=Transaction(
            sender=sender, to=target, gas_limit=100_000, gas_price=0, data=b"\x01\x02"
        ),
        post={target: Account(storage={0: 2})},
    )
