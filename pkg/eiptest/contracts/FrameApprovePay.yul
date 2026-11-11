// SPDX-License-Identifier: CC0-1.0
object "FrameApprovePay" {
    code {
        // APPROVE(offset=0, length=0, scope=1): payment approval only.
        // Used by the sponsor's contract in a later VERIFY frame; requires
        // sender approval from an earlier frame (EIP-8141 §APPROVE scope 1).
        verbatim_0i_0o(hex"600160006000aa")
    }
}
