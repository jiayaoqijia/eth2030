// SPDX-License-Identifier: CC0-1.0
object "FrameApproveExec" {
    code {
        // APPROVE(offset=0, length=0, scope=0): execution approval only.
        // Used by the sender's own contract in VERIFY frame 0 of a
        // sponsored frame transaction (EIP-7805-style gas sponsoring per
        // EIP-8141 §APPROVE scope operand).
        verbatim_0i_0o(hex"600060006000aa")
    }
}
