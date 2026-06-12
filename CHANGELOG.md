# Changelog

## [2.4.0](https://github.com/taikoxyz/taiko-geth/compare/v2.3.0...v2.4.0) (2026-03-24)


### Features

* **miner:** revert Gattaca preconfirmation simulator API ([#533](https://github.com/taikoxyz/taiko-geth/issues/533)) ([2c45f27](https://github.com/taikoxyz/taiko-geth/commit/2c45f2787b30db6f3b9973eeeff8df8443538fcd))
* **taiko_genesis:** remove `shasta-transition-devnet` network ([#529](https://github.com/taikoxyz/taiko-geth/issues/529)) ([a280c91](https://github.com/taikoxyz/taiko-geth/commit/a280c912e03d881ceaf139efb9d92071cb3ff400))
* **taiko:** introduce auth api `taikoAuth_lastCertainBlockIDByBatchID` ([#536](https://github.com/taikoxyz/taiko-geth/issues/536)) ([f3f42a9](https://github.com/taikoxyz/taiko-geth/commit/f3f42a9813acd1a74fe6c5ce2d3f4a6ed75d26e2))


### Bug Fixes

* **rawdb:** add `hexutil.Bytes` marshaling override for `L1Origin.Signature` ([#534](https://github.com/taikoxyz/taiko-geth/issues/534)) ([01dfc26](https://github.com/taikoxyz/taiko-geth/commit/01dfc264ab781b98d89dd45cdfddb9c59a1dfa75))


### Chores

* **taiko_api_backend:** adjust payload attributes `baseFee` encoding and `L1Origin` signature ID type ([#531](https://github.com/taikoxyz/taiko-geth/issues/531)) ([3c2eee1](https://github.com/taikoxyz/taiko-geth/commit/3c2eee10ddf0749de9dee3636fb8f9fceca3ecfe))
* **taiko_genesis:** set `MainnetShastaTime` ([#535](https://github.com/taikoxyz/taiko-geth/issues/535)) ([1fda81a](https://github.com/taikoxyz/taiko-geth/commit/1fda81a9372dd79056427487caef38cee2be75b5))

## [2.3.0](https://github.com/taikoxyz/taiko-geth/compare/v2.2.1...v2.3.0) (2026-02-18)


### Features

* **consensus:** set Shasta EIP-4396 min basefee clamp to `0.01` Gwei on Mainnet ([#528](https://github.com/taikoxyz/taiko-geth/issues/528)) ([7f74d5d](https://github.com/taikoxyz/taiko-geth/commit/7f74d5dcbf4b49491afee9af53bb284ccbb4ab08))
* **taiko:** introduce new network named `shasta-transition-devnet` ([#525](https://github.com/taikoxyz/taiko-geth/issues/525)) ([f5c213e](https://github.com/taikoxyz/taiko-geth/commit/f5c213e2ef1be77fa08016f37a567ca2277aeae8))
* **txpool:** update `minL2BaseFee` to `0.005 GWei` ([#524](https://github.com/taikoxyz/taiko-geth/issues/524)) ([dec5974](https://github.com/taikoxyz/taiko-geth/commit/dec59749627c7e9bb4fcd8df0118539d548752eb))


### Bug Fixes

* **consensus:** refresh engine chain config after genesis config migration ([#521](https://github.com/taikoxyz/taiko-geth/issues/521)) ([db81096](https://github.com/taikoxyz/taiko-geth/commit/db81096359ed522899d4f7bd2aede4d3ba4ccf1d))
* **crypto:** validate `secp256k1` points and `ECIES` handshake curve handling ([#530](https://github.com/taikoxyz/taiko-geth/issues/530)) ([29fa1d9](https://github.com/taikoxyz/taiko-geth/commit/29fa1d99836f266f6520f84963b136eaa9071b58))


### Chores

* **params:** include Taiko forks in `ChainConfig` description ([#522](https://github.com/taikoxyz/taiko-geth/issues/522)) ([2613ba7](https://github.com/taikoxyz/taiko-geth/commit/2613ba7da853cc4e246f3124571dc542cc0464a3))
* **taiko:** set `STDShastaTime` to `1_770_987_600` ([#527](https://github.com/taikoxyz/taiko-geth/issues/527)) ([a46ba7e](https://github.com/taikoxyz/taiko-geth/commit/a46ba7e3a2758b5e8961c7558ef2053c2b7dcdce))
* **taiko:** update AGENTS rules ([#526](https://github.com/taikoxyz/taiko-geth/issues/526)) ([68767f3](https://github.com/taikoxyz/taiko-geth/commit/68767f3b4d73f7462b3deac2d90b67e801e64fd9))

## [2.2.1](https://github.com/taikoxyz/taiko-geth/compare/v2.2.0...v2.2.1) (2026-02-04)


### Chores

* **taiko_api_backend:** improve `ErrProposalLastBlockUncertain` checks ([#520](https://github.com/taikoxyz/taiko-geth/issues/520)) ([c488d32](https://github.com/taikoxyz/taiko-geth/commit/c488d324bc314d57e9a78c7dd3fea0e22dbf4aff))


### Performance Improvements

* **taiko_api_backend:** optimize `getLastBlockByBatchId` scan ([#518](https://github.com/taikoxyz/taiko-geth/issues/518)) ([24e18fc](https://github.com/taikoxyz/taiko-geth/commit/24e18fc77e6f5b8d53c46b095414ce517442875e))

## [2.2.0](https://github.com/taikoxyz/taiko-geth/compare/v2.1.0...v2.2.0) (2026-02-04)


### Features

* **taiko_genesis:** update `HoodiShastaTime` ([#515](https://github.com/taikoxyz/taiko-geth/issues/515)) ([f23c7a3](https://github.com/taikoxyz/taiko-geth/commit/f23c7a3505a69647a0a881ead22a1d1fbb7f0e20))


### Chores

* **core:** add shasta fork time on Hoodi ([#512](https://github.com/taikoxyz/taiko-geth/issues/512)) ([7090de7](https://github.com/taikoxyz/taiko-geth/commit/7090de77bcb4a99816b8df2014dc79b8cbc68ca0))
* **eth:** set `maxBatchLookupBlocks` to upper limit ([#517](https://github.com/taikoxyz/taiko-geth/issues/517)) ([a13a79d](https://github.com/taikoxyz/taiko-geth/commit/a13a79d05f9048122efb8024690bba7c1fc3af26))
* **taiko:** release 2.1.0 ([#514](https://github.com/taikoxyz/taiko-geth/issues/514)) ([0de4018](https://github.com/taikoxyz/taiko-geth/commit/0de4018d9d0901befc122ae3941e3c64f562b58b))

## [2.1.0](https://github.com/taikoxyz/taiko-geth/compare/v2.0.0...v2.1.0) (2026-02-02)


### Features

* **all:** changes based on Taiko protocol ([7e1b8b6](https://github.com/taikoxyz/taiko-geth/commit/7e1b8b65a3f8b931a5f141281c6ff82ad17028d0))
* **beacon:** introduce soft blocks ([#342](https://github.com/taikoxyz/taiko-geth/issues/342)) ([a2cbf90](https://github.com/taikoxyz/taiko-geth/commit/a2cbf904eaaed308bea29ab67ab9059bde013634))
* **beacon:** remove soft blocks implementation ([#366](https://github.com/taikoxyz/taiko-geth/issues/366)) ([3b30523](https://github.com/taikoxyz/taiko-geth/commit/3b305232a53a8fe85ca49d93fe80b289cf3ba8cf))
* **cmd:** add flag to override devnet Shasta HF timestamp ([#467](https://github.com/taikoxyz/taiko-geth/issues/467)) ([609e06d](https://github.com/taikoxyz/taiko-geth/commit/609e06d34a82abbe734f43cab53950e571c91d30))
* **consensus:** add Shasta EOP flag and remove batch-to-last-block DB mapping ([#500](https://github.com/taikoxyz/taiko-geth/issues/500)) ([32faf0c](https://github.com/taikoxyz/taiko-geth/commit/32faf0c0076ec3d44cfb64ab8ca4ed34b3ec0365))
* **consensus:** change Shasta to time activated hardfork ([#466](https://github.com/taikoxyz/taiko-geth/issues/466)) ([9405625](https://github.com/taikoxyz/taiko-geth/commit/94056254d3fa3cda463f3eb2edc3fd106e21ffb6))
* **consensus:** changes based on protocol [#20413](https://github.com/taikoxyz/taiko-geth/issues/20413) ([#460](https://github.com/taikoxyz/taiko-geth/issues/460)) ([f8976b8](https://github.com/taikoxyz/taiko-geth/commit/f8976b8039d34706a046c8ee37334c1deef05b60))
* **consensus:** improve `EIP-4396` calculation ([#457](https://github.com/taikoxyz/taiko-geth/issues/457)) ([e070400](https://github.com/taikoxyz/taiko-geth/commit/e070400a12e9ad66216deaff5467772ba8ee58b5))
* **consensus:** improve `VerifyHeaders` for `taiko` consensus ([#238](https://github.com/taikoxyz/taiko-geth/issues/238)) ([4f36879](https://github.com/taikoxyz/taiko-geth/commit/4f368792dc27d1e5c5d92f44b2d4b0a3f2986e02))
* **consensus:** introduce `AnchorV3GasLimit` ([#378](https://github.com/taikoxyz/taiko-geth/issues/378)) ([a0b97be](https://github.com/taikoxyz/taiko-geth/commit/a0b97be30cc01a93cebbd2d7188d28b0dcc5989a))
* **consensus:** introduce `AnchorV3V4GasLimit` to simplify anchor transaction gas limit checks ([#469](https://github.com/taikoxyz/taiko-geth/issues/469)) ([05c5a98](https://github.com/taikoxyz/taiko-geth/commit/05c5a9830cd1238b62c5e4710b80408b2ea682df))
* **consensus:** introduce `Shasta` fork ([#431](https://github.com/taikoxyz/taiko-geth/issues/431)) ([e5689b3](https://github.com/taikoxyz/taiko-geth/commit/e5689b37d777409848236a228c038ae4c5e43d4d))
* **consensus:** introduce `ShastaExtraDataLen` constant && update `verifyHeader` ([#479](https://github.com/taikoxyz/taiko-geth/issues/479)) ([9080fb8](https://github.com/taikoxyz/taiko-geth/commit/9080fb8f32ea5ce5da97a9210cfc93699732a6b0))
* **consensus:** introduce cache for the payloads ([#380](https://github.com/taikoxyz/taiko-geth/issues/380)) ([36430eb](https://github.com/taikoxyz/taiko-geth/commit/36430eb4f53a1455eb331f58c3ad2e88d8f40ecf))
* **consensus:** introduce protocol `MIN_BASE_FEE` and `MAX_BASE_FEE` for Shasta blocks ([#464](https://github.com/taikoxyz/taiko-geth/issues/464)) ([627e2d9](https://github.com/taikoxyz/taiko-geth/commit/627e2d9cc834ee07e40ca8a7a5f3d438c9bc6eb5))
* **consensus:** remove the latest `anchorV4` change introduced in protocol [#20304](https://github.com/taikoxyz/taiko-geth/issues/20304) ([#477](https://github.com/taikoxyz/taiko-geth/issues/477)) ([b260aa9](https://github.com/taikoxyz/taiko-geth/commit/b260aa96ad5203cb12de135ca6ea88106a84adac))
* **consensus:** restore batch-to-last-block mapping and bound Shasta batch lookup ([#504](https://github.com/taikoxyz/taiko-geth/issues/504)) ([9680af1](https://github.com/taikoxyz/taiko-geth/commit/9680af1dc948b71e63e84431466bfbd464fd2563))
* **consensus:** update `anchorV4` based on protocol [#20304](https://github.com/taikoxyz/taiko-geth/issues/20304) ([#476](https://github.com/taikoxyz/taiko-geth/issues/476)) ([428fb34](https://github.com/taikoxyz/taiko-geth/commit/428fb34564d2f4aae00ebf969ab992d77f5987a5))
* **consensus:** update `anchorV4` selector for sequential proving design ([#485](https://github.com/taikoxyz/taiko-geth/issues/485)) ([96433ab](https://github.com/taikoxyz/taiko-geth/commit/96433ab6bcbcc220fe366f4b44950b45d6da1591))
* **consensus:** update `AnchorV4Selector` ([#468](https://github.com/taikoxyz/taiko-geth/issues/468)) ([55bfbd7](https://github.com/taikoxyz/taiko-geth/commit/55bfbd7e6f31a7dc61298d90eb839e1792a237ea))
* **consensus:** update `TaikoAnchor.anchorV3` selector ([#379](https://github.com/taikoxyz/taiko-geth/issues/379)) ([1e948cf](https://github.com/taikoxyz/taiko-geth/commit/1e948cff4c83e7a5cb0d8a4db27cbe59ce2a8884))
* **consensus:** update `ValidateAnchorTx` ([#289](https://github.com/taikoxyz/taiko-geth/issues/289)) ([8ff161f](https://github.com/taikoxyz/taiko-geth/commit/8ff161fb39b76ef15585d26033131433c4530a3e))
* **core:** add optional isForcedInclusion and Signature to L1Origin ([#434](https://github.com/taikoxyz/taiko-geth/issues/434)) ([a1fe1d8](https://github.com/taikoxyz/taiko-geth/commit/a1fe1d8309492866f21993b7065d608081b5750e))
* **core:** align the upstream `types.Header` ([#393](https://github.com/taikoxyz/taiko-geth/issues/393)) ([573f8fc](https://github.com/taikoxyz/taiko-geth/commit/573f8fc144670d7221b387661f1f18dcd0935fe1))
* **core:** changes based on the latest `block.extradata` format ([#295](https://github.com/taikoxyz/taiko-geth/issues/295)) ([a875cc8](https://github.com/taikoxyz/taiko-geth/commit/a875cc83b907b026b88da887ce0a0d46c91d6980))
* **core:** decode basefee params from `block.extraData` ([#290](https://github.com/taikoxyz/taiko-geth/issues/290)) ([83564ba](https://github.com/taikoxyz/taiko-geth/commit/83564ba6fc9c20b1fa28ff94d65d5e19211a1aa2))
* **core:** introduce `BasefeeSharingPctg` in `BlockMetadata` ([#287](https://github.com/taikoxyz/taiko-geth/issues/287)) ([e6487f0](https://github.com/taikoxyz/taiko-geth/commit/e6487f00ed74139fb4169cf4ccd70488d933a01a))
* **core:** ll1 origin updates ([#439](https://github.com/taikoxyz/taiko-geth/issues/439)) ([8c15fa3](https://github.com/taikoxyz/taiko-geth/commit/8c15fa30e3cd178ce646a71b9c13993369af8f64))
* **core:** set `MainnetPacayaBlock` ([#428](https://github.com/taikoxyz/taiko-geth/issues/428)) ([7e0d8a0](https://github.com/taikoxyz/taiko-geth/commit/7e0d8a02c8966b9c14b2269084435e9a2d124d6d))
* **core:** update `InternalDevnetPacayaBlock` to `0` ([#430](https://github.com/taikoxyz/taiko-geth/issues/430)) ([c3ec5f8](https://github.com/taikoxyz/taiko-geth/commit/c3ec5f88cb4a2aef1c62bc7a21beb6f2609f6e12))
* **core:** update `MainnetOntakeBlock` ([#330](https://github.com/taikoxyz/taiko-geth/issues/330)) ([cd72c5b](https://github.com/taikoxyz/taiko-geth/commit/cd72c5bf056cce5870b685226ae70e0d2620dc5e))
* **core:** update `ontakeForkHeight` to Sep 24, 2024 ([#309](https://github.com/taikoxyz/taiko-geth/issues/309)) ([4e05e58](https://github.com/taikoxyz/taiko-geth/commit/4e05e5893b18482a90b1560019f93e90745cc0e0))
* **core:** update devnet genesis JSON ([#481](https://github.com/taikoxyz/taiko-geth/issues/481)) ([a9ab08f](https://github.com/taikoxyz/taiko-geth/commit/a9ab08f334993d69eaa9824be4c4253188fdbf20))
* **core:** update devnet ontake fork height ([#345](https://github.com/taikoxyz/taiko-geth/issues/345)) ([4578ce1](https://github.com/taikoxyz/taiko-geth/commit/4578ce1ffeed8065d70fb2bc39bff20a9df50f56))
* **eip1559:** remove `CalcBaseFeeOntake()` method ([#293](https://github.com/taikoxyz/taiko-geth/issues/293)) ([124fde7](https://github.com/taikoxyz/taiko-geth/commit/124fde7e025d6ba88c5cf796d6a0a5fd19c21a19))
* **eth:** add default gpo price flag ([#258](https://github.com/taikoxyz/taiko-geth/issues/258)) ([0fb7ce1](https://github.com/taikoxyz/taiko-geth/commit/0fb7ce1999e6b8f4d39e78787525e236e007948f))
* **eth:** changes based on protocol `Pacaya` fork ([#367](https://github.com/taikoxyz/taiko-geth/issues/367)) ([7bf5c0d](https://github.com/taikoxyz/taiko-geth/commit/7bf5c0d259f60f9d62d481c873053548c87b6fb5))
* **eth:** expose `AnchorV4ProposalID` method ([#478](https://github.com/taikoxyz/taiko-geth/issues/478)) ([087dce7](https://github.com/taikoxyz/taiko-geth/commit/087dce755f0ac56887bd8e795df4f44db464375f))
* **eth:** update `anchorV4` calldata parsing ([#475](https://github.com/taikoxyz/taiko-geth/issues/475)) ([5812bdf](https://github.com/taikoxyz/taiko-geth/commit/5812bdf697ac516fcfa22b2945209a7138483f1c))
* **miner:** change invalid transaction log level to `DEBUG` ([#224](https://github.com/taikoxyz/taiko-geth/issues/224)) ([286ffe2](https://github.com/taikoxyz/taiko-geth/commit/286ffe2cbfd6e1b234c9ab3976b4daa60c8a24ce))
* **miner:** compress the txlist bytes after checking the transaction is executable ([#269](https://github.com/taikoxyz/taiko-geth/issues/269)) ([aa70708](https://github.com/taikoxyz/taiko-geth/commit/aa70708a69d9612bf2dffd218db7e703de1654c1))
* **miner:** count last oversized transaction ([#273](https://github.com/taikoxyz/taiko-geth/issues/273)) ([451a668](https://github.com/taikoxyz/taiko-geth/commit/451a668d79bb9e41bb34dfb5fdbd1e0301977a9b))
* **miner:** improve `prepareWork()` ([#292](https://github.com/taikoxyz/taiko-geth/issues/292)) ([06b2903](https://github.com/taikoxyz/taiko-geth/commit/06b29039cbf1f72d6163c0c4f658053acfcc5c47))
* **miner:** improve `pruneTransactions` ([#411](https://github.com/taikoxyz/taiko-geth/issues/411)) ([7a019ea](https://github.com/taikoxyz/taiko-geth/commit/7a019ea44bc98be082a1a4dfa0c6975b30939196))
* **miner:** move `TAIKO_MIN_TIP` check to `commitL2Transactions` ([#272](https://github.com/taikoxyz/taiko-geth/issues/272)) ([f3a7fb6](https://github.com/taikoxyz/taiko-geth/commit/f3a7fb6311e9d59ba2fb55799b9eab614d488095))
* **miner:** preconf simulator api ([#433](https://github.com/taikoxyz/taiko-geth/issues/433)) ([1ce30d2](https://github.com/taikoxyz/taiko-geth/commit/1ce30d29a96b1a39c7fbb7f3f3f8065595e757da))
* **miner:** reduce the number compression attempts when fetching transactions list ([#406](https://github.com/taikoxyz/taiko-geth/issues/406)) ([9e6edc5](https://github.com/taikoxyz/taiko-geth/commit/9e6edc51dbb37b3f9b280c95a031c0a2f68af53a))
* **miner:** update logs in worker ([#429](https://github.com/taikoxyz/taiko-geth/issues/429)) ([6caf96f](https://github.com/taikoxyz/taiko-geth/commit/6caf96ffda260aef973b5874b4b3b5b53cf49637))
* **miner:** use `[]*ethapi.RPCTransaction` in RPC response body ([#391](https://github.com/taikoxyz/taiko-geth/issues/391)) ([afd09af](https://github.com/taikoxyz/taiko-geth/commit/afd09afd03871f9c66231a92bba706f4c491b877))
* **rawdb:** introduce `BuildPayloadArgsID` to `L1Origin` ([#426](https://github.com/taikoxyz/taiko-geth/issues/426)) ([187f85d](https://github.com/taikoxyz/taiko-geth/commit/187f85d7725fbf510bb6e3bd5dd8d48e6776ed17))
* **repo:** `geth/v1.14.11` upstream merge ([#313](https://github.com/taikoxyz/taiko-geth/issues/313)) ([5c84a20](https://github.com/taikoxyz/taiko-geth/commit/5c84a20827473cbe60ed16827df21b4ad395c9c2))
* **repo:** `go-ethereum` v1.15.5 upstream merge  ([#395](https://github.com/taikoxyz/taiko-geth/issues/395)) ([364acd0](https://github.com/taikoxyz/taiko-geth/commit/364acd00d1f2b45a07b7b1d20ec9b0f77be50b91))
* **repo:** add claude code workflows ([#445](https://github.com/taikoxyz/taiko-geth/issues/445)) ([9aa789d](https://github.com/taikoxyz/taiko-geth/commit/9aa789de8225ecd7808e62214d6aa0e06c660c12))
* **repo:** add do-not-merge and rename files ([#390](https://github.com/taikoxyz/taiko-geth/issues/390)) ([3792f93](https://github.com/taikoxyz/taiko-geth/commit/3792f9356bfdc391fc8e112fccc7bf31d9edbb63))
* **repo:** allow claude to review forks when tagged, default skips review ([#472](https://github.com/taikoxyz/taiko-geth/issues/472)) ([5ccd86f](https://github.com/taikoxyz/taiko-geth/commit/5ccd86f0f0ba7bf33fb1ea6f520551267876c8ce))
* **rpc:** move last block in proposal lookup RPCs to `taikoAuth` ([#510](https://github.com/taikoxyz/taiko-geth/issues/510)) ([a6027c7](https://github.com/taikoxyz/taiko-geth/commit/a6027c7ecce1d7fd766556c457e6ab1b3fb4b169))
* **taiko_api_backend:** cap `getLastBlockByBatchId` lookback ([#506](https://github.com/taikoxyz/taiko-geth/issues/506)) ([656086a](https://github.com/taikoxyz/taiko-geth/commit/656086ab54ea5b675dc05acfea47983fe3bb0815))
* **taiko_api:** reduce the frequency of `zlib` compression when fetching txpool content ([#323](https://github.com/taikoxyz/taiko-geth/issues/323)) ([27b4d6e](https://github.com/taikoxyz/taiko-geth/commit/27b4d6ebf9959b096fb6c6ed7f5910fa93a59df3))
* **taiko_genesis:** changes for moving bond to L1 ([#492](https://github.com/taikoxyz/taiko-geth/issues/492)) ([9602a0a](https://github.com/taikoxyz/taiko-geth/commit/9602a0aa89360992e579ba4c8b7cb367c4eb0441))
* **taiko_genesis:** update `TaikoGenesisBlock` configs ([#400](https://github.com/taikoxyz/taiko-geth/issues/400)) ([139e562](https://github.com/taikoxyz/taiko-geth/commit/139e56205075ba5897c2a4ca707a52b096a3f200))
* **taiko_genesis:** update devnet genesis json ([#473](https://github.com/taikoxyz/taiko-geth/issues/473)) ([d130d73](https://github.com/taikoxyz/taiko-geth/commit/d130d73a469f3fa880a9dbaa8336784ec1ccf254))
* **taiko_genesis:** update devnet genesis JSON ([#487](https://github.com/taikoxyz/taiko-geth/issues/487)) ([1c80068](https://github.com/taikoxyz/taiko-geth/commit/1c8006836d93fdf1c905e1ed821ed0647fde03d8))
* **taiko_genesis:** update devnet genesis JSON ([#489](https://github.com/taikoxyz/taiko-geth/issues/489)) ([ac3dba7](https://github.com/taikoxyz/taiko-geth/commit/ac3dba7fb15ec6e3b9d5383cb09e17522a31eaaa))
* **taiko_genesis:** update devnet genesis JSON ([#490](https://github.com/taikoxyz/taiko-geth/issues/490)) ([dea8996](https://github.com/taikoxyz/taiko-geth/commit/dea8996c7da98a7fdd34c10f937b28ef1083b315))
* **taiko_genesis:** update devnet genesis JSONs for new `AnchorV3` method ([#382](https://github.com/taikoxyz/taiko-geth/issues/382)) ([2448fb9](https://github.com/taikoxyz/taiko-geth/commit/2448fb97a8b873c7bd7c0051cd83aaea339050e0))
* **taiko_genesis:** update genesis JSONs ([#305](https://github.com/taikoxyz/taiko-geth/issues/305)) ([73df1f1](https://github.com/taikoxyz/taiko-geth/commit/73df1f1a116bdb530c5a8bd7fc20b64b491f2f3c))
* **taiko_genesis:** update genesis JSONs ([#315](https://github.com/taikoxyz/taiko-geth/issues/315)) ([ae8a194](https://github.com/taikoxyz/taiko-geth/commit/ae8a194c517e39fda7a4c330cd6e5a49a8df3621))
* **taiko_genesis:** update interanl devnet genesis JSON for ontake hardfork ([#288](https://github.com/taikoxyz/taiko-geth/issues/288)) ([a748b91](https://github.com/taikoxyz/taiko-geth/commit/a748b914abb1b5bc2a25fe40de6e38bb70e4235a))
* **taiko_genesis:** update interanl devnet genesis JSON for ontake hardfork ([#291](https://github.com/taikoxyz/taiko-geth/issues/291)) ([217c9ec](https://github.com/taikoxyz/taiko-geth/commit/217c9ec0f42f4785b44b8d2dbc4c046eb43e1d02))
* **taiko_genesis:** update internal devnet genesis JSON ([#285](https://github.com/taikoxyz/taiko-geth/issues/285)) ([b137b2a](https://github.com/taikoxyz/taiko-geth/commit/b137b2ac113dfe899bc538220cbdadf45b24f133))
* **taiko_genesis:** update internal devnet genesis JSON ([#296](https://github.com/taikoxyz/taiko-geth/issues/296)) ([882a6cd](https://github.com/taikoxyz/taiko-geth/commit/882a6cd3294cd1c74eac37fbc37c54e64f0dc363))
* **taiko_miner:** add `BuildTransactionsListsWithMinTip` method ([#283](https://github.com/taikoxyz/taiko-geth/issues/283)) ([c777d24](https://github.com/taikoxyz/taiko-geth/commit/c777d24af16915030536564b8cb44346866ab0b1))
* **taiko_miner:** remove an unnecessary check ([#239](https://github.com/taikoxyz/taiko-geth/issues/239)) ([974b338](https://github.com/taikoxyz/taiko-geth/commit/974b338e20c3a2ff48ecfd0174c595d6cb02e935))
* **taiko_worker:** skip blob transactions ([#280](https://github.com/taikoxyz/taiko-geth/issues/280)) ([30a615b](https://github.com/taikoxyz/taiko-geth/commit/30a615b4c3aafd0d395309035d58b86ff53c8eb0))
* **taiko-api:** set l1 origin sig ([#440](https://github.com/taikoxyz/taiko-geth/issues/440)) ([8a11e69](https://github.com/taikoxyz/taiko-geth/commit/8a11e69d35c7a1cc7b7176d8a01dab8f2fb8c3d5))
* **txpool:** introduce `TAIKO_MIN_TIP` env ([#264](https://github.com/taikoxyz/taiko-geth/issues/264)) ([a29520e](https://github.com/taikoxyz/taiko-geth/commit/a29520e066809dda21af463272b6ec1ef1cdfcae))
* **txpool:** update `ValidateTransaction` ([#237](https://github.com/taikoxyz/taiko-geth/issues/237)) ([6cc43e1](https://github.com/taikoxyz/taiko-geth/commit/6cc43e1d9c1ef34cba5fff2db3735ced3ad0a3a0))
* **txpool:** update `ValidateTransaction` ([#255](https://github.com/taikoxyz/taiko-geth/issues/255)) ([87f4206](https://github.com/taikoxyz/taiko-geth/commit/87f42062d9d02fd99be1f8c318baf573ef08135f))
* **txpool:** update max fee check in `ValidateTransaction()` ([#259](https://github.com/taikoxyz/taiko-geth/issues/259)) ([ef40d46](https://github.com/taikoxyz/taiko-geth/commit/ef40d46c0efbda50f0a2b84987291a4b8f9f2a2d))
* **worker:** add `chainId` check in `worker` ([#228](https://github.com/taikoxyz/taiko-geth/issues/228)) ([4ebcf66](https://github.com/taikoxyz/taiko-geth/commit/4ebcf6656c507c3164722148c16e76f7766fe52e))


### Bug Fixes

* **ci:** fix `docker` action for `main` branch ([#449](https://github.com/taikoxyz/taiko-geth/issues/449)) ([fb1c1c8](https://github.com/taikoxyz/taiko-geth/commit/fb1c1c891b217b5b2ad3011f1888409b9fe49117))
* **consensus:** fix Shasta basefee calculation in `verifyHeader` ([#470](https://github.com/taikoxyz/taiko-geth/issues/470)) ([2af0196](https://github.com/taikoxyz/taiko-geth/commit/2af01965ac05901b63529e93e03e11cd082c17cc))
* **consensus:** replace `GetHeaderByHash` with `GetHeader` ([#495](https://github.com/taikoxyz/taiko-geth/issues/495)) ([6e37740](https://github.com/taikoxyz/taiko-geth/commit/6e37740966eca31b7a344b0dafc0f3995abbfb1b))
* **consensus:** skip EIP-4396 check when ancestor is missing ([#507](https://github.com/taikoxyz/taiko-geth/issues/507)) ([5d75998](https://github.com/taikoxyz/taiko-geth/commit/5d7599848d54294817b54f2f4e12abe5b6c0c7c5))
* **core:** fix a transaction `Message` assembling issue ([#308](https://github.com/taikoxyz/taiko-geth/issues/308)) ([04d76e8](https://github.com/taikoxyz/taiko-geth/commit/04d76e8f012e8a3d89d04f38dabac08e758f5a00))
* **core:** revert PR ([#438](https://github.com/taikoxyz/taiko-geth/issues/438)) ([bdc0f79](https://github.com/taikoxyz/taiko-geth/commit/bdc0f79a6c8cb6e98bceac4bb2d64091ef61f50a))
* **crypto:** use aes `blocksize` ([#497](https://github.com/taikoxyz/taiko-geth/issues/497)) ([9352671](https://github.com/taikoxyz/taiko-geth/commit/9352671643683e4117de023afc9d482d74c6ab07))
* **eth:** mark anchor transaction in `traceBlockParallel` ([#243](https://github.com/taikoxyz/taiko-geth/issues/243)) ([8622b2c](https://github.com/taikoxyz/taiko-geth/commit/8622b2cce09330fc4957e22be5bd4685675411d9))
* **eth:** return correct type on `SetBatchToLastBlock` ([#482](https://github.com/taikoxyz/taiko-geth/issues/482)) ([d677404](https://github.com/taikoxyz/taiko-geth/commit/d677404c497e5d618e4a1e151d6f371b259a378e))
* **eth:** write `HeadL1Origin` even if the payload has been cached ([#483](https://github.com/taikoxyz/taiko-geth/issues/483)) ([b3feaa3](https://github.com/taikoxyz/taiko-geth/commit/b3feaa3f0e9f581b13eb16262a53cac81e364bdf))
* **eth:** write `L1Origin` even if the payload is already in the cache ([#396](https://github.com/taikoxyz/taiko-geth/issues/396)) ([43a60b3](https://github.com/taikoxyz/taiko-geth/commit/43a60b36ea53a416ba01f271749d20f1251ce607))
* fix some (ST1005)go-staticcheck ([2814ee0](https://github.com/taikoxyz/taiko-geth/commit/2814ee0547cb49dddf182bad802f19100608d5f8))
* management api links ([4c15d58](https://github.com/taikoxyz/taiko-geth/commit/4c15d58007422069794cada5e38ec8b90940a969))
* **miner:** change RPC response `taikoAuth_txPoolContent` from PascalCase to camelCase ([#499](https://github.com/taikoxyz/taiko-geth/issues/499)) ([f1856b6](https://github.com/taikoxyz/taiko-geth/commit/f1856b66785e0b0b2815e4cad364381450aafe1f))
* **miner:** introduce `block.extradata` into payload building ([#498](https://github.com/taikoxyz/taiko-geth/issues/498)) ([30c426e](https://github.com/taikoxyz/taiko-geth/commit/30c426e19a67bef2f5fd77eac70f251fcaa5a7fe))
* **repo:** fix json-rpc docs autogen workflow ([#419](https://github.com/taikoxyz/taiko-geth/issues/419)) ([190d6af](https://github.com/taikoxyz/taiko-geth/commit/190d6afff7f199744adebf07c082898713d2fb73))
* **repo:** fix rpc api generation script ([#452](https://github.com/taikoxyz/taiko-geth/issues/452)) ([115de06](https://github.com/taikoxyz/taiko-geth/commit/115de06c5aa97c3d3c3b7b7a72a489b122b4769d))
* **repo:** fix workflow to use configs ([#402](https://github.com/taikoxyz/taiko-geth/issues/402)) ([177750c](https://github.com/taikoxyz/taiko-geth/commit/177750c73ee40cb32f10b4e4d2276f1a3b0cad3b))
* **taiko_api_backend:** derive proposal scan head from head L1 origin ([#505](https://github.com/taikoxyz/taiko-geth/issues/505)) ([0590c0d](https://github.com/taikoxyz/taiko-geth/commit/0590c0d942b0281e5a4bdcc916266e9b9c90ecb4))
* **taiko_api_backend:** use correct value for `LastBlockIDByBatchID` ([#496](https://github.com/taikoxyz/taiko-geth/issues/496)) ([b472cd3](https://github.com/taikoxyz/taiko-geth/commit/b472cd3eb6fd2d762df361766c65656ff33cf0c4))
* **taiko_api:** fix an `EstimatedGasUsed` calculation issue ([#322](https://github.com/taikoxyz/taiko-geth/issues/322)) ([96296fb](https://github.com/taikoxyz/taiko-geth/commit/96296fb42e08da4f0db1c836efb9c427740c92e4))
* **taiko_genesis:** update devnet Ontake fork hight ([#302](https://github.com/taikoxyz/taiko-geth/issues/302)) ([d065dd2](https://github.com/taikoxyz/taiko-geth/commit/d065dd2c3d005fb01590ecc82cda9c91678dfd13))
* **taiko_miner:** fix a typo ([#299](https://github.com/taikoxyz/taiko-geth/issues/299)) ([5faa71b](https://github.com/taikoxyz/taiko-geth/commit/5faa71b531cc889fb66868380d9063e8c78c7646))
* **taiko_worker:** fix a `maxBytesPerTxList` check issue ([#282](https://github.com/taikoxyz/taiko-geth/issues/282)) ([f930382](https://github.com/taikoxyz/taiko-geth/commit/f930382f4bf789bdc6c6fae5a410758a9f9bed7c))
* **taiko_worker:** fix a size limit check in `commitL2Transactions` ([#245](https://github.com/taikoxyz/taiko-geth/issues/245)) ([7a75d5e](https://github.com/taikoxyz/taiko-geth/commit/7a75d5e6b42ee57fed4df8713049c71e9b08657a))
* **taiko-client:** fix an issue in `encodeAndCompressTxList` ([#404](https://github.com/taikoxyz/taiko-geth/issues/404)) ([8d5d308](https://github.com/taikoxyz/taiko-geth/commit/8d5d308dfbc465d111e044b0c4f245e3b1ef5c3a))
* **taiko-geth:** fix a mempool fetch issue ([#333](https://github.com/taikoxyz/taiko-geth/issues/333)) ([1340ded](https://github.com/taikoxyz/taiko-geth/commit/1340ded3811193b46d18241e5810c5b47083821f))
* **taiko-geth:** revert a `tx.Shift()` change ([#335](https://github.com/taikoxyz/taiko-geth/issues/335)) ([46576d2](https://github.com/taikoxyz/taiko-geth/commit/46576d27209194db9e02ba38b9ab6b919679fcbd))
* **taiko-geth:** stop using `RevertToSnapshot` when fetching mempool ([#336](https://github.com/taikoxyz/taiko-geth/issues/336)) ([1216d8d](https://github.com/taikoxyz/taiko-geth/commit/1216d8d6051ba6f73ee42b395e973dccf1d90cf9))
* **taiko:** decode basefeeSharingPctg from extradata for ontake blocks ([#370](https://github.com/taikoxyz/taiko-geth/issues/370)) ([cdca791](https://github.com/taikoxyz/taiko-geth/commit/cdca79128bc606f89c12e08474f228ad5d0d89c3))
* **txpool:** basefee requires mintip to not be nil. ([#297](https://github.com/taikoxyz/taiko-geth/issues/297)) ([6315fd4](https://github.com/taikoxyz/taiko-geth/commit/6315fd49697701beb1f18b8c8c0a6bdf97e862d5))
* **txpool:** fix the unit in a log ([#266](https://github.com/taikoxyz/taiko-geth/issues/266)) ([9594e0a](https://github.com/taikoxyz/taiko-geth/commit/9594e0a6a87d14bdaa594b3a31eec116ce24c948))
* update link to trezor ([1a79089](https://github.com/taikoxyz/taiko-geth/commit/1a79089193f2046c0cab60954bc05be2f52a2a90))
* update outdated link to trezor docs ([#28966](https://github.com/taikoxyz/taiko-geth/issues/28966)) ([1a79089](https://github.com/taikoxyz/taiko-geth/commit/1a79089193f2046c0cab60954bc05be2f52a2a90))
* **wokrer:** fix an issue in `sealBlockWith` ([#240](https://github.com/taikoxyz/taiko-geth/issues/240)) ([02c6ee9](https://github.com/taikoxyz/taiko-geth/commit/02c6ee9672c1b47ac534ec7224f45d9ab0652cdf))


### Chores

* **catalyst:** increase `maxTrackedPayloads` ([#424](https://github.com/taikoxyz/taiko-geth/issues/424)) ([c091dd6](https://github.com/taikoxyz/taiko-geth/commit/c091dd6c8fe90c3797f84e97f55ace5124ff1d6b))
* **ci:** add docker multi arch image build ([#339](https://github.com/taikoxyz/taiko-geth/issues/339)) ([eeab36d](https://github.com/taikoxyz/taiko-geth/commit/eeab36de475a23b8d6f37d4845f1517c8464dd9e))
* **ci:** add go cache dependency in action ([#235](https://github.com/taikoxyz/taiko-geth/issues/235)) ([a998c80](https://github.com/taikoxyz/taiko-geth/commit/a998c80533832202c576120974c8049f2d03e623))
* **ci:** fix an issue in docker build cache ([#262](https://github.com/taikoxyz/taiko-geth/issues/262)) ([037640e](https://github.com/taikoxyz/taiko-geth/commit/037640eccdd161454fb144fd57909b47a84c259a))
* **ci:** introduce docker build cache ([#234](https://github.com/taikoxyz/taiko-geth/issues/234)) ([fdb980a](https://github.com/taikoxyz/taiko-geth/commit/fdb980ab0600d1f7b143a7b6c53648fa1934a646))
* **ci:** use `arc-runner-set` ([#340](https://github.com/taikoxyz/taiko-geth/issues/340)) ([fe966d4](https://github.com/taikoxyz/taiko-geth/commit/fe966d48ab1f1b16f34a4357fbdc93f766dad194))
* **cmd:** add `--taiko` flag ([#365](https://github.com/taikoxyz/taiko-geth/issues/365)) ([ca784a2](https://github.com/taikoxyz/taiko-geth/commit/ca784a23df4a62529a79e779e3c3083f41c00129))
* **cmd:** remove `--taiko.preconfirmationForwardingUrl` flag ([#362](https://github.com/taikoxyz/taiko-geth/issues/362)) ([283fedd](https://github.com/taikoxyz/taiko-geth/commit/283fedd05b57bbedc8142601ac86e9992b3c12cd))
* **consensus:** change `SetHeadL1Origin` && `UpdateL1Origin` to `taikoauth_` namespace ([#386](https://github.com/taikoxyz/taiko-geth/issues/386)) ([838f653](https://github.com/taikoxyz/taiko-geth/commit/838f65315354488ada3c18e8a9924b69cb61b002))
* **consensus:** remove `London` hardfork check in `Shasta` fork activation checks ([#471](https://github.com/taikoxyz/taiko-geth/issues/471)) ([5c247cc](https://github.com/taikoxyz/taiko-geth/commit/5c247cc9a2e4a0cee7f609ef076ab11d125d8b1a))
* **consensus:** update `maxTrackedPayloads` to `maxBlocksPerBatch` ([#389](https://github.com/taikoxyz/taiko-geth/issues/389)) ([516eff7](https://github.com/taikoxyz/taiko-geth/commit/516eff7ae9a9504d7d3cc42db7ce83c2242d870c))
* **core,eth:** fix a couple of typos ([edc864f](https://github.com/taikoxyz/taiko-geth/commit/edc864f9ba186fd307d9c98c42136db6c9411cf9))
* **core:** add shasta block for tolba network ([#458](https://github.com/taikoxyz/taiko-geth/issues/458)) ([7225337](https://github.com/taikoxyz/taiko-geth/commit/722533764177a7067d119c247a58ba5f8e611bf0))
* **core:** add shasta fork time on Hoodi ([#512](https://github.com/taikoxyz/taiko-geth/issues/512)) ([7090de7](https://github.com/taikoxyz/taiko-geth/commit/7090de77bcb4a99816b8df2014dc79b8cbc68ca0))
* **core:** clean up deprecated networks ([#454](https://github.com/taikoxyz/taiko-geth/issues/454)) ([1b0b437](https://github.com/taikoxyz/taiko-geth/commit/1b0b437edc7354771a530996e1644e07e094282b))
* **core:** reset chainID of Taiko Hoodi ([#461](https://github.com/taikoxyz/taiko-geth/issues/461)) ([0a548ad](https://github.com/taikoxyz/taiko-geth/commit/0a548adde21cf2705f426fe9e04c11c1a5404145))
* **core:** revert `[]*ethapi.RPCTransaction` changes in miner ([#394](https://github.com/taikoxyz/taiko-geth/issues/394)) ([03f614f](https://github.com/taikoxyz/taiko-geth/commit/03f614fb278a7da17fcd6231d9de1ff1086d704f))
* **core:** update devnet genesis JSON ([#463](https://github.com/taikoxyz/taiko-geth/issues/463)) ([9ab5151](https://github.com/taikoxyz/taiko-geth/commit/9ab515193aab819562fe5b75d36eada8e420e841))
* **core:** update Hekla Pacaya fork height ([#397](https://github.com/taikoxyz/taiko-geth/issues/397)) ([3aefd22](https://github.com/taikoxyz/taiko-geth/commit/3aefd22fccd97eec2c3dec9508b51eda179988c1))
* **core:** update Masaya's genesis for shared devnet ([#491](https://github.com/taikoxyz/taiko-geth/issues/491)) ([5938805](https://github.com/taikoxyz/taiko-geth/commit/5938805a2943f68aefa31a41bca1aaf6a53803db))
* **eth:** always use the latest block number for pending state in RPC calls ([#410](https://github.com/taikoxyz/taiko-geth/issues/410)) ([6822358](https://github.com/taikoxyz/taiko-geth/commit/682235849b5df653c4108f2a4099ee39b8cde6b6))
* **ethclient:** remove some unused APIs ([#387](https://github.com/taikoxyz/taiko-geth/issues/387)) ([437b5c6](https://github.com/taikoxyz/taiko-geth/commit/437b5c6114e34fd7b34d8d04fba1ee3e1eab5d45))
* **eth:** return `NotFound` for missing batch in `LastBlockIDByBatchID` ([#502](https://github.com/taikoxyz/taiko-geth/issues/502)) ([046aef8](https://github.com/taikoxyz/taiko-geth/commit/046aef886c78d37054e89b166ae0e942e0a4ecf1))
* log buildpayloadargs and its id ([#441](https://github.com/taikoxyz/taiko-geth/issues/441)) ([7810ed8](https://github.com/taikoxyz/taiko-geth/commit/7810ed871c46e9753b91889deb59a2a6fd7210d6))
* **params:** fix some comments ([#423](https://github.com/taikoxyz/taiko-geth/issues/423)) ([f4cb12a](https://github.com/taikoxyz/taiko-geth/commit/f4cb12a20321d93ebefeac3f44eaedf2fe40b90e))
* **repo:** add changelog sections ([#398](https://github.com/taikoxyz/taiko-geth/issues/398)) ([772a559](https://github.com/taikoxyz/taiko-geth/commit/772a55954cb5e94b08b89b3d61a639919362cb59))
* **repo:** add concurrency gate for pages ([#508](https://github.com/taikoxyz/taiko-geth/issues/508)) ([3a430e4](https://github.com/taikoxyz/taiko-geth/commit/3a430e446f115099de4b8433b062aa8266d26990))
* **repo:** update `docker-build` workflow ([#421](https://github.com/taikoxyz/taiko-geth/issues/421)) ([19dca2f](https://github.com/taikoxyz/taiko-geth/commit/19dca2f0b981b6d3b113cc2865e8fd80680bc407))
* **taiko_api_backend:** update error checks ([#509](https://github.com/taikoxyz/taiko-geth/issues/509)) ([53e7307](https://github.com/taikoxyz/taiko-geth/commit/53e730791ebbc30bdd1a7fa28ff7faed36bfc459))
* **taiko_genesis:** add genesis for alethia-hoodi testnet ([#448](https://github.com/taikoxyz/taiko-geth/issues/448)) ([0ea6057](https://github.com/taikoxyz/taiko-geth/commit/0ea6057b4c2a428dad564ea421cb94f17049b5cc))
* **taiko_genesis:** bump devnet account balances ([#432](https://github.com/taikoxyz/taiko-geth/issues/432)) ([4bd4264](https://github.com/taikoxyz/taiko-geth/commit/4bd4264f1a53b9ac0cbfddfcfc70fa35bfa5e4aa))
* **taiko_genesis:** update `TaikoGenesisBlock` configs ([#399](https://github.com/taikoxyz/taiko-geth/issues/399)) ([3da62a2](https://github.com/taikoxyz/taiko-geth/commit/3da62a24383c27c7bdda8ae9e68586e22a97ee87))
* **taiko_genesis:** update devnet genesis JSON ([#341](https://github.com/taikoxyz/taiko-geth/issues/341)) ([50f97ba](https://github.com/taikoxyz/taiko-geth/commit/50f97ba6835058036536a0ba669c232186cf5b7b))
* **taiko_genesis:** update devnet genesis JSON ([#486](https://github.com/taikoxyz/taiko-geth/issues/486)) ([820b93f](https://github.com/taikoxyz/taiko-geth/commit/820b93f79a5bdcbe1bdd80a3a7649877b0f9b71b))
* **taiko_genesis:** update devnet genesis JSONs ([#383](https://github.com/taikoxyz/taiko-geth/issues/383)) ([a3e2b34](https://github.com/taikoxyz/taiko-geth/commit/a3e2b34b4857d4b37c54ecd1f6bbac2ce8f6836a))
* **taiko_genesis:** update genesis block configs ([#304](https://github.com/taikoxyz/taiko-geth/issues/304)) ([062d4b7](https://github.com/taikoxyz/taiko-geth/commit/062d4b71f94ebf663a1f3045b432847199be6e82))
* **taiko_genesis:** update genesis JSON ([#344](https://github.com/taikoxyz/taiko-geth/issues/344)) ([699e2d9](https://github.com/taikoxyz/taiko-geth/commit/699e2d938c9092f3625ad5db10bae61cd8849e47))
* **taiko_genesis:** update genesis JSONs ([#233](https://github.com/taikoxyz/taiko-geth/issues/233)) ([68308e3](https://github.com/taikoxyz/taiko-geth/commit/68308e37810e3a16e443ffa84e5f1c33327b580e))
* **taiko_genesis:** update genesis JSONs ([#236](https://github.com/taikoxyz/taiko-geth/issues/236)) ([471db71](https://github.com/taikoxyz/taiko-geth/commit/471db7166393f7413fbe21b1df0971bf45ca774a))
* **taiko_genesis:** update genesis JSONs ([#246](https://github.com/taikoxyz/taiko-geth/issues/246)) ([dcb6c4e](https://github.com/taikoxyz/taiko-geth/commit/dcb6c4e978ad96cbad52148993a5b392d36214c4))
* **taiko_genesis:** update genesis JSONs ([#247](https://github.com/taikoxyz/taiko-geth/issues/247)) ([9efa13f](https://github.com/taikoxyz/taiko-geth/commit/9efa13f84a2cceaf21ef7add73f950ebc2b70316))
* **taiko_genesis:** update genesis JSONs ([#248](https://github.com/taikoxyz/taiko-geth/issues/248)) ([ac9ccc8](https://github.com/taikoxyz/taiko-geth/commit/ac9ccc8caffd5d38a37c0446d29fabd201ec8297))
* **taiko_genesis:** update genesis JSONs ([#253](https://github.com/taikoxyz/taiko-geth/issues/253)) ([91be6dd](https://github.com/taikoxyz/taiko-geth/commit/91be6dd14ac84ebebfcaf4efcecc3ae261e3de4d))
* **taiko_genesis:** update genesis JSONs ([#254](https://github.com/taikoxyz/taiko-geth/issues/254)) ([7874437](https://github.com/taikoxyz/taiko-geth/commit/78744374c1c009c5fde6382d6a8fd571f541eb61))
* **taiko_genesis:** update genesis JSONs ([#298](https://github.com/taikoxyz/taiko-geth/issues/298)) ([2134337](https://github.com/taikoxyz/taiko-geth/commit/21343370352005e02db1c7d7eeeb51bb0d9f3bce))
* **taiko_genesis:** update genesis JSONs ([#301](https://github.com/taikoxyz/taiko-geth/issues/301)) ([c65e9b9](https://github.com/taikoxyz/taiko-geth/commit/c65e9b9a95c4a315a186c2d77a96ffc9778f3e9a))
* **taiko_genesis:** update genesis JSONs ([#307](https://github.com/taikoxyz/taiko-geth/issues/307)) ([b5ac526](https://github.com/taikoxyz/taiko-geth/commit/b5ac526d9a0707e5dfcf0b8e5941d4f0bb770d26))
* **taiko_genesis:** update genesis JSONs ([#347](https://github.com/taikoxyz/taiko-geth/issues/347)) ([fdd905a](https://github.com/taikoxyz/taiko-geth/commit/fdd905ad9d7eb0bf2aa1c0235cc36f5bf1ad412d))
* **taiko_genesis:** update genesis JSONs ([#359](https://github.com/taikoxyz/taiko-geth/issues/359)) ([594cdab](https://github.com/taikoxyz/taiko-geth/commit/594cdabbdb5bd53aade9cb447ad6b171b180671e))
* **taiko_genesis:** update genesis JSONs ([#360](https://github.com/taikoxyz/taiko-geth/issues/360)) ([5ef7421](https://github.com/taikoxyz/taiko-geth/commit/5ef74219ab7df769e01d5521f34b5934444e5444))
* **taiko_genesis:** update Masaya's genesis for shared devnet ([#494](https://github.com/taikoxyz/taiko-geth/issues/494)) ([b3bba85](https://github.com/taikoxyz/taiko-geth/commit/b3bba850d85c96da827cff587138c3056766fe06))
* **taiko:** release 1.0.0 ([#251](https://github.com/taikoxyz/taiko-geth/issues/251)) ([f2d2957](https://github.com/taikoxyz/taiko-geth/commit/f2d2957421cdedfa956b65939847a4151b8dd8b5))
* **taiko:** release 1.1.0 ([#260](https://github.com/taikoxyz/taiko-geth/issues/260)) ([67b74ce](https://github.com/taikoxyz/taiko-geth/commit/67b74ce05576ad34f48dbd49b481238a7e54e149))
* **taiko:** release 1.10.0 ([#319](https://github.com/taikoxyz/taiko-geth/issues/319)) ([92f5d06](https://github.com/taikoxyz/taiko-geth/commit/92f5d06dc2f5e44038b49b3fff40427c03a3e293))
* **taiko:** release 1.11.0 ([#331](https://github.com/taikoxyz/taiko-geth/issues/331)) ([3229807](https://github.com/taikoxyz/taiko-geth/commit/3229807585f16e6858f08e415a0a874881843f02))
* **taiko:** release 1.11.1 ([#334](https://github.com/taikoxyz/taiko-geth/issues/334)) ([467c93e](https://github.com/taikoxyz/taiko-geth/commit/467c93e79802e61e6489b7080f76f21ccb5b19f1))
* **taiko:** release 1.12.0 ([#346](https://github.com/taikoxyz/taiko-geth/issues/346)) ([02597d7](https://github.com/taikoxyz/taiko-geth/commit/02597d73f9c012caea5d55c69e09c530be499091))
* **taiko:** release 1.13.0 ([#376](https://github.com/taikoxyz/taiko-geth/issues/376)) ([55a75b6](https://github.com/taikoxyz/taiko-geth/commit/55a75b6d207cdc6393aa79c4557702fe7427f774))
* **taiko:** release 1.14.0 ([#401](https://github.com/taikoxyz/taiko-geth/issues/401)) ([53531d9](https://github.com/taikoxyz/taiko-geth/commit/53531d9614cab58bbc6cbcb02cd9b970374895a0))
* **taiko:** release 1.14.1 ([#403](https://github.com/taikoxyz/taiko-geth/issues/403)) ([c98487d](https://github.com/taikoxyz/taiko-geth/commit/c98487d175300843c9d5669b2eb2e8cb6c9dcb80))
* **taiko:** release 1.15.0 ([#409](https://github.com/taikoxyz/taiko-geth/issues/409)) ([2f9a84e](https://github.com/taikoxyz/taiko-geth/commit/2f9a84e4cb6c162743cbc215e6ddbbb2eb24fa1c))
* **taiko:** release 1.16.0 ([#418](https://github.com/taikoxyz/taiko-geth/issues/418)) ([17fbc6b](https://github.com/taikoxyz/taiko-geth/commit/17fbc6b74c34a344176841be4708e1b619c29b81))
* **taiko:** release 1.17.0 ([#436](https://github.com/taikoxyz/taiko-geth/issues/436)) ([d888d7a](https://github.com/taikoxyz/taiko-geth/commit/d888d7ac909f22867cff9f1e1cd0e28ce6212814))
* **taiko:** release 1.18.0 ([#444](https://github.com/taikoxyz/taiko-geth/issues/444)) ([6ac43db](https://github.com/taikoxyz/taiko-geth/commit/6ac43db535a9f347e7375890191fc134a2f9e05a))
* **taiko:** release 1.2.0 ([#265](https://github.com/taikoxyz/taiko-geth/issues/265)) ([8bd80e4](https://github.com/taikoxyz/taiko-geth/commit/8bd80e422f02bdef240768331ef173df47aa2088))
* **taiko:** release 1.3.0 ([#271](https://github.com/taikoxyz/taiko-geth/issues/271)) ([b93ef66](https://github.com/taikoxyz/taiko-geth/commit/b93ef66540676dd6ad6e69ff4b2cc39a7dc9cef1))
* **taiko:** release 1.4.0 ([#275](https://github.com/taikoxyz/taiko-geth/issues/275)) ([47893ae](https://github.com/taikoxyz/taiko-geth/commit/47893aead41c29d1cd14eec3df30f5e44cc5f55e))
* **taiko:** release 1.5.0 ([#284](https://github.com/taikoxyz/taiko-geth/issues/284)) ([4954004](https://github.com/taikoxyz/taiko-geth/commit/4954004f4e2964d46f89d36b04af721168ca40c1))
* **taiko:** release 1.6.0 ([#286](https://github.com/taikoxyz/taiko-geth/issues/286)) ([80e3cb4](https://github.com/taikoxyz/taiko-geth/commit/80e3cb427a03823c6e153bb72a077619b3700ca2))
* **taiko:** release 1.6.1 ([#303](https://github.com/taikoxyz/taiko-geth/issues/303)) ([5b4a961](https://github.com/taikoxyz/taiko-geth/commit/5b4a9619f95a40395d23f25274450949a363dd32))
* **taiko:** release 1.7.0 ([#306](https://github.com/taikoxyz/taiko-geth/issues/306)) ([50da615](https://github.com/taikoxyz/taiko-geth/commit/50da61542536b03e76ea6e88d2c8548092c53681))
* **taiko:** release 1.8.0 ([#310](https://github.com/taikoxyz/taiko-geth/issues/310)) ([c29e304](https://github.com/taikoxyz/taiko-geth/commit/c29e3043e451e28ce10942f4f371942d30bc1fd3))
* **taiko:** release 1.9.0 ([#317](https://github.com/taikoxyz/taiko-geth/issues/317)) ([89b85fb](https://github.com/taikoxyz/taiko-geth/commit/89b85fb4b27ebca3d4811f1ec8dd3cce458ec4c1))
* **taiko:** release 2.1.0 ([#450](https://github.com/taikoxyz/taiko-geth/issues/450)) ([1f5afff](https://github.com/taikoxyz/taiko-geth/commit/1f5afff00a7a88c15d735e82862bc6b5049c46cb))
* **taiko:** rm unused `--gpo.defaultprice` flag ([#446](https://github.com/taikoxyz/taiko-geth/issues/446)) ([69e2b71](https://github.com/taikoxyz/taiko-geth/commit/69e2b71acc311e0738f99d25ef758e0db27b22a2))
* **taiko:** update preconf devnet genesis jsons ([#381](https://github.com/taikoxyz/taiko-geth/issues/381)) ([965043b](https://github.com/taikoxyz/taiko-geth/commit/965043b97723de0286411430e20083b159cadddc))


### Documentation

* fix badge in README ([#28796](https://github.com/taikoxyz/taiko-geth/issues/28796)) ([5c2de7f](https://github.com/taikoxyz/taiko-geth/commit/5c2de7fcbebe3aa7ea3a00414038a604067a4ef4))
* remove reference to being official ([#28858](https://github.com/taikoxyz/taiko-geth/issues/28858)) ([6a724b9](https://github.com/taikoxyz/taiko-geth/commit/6a724b94db95a58fae772c389e379bb38ed5b93c))
* **taiko:** autogen json-rpc docs ([#417](https://github.com/taikoxyz/taiko-geth/issues/417)) ([2fc319d](https://github.com/taikoxyz/taiko-geth/commit/2fc319d9e74710faa8631ee6f1310c701f75d882))


### Code Refactoring

* **accounts/abi:** use embed pkg to split default template to file ([5c84a20](https://github.com/taikoxyz/taiko-geth/commit/5c84a20827473cbe60ed16827df21b4ad395c9c2))
* improve readability of NewMethod print ([db7895d](https://github.com/taikoxyz/taiko-geth/commit/db7895d3b6e449cd4be6b5dbbd921979612f0d5f))


### Tests

* **raw_db:** add more `L1Origin` tests ([#427](https://github.com/taikoxyz/taiko-geth/issues/427)) ([d0c33f5](https://github.com/taikoxyz/taiko-geth/commit/d0c33f5bafdeac62f356fd7ab81fd83cd5710261))


### Workflow

* add release please and remove old workflow ([#250](https://github.com/taikoxyz/taiko-geth/issues/250)) ([d5f5f19](https://github.com/taikoxyz/taiko-geth/commit/d5f5f191b689570672435aadb152d65b1ce5412a))
* add taiko-kitty bot ([#256](https://github.com/taikoxyz/taiko-geth/issues/256)) ([8152c90](https://github.com/taikoxyz/taiko-geth/commit/8152c90c4fb9487d331376a66b9724a8029abd04))
* disable lint on travis ([#28706](https://github.com/taikoxyz/taiko-geth/issues/28706)) ([435bed5](https://github.com/taikoxyz/taiko-geth/commit/435bed5da04a386198ca25c5e1264330c7a0da5b))


### Build

* add support for ubuntu 23.10 (mantic minotaur) ([#28728](https://github.com/taikoxyz/taiko-geth/issues/28728)) ([76a5474](https://github.com/taikoxyz/taiko-geth/commit/76a5474b3245ef07cdeaaaeed298b0101bea246b))
* **deps:** bump golang.org/x/crypto from 0.15.0 to 0.17.0 ([#28702](https://github.com/taikoxyz/taiko-geth/issues/28702)) ([0cc192b](https://github.com/taikoxyz/taiko-geth/commit/0cc192bd3a89cae6d3c2a787b9265dda631d6529))
* fix hash for go1.23.0.linux-riscv64.tar.gz ([5c84a20](https://github.com/taikoxyz/taiko-geth/commit/5c84a20827473cbe60ed16827df21b4ad395c9c2))
* fix problem with windows line-endings in CI download ([#28900](https://github.com/taikoxyz/taiko-geth/issues/28900)) ([3adf1ce](https://github.com/taikoxyz/taiko-geth/commit/3adf1cecf203e9506d6ef87147693de4087e7d97)), closes [#28890](https://github.com/taikoxyz/taiko-geth/issues/28890)
* fix typo in comment ([#28800](https://github.com/taikoxyz/taiko-geth/issues/28800)) ([7280a5b](https://github.com/taikoxyz/taiko-geth/commit/7280a5b31a6e385b54e006ee476b76bfdbbde744))
* make linter emit output ([#28704](https://github.com/taikoxyz/taiko-geth/issues/28704)) ([952b343](https://github.com/taikoxyz/taiko-geth/commit/952b343cb3d319b77076ef3acb60e29e04cd51fd))
* remove ubuntu 'lunar' build ([#28962](https://github.com/taikoxyz/taiko-geth/issues/28962)) ([f0c5b67](https://github.com/taikoxyz/taiko-geth/commit/f0c5b6765d1815a3c6a0cd1b2740607a8b5bb1f8))
* upgrade -dlgo version to Go 1.21.4 ([#28505](https://github.com/taikoxyz/taiko-geth/issues/28505)) ([49b2c5f](https://github.com/taikoxyz/taiko-geth/commit/49b2c5f43c00b12f345182096f12b25f6599786a))
* upgrade -dlgo version to Go 1.21.5 ([#28648](https://github.com/taikoxyz/taiko-geth/issues/28648)) ([77c4bbc](https://github.com/taikoxyz/taiko-geth/commit/77c4bbcaa5f554f4cd73bdb7033d17b1fec493e9))
* upgrade -dlgo version to Go 1.21.6 ([#28836](https://github.com/taikoxyz/taiko-geth/issues/28836)) ([4c8d92d](https://github.com/taikoxyz/taiko-geth/commit/4c8d92d30342ccaa839ca590bafd5bfe5ca8c130))
* upgrade to golangci-lint v1.55.2 ([#28712](https://github.com/taikoxyz/taiko-geth/issues/28712)) ([8c2d455](https://github.com/taikoxyz/taiko-geth/commit/8c2d455ccd216fb8589c15339392ce9640d8090d))

## [1.18.0](https://github.com/taikoxyz/taiko-geth/compare/v1.17.0...v1.18.0) (2025-09-16)


### Features

* **miner:** preconf simulator api ([#433](https://github.com/taikoxyz/taiko-geth/issues/433)) ([1ce30d2](https://github.com/taikoxyz/taiko-geth/commit/1ce30d29a96b1a39c7fbb7f3f3f8065595e757da))
* **repo:** add claude code workflows ([#445](https://github.com/taikoxyz/taiko-geth/issues/445)) ([9aa789d](https://github.com/taikoxyz/taiko-geth/commit/9aa789de8225ecd7808e62214d6aa0e06c660c12))


### Chores

* **taiko_genesis:** add genesis for alethia-hoodi testnet ([#448](https://github.com/taikoxyz/taiko-geth/issues/448)) ([0ea6057](https://github.com/taikoxyz/taiko-geth/commit/0ea6057b4c2a428dad564ea421cb94f17049b5cc))
* **taiko_genesis:** bump devnet account balances ([#432](https://github.com/taikoxyz/taiko-geth/issues/432)) ([4bd4264](https://github.com/taikoxyz/taiko-geth/commit/4bd4264f1a53b9ac0cbfddfcfc70fa35bfa5e4aa))
* **taiko:** rm unused `--gpo.defaultprice` flag ([#446](https://github.com/taikoxyz/taiko-geth/issues/446)) ([69e2b71](https://github.com/taikoxyz/taiko-geth/commit/69e2b71acc311e0738f99d25ef758e0db27b22a2))

## [1.17.0](https://github.com/taikoxyz/taiko-geth/compare/v1.16.0...v1.17.0) (2025-07-16)


### Features

* **core:** add optional isForcedInclusion and Signature to L1Origin ([#434](https://github.com/taikoxyz/taiko-geth/issues/434)) ([a1fe1d8](https://github.com/taikoxyz/taiko-geth/commit/a1fe1d8309492866f21993b7065d608081b5750e))
* **core:** ll1 origin updates ([#439](https://github.com/taikoxyz/taiko-geth/issues/439)) ([8c15fa3](https://github.com/taikoxyz/taiko-geth/commit/8c15fa30e3cd178ce646a71b9c13993369af8f64))
* **taiko-api:** set l1 origin sig ([#440](https://github.com/taikoxyz/taiko-geth/issues/440)) ([8a11e69](https://github.com/taikoxyz/taiko-geth/commit/8a11e69d35c7a1cc7b7176d8a01dab8f2fb8c3d5))


### Bug Fixes

* **core:** revert PR ([#438](https://github.com/taikoxyz/taiko-geth/issues/438)) ([bdc0f79](https://github.com/taikoxyz/taiko-geth/commit/bdc0f79a6c8cb6e98bceac4bb2d64091ef61f50a))


### Chores

* log buildpayloadargs and its id ([#441](https://github.com/taikoxyz/taiko-geth/issues/441)) ([7810ed8](https://github.com/taikoxyz/taiko-geth/commit/7810ed871c46e9753b91889deb59a2a6fd7210d6))

## [1.16.0](https://github.com/taikoxyz/taiko-geth/compare/v1.15.0...v1.16.0) (2025-05-21)


### Features

* **core:** set `MainnetPacayaBlock` ([#428](https://github.com/taikoxyz/taiko-geth/issues/428)) ([7e0d8a0](https://github.com/taikoxyz/taiko-geth/commit/7e0d8a02c8966b9c14b2269084435e9a2d124d6d))
* **core:** update `InternalDevnetPacayaBlock` to `0` ([#430](https://github.com/taikoxyz/taiko-geth/issues/430)) ([c3ec5f8](https://github.com/taikoxyz/taiko-geth/commit/c3ec5f88cb4a2aef1c62bc7a21beb6f2609f6e12))
* **miner:** update logs in worker ([#429](https://github.com/taikoxyz/taiko-geth/issues/429)) ([6caf96f](https://github.com/taikoxyz/taiko-geth/commit/6caf96ffda260aef973b5874b4b3b5b53cf49637))
* **rawdb:** introduce `BuildPayloadArgsID` to `L1Origin` ([#426](https://github.com/taikoxyz/taiko-geth/issues/426)) ([187f85d](https://github.com/taikoxyz/taiko-geth/commit/187f85d7725fbf510bb6e3bd5dd8d48e6776ed17))


### Bug Fixes

* **repo:** fix json-rpc docs autogen workflow ([#419](https://github.com/taikoxyz/taiko-geth/issues/419)) ([190d6af](https://github.com/taikoxyz/taiko-geth/commit/190d6afff7f199744adebf07c082898713d2fb73))


### Chores

* **catalyst:** increase `maxTrackedPayloads` ([#424](https://github.com/taikoxyz/taiko-geth/issues/424)) ([c091dd6](https://github.com/taikoxyz/taiko-geth/commit/c091dd6c8fe90c3797f84e97f55ace5124ff1d6b))
* **params:** fix some comments ([#423](https://github.com/taikoxyz/taiko-geth/issues/423)) ([f4cb12a](https://github.com/taikoxyz/taiko-geth/commit/f4cb12a20321d93ebefeac3f44eaedf2fe40b90e))
* **repo:** update `docker-build` workflow ([#421](https://github.com/taikoxyz/taiko-geth/issues/421)) ([19dca2f](https://github.com/taikoxyz/taiko-geth/commit/19dca2f0b981b6d3b113cc2865e8fd80680bc407))
* **taiko:** update preconf devnet genesis jsons ([#381](https://github.com/taikoxyz/taiko-geth/issues/381)) ([965043b](https://github.com/taikoxyz/taiko-geth/commit/965043b97723de0286411430e20083b159cadddc))


### Documentation

* **taiko:** autogen json-rpc docs ([#417](https://github.com/taikoxyz/taiko-geth/issues/417)) ([2fc319d](https://github.com/taikoxyz/taiko-geth/commit/2fc319d9e74710faa8631ee6f1310c701f75d882))


### Tests

* **raw_db:** add more `L1Origin` tests ([#427](https://github.com/taikoxyz/taiko-geth/issues/427)) ([d0c33f5](https://github.com/taikoxyz/taiko-geth/commit/d0c33f5bafdeac62f356fd7ab81fd83cd5710261))

## [1.15.0](https://github.com/taikoxyz/taiko-geth/compare/v1.14.1...v1.15.0) (2025-04-01)


### Features

* **miner:** improve `pruneTransactions` ([#411](https://github.com/taikoxyz/taiko-geth/issues/411)) ([7a019ea](https://github.com/taikoxyz/taiko-geth/commit/7a019ea44bc98be082a1a4dfa0c6975b30939196))
* **miner:** reduce the number compression attempts when fetching transactions list ([#406](https://github.com/taikoxyz/taiko-geth/issues/406)) ([9e6edc5](https://github.com/taikoxyz/taiko-geth/commit/9e6edc51dbb37b3f9b280c95a031c0a2f68af53a))


### Chores

* **eth:** always use the latest block number for pending state in RPC calls ([#410](https://github.com/taikoxyz/taiko-geth/issues/410)) ([6822358](https://github.com/taikoxyz/taiko-geth/commit/682235849b5df653c4108f2a4099ee39b8cde6b6))

## [1.14.1](https://github.com/taikoxyz/taiko-geth/compare/v1.14.0...v1.14.1) (2025-03-21)


### Bug Fixes

* **repo:** fix workflow to use configs ([#402](https://github.com/taikoxyz/taiko-geth/issues/402)) ([177750c](https://github.com/taikoxyz/taiko-geth/commit/177750c73ee40cb32f10b4e4d2276f1a3b0cad3b))
* **taiko-client:** fix an issue in `encodeAndCompressTxList` ([#404](https://github.com/taikoxyz/taiko-geth/issues/404)) ([8d5d308](https://github.com/taikoxyz/taiko-geth/commit/8d5d308dfbc465d111e044b0c4f245e3b1ef5c3a))

## [1.14.0](https://github.com/taikoxyz/taiko-geth/compare/v1.13.0...v1.14.0) (2025-03-21)


### Features

* **taiko_genesis:** update `TaikoGenesisBlock` configs ([#400](https://github.com/taikoxyz/taiko-geth/issues/400)) ([139e562](https://github.com/taikoxyz/taiko-geth/commit/139e56205075ba5897c2a4ca707a52b096a3f200))

## [1.13.0](https://github.com/taikoxyz/taiko-geth/compare/v1.12.0...v1.13.0) (2025-03-15)


### Features

* **consensus:** introduce `AnchorV3GasLimit` ([#378](https://github.com/taikoxyz/taiko-geth/issues/378)) ([a0b97be](https://github.com/taikoxyz/taiko-geth/commit/a0b97be30cc01a93cebbd2d7188d28b0dcc5989a))
* **consensus:** introduce cache for the payloads ([#380](https://github.com/taikoxyz/taiko-geth/issues/380)) ([36430eb](https://github.com/taikoxyz/taiko-geth/commit/36430eb4f53a1455eb331f58c3ad2e88d8f40ecf))
* **consensus:** update `TaikoAnchor.anchorV3` selector ([#379](https://github.com/taikoxyz/taiko-geth/issues/379)) ([1e948cf](https://github.com/taikoxyz/taiko-geth/commit/1e948cff4c83e7a5cb0d8a4db27cbe59ce2a8884))
* **core:** align the upstream `types.Header` ([#393](https://github.com/taikoxyz/taiko-geth/issues/393)) ([573f8fc](https://github.com/taikoxyz/taiko-geth/commit/573f8fc144670d7221b387661f1f18dcd0935fe1))
* **eth:** changes based on protocol `Pacaya` fork ([#367](https://github.com/taikoxyz/taiko-geth/issues/367)) ([7bf5c0d](https://github.com/taikoxyz/taiko-geth/commit/7bf5c0d259f60f9d62d481c873053548c87b6fb5))
* **miner:** use `[]*ethapi.RPCTransaction` in RPC response body ([#391](https://github.com/taikoxyz/taiko-geth/issues/391)) ([afd09af](https://github.com/taikoxyz/taiko-geth/commit/afd09afd03871f9c66231a92bba706f4c491b877))
* **repo:** `go-ethereum` v1.15.5 upstream merge  ([#395](https://github.com/taikoxyz/taiko-geth/issues/395)) ([364acd0](https://github.com/taikoxyz/taiko-geth/commit/364acd00d1f2b45a07b7b1d20ec9b0f77be50b91))
* **repo:** add do-not-merge and rename files ([#390](https://github.com/taikoxyz/taiko-geth/issues/390)) ([3792f93](https://github.com/taikoxyz/taiko-geth/commit/3792f9356bfdc391fc8e112fccc7bf31d9edbb63))
* **taiko_genesis:** update devnet genesis JSONs for new `AnchorV3` method ([#382](https://github.com/taikoxyz/taiko-geth/issues/382)) ([2448fb9](https://github.com/taikoxyz/taiko-geth/commit/2448fb97a8b873c7bd7c0051cd83aaea339050e0))


### Bug Fixes

* **eth:** write `L1Origin` even if the payload is already in the cache ([#396](https://github.com/taikoxyz/taiko-geth/issues/396)) ([43a60b3](https://github.com/taikoxyz/taiko-geth/commit/43a60b36ea53a416ba01f271749d20f1251ce607))
