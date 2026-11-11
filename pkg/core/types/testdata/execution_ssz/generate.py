"""Generate execution SSZ vectors with ethereum/remerkleable 2f0baeef0082.

Pass EIP-6404 and EIP-7807 Markdown from ethereum/EIPs master, and Electra
beacon-chain.md from the pinned consensus-specs checkout. Run with
PYTHONPATH pointing at the remerkleable checkout and redirect stdout to
vectors.json.
"""

import ast
import json
import re
import sys
from pathlib import Path

from remerkleable.basic import uint8, uint64, uint256
from remerkleable.byte_arrays import ByteList, ByteVector, Bytes32
from remerkleable.complex import Container, List
from remerkleable.progressive import (
    CompatibleUnion,
    ProgressiveByteList,
    ProgressiveContainer,
    ProgressiveList,
)


def load_classes(path, names, scope):
    """Evaluate only the SSZ schema declarations from the spec text."""
    pending = set(names)
    for source in re.findall(r"```python\n(.*?)```", Path(path).read_text(), re.S):
        if not any(f"class {name}(" in source for name in pending):
            continue
        for declaration in ast.parse(source).body:
            if isinstance(declaration, ast.ClassDef) and declaration.name in pending:
                node = ast.Module(body=[declaration], type_ignores=[])
                exec(compile(node, path, "exec"), scope)
                pending.remove(declaration.name)
    if pending:
        raise ValueError(f"missing SSZ declarations: {pending}")


