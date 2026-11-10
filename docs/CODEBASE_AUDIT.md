# eth2030 代码库审计报告（死代码 / 无效测试 / 安全依赖）

- **日期**：2026-11-10
- **范围**：仅 `eth2030` 仓库（不含其他仓库；`refs/` 只读 submodule 不在审计范围）
- **代码基线**：分支 `master`，`pkg/` 下 2286 个 `.go` 文件（不含 refs）、1092 个 `*_test.go`（约 44 万行），`go list ./...` 共 228 个包；另有 `site/`（Next.js 官网）
- **工具链**：`govulncheck`（安全扫描）、`deadcode`（含 `-test` 口径）、import 路径全仓 grep 交叉验证、`npm audit`
- **前置说明**：`docs/UNWIRED_CODE.md`（2026-03-09 旧审计）多处结论已过时，本文以其为线索但全部结论经重新 grep 复核（差异点在文中标注）。

---

## 一、死代码与无用历史代码

### 总体量级

| 口径 | 不可达函数数 |
|---|---|
| `deadcode ./...`（不含测试入口） | **12,049**（core 2987 / consensus 1742 / crypto 951 / p2p 827 / das 821 / engine 513 / trie 492 / txpool 409 …） |
| `deadcode -test ./...`（含测试入口） | **1,773** |

大部分「死」实为仅测试引用的辅助代码；把测试算作入口后仍彻底不可达的约 1,773 个。真正的死代码集中在**包级孤儿**，见下。

### A. 确定死代码（可直接删，约 40+ 文件）

#### A1. 孤儿包（import 路径全仓库 0 引用，含 `_test.go`）

| 包 | 说明 / 证据 |
|---|---|
| `pkg/txpool/validation` | `txpool.go validateTx()` 的「重构版替身」，从未接入 |
| `pkg/txpool/queue` | `txpool.go txSortedList` 的替身实现 |
| `pkg/txpool/replacement` | `txpool.go hasSufficientBump()` 的替身实现 |
| `pkg/txpool/fees` | 4 文件 + 4 测试，纯孤儿（已 wired 的是 `txpool/pricing`） |
| `pkg/das/gf` | GF(256)/Reed-Solomon 库；`das/blockerasure`、`das/reconstruction` 各自带擦除码实现，本包无人用 |
| `pkg/das/proof` | `block_blob_proof.go`，与 `pkg/proofs/` 职责重叠 |
| `pkg/das/rpo` | RPO 存储槽，0 引用 |
| `pkg/p2p/netutil` | IBLT/集合对账/带宽追踪，无 importers |
| `pkg/proofs/groth16` | **与 `pkg/proofs/groth16_gnark.go` 几乎逐行重复**（仅 `package groth16` vs `package proofs` 之差）；根包版本已被 `node/`、`txpool/stark` 引用，子包是复制死副本 |
| `pkg/core/eftest` | EF state test 运行器（7 文件），从未接入任何入口。⚠️ 注意：EF 37,126 向量的运行链路实际不在生产入口可达路径上，CI 中它靠 fixture skip（见二） |
| `pkg/core/ratemeter` | 仅 `core/rate_meter_test.go` 1 处测试引用；wired 的是 `core/gigagas` 的 GasRateTracker |

#### A2. `pkg/core` 根包内的死文件

| 文件 | 说明 |
|---|---|
| `core/state_transition.go` | 完整 `StateTransition` 实现 + 8 个 `ErrST*` 错误；实际执行路径在 `core/execution`，此为**死的状态机副本** |
| `core/genesis_commit.go` | `CommitGenesis()`，是 `core/config.CommitGenesisBlock()` 的死副本，0 调用 |
| `core/bloombits.go` | `BloomMatchesAddresses` 等，0 外部引用 |
| `core/block_helpers.go` | 注释自述「函数已搬到 core/block，这里是给本包测试用的 unexported 包装」，纯测试垫片 |

> 删除时需一并处理 `core` 根包内依赖这些文件的 `_test.go`。

#### A3. build tag 永不编译的文件 → 死依赖

