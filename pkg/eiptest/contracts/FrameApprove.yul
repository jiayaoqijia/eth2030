// SPDX-License-Identifier: CC0-1.0
object "FrameApprove" {
    code {
        // APPROVE takes offset, length, and scope from the top of the stack.
        // Scope 2 approves both sender execution and payer gas settlement.
        verbatim_0i_0o(hex"600260006000aa")
    }
}
