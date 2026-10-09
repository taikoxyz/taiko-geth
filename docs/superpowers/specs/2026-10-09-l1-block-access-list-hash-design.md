# L1 block access list hash compatibility

## Decision and delivery

Add EIP-7928 header support in taiko-geth first, then update taiko-mono to consume
the resulting commit. The user approved two linked pull requests and requested
that the taiko-geth pull request be opened first.

The taiko-geth change starts from `e8c26b7d9345205edf3d6fdc88951ec69d1b3c79`
on the `taiko` branch. Work is isolated in branch
`codex/l1-block-access-list-hash`.

## Problem

Glamsterdam L1 headers include `blockAccessListHash`. The current `types.Header`
omits this field, so JSON decoding discards it and `Header.Hash()` computes an
incomplete RLP hash. taiko-client uses this computed hash both in anchor calldata
and when comparing canonical L1 headers with recorded L1 origins.

The existing `SlotNumber` field is already supported. The Ethereum header field
order after `RequestsHash` is `BlockAccessListHash`, then `SlotNumber`.

## Implementation boundary

Add an optional `BlockAccessListHash *common.Hash` field to `types.Header` between
`RequestsHash` and `SlotNumber`, with the JSON name `blockAccessListHash`.
Regenerate the header JSON and RLP encoders using the repository generators.
Extend `CopyHeader` to copy the hash value into an independent allocation.
Existing `Block.Header()` and `Block.Hash()` must preserve the field through
their existing header-copy and hashing paths.

The ordinary typed JSON decoder validates the hash length. The RLP decoder
validates the field's encoding and length. No error fallback may silently drop
a present but malformed BAL hash.

This patch adds data-model compatibility for reading L1 headers. It does not
activate Amsterdam on Taiko, implement BAL execution or validation, change
Taiko's Engine API, or include the separate Gloas activation-block lookup fix.

## Compatibility requirements

For supported pre-Glamsterdam L1 and existing Taiko L2 headers, the new field and
`SlotNumber` are absent. Both optional trailing fields must remain omitted, so
their RLP bytes and block hashes remain unchanged.

For Glamsterdam headers, BAL hash must precede slot number in RLP. A header with
a non-nil slot number necessarily encodes the preceding BAL position. Do not
invent a second chain-specific header encoding to preserve synthetic pre-BAL
slot-bearing headers; existing Taiko networks do not activate that schema.

## Verification

1. A deterministic Glamsterdam header fixture has an independently established
   RLP encoding and expected hash. JSON decoding, RLP round trips, and block
   construction must all preserve that expected hash and both new-era fields.
2. Mutating the BAL hash changes the computed header hash, preventing a
   regression that silently ignores the field.
3. `CopyHeader` and `Block.Header()` preserve the field without sharing its
   mutable pointer with their input.
4. Existing supported L1 and Taiko L2 header fixtures retain their exact RLP bytes
   and hashes with the new optional field absent.
5. Malformed BAL hash JSON and malformed RLP fail explicitly.
6. Run the complete `core/types` suite, affected `ethclient` tests, generated-code
   consistency checks, formatting checks, and `git diff --check`. Compile the
   downstream taiko-client against the patch before opening its dependency PR.

The taiko-geth PR describes the compatibility boundary and verification results.
The subsequent taiko-mono PR pins the taiko-geth commit and tests the affected
L1 anchor/reorg paths. Neither PR is merged or deployed as part of this request.

## References

- https://eips.ethereum.org/EIPS/eip-7928
- https://eips.ethereum.org/EIPS/eip-7843
- https://github.com/ethereum/go-ethereum/blob/v1.17.7/core/types/block.go