- `pkg/crypto/bls/bls_blst_init.go`、`pkg/crypto/bls/bls_blst_adapter.go`（+ 各自 test）：`//go:build blst`，而 `pkg/Makefile` 的 `BUILD_TAGS := goethkzg`，标准构建永不包含。
- 连带结论：go.mod 中 `supranational/blst v0.3.17` **只被这两个文件 import**，是事实上的死依赖，可移除。

#### A4. 生产文件中的死符号（与 deadcode 输出互相印证）

- `pkg/e2e_advanced_helpers.go`（会被编译进正式构建）：`MockBlockExecutor`（:31）、`MockProofValidator.ValidateProof`（:46）、`AlwaysValidBLS` 全部 4 方法（:55-58）、`SimulateAttack`（:155）—— deadcode 标记 unreachable 且 grep 确认**没有任何 `_test.go` 使用**（该文件共 21 个 unreachable func）。
- `pkg/e2e_helpers.go`：`MakeBlobTx`、`MakeContractTx` 全仓库除「测试 helper 的 meta 测试」外零调用（见二 §2.4）。

#### A5. 传递性死代码

- **`pkg/geth`（geth 兼容层，42 个 unreachable func）**：全仓库唯一 import 它的是 `core/eftest/geth_runner.go`，而 `core/eftest` 本身无人引用 → 从入口不可达。删 `core/eftest` + `pkg/geth` 可一起带走。
  - ⚠️ 旧文档勘误：`docs/UNWIRED_CODE.md:750` 声称 `cmd/eth2030-geth/precompiles.go:77` 调用 `ListCustomPrecompiles()`，grep 实测 `cmd/eth2030-geth` **并不 import `pkg/geth`**，该结论已过时。

### B. 疑似死代码（需确认后处理）

| 项 | 说明 |
|---|---|
| `pkg/epbs`、`pkg/rollup` 根包 | 仅被 e2e 根包测试宿主和自身子包 import，未从 `cmd`/`node` 可达；子包均已 wired。根包公共类型去留需人工确认 |
| 15+ 个 `*_compat.go` 再导出垫片 | `log/formatter_compat.go`、`node/events_compat.go`、`node/health_checker_compat.go`、`node/lifecycle_compat.go`、`rlp/errors_compat.go`、`proofs/{queue,prover,kzg}_compat.go`、`rollup/anchor_chain_tracker_compat.go`、`eth/protocol_version_compat.go`、`engine/{errors,util}_compat.go`、`consensus/voting_compat.go`、`crypto/{vdf,threshold}_compat.go`、`witness/cache_compat.go`。符号确有引用，但属迁移残留，可合并 |
| `consensus` 两套平行 beacon state | `bsn_beacon_state.go`（`BsnBeaconState`，depproc_* 用）与 `beacon_state_v2.go`（`BeaconStateV2`，finality_rules/fork_choice_lmd 用），两套同构状态机并存，疑似演进中只留一套 |
| `crypto/ipa` 的 `GoIPABackend` | `ipa_integration.go:262-289`，自述 placeholder，永远 fallback 到 PureGo；`:265` 的 TODO「创建 ipa_goipa.go + goipa tag」从未落实 |
| `eth/protocol_extended.go:15` | `ExtETH72 = 72 // EIP-XXXX` 占位常量，协议版本号还是 XXX |

### C. 历史遗留杂物

| 项 | 说明 / 处置 |
|---|---|
| `cl.log`（仓库根，40KB） | **被 git 跟踪**的 Lighthouse 运行日志（先提交后忽略）。→ `git rm` |
| `pkg/bin/`（74MB 二进制 ×2）、`pkg/eth2030-geth`（55MB）、`pkg/coverage/`（18MB，2026-02-25 过期产物） | 均 gitignored 但占盘，本地清理即可 |
| `pkg/Dockerfile` | 仓库根无 Dockerfile，无重复；但其 `go build` 未带 `-tags goethkzg`，与 Makefile `BUILD_TAGS` 不一致 → 容器内 KZG 走非生产后端（配置遗留，非死代码） |
| `pkg/cmd/send-frame` | 单文件 dev 工具（发 frame tx），仅本地手工使用 |
| `site/` | Next.js 官网，与 Go 代码无关，独立评估 |
| 大段注释掉的代码 | 逐模式扫描（`// if/for/func/return/:=` 连续块）**未发现** 5 行以上，这一项干净 |

