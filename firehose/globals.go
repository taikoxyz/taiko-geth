package firehose

// Enabled determines if firehose instrumentation is enabled. Controlling
// firehose behavior is then controlled via other flag like.
var Enabled = false

// SyncInstrumentationEnabled determines if standard block syncing prints to standard
// console or if it discards it entirely and does not print nothing.
//
// This feature is enabled by default, can be disabled on a miner node used for
// speculative execution.
var SyncInstrumentationEnabled = true

// MiningEnabled determines if mining code should stay enabled even when firehose
// is active. In normal production setup, we always activate firehose on syncer node
// only. However, on local development setup, one might need to test speculative execution
// code locally. To achieve this, it's possible to enable firehose on miner, disable
// sync instrumentation and enable mining. This way, new blocks are mined, sync logs are
// not printed and speculative execution log can be accumulated.
var MiningEnabled = false

// BlockProgressEnabled enable output of finalize block line only.
//
// Currently, when taking backups, the best way to know about current
// last block seen is to track firehose logging. However, while doing
// the actual backups, it's not necessary to print all firehose logs.
//
// This settings will only affect printing the finalized block log line
// and will not impact any other firehose logs. If you need firehose
// instrumentation, activate firehose. The firehose setting has
// precedence over this setting.
var BlockProgressEnabled = false

// GenesisConfig keeps globally for the process the genesis config of the chain.
// The genesis config extracted from the initialization code of Geth, otherwise
// the operator will need to set the flag `--firehose-genesis-file` pointing
// it to correct genesis.json file for the chain.
//
// **Note** We use `interface{}` here instead of `*core.Genesis` because we otherwise
// have a compilation cycle because `core` package already uses `firehose` package.
// Consumer of this library make the cast back to the correct types when needed.
var GenesisConfig interface{}
