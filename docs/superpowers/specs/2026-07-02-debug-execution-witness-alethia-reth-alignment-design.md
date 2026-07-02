# debug_executionWitness Alethia-Reth Alignment Design

## Context

This branch already fixes absent-account account-trie proof collection in `StateDB.getStateObject` for witness-enabled execution. That is enough for `debug_executionWitnessForTxList` to include the missing proof nodes for absent system contracts such as EIP-2935 HistoryStorage and EIP-4788 BeaconRoots.

The remaining alignment gap is the canonical `debug_executionWitness` RPC response. Alethia-reth returns an `ExecutionWitness` with four byte-array fields: `state`, `codes`, `keys`, and `headers`. Its `headers` entries are RLP-encoded header bytes, and `keys` contains state-key preimages observed by its witness recorder.

Taiko-geth currently returns `stateless.ExtWitness` from `DebugAPI.ExecutionWitness`. That shape exposes `headers` as Go header objects and drops `keys` in `Witness.ToExtWitness`, even though `ExtWitness` has a `Keys` JSON field. The tx-list RPC already uses a custom converter that returns the alethia-reth-style byte-array shape.

## Goal

Fully align taiko-geth's canonical `debug_executionWitness` RPC wire shape with alethia-reth while keeping the existing internal stateless witness encoding stable.

The target RPC response shape is:

- `state`: array of hex-encoded MPT node bytes
- `codes`: array of hex-encoded bytecode bytes
- `keys`: array of hex-encoded unhashed address or storage-slot preimages
- `headers`: array of hex-encoded RLP header bytes

## Non-Goals

- Do not change consensus execution behavior.
- Do not change the existing witness collection mechanics beyond the current branch's absent-account prefetch.
- Do not change the internal `stateless.ExtWitness` RLP type unless implementation proves that an internal consumer requires key round-tripping.
- Do not try to make the output byte-for-byte identical to alethia-reth ordering. Map iteration order in taiko-geth is not stable; consumers must treat these arrays as sets.

## Recommended Approach

Introduce a shared RPC-only converter that turns `*stateless.Witness` into the alethia-reth-compatible debug RPC witness shape. Reuse it from both `debug_executionWitness` and `debug_executionWitnessForTxList`.

This keeps `stateless.ExtWitness` available for internal RLP/stateless payload flows and avoids changing engine/stateless payload consumers while making both debug RPCs return the same cross-client response contract.

## Design

### RPC Type

Replace the tx-list-only response type with a shared debug RPC response type, for example:

```go
type executionWitness struct {
    State   []hexutil.Bytes `json:"state"`
    Codes   []hexutil.Bytes `json:"codes"`
    Keys    []hexutil.Bytes `json:"keys"`
    Headers []hexutil.Bytes `json:"headers"`
}
```

The existing `txListExecutionWitness` can be renamed or aliased to this shared type to avoid duplicate converters.

### Converter

Move the existing tx-list converter into a shared helper such as `newExecutionWitness(w *stateless.Witness) (*executionWitness, error)`.

The helper will:

- copy each `w.State` node into `State`
- copy each `w.Codes` bytecode into `Codes`
- copy each `w.Keys` preimage into `Keys`
- RLP-encode each `w.Headers` entry into `Headers`
- return a contextual error if any header cannot be encoded

### Canonical RPC

Change `DebugAPI.ExecutionWitness` to return `(*executionWitness, error)` and call the shared converter on `result.Witness()`.

The execution path remains:

1. resolve block and parent
2. call `bc.ProcessBlock` with `MakeWitness: true`
3. convert the collected internal witness to the shared RPC shape

### Tx-List RPC

Keep `debug_executionWitnessForTxList` behavior unchanged except for routing through the shared converter and shared response type.

### Internal Encoding

Leave `stateless.ExtWitness`, `ToExtWitness`, and `FromExtWitness` unchanged in this change unless tests expose an internal round-trip problem. This avoids coupling the debug RPC alignment to stateless payload RLP compatibility.

## Error Handling

- Preserve existing block resolution and process-block errors from `debug_executionWitness`.
- If header RLP encoding fails, return an explicit conversion error from the RPC.
- Do not silently return a partial witness if conversion fails.

## Tests

Add focused tests covering the canonical path:

- `debug_executionWitness` returns non-empty `keys` for a witness that touches accounts and slots.
- `debug_executionWitness.headers[0]` RLP-decodes into a `types.Header`.
- On the absent-system-contract regression chain, canonical witness generation includes:
  - the absent HistoryStorage and BeaconRoots address key preimages, and
  - every account-trie exclusion-proof node for those absent contracts.

Keep existing tx-list tests green and update only type names if the shared response type changes.

## Success Criteria

- `debug_executionWitness` and `debug_executionWitnessForTxList` both return alethia-reth-compatible byte-array fields.
- The branch still fixes the original `state trie unresolved` issue for the tx-list RPC.
- No consensus or internal stateless payload behavior changes.
- Focused `go test ./eth ./core/state` witness suites pass.