> 符号级抽查显示 `bal/`、`zkvm/`、`witness/`、`proofs/`（根包）导出符号均有外部引用，wiring 质量尚可。

---

## 二、无效测试审查

CI 事实基线：`.github/workflows/ci.yml` 只跑 `go test ./...`（无 build tag、无 `-race`、无 `-short`、**不 checkout submodules**）。
整体质量基线良好（`consensus/ssf_engine_test.go`、`core/vm/gas_table_test.go`、`crypto/bls/bls12381_test.go` 等断言密集），多数可疑命中经核实为「委托给带断言的 helper」的有效测试，以下为确认的问题。

### 1. 确定无效：约 169 个

#### 1.1 无条件永久 skip 的死测试 — 25 个
首行即 `t.Skip(...)`，永远不执行任何逻辑（均与 `refs/blst` 子模块构建相关，即使构建了 blst 也不会跑）：

| 位置 | 数量 |
|---|---|
| `pkg/crypto/bls/bls_aggregate_test.go:99,184,224,282` | 4 |
| `pkg/crypto/bls/bls_aggregate_extended_test.go:82,192,232,250,369` | 5 |
| `pkg/crypto/bls/bls_integration_test.go:11,46,92,360` | 4（文件名自称 PureGo 后端，skip 理由却写 blst —— 复制粘贴的失效 skip） |
| `pkg/consensus/bls_operations_test.go:172,225,302` | 3 |
| `pkg/consensus/finality_bls_adapter_test.go:48,96,228,282` | 4 |
| `pkg/light/bls/bls_verifier_test.go:151,230,250` | 3 |
| `pkg/core/eftest/fixture_loader_test.go:371,698` | 2 |

#### 1.2 空测试 / 纯占位 — 2 个
- `pkg/node/backend/backend_test.go:41` `TestNewRPCBackend` —— 函数体只有注释，连 Skip 都没调，空体通过。
- `pkg/node/backend/backend_test.go:46` `TestNewEngineBackend` —— 同上。

#### 1.3 断言恒不失败（失败分支只 t.Log / t.Skip / 空 if / 吞 err）— 20 个

| 文件:行 | 测试 | 问题 |
|---|---|---|
| `pkg/das/das_test.go:424` | TestDifferentNodeIDsGetDifferentGroups | 失败分支 `t.Log("warning: ...")`（:441） |
| `pkg/das/network/network_test.go:348` | TestAssignCustodyDifferentNodes | 同模式（:363） |
| `pkg/das/network/das_network_mgr_test.go:736` | TestSelectSampleColumnsDifferentSlots | 同模式（:750） |
| `pkg/das/cell/cell_gossip_test.go:78` | TestAssignSubnetsDifferentNodes | 同模式（:93） |
| `pkg/das/validator/validator_test.go:309,329` | TestSampleColumnsDifferentSlots / …DifferentNodes | 同模式（:325,:345） |
| `pkg/consensus/committee_selection_test.go:73` | TestComputeShuffledIndexDifferentSeeds | 同模式（:83） |
| `pkg/consensus/committee_assignment_test.go:98,273` | TestSwapOrNotShuffleDifferentSeeds / TestProposerWeightedSelection | 同模式（:110,:299） |
| `pkg/consensus/committee_rotation_test.go:420` | TestCREpochTransitionRotation | 同模式（:432） |
| `pkg/core/types/bloom_test.go:127,~168` | TestBloomContainsNegative 等 | `t.Log("false positive...")`（:134,:175） |
| `pkg/core/types/bloom_methods_test.go:126` | TestBloomTestNonMembership | 同模式（:131） |
| `pkg/txpool/shared/shared_mempool_test.go:222` | TestIsKnown_UnknownTx | 失败分支 `t.Skip(...)`（:229），且空 bloom 不会有假阳性，skip 理由错误 |
| `pkg/ssz/merkle_test.go:156,328,363` | TestMerkleizeEmptyChunks 等 | `if root == [32]byte{} { /* 只有注释 */ }` 空分支 |
| `pkg/node/genesis_json_test.go:128` | TestAddressJSON_WithoutPrefix | `if err != nil { _ = err }`，成功失败都过 |
| `pkg/e2e_rv_guest_test.go:156` | TestE2E_RV_PreIPlusForkBoundary | `_ = err` 吞掉结果，注释自认「just verify no panic」 |
| `pkg/core/vm/call_gas_test.go:565` | TestCallWithValue_StipendNotReturnedToCaller | 失败分支 `t.Logf("note: ...")`（:616） |