def rlp_prefix(size, base):
    if size < 56:
        return bytes([base + size])
    length = size.to_bytes((size.bit_length() + 7) // 8, "big")
    return bytes([base + 55 + len(length)]) + length


def rlp(value):
    if isinstance(value, list):
        payload = b"".join(map(rlp, value))
        return rlp_prefix(len(payload), 0xC0) + payload
    if isinstance(value, int):
        value = value.to_bytes((value.bit_length() + 7) // 8, "big")
    if len(value) == 1 and value[0] < 0x80:
        return value
    return rlp_prefix(len(value), 0x80) + value


def signature(r, s, parity):
    return b"\x00" + r.to_bytes(32, "big") + s.to_bytes(32, "big") + bytes([parity])


def vector(view):
    return {"ssz": view.encode_bytes().hex(), "root": view.hash_tree_root().hex()}


def main(tx_spec, block_spec, electra_spec):
    scope = {
        **globals(),
        "TransactionType": uint8,
        "ChainId": uint256,
        "GasAmount": uint64,
        "FeePerGas": uint256,
        "Hash32": Bytes32,
        "Root": Bytes32,
        "VersionedHash": Bytes32,
        "ExecutionAddress": ByteVector[20],
        "ExecutionSignature": ProgressiveByteList,
        "MAX_EXTRA_DATA_BYTES": 32,
        "BLSPubkey": ByteVector[48],
        "BLSSignature": ByteVector[96],
        "Gwei": uint64,
        "MAX_DEPOSIT_REQUESTS_PER_PAYLOAD": 8192,
        "MAX_WITHDRAWAL_REQUESTS_PER_PAYLOAD": 16,
        "MAX_CONSOLIDATION_REQUESTS_PER_PAYLOAD": 2,
    }
    transaction_variants = [
        "RlpLegacyReplayableBasicTransactionPayload",
        "RlpLegacyReplayableCreateTransactionPayload",
        "RlpLegacyBasicTransactionPayload",
        "RlpLegacyCreateTransactionPayload",
        "RlpAccessListBasicTransactionPayload",
        "RlpAccessListCreateTransactionPayload",
        "RlpBasicTransactionPayload",
        "RlpCreateTransactionPayload",
        "RlpBlobTransactionPayload",
        "RlpSetCodeTransactionPayload",
    ]
    load_classes(tx_spec, transaction_variants + [
        "BasicFeesPerGas", "BlobFeesPerGas", "AccessTuple",
        "RlpReplayableBasicAuthorizationPayload", "RlpBasicAuthorizationPayload",
        "RlpSetCodeAuthorizationPayload", "RlpSetCodeAuthorization",
        "TransactionPayload", "Transaction",
    ], scope)
    load_classes(block_spec, ["GasAmounts", "ExecutionPayload"], scope)
    load_classes(electra_spec, [
        "DepositRequest", "WithdrawalRequest", "ConsolidationRequest", "ExecutionRequests",
    ], scope)

    to = b"\x35" * 20
    key = b"\xa0" * 32
    versioned_hash = b"\x01" + b"\x55" * 31
    auths = []
    auth_rlp = []
    for selector, chain_id in ((1, 0), (2, 1337)):
        auth_data = {"magic": 5, "address": to, "nonce": selector + 10}
        if chain_id:
            auth_data["chain_id"] = chain_id
        auth_type = scope["RlpReplayableBasicAuthorizationPayload" if selector == 1 else "RlpBasicAuthorizationPayload"]
        auth_payload = scope["RlpSetCodeAuthorizationPayload"](selector, auth_type(**auth_data))
        auths.append(scope["RlpSetCodeAuthorization"](payload=auth_payload, signature=signature(7, 8, 1)))
        auth_rlp.append([chain_id, to, selector + 10, 1, 7, 8])

    views, raws, tx_vectors = [], [], []
    for selector, variant in enumerate(transaction_variants, 1):
        tx_type = (0, 0, 0, 0, 1, 1, 2, 2, 3, 4)[selector - 1]
        recipient = b"" if selector in (2, 4, 6, 8) else to
        chain_id = 0 if selector == 1 else (1 if selector == 3 else 2**255 + 1337)
        nonce, fee, gas, value, data, parity, r, s = selector, 10, 100000, 7, b"\x99\x00", 1, 3, 5
        if selector == 3:
            # Published EIP-155 signed transaction example.
            nonce, fee, gas, value, data, parity = 9, 20 * 10**9, 21000, 10**18, b"", 0
            r = 18515461264373351373200002665853028612451056578545711640558177340181847433846
            s = 46948507304638947509940763649030358759909902576025900602547168820602576006531
        values = dict(
            type_=tx_type, chain_id=chain_id, nonce=nonce, gas=gas, to=recipient,
            value=value, input_=data,
            max_fees_per_gas=scope["BlobFeesPerGas"](regular=fee, blob=13) if tx_type == 3 else scope["BasicFeesPerGas"](regular=fee),
            access_list=[scope["AccessTuple"](address=to, storage_keys=[key])],
            max_priority_fees_per_gas=scope["BasicFeesPerGas"](regular=3),
            blob_versioned_hashes=[versioned_hash], authorization_list=auths,
        )
        view_type = scope[variant]
        view = scope["Transaction"](
            payload=scope["TransactionPayload"](selector, view_type(**{key: values[key] for key in view_type.fields()})),
            signature=signature(r, s, parity),
        )
        tail = [gas, recipient, value, data]
        if tx_type == 0:
            v = 27 + parity if selector < 3 else 35 + 2 * chain_id + parity
            raw = rlp([nonce, fee, *tail, v, r, s])
        else:
            fees = [fee] if tx_type == 1 else [3, fee]
            inner = [chain_id, nonce, *fees, *tail, [[to, [key]]]]
            if tx_type == 3:
                inner += [13, [versioned_hash]]
            if tx_type == 4:
                inner.append(auth_rlp)
            raw = bytes([tx_type]) + rlp([*inner, parity, r, s])
        tx_vectors.append({"name": variant, "rlp": raw.hex(), **vector(view)})
        views.append(view)
        raws.append(raw)

    deposit = scope["DepositRequest"](
        pubkey=b"\x01" * 48, withdrawal_credentials=b"\x02" * 32,
        amount=17, signature=b"\x03" * 96, index=4,
    )
    withdrawal_request = scope["WithdrawalRequest"](
        source_address=to, validator_pubkey=b"\x05" * 48, amount=19,
    )
    consolidation = scope["ConsolidationRequest"](
        source_address=to, source_pubkey=b"\x06" * 48, target_pubkey=b"\x07" * 48,
    )
    requests = scope["ExecutionRequests"](
        deposits=[deposit], withdrawals=[withdrawal_request], consolidations=[consolidation],
    )
    receipt = bytes([2]) + rlp([1, 21000, bytes(256), []])
    block_fields = dict(
        parent_hash=b"\x11" * 32, miner=to, state_root=b"\x12" * 32,
        transactions=[raws[6], raws[8]],
        receipts_root=ProgressiveList[ProgressiveByteList]([receipt]).hash_tree_root(),
        number=23, gas_limits=scope["GasAmounts"](regular=30_000_000, blob=786432),
        gas_used=scope["GasAmounts"](regular=21000, blob=131072), timestamp=1234,
        extra_data=b"example", mix_hash=b"\x13" * 32,
        base_fees_per_gas=scope["BlobFeesPerGas"](regular=7, blob=2**100), withdrawals=[rlp([7, 9, to, 11])],
        excess_gas=scope["GasAmounts"](regular=27, blob=131072), parent_beacon_block_root=b"\x14" * 32,
        requests_hash=requests.hash_tree_root(), block_access_list=b"\xc0", slot_number=42,
    )
    payload_type = scope["ExecutionPayload"]
    payload = payload_type(**block_fields)
    summary_fields = payload_type.fields().copy()
    summary_values = block_fields.copy()
    for name in ("transactions", "withdrawals", "block_access_list"):
        summary_fields[name] = Bytes32
        summary_values[name] = getattr(payload, name).hash_tree_root()
    summary_type = type("ExecutionHeader", (ProgressiveContainer(active_fields=[1] * 18),),
                        {"__annotations__": summary_fields})
    summary = summary_type(**summary_values)
    assert summary.hash_tree_root() == payload.hash_tree_root()
    output = {
        "reference": "ethereum/remerkleable@2f0baeef0082d4278acaef7d822deb7009d7db7e",
        "transactions": tx_vectors,
        "transactions_root": ProgressiveList[scope["Transaction"]](views).hash_tree_root().hex(),
        "payload": vector(payload),
        "summary": vector(summary),
        "requests": [item.encode_bytes().hex() for item in (deposit, withdrawal_request, consolidation)],
        "receipt": receipt.hex(),
    }
    print(json.dumps(output, indent=2))


if __name__ == "__main__":
    main(*sys.argv[1:])