#### 1.4 build tag 导致 CI 从不编译 — 122 个测试（10 个文件）
CI 无 `-tags`，以下文件在 CI 中完全不参与编译，也没有任何 workflow 用对应 tag 跑：

- `//go:build integration`（76 个）：`core/e2e_test.go`(15)、`core/bal_integration_test.go`(18)、`core/e2e_block_test.go`(5)、`core/e2e_eip_spec_test.go`(7)、`core/e2e_eip_spec_ext_test.go`(11)、`core/e2e_local_tx_test.go`(11)、`core/dim_storage_chain_test.go`(7)、`p2p/tor_integration_test.go`(2)
- `//go:build blst`（25）：`crypto/bls/bls_blst_adapter_test.go`
- `//go:build goethkzg`（21）：`crypto/bls/kzg_goeth_adapter_test.go`（Makefile 仅构建二进制时用该 tag）

#### 1.5 复制粘贴重复 — 3 组
- `pkg/zkvm/types_test.go:190` `TestProverBackendInterfaceImplementations` ≡ `pkg/zkvm/zkvm_test.go:280` `TestProverBackendInterface`（逐字相同）。
- `pkg/e2e_roadmap_test.go:783` `TestE2E_Roadmap_ProofAggregation` ⊂ `pkg/e2e_pq_binary_test.go:333` `TestE2E_PQBinary_ProofAggregation`（PQ 版是严格超集，还加了篡改校验）。
- tx 生命周期三重覆盖：`e2e_test.go:340` / `e2e_pipeline_test.go:18` / `integration_test.go:102`，同一「充值→入池→出块→断言」流水线仅常数不同。

### 2. 疑似无效：约 110 个

- **CI 中必然 skip 的 fixture 依赖测试（~40）**：`core/eftest/fixture_loader_test.go`（35 Test）+ `geth_runner_test.go`（5）依赖 `refs/go-ethereum` 子模块的 `GeneralStateTests`；CI `actions/checkout@v4` 未配 `submodules: recursive` → 全部 skip。**EF 向量这套「primary validation target」实际从未在 CI 跑过**。
- **"should not panic" 冒烟测试（~45）**：无任何断言，功能退化不报错。代表：`p2p/discover/v5_extra_test.go`（15 个）、`rpc/conformance_test.go:1004,1011`、`core/state/access_list_test.go:177,202,208`、`core/vm/eip7708_test.go:127,375`、`rpc/subscription/manager_test.go` NoSubscribers 家族、`witness/collector_test.go:276`（注释自认 no assertion on witness）等。
- **无断言并发测试（~40）**：CI 无 `-race`，只能靠 panic/deadlock 超时失败。代表：`consensus/{epoch_processor,forkchoice,validator_lifecycle}_test.go`、`focil/enhanced_test.go:678,724`、`das/custody/custody_verify_test.go:667` 等。若本意是抓 data race，CI 配置使其失效。
- **meta 测试（16）**：`pkg/e2e_helpers_test.go` 全部测的是 `e2e_helpers.go` 工厂函数；其中 `MakeBlobTx`、`MakeContractTx` 除该 meta 测试外零调用（死 helper），对应测试保护的是死代码（见一 §A4）。

### 3. 建议

1. **CI 加 `-race`**（否则 ~40 个并发测试形同虚设）。
2. **CI 配 `submodules: recursive`** 让 EF 向量真正跑起来，否则 `crypto/bls` 名义覆盖的 ~80% 是虚假的。
3. 对 `integration` / `blst` / `goethkzg` 三个 tag 明确取舍：加 tag job 或删文件（122 个测试 + 25 个永久 skip 合计构成虚假名义覆盖的主体）。
4. `pkg/e2e_roadmap_test.go:376` `TestE2E_Roadmap_QuickSlotsTiming` 用 `time.Now()` 推算 slot，慢机器上有 flaky 风险。
5. `pkg/core/vm/precompiles_json_test.go:239` `TestJsonPointEvaluation` 无条件 skip，该 precompile 实际无向量覆盖。
6. site/（Next.js）零测试，`package.json` 无 test script。

---

## 三、安全告警与依赖升级（已执行 ✅）

### pkg/（Go）

| 项 | 结果 |
|---|---|
| govulncheck 扫描 | 唯一告警 **GO-2026-5932**：`golang.org/x/crypto/openpgp` 不维护且有已知安全问题（模块级，项目代码未调用） |
| 已升级 | `golang.org/x/crypto` v0.58.0 → **v0.58.1-0.20261009181718-2f1f834330b2**（修复版）；`golang.org/x/sync` v0.24.0 → **v0.25.0**（唯一有更新的直接依赖） |
| 验证 | `go mod tidy` + `go build ./...` **BUILD_OK**；复扫 govulncheck **No vulnerabilities found** |

其余直接依赖（`go-ethereum v1.17.10`、`gnark`、`go-eth-kzg`、`uint256`、`blake3` 等）均已是最新无更新。
另：`supranational/blst v0.3.17` 为死依赖（见一 §A3），待删除 `blst` build tag 文件后一并移除。

### site/（Next.js 官网）

| 项 | 结果 |
|---|---|
| npm audit（升级前） | **5 漏洞（1 critical + 4 high）**：`next`（request smuggling / RCE / SSRF / 缓存投毒等 30 条 GHSA，critical）、`postcss`（路径穿越 / 任意文件读取，high）、`sharp`（libvips/libheif/librsvg 继承漏洞，high）、`nanoid`（high）、`source-map-js`（event-loop DoS，high） |
| 已升级 | `next` ^15.1.0 → **^16.4.5**（`npm audit fix` + `--force` 大版本升级）；`nanoid` / `sharp` / `source-map-js` 随锁文件修复 |
| 验证 | `npm audit` → **found 0 vulnerabilities**；`npm run build` 成功，**149 页面全部生成**（SSG 142 路径 + 静态页） |

### 本次落盘的变更（未提交）

```
pkg/go.mod           x/crypto 安全修复版 + x/sync 升级
pkg/go.sum           随动
site/package.json    next ^16.4.5
site/package-lock.json
site/tsconfig.json   Next 16 自动更新
site/next-env.d.ts   Next 16 自动更新
```

> 提交建议（遵循仓库规则：独立提交、简洁动作导向、无 AI Co-Authored-By）：
> - `deps: bump x/crypto to security fix, x/sync to v0.25.0`
> - `site: upgrade next to 16.4.5, fix npm audit vulnerabilities`

---

## 四、建议的后续动作（未执行，需拍板）

按优先级：

1. **删除确定死代码**（一 §A）：孤儿包 11 个、core 根包死文件 4 个、`core/eftest` + `pkg/geth` 链路、`blst` tag 文件及死依赖、`e2e_advanced_helpers.go` 死符号。
2. **`git rm cl.log`**；本地清理 `pkg/bin/`、`pkg/coverage/`、`pkg/eth2030-geth` 二进制残留。
3. **Dockerfile 加 `-tags goethkzg`** 与 Makefile 对齐。
4. **测试清理**（二 §1）：删永久 skip 的 25 个、空测试 2 个；恒不失败的 20 个补断言；对 122 个 tag 测试做取舍。
5. **CI 强化**：`-race`、`submodules: recursive`、tag job 取舍。
6. **疑似项逐个确认**（一 §B）：compat 垫片合并、双 beacon state 收敛、`GoIPABackend` 落地或删除。

---

## 附录：审计方法与可复现命令

```bash
cd pkg
govulncheck ./...                     # 安全扫描（含 -show verbose 看模块级告警）
deadcode ./...                        # 12,049 个不可达函数
deadcode -test ./...                  # 1,773 个（含测试入口口径）
go list ./...                         # 228 个包；对每个包 grep import 路径验证孤儿
cd ../site && npm audit --omit=dev    # npm 漏洞审计
```

孤儿包验证方式：`go list ./...` 得到包列表 → 对每个非 main 包 grep 其 import 路径在全仓的引用数（排除包自身与 `_test.go`）→ 引用为 0 者列入 A1。
