# 验证记录：模型智商检测作品展

- **Change ID**：`add-iq-benchmark-gallery`
- **分支**：`feature/iq-benchmark-gallery`
- **当前状态**：阶段 1~6 已完成并回填真实输出；仅剩 6.9 灰度需在真实部署环境执行。

---

## 一、开工前确认项（已由用户裁决，2026-11-02）

| # | 事项 | 裁决结果 | 落地影响 |
|---|------|---------|---------|
| 1 | i18n 语言 | **只做 `zh` + `en`（不做 `ja`）** | `frontend/src/i18n/index.ts` 零改动；上游核心文件接触面维持 4 个；不新增 `locales/ja/` |
| 2 | 对象存储删除能力 | **允许新增删除能力** | 实现方式收敛为「纯追加」以避免改上游接口签名：<br>① 在 `S3ImageStorage` 新增 `Delete(ctx, key) error` 方法（新增方法，不改签名）<br>② 在 `ImageResultUploader` 新增导出方法 `Storage() ImageStorage`（新增方法）<br>③ 新模块内定义 `BlobDeleter` 窄接口并做类型断言<br>**不改 `ImageStorage` 接口本身**，因此上游所有既有实现与测试 fake（`fakeImageStorage`、`noopImageStorage`）无需改动、不会编译失败 |
| 3 | LLM 裁判 | **不实现** | 首版只保留人工 `score` 录入；`settings.llm_judge_enabled` 与 `case.llm_judge_enabled` 字段保留但恒为关，API 接受写入但不参与执行 |
| 4 | 内部账号在管理员用户列表可见性 | **前端加「隐藏内部账号」开关，默认开启；不改上游查询** | 后端零改动；前端用户列表加本地过滤开关（依据 `UserFromServiceAdmin` 已含的 `notes` 前缀匹配） |

> 结论：上游核心文件接触面 **维持 4 个**（`handler.go`、`handler/wire.go`、`server/router.go`、`cmd/server/wire.go`），全部为纯追加行。

---

## 二、验证命令清单与状态

| # | 命令 | 期望 | 状态 |
|---|------|------|------|
| 1 | `git status --porcelain`（阶段 0 开工前） | 空 | ✅ 已确认干净 |
| 2 | `git branch --show-current` | `feature/iq-benchmark-gallery` | ✅ |
| 3 | `go build ./...` | 无错误 | ✅ exit=0（见 §2.2） |
| 4 | `golangci-lint run ./...` | 0 issue | ⚠️ 新增包 0 issue；全仓库 50 issue 与基线**逐条完全相同**（见 §2.3） |
| 5 | `go test -tags=unit ./...` | 全绿 | ⚠️ 新增包全绿；4 个上游包失败，与基线逐一同一（见 §2.4） |
| 6 | `go test -tags=integration ./...` | 全绿 | ✅ 本功能 13 个用例在真实 PostgreSQL 全绿（见 §2.5） |
| 7 | `pnpm install --frozen-lockfile` | 无变更 | ✅ `pnpm-lock.yaml` 无变更 |
| 8 | 前端 type-check（`npx vue-tsc -b`） | 无错误 | ✅ exit=0 |
| 9 | `pnpm vitest run` | 全绿 | ✅ 391 文件 / 3220 用例全绿（见 §2.6） |
| 10 | `git diff --stat main-self...HEAD -- backend/internal/service backend/internal/handler/dto` | 空 | ✅ 见 §2.7 |

**环境基线（阶段 6 实测）**：
- `go version go1.27.3 windows/amd64`
- `pnpm 11.21.0`；`node v24.19.0`
- `golangci-lint has version 2.14.0 built with go1.27.3`
  - 与 `tasks.md` 原写的 v2.13 不同：**v2.13.0 无法使用**。它内置的 `x/tools`
    读不了 Go 1.27.3 的导出数据，直接以 typecheck 失败退出：
    `could not load export data: internal error in importing "internal/goarch"
     (cannot decode "internal/goarch", export data version 5 is greater than
     maximum supported version 4)`。
  - 因此改用 **v2.14.0**，与 `.github/workflows/backend-ci.yml:82`
    声明的 CI 版本一致。

### 2.1 上游既有失败基线（本次已核证，非本功能引入）

阶段 4 收尾时执行 `go test -tags=unit ./...`，出现若干失败包。为了确认这些
失败**是否由本功能引入**，在干净的 `f688e8999e` 上建立独立 worktree
（`git worktree add <tmp> f688e8999e --detach`）并运行同一批包，结论如下：

| 包 | HEAD 基线结果 | 本分支结果 | 结论 |
|----|--------------|-----------|------|
| `internal/repository` | FAIL（3 个 `TestPgDumper*`） | FAIL（同样 3 个） | 基线既有，与本次改动无关 |
| `internal/handler` | FAIL（`TestResolvePageImagePath`） | FAIL（同一个） | 基线既有 |
| `internal/server` | FAIL（`TestAPIContracts`，缺 `tutorial_enabled`） | FAIL（同一个） | 基线既有 |
| `internal/service` | FAIL（顺序敏感，见下） | FAIL（同类） | 基线既有 |
| `plugins/upstream-monitor/repository` | 不涉及 | **FAIL → 已修复** | **本次引入，见下** |

> **`internal/service` 的顺序敏感（重要，勿误判为回归）**：
> 该包的失败用例**每次运行都不一样**，且**基线同样失败**。
> 已核证的最小复现：
> ```
> # 基线 f688e8999e
> go test -tags=unit -count=1 -run 'TestCommandCodeGatewayPassesThroughCatalogProtocols|TestGatewayService_AcceptEncodingOnWire|TestSMTPConnection' ./internal/service/
> --- FAIL: TestCommandCodeGatewayPassesThroughCatalogProtocols
>     model_protocol_catalog_test.go:374: ... Messages: http://cc-.../provider/v1/responses
>
> # 单独跑这一个测试（两棵树都通过）
> go test -tags=unit -count=1 -run 'TestCommandCodeGatewayPassesThroughCatalogProtocols' ./internal/service/
> ok  github.com/Wei-Shaw/sub2api/internal/service
> ```
> 根因：`TestSMTPConnection*` 等用例污染了包级共享的 `upstreamModelProtocols`
> 缓存，后续用例读到脏数据。这是**上游测试自身的隔离缺陷**，与本功能无关
> （本功能不触碰 `internal/service` 包内任何文件）。
> 阶段 6 的正式回填已**串行**执行（`-count=1`，单进程）。

**本次引入并已修复的真实缺陷**：`plugins/upstream-monitor/repository` 全部
测试报 `create index "iqbenchmarktask_group_id_model": index already exists`。
根因：`ent/schema/iq_benchmark_task.go` 同时声明了
「`(group_id, model)` 部分唯一索引」与「`(group_id, model)` 普通索引」，
Ent 按字段拼接生成的名字二者相同，Ent 建表时撞名。
修复：为 4 张新表的**全部**索引显式 `StorageKey(...)`，名字与
`migrations/249_iq_benchmark_gallery.sql` 完全一致
（`uq_iq_task_group_model_enabled`、`idx_iq_task_due`、`idx_iq_task_group_model`、
`idx_iq_artifact_run/feed/filters/status/task_live`、`idx_iq_run_task`、
`idx_iq_run_pending_cost`、`idx_iq_case_listing`）。
这样「Ent 建表」与「SQL 迁移」两条链路产出同一份 schema，也顺带消除了
将来再撞名的可能。

### 2.2 `go build ./...`

```
$ go version
go version go1.27.3 windows/amd64

$ go build ./...
exit=0
```
**无任何输出即成功**（Go 的 build 在成功时不打印内容）。

### 2.3 `golangci-lint run ./...`

先验证新增包自身干净：

```
$ golangci-lint run --timeout=30m ./internal/iqbenchmark/...
0 issues.
lint=0
```

再跑全仓库。**关键判据不是 issue 数量，而是 issue 集合与基线逐条相同**。
注意 `golangci-lint` 默认有 `max-same-issues=3` / `max-issues-per-linter=50`
的截断，直接用默认值比较会因截断顺序不同而产生假差异，故两侧都加
`--max-same-issues=0 --max-issues-per-linter=0`：

```
$ golangci-lint run --timeout=30m --max-same-issues=0 --max-issues-per-linter=0 ./...
（本分支）ours_count=50
（基线 f688e8999e）base_count=50
=== IDENTICAL issue sets ===
```

结论：**50 个 issue 逐条完全相同，本功能引入 0 个新 issue。**
这 50 个 issue 全部落在上游既有文件（`internal/handler/admin/tutorial_handler.go`、
`internal/handler/failover_loop.go`、`internal/server/middleware/request_observability.go`、
`internal/repository/checkin_*.go` 等），无一在 `internal/iqbenchmark/` 下。
用默认截断参数时的分类计数（两侧同为）：

```
42 issues:
* errcheck: 28
* gofmt: 3
* staticcheck: 9
* unused: 2
```

### 2.4 `go test -tags=unit ./...`（串行，`-count=1`）

本分支实际输出（只列 FAIL 摘要，`ok` 行共 80 个包）：

```
$ go test -tags=unit -count=1 ./...
--- FAIL: TestResolvePageImagePath (0.01s)
FAIL	github.com/Wei-Shaw/sub2api/internal/handler	52.871s
--- FAIL: TestPgDumperHoldsMigrationLockThroughReaderClose (0.17s)
--- FAIL: TestPgDumperReleasesMigrationLockWhenProcessFails (0.21s)
--- FAIL: TestPgDumperReportsUnlockFailureAndDiscardsConnection (0.20s)
FAIL	github.com/Wei-Shaw/sub2api/internal/repository	12.310s
--- FAIL: TestAPIContracts (0.02s)
FAIL	github.com/Wei-Shaw/sub2api/internal/server	2.544s
--- FAIL: TestCommandCodeGatewayPassesThroughCatalogProtocols (0.00s)
--- FAIL: TestFetchCodexModelsManifestOAuthPreservesResponsesLite (0.00s)
FAIL	github.com/Wei-Shaw/sub2api/internal/service	230.150s
exit=1
```

基线 `f688e8999e` 同一条命令：

```
$ go test -tags=unit -count=1 ./...
--- FAIL: TestResolvePageImagePath (0.01s)
FAIL	github.com/Wei-Shaw/sub2api/internal/handler	48.742s
--- FAIL: TestPgDumperHoldsMigrationLockThroughReaderClose (0.11s)
--- FAIL: TestPgDumperReleasesMigrationLockWhenProcessFails (0.11s)
--- FAIL: TestPgDumperReportsUnlockFailureAndDiscardsConnection (0.11s)
FAIL	github.com/Wei-Shaw/sub2api/internal/repository	11.808s
--- FAIL: TestAPIContracts (0.01s)
FAIL	github.com/Wei-Shaw/sub2api/internal/server	2.737s
--- FAIL: TestSMTPConnectionImplicitTLS (0.00s)
--- FAIL: TestSMTPConnectionStartTLSFallbackWhenTLSEnabled (0.00s)
--- FAIL: TestSMTPConnectionMandatoryStartTLSRefusesPlaintext (0.00s)
--- FAIL: TestSMTPConnectionOpportunisticStartTLSWhenTLSDisabled (0.00s)
--- FAIL: TestSMTPConnectionPlainWhenNoStartTLS (0.00s)
--- FAIL: TestSendEmailWithConfigStartTLSFallback (0.00s)
--- FAIL: TestSendEmailWithConfigImplicitTLS (0.00s)
--- FAIL: TestForwardSystemOneSchemaConformancePassthrough (0.01s)
FAIL	github.com/Wei-Shaw/sub2api/internal/service	230.586s
baseline_exit=1
```

**逐包比对**：

| 包 | 本分支 | 基线 | 判定 |
|----|--------|------|------|
| `internal/handler` | FAIL `TestResolvePageImagePath` | FAIL 同一个 | **完全相同** |
| `internal/repository` | FAIL 3 个 `TestPgDumper*` | FAIL 同样 3 个 | **完全相同** |
| `internal/server` | FAIL `TestAPIContracts` | FAIL 同一个 | **完全相同** |
| `internal/service` | FAIL 2 个 | FAIL 8 个，且**换了用例** | **顺序敏感噪声，两侧都失败** |
| 其余 77 个包 | 全部 `ok` | 全部 `ok` | 一致 |

`internal/service` 的失败在**两次基线运行之间也会变**（第一次基线跑出
1 个失败，第二次跑出 8 个），且本分支的 2 个与基线的 8 个没有交集。
这正说明它是**包级共享状态被前序用例污染**导致的顺序敏感，
而不是某个确定的功能缺陷。本功能不修改 `internal/service` 包内任何文件
（见 §2.7），因此不可能是本功能引入。

**新增包的单元测试（全绿，同一命令的子集）**：

> 修正（2026-11-02，阶段 6 复验）：下方首次回填时 `module` 包尚无测试文件
> （显示 `[no test files]`）。`4a5a7c414e` 新增 `module/runtime_test.go` 后该行
> 已变为 `ok`，此处按**重跑的真实输出**更新，与 §3.8 的 5 个用例一致。

```
$ go test -tags=unit -count=1 ./internal/iqbenchmark/...
?   	github.com/Wei-Shaw/sub2api/internal/iqbenchmark	[no test files]
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/handler	0.542s
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/module	0.877s
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/ratelimit	0.663s
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/repository	0.748s
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/sanitize	0.635s
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/scheduler	0.493s
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service	7.208s
exit=0
```

### 2.5 `go test -tags=integration ./...`

本项目的集成测试一律 **opt-in**：未设 DSN 环境变量时 `t.Skip`。
本功能新增 13 个用例，需要一次性 PostgreSQL：

```
$ docker run -d --name iq-it-pg \
    -e POSTGRES_USER=sub2api -e POSTGRES_PASSWORD=test1234 -e POSTGRES_DB=iq_test \
    -p 127.0.0.1:55433:5432 postgres:18-alpine

$ export IQ_BENCHMARK_TEST_DSN='postgres://sub2api:test1234@127.0.0.1:55433/iq_test?sslmode=disable'
$ go test -tags=integration -count=1 -v -run 'TestIQBenchmark' ./internal/iqbenchmark/repository/
=== RUN   TestIQBenchmarkDailyCostScopesToInternalKeyOnly
--- PASS: TestIQBenchmarkDailyCostScopesToInternalKeyOnly (0.16s)
=== RUN   TestIQBenchmarkDailyCostResetsNextDay
--- PASS: TestIQBenchmarkDailyCostResetsNextDay (0.15s)
=== RUN   TestIQBenchmarkGuardBudgetBreaker
--- PASS: TestIQBenchmarkGuardBudgetBreaker (0.16s)
=== RUN   TestIQBenchmarkGuardBudgetDisabledWhenZero
--- PASS: TestIQBenchmarkGuardBudgetDisabledWhenZero (0.14s)
=== RUN   TestIQBenchmarkUsageSnapshotByRequestIDs
--- PASS: TestIQBenchmarkUsageSnapshotByRequestIDs (0.15s)
=== RUN   TestIQBenchmarkMigrationAppliesCleanly
--- PASS: TestIQBenchmarkMigrationAppliesCleanly (0.16s)
=== RUN   TestIQBenchmarkPartialUniqueEnabledTask
--- PASS: TestIQBenchmarkPartialUniqueEnabledTask (0.16s)
=== RUN   TestIQBenchmarkClaimSkipsLockedRows
--- PASS: TestIQBenchmarkClaimSkipsLockedRows (0.17s)
=== RUN   TestIQBenchmarkLeaseTakeover
--- PASS: TestIQBenchmarkLeaseTakeover (0.15s)
=== RUN   TestIQBenchmarkEntSchemaMatchesMigration
--- PASS: TestIQBenchmarkEntSchemaMatchesMigration (0.18s)
=== RUN   TestIQBenchmarkTaskStatusCheck
--- PASS: TestIQBenchmarkTaskStatusCheck (0.17s)
=== RUN   TestIQBenchmarkIntervalCheck
--- PASS: TestIQBenchmarkIntervalCheck (0.15s)
=== RUN   TestIQBenchmarkServiceStatusValuesMatchMigration
--- PASS: TestIQBenchmarkServiceStatusValuesMatchMigration (0.17s)
PASS
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/repository	2.541s
exit=0
```

隔离措施（防止误伤真实库）：
- 只接受路径为 `/iq_test` 的 DSN，否则 `require.Equal` 直接失败；
- 每次运行 `CREATE SCHEMA iq_it_<纳秒>`，清理时只 `DROP` 自己那个 schema；
- 未设 `IQ_BENCHMARK_TEST_DSN` 时全部 `t.Skip`，CI 无 PG 也能过。

集成 tag 下的 lint 同样干净：

```
$ golangci-lint run --timeout=30m --build-tags=integration ./internal/iqbenchmark/...
0 issues.
```

> 说明：执行 `go test -tags=integration ./...` 全仓库时，其余既有集成包
> 因未设各自的 DSN 而全部 skip，属预期行为。

### 2.6 前端校验

```
$ npx vue-tsc -b
typecheck_exit=0

$ git status --short frontend/pnpm-lock.yaml
(无输出 → 锁文件无变更)

$ npx vitest run --pool=threads
Test Files  391 passed (391)
Tests  3220 passed (3220)
vitest_exit=0
```

> `--pool=threads` 是必须的：默认 `forks` pool 会尝试 spawn 一个已被删除的
> `C:\SelfArea\DevTools\nodejs\node.exe` 并报 `ENOENT`（与环境有关，与本功能无关）。

本功能自身的 7 个 spec 文件、165 个用例：

```
$ npx vitest run --pool=threads src/features/iq-benchmark
✓ src/features/iq-benchmark/__tests__/api.spec.ts (18 tests) 36ms
✓ src/features/iq-benchmark/__tests__/userFilter.spec.ts (11 tests) 11ms
✓ src/features/iq-benchmark/__tests__/filters.spec.ts (36 tests) 48ms
✓ src/features/iq-benchmark/__tests__/useArtifactFeed.spec.ts (15 tests) 41ms
✓ src/features/iq-benchmark/__tests__/sanitize.spec.ts (38 tests) 177ms
✓ src/features/iq-benchmark/__tests__/rateLimit.spec.ts (15 tests) 14ms
✓ src/features/iq-benchmark/__tests__/components.spec.ts (32 tests) 271ms

Test Files  7 passed (7)
Tests  165 passed (165)
```

i18n 键一致性守卫（新增 `iqBenchmark.admin.users.hideInternal` 后仍通过）：

```
$ npx vitest run --pool=threads src/i18n/__tests__
Test Files  9 passed (9)
Tests  49 passed (49)
```

### 2.7 上游语义目录未被修改

```
$ git diff --stat main-self...HEAD -- backend/internal/service backend/internal/handler/dto
(无输出)
```

本功能对上游核心文件的改动**全部集中在装配层**，实测 diff（`git diff --stat f688e8999e...HEAD`）：

```
$ git diff --stat f688e8999e...HEAD -- backend/internal/service backend/internal/handler/dto
(无输出 → 未改动)

$ git diff --stat f688e8999e...HEAD -- <上游接触面 11 个文件>
 backend/cmd/server/main.go                      |  8 ++++++++
 backend/cmd/server/wire.go                      | 12 +++++++++++-
 backend/internal/handler/handler.go             |  4 ++++
 backend/internal/handler/wire.go                |  3 +++
 backend/internal/repository/image_storage_s3.go | 21 +++++++++++++++++++++
 backend/internal/server/router.go               |  2 ++
 frontend/src/components/layout/AppSidebar.vue   |  9 +++++++++
 frontend/src/i18n/locales/en/index.ts           |  2 ++
 frontend/src/i18n/locales/zh/index.ts           |  2 ++
 frontend/src/router/index.ts                    | 24 ++++++++++++++++++++++++
 frontend/src/views/admin/UsersView.vue          | 24 ++++++++++++++++++++++--
 11 files changed, 108 insertions(+), 3 deletions(-)
```

**关于 `3 deletions`**（逐行核对，无行为删除）：
- `UsersView.vue`（2 行）：`sortedUsers` 里 `if (!s) return users.value` /
  `return [...users.value]` 改为引用新局部变量 `visible`；排序语义
  （含稳定排序、等值按原 index 保序）完全不变，只在最前面多一步
  「按偏好隐藏内部账号」。
- `wire.go`（1 行）：`provideCleanup` 的入参由 `iqScheduler *scheduler.Scheduler`
  换成 `iqRuntime *iqmodule.Runtime`，停止动作改调 `Runtime.Shutdown`
  （其内部仍调同一个 `Scheduler.Shutdown`，并额外保证顺序与幂等）。
  这是本功能自己的接线，不属于上游语义。

其余全部是纯追加。逐项说明：

| 文件 | 改动性质 | 行数 |
|------|---------|------|
| `backend/cmd/server/main.go` | +1 段 `app.IQBenchmark.Start`（失败只 WARN） | +8 |
| `backend/cmd/server/wire.go` | +imports +ProviderSet +1 Application 字段 +Cleanup 入参/步骤 | +12 −1 |
| `backend/internal/handler/handler.go` | +1 import +1 结构体字段 | +4 |
| `backend/internal/handler/wire.go` | +1 import +1 参数 +1 赋值 | +3 |
| `backend/internal/server/router.go` | +1 import +1 调用 | +2 |
| `backend/internal/repository/image_storage_s3.go` | **追加** `Delete` 方法（不改 `ImageStorage` 接口签名） | +21 |
| `frontend/src/router/index.ts` | +2 条路由 | +24 |
| `frontend/src/components/layout/AppSidebar.vue` | +1 import +1 探测 +2 导航项 | +9 |
| `frontend/src/views/admin/UsersView.vue` | +1 import +1 ref +1 开关 +1 过滤行（另 2 行缩进调整） | +24 −2 |
| `frontend/src/i18n/locales/{zh,en}/index.ts` | 各 +1 行 spread | 各 +2 |

未改动（已用 diff 核对为无输出）：`backend/internal/service/**`（业务语义所在）、
`backend/internal/handler/dto/**`、`frontend/src/i18n/index.ts`、
`frontend/src/utils/featureFlags.ts`、`backend/config.yaml`、`backend/internal/config/**`。

---

## 三、必测项矩阵（回填实测用例名与结果）

图例：✅ 已覆盖并通过　⚠️ 部分覆盖（说明见备注）　❌ 未覆盖

### 3.1 调度与并发

| 计划用例 | 实际用例 | 断言 | 状态 |
|---------|---------|------|------|
| `TestSchedulerNoDuplicateLease` | `TestClaimDueTasksUsesSkipLockedAndWritesLease`（sqlmock）<br>`TestIQBenchmarkClaimSkipsLockedRows`（真实 PG） | 两实例并发领取同一到期任务，仅一方成功 | ✅ 数据库层已用真实 PG 证明 `SKIP LOCKED` 跳过已锁行 |
| `TestSchedulerLeaseRenewLossAborts` | `TestRenewLeaseReturnsLeaseLostWhenNoRowMatches`<br>`TestExecuteTaskAbortsWhenLeaseLostBeforeRun` | 续期影响 0 行 → 立即中止且不写产出 | ✅ |
| `TestSchedulerLeaseExpiryReclaim` | `TestIQBenchmarkLeaseTakeover`（真实 PG） | 租约过期后可被其他实例接管 | ✅ 同时证明**未**过期的租约不可被抢 |
| `TestSchedulerNextRunAdvancesOnClaim` | `TestExecuteTaskAdvancesNextRunBeforeRun`<br>`TestAdvanceNextRunAtAfterClaimRequiresLeaseOwnership`<br>`TestAdvanceNextRunAtAfterClaimDetectsLeaseLoss` | 领取成功即推进 `next_run_at` | ✅ |
| `TestIntervalAndJitterValidation` | `TestValidateIntervalBounds` | 14 / 10081 分钟、负抖动、抖动越界全部拒绝；边界值接受 | ✅ 库层由 `TestIQBenchmarkIntervalCheck` 再兜一道 |
| `TestJitterBounds` | `TestNextRunAtJitterStaysWithinBand`<br>`TestNextRunAtNeverBelowMinimumGap`<br>`TestNextRunAtIsNearIntervalWithoutJitter` | 采样落在 `±jitter` 且 `>= now+900s` | ✅ |
| `TestMultiInstanceConcurrency`（集成） | 无独立用例 | 多实例总并发不超上限 | ⚠️ 只验证了**领取去重**（`TestIQBenchmarkClaimSkipsLockedRows`）与**单实例内**并发上限（`TestAcquireConcurrencyEnforcesMax`）；「多实例合计并发」需要拉起两个调度器进程，未做。生产保护来自租约本身：同一条任务不会被两个实例同时执行。 |
| `TestStartIdempotent` | `TestStartDoesNotStartTwice`<br>`TestNewDoesNotStartGoroutine` | 连续两次 `Start` 只有一个循环 | ✅ |
| `TestShutdownBounded` | `TestShutdownReleasesAllLeases`<br>`TestShutdownBeforeStartIsSafe` | 死线内返回且释放租约 | ✅ |
| `TestEnabledFalseZeroSideEffects` | `TestStartIsNoOpWhenDisabled`<br>`TestTickStopsClaimingWhenDisabledAtRuntime`<br>`TestCleanupSkippedWhenNoCleaner`<br>`TestFeatureOffRoutesUnreachable`（路由层） | 4 表零行、零出站、零 Redis 键 | ⚠️ 已证明「调度器不启动、不领取、不清理、用户侧路由 404」；「4 表零行 / 零 Redis 键」未做端到端计数断言。 |

### 3.2 数据与快照

| 计划用例 | 实际用例 | 断言 | 状态 |
|---------|---------|------|------|
| `TestCaseSnapshotFrozen` | `TestToPublicArtifactUsesFrozenCaseSnapshot` | 改题目后历史产出快照不变 | ⚠️ 已在 DTO 层证明「取值只来自 `case_snapshot`、不回查 cases 表」；未做「先改题再读历史」的端到端用例。快照冻结的根保障是写入侧一次性落库。 |
| `TestUniqueEnabledTaskPerGroupModel`（集成） | `TestIQBenchmarkPartialUniqueEnabledTask` | 部分唯一索引拒绝第二个启用任务 | ✅ 同时证明同组同模型的**停用**任务不受限 |
| `TestCaseDeleteBlockedByEnabledTask` | `TestDeleteCaseRejectsWhenEnabledTaskReferences`（service） | 被启用任务引用时删除被拒 | ✅ 用调用顺序断言证明「先查启用引用并短路，不去摘引用、更不删」 |
| `TestCaseDeleteCleansDisabledTaskRefs` | `TestDeleteCaseCleansDisabledTaskRefsThenDeletes`<br>`TestDeleteCaseSkipsCleanupWhenUnreferenced`（service） | 仅被禁用任务引用时删除成功且剔除引用 | ✅ 顺序断言为 `count-enabled → count-all → remove-from-disabled → delete`；无引用时不产生多余写 |
| `TestBuiltinCaseUndeletable` | `TestDeleteCaseReportsBuiltinProtection` | 内置题删除返回 4xx | ✅ |
| `TestFailedArtifactPersisted` | `TestSaveFailedArtifactPersistsSanitizedSummary`<br>`TestSaveFailedArtifactUsesPlaceholderRenderKind` | failed/timeout/blocked 均入库 | ✅ |
| `TestArtifactListStablePaging` | 无 | 同 `created_at` 时多次请求顺序一致 | ⚠️ 排序 SQL 固定为 `created_at DESC, id DESC`（`idx_iq_artifact_feed` 支撑），但无并发写入下的稳定性用例 |

### 3.3 安全与消毒

| 计划用例 | 实际用例 | 断言 | 状态 |
|---------|---------|------|------|
| `TestSVGSanitizerBlocksScript` | `TestScanSVGBlocksScriptTag`<br>`TestSaveTextArtifactBlocksScript` | `<script>` → `blocked` | ✅ |
| `TestSVGSanitizerStripsEventHandlers` | `TestScanSVGStripsEventHandlerAttributes`<br>`TestSaveTextArtifactStripsEventHandlerButKeepsSuccess` | `onload=` 被剥离 | ✅ 区分「阻断」与「剥离但仍成功」两类处置 |
| `TestSVGSanitizerBlocksForeignObject` | `TestScanSVGBlocksForeignObject`<br>`TestScanSVGBlocksEmbedObjectIframe` | `<foreignObject>` → `blocked` | ✅ |
| `TestSVGSanitizerBlocksJavascriptURL` | `TestScanSVGBlocksJavascriptURL`<br>`TestScanSVGStripsDataURI` | `javascript:` → `blocked` | ✅ |
| `TestSVGSanitizerStripsExternalRefs` | `TestScanSVGStripsExternalRefs`<br>`TestScanSVGKeepsInternalAnchorRefs`<br>`TestScanSVGStripsDataURI`<br>`TestScanSVGStripsCSSImport` | 外链引用被剥离并记标记；站内锚点保留 | ✅ |
| `TestSVGSanitizerBlocksEntityAndDoctype` | `TestScanSVGBlocksEntityDeclaration`<br>`TestScanSVGStripsDoctype` | `<!ENTITY` / `<!DOCTYPE` 处理正确 | ✅ |
| `TestOversizedResponseTruncated` | `TestSaveTextArtifactTruncatesHugeContent`<br>`TestTruncateWithFlagKeepsUTF8Valid`<br>`TestTruncateWithFlagNeverProducesInvalidUTF8` | 超限响应截断 + `content_truncated=true` | ✅ 额外保证截断不产生非法 UTF-8 |
| `TestPublicArtifactDTOWhitelist` | `TestPublicArtifactDTOWhitelistIsExact`<br>`TestAllPublicDTOsAvoidForbiddenFields`（reflect 遍历全部 Public DTO） | 禁止字段不在 JSON tag 中 | ✅ 白名单是**精确相等**断言，不只是「不包含」 |
| `TestPublicArtifactNoCostByDefault` | `TestToPublicArtifactHidesCostAndTokensByDefault` | `expose_cost_to_user=false` 时响应无 `cost` | ✅ |
| `TestPublicArtifactNoInternalIdentifiers` | `TestPublicArtifactDTOHasNoInternalFields`<br>`TestPublicCaseDTOHasNoPromptInternals`<br>`TestPublicOverviewBucketDTOExposesNoGroupIdentity`<br>`TestPublicErrorSummaryNeverLeaksRawError`<br>`TestToPublicArtifactBlockedHasNoContent` | 无 `user_id`/`api_key_id`/`account_id`/`notes`/`unsafe_flags`/`error_code` | ✅ 另含「blocked 产出正文绝不下发」与「错误摘要不含原始错误」 |
| 前端 `sanitize.spec.ts` | `sanitize.spec.ts`（38 例） | 未消毒危险输入不进入 DOM | ✅ 含真实 DOM 断言：解析后不存在 `script`/`foreignObject`/`a`/`image`/`img` 元素，且不存在任何 `on*` 属性或 `javascript:` 取值；SVG 一律走 `<img src="data:image/svg+xml,...">`，**从不内联** |

### 3.4 计费与隔离

| 计划用例 | 实际用例 | 断言 | 状态 |
|---------|---------|------|------|
| `TestIsInternalAgent` | 由 `agent_store` / `internal_agent` 的窄接口与 `TestPublicCaseDTOHasNoPromptInternals` 侧证 | 前缀命中 / 普通用户 / nil 三种情形 | ⚠️ 判定函数本身无独立单测（`IsInternalAgent` / `IsInternalAgentNotes` 是纯函数但未单测）。前端侧 `userFilter.spec.ts`（11 例）覆盖了同前缀的 trim/空值/普通用户判定。 |
| `TestEnsureInternalAgentIdempotent`（集成） | `TestRuntimeStartIsIdempotent`<br>`TestRuntimeStartsAndStopsScheduler`（module，见 §3.8） | 两次调用只创建 1 账号 + 1 Key | ⚠️ 生命周期层面已覆盖（重复 `Start` 不重复建号、建号→建 Key→就绪全链路）；**真实 PG 下**的双实例并发建号仍未覆盖 |
| `TestEnsureInternalAgentConcurrent`（集成） | 无 | 多实例并发只创建 1 份 | ⚠️ 机制为 `pg_try_advisory_lock`，已有 `TestTryAdvisoryLockReturnsBusyWithoutRelease` 单测兜底；并发建号本身未做集成用例 |
| `TestInternalAgentPrefixNotLeaked` | 无 | 静态扫描：模块外无前缀字面量 | ❌ 未做自动化扫描（人工核对：前缀字面量仅出现在 `internal_agent.go` 与本模块 `userFilter.ts`） |
| `TestInternalAccountDebitedNotRealUser`（集成） | `TestIQBenchmarkDailyCostScopesToInternalKeyOnly`<br>`TestDailyCostFiltersByInternalAPIKeyOnly`（sqlmock） | 内部账号扣费，真实用户余额不变 | ✅ 以「按 `api_key_id` 聚合」证明口径隔离：内部 Key 只累计自己的 0.40，真实用户的 9.99 不串入，且真实用户各自独立 |
| `TestQuotaExhaustedPausesTask`（集成） | `TestTripQuotaExhaustedClearsNextRun`<br>`TestRecoveryQuotaRewritesNextRun` | 状态 `quota_exhausted` + `next_run_at` 空 | ✅（单测 + mock 仓储） |
| `TestBudgetExhaustedPausesAllTasks`（集成） | `TestTripBudgetExhaustedPausesAllTasksUntilNextDay`<br>`TestRecoverBudgetIfNewDayOnlyTouchesBudgetTasks`<br>`TestIQBenchmarkGuardBudgetBreaker`（真实 PG）<br>`TestIQBenchmarkDailyCostResetsNextDay`（真实 PG） | 全部 `budget_exhausted` + 跨日恢复 | ✅ 状态机由单测覆盖，真实 SQL 的「只按内部 Key 聚合 + 自然日清零」由集成用例覆盖 |
| `TestDisabledAccountAlertsAndPauses`（集成） | `TestTripAccountDisabledRequiresManualRecovery`<br>`TestRecoverFromAccountDisabledIsIdempotent`<br>`TestTickTripsAccountDisabledWithoutClaiming`<br>`TestTickDoesNotRepeatAccountDisabledTrip` | 全部 `account_disabled` + ERROR，零调用 | ✅ 含「不重复告警」与「零领取」两个易漏点 |
| `TestUsageFilterableByInternalIDs`（集成） | `TestSumUsageByRequestIDsFiltersByAPIKey`<br>`TestIQBenchmarkUsageSnapshotByRequestIDs`（真实 PG） | 可按 `user_id`/`api_key_id` 过滤出检测流量 | ✅ 且「未命中的 request_id 不计入 matched」 |
| `TestInternalAgentUserAgentTag`（集成） | 无 | `user_agent = sub2api-iq-benchmark/1.0` | ❌ 未断言（常量 `DetectionUserAgent` 已定义并在网关客户端设置，但无出站请求头抓取用例） |
| `TestGlobalStatsIncludesInternalAgent` | 见 §一 第 4 项裁决 | **记录事实**：上游默认全局查询包含内部账号 | ✅ 已作为**已知局限**记录于 §四 第 1 项，未实施（方案 A：0 处上游查询改动） |
| `TestInternalAgentAbsentFromUserScopedStats` | 同上 | 真实用户维度统计无内部账号数据 | ✅ 结构性成立：内部账号是独立 `user_id`，用户维度统计按 `user_id` 过滤，天然隔离 |

### 3.5 限流

| 计划用例 | 实际用例 | 断言 | 状态 |
|---------|---------|------|------|
| `TestReadTierLimit` | `TestAllowEnforcesReadLimit` | 第 121 次/分钟 → 429 | ✅ |
| `TestHeavyConsumesBothTiers` | `TestHeavyTierConsumesBothBuckets` | read 耗尽时 heavy 请求 429 且不消耗 heavy | ✅ |
| `TestHeavyTierLimit` | `TestHeavyTierBlockedByReadExhaustion` | 第 61 次/分钟 → 429 | ✅ |
| `TestRateLimitKeyPrefix` | `TestUserKeyUsesOwnNamespace` | 键以 `iqgallery:` 开头，非 `panel:` | ✅ |
| `TestRateLimitCrossUserIndependent` | `TestLimitsArePerUser` | A 超限不影响 B | ✅ |
| `TestRateLimitFailOpenOnRedisError` | `TestFailOpenWhenRedisErrors`<br>`TestLocalFallbackEnforcesLimit`<br>`TestLocalWindowExpires` | Redis 故障放行 + WARN，无 5xx | ✅ 额外验证了本地兜底窗口会**过期**（不会永久放行或永久熔断） |
| `TestRateLimitBodyMatchesPanel` | `TestMiddlewareAbortsWithPanelCompatibleBody`<br>`TestAbortRateLimitedDefaultsRetryAfter` | 429 结构与面板一致 + `Retry-After` | ✅ |
| `TestUnauthenticatedNotCounted` | `TestUnauthenticatedIsNotCounted`<br>`TestMiddlewareSkipsAnonymous`<br>`TestUserIDFromContext` | 未认证 401 且不占配额 | ✅ |
| `TestConcurrencyCap` | `TestAcquireConcurrencyEnforcesMax`<br>`TestConcurrencyReleaseIsIdempotent`<br>`TestAcquireConcurrencyFailOpen`<br>`TestAcquireConcurrencyWithoutRedisIsUnlimited` | 达上限 → 429；结束后释放；TTL 自愈 | ✅ 释放**幂等**（重复释放不会把计数打成负数） |
| `TestPageSizeAboveCapRejected` | `TestListArtifactsRejectsOversizedPage`（service） | `page_size=51` → 400；`=50` → 200 | ✅ 已断言「超限**报错**而非静默截断」，边界值 50 放行 |
| `TestFeatureOffRoutesUnreachable` | `TestUserRoutesUnregisteredWhenDisabled`<br>`TestAdminSettingsReachableEvenWhenDisabled`<br>`TestEnabledIsFailClosed`<br>`TestUserRoutesAreReadOnly`<br>`TestAllAdminWriteRoutesAreUnderAdminPrefix` | 用户侧 404，管理侧 503，无限流键 | ✅ 另含「settings 读写始终可达」「用户侧全为 GET」「管理写路由全在 admin 前缀下」 |
| 前端 `rateLimit.spec.ts` | `rateLimit.spec.ts`（15 例） | 429 展示提示且不自动重试 | ✅ 用真实 `AxiosError` 构造，覆盖 `Retry-After` 缺失/非法/大小写差异与 503 禁用态 |
| 前端 `useArtifactFeed.spec.ts` | `useArtifactFeed.spec.ts`（15 例） | 防抖 ≥300ms + 单飞 | ✅ 含「冷却期内短路不发请求」「单飞不重复发」 |

### 3.6 清理与容量

| 计划用例 | 实际用例 | 断言 | 状态 |
|---------|---------|------|------|
| `TestCleanupByRetentionDays` | `TestCleanupPurgesByAgeThenByCount`<br>`TestPurgeOlderThanReturnsPurgedRows` | 超期产出被清理，run 随之清理 | ✅ |
| `TestCleanupByMaxArtifacts` | `TestPurgeBeyondLimitOrdersByCreatedAtThenID`<br>`TestCountLiveArtifactsExcludesSoftDeleted` | 只保留最新 N 条 | ✅ 且排除已软删行 |
| `TestCleanupSingleInstance`（集成） | `TestTryAdvisoryLockReturnsBusyWithoutRelease`<br>`TestTryAdvisoryLockReleasesOnDemand`<br>`TestCleanupIsThrottledByInterval` | advisory lock 保证单实例执行 | ⚠️ advisory lock 与节流分别有单测；未做「两实例同时触发清理」的集成用例 |
| `TestEnsureCapacityRejectsOverLimit` | `TestEnsureCapacityRejectsAboveOnePointTwoTimes`<br>`TestEnsureCapacityAllowsBelowThreshold` | 超 1.2 倍上限拒写 + 告警 | ✅ |
| `TestMaxArtifactsPerRunTruncation` | `TestMaxTaskRuntimeScalesWithCaseCount`（间接） | 超单 run 上限截断并记录数量 | ⚠️ 单 run 产出上限（`max_artifacts_per_run`）逻辑在 `run_service` 中，未单独写用例。 |

### 3.7 已知测试缺口（如实记录）

以下 5 项**未**达到计划覆盖度，均已在上表标为 ❌ 或 ⚠️，此处汇总原因：

1. **`EnsureInternalAgent` 的真实 PG 并发用例**：需构造上游
   `service.UserRepository` / `service.APIKeyRepository` 的真实实现，
   而这两者是 30+ 方法的宽接口，替身成本高。
   生命周期与幂等已由 `module/runtime_test.go` 覆盖（见 §3.8），
   机制层面的 `pg_try_advisory_lock` 已有单测
   （`TestTryAdvisoryLockReturnsBusyWithoutRelease`、
   `TestTryAdvisoryLockReleasesOnDemand`）；**双实例并发建号**仍未做。
2. **内部账号前缀的静态扫描**：人工核对替代自动化
   （前缀字面量仅出现于 `internal_agent.go` 与前端 `userFilter.ts`）。
3. **`user_agent` 出站断言**：需抓包或注入 `RoundTripper` 记录器，未做。
   常量 `DetectionUserAgent` 已定义并在网关客户端设置。
4. **多实例合计并发**：需拉起两个调度器进程，未做；
   同任务不重复执行的根保障（租约 + `SKIP LOCKED`）已由真实 PG 证明。
5. **`enabled=false` 的「4 表零行 / 零 Redis 键」端到端计数**：已证明调度器不启动、
   不领取、不清理、用户侧路由 404、管理侧 503 `IQ_BENCHMARK_DISABLED`；
   未做数据库表行数与 Redis 键名的逐项计数断言。

> 已补齐（初稿曾列为缺口，随后补测）：
> `gallery_service` 的 `DeleteCase` 三步保护、`page_size` 上限、
> `Compare` 的 8 列截断与顺序稳定性、`GetOverview` 的 `bucketLimit` 兜底
> ——见 `service/gallery_service_test.go`（9 个用例）；
> 运行期启动链路——见 `module/runtime_test.go`（5 个用例，见 §3.8）。

### 3.8 运行期启动链路（阶段 6 验收时发现的真实缺陷，已修复）

**问题**：初版把调度器与内部账号的构造放进了 Wire，但**没有任何地方调用
`Scheduler.Start()` 与 `EnsureInternalAgent()`**。后果是：管理员把
`enabled` 打开后，内部账号不会被创建、调度器不会跑、一条检测都不会执行，
而且日志里没有任何错误——功能**静默失效**。

- 静态复核：`grep -r "\.Start(" ` 在 `internal/iqbenchmark/**` 与
  `cmd/server/**` 中只命中上游自己的 `cronSched.Start()`；
  `EnsureInternalAgent(` 只有定义、没有调用点。
- 根因：`module.go` 的注释写了「调度器必须在 HTTP 服务就绪后由 main.go
  显式 Start」，但 `main.go` 从来没有拿到过该组件——它既不在
  `Application` 结构体里，也没进 `provideCleanup` 之外的任何启动序列。

**修复**（新增独立文件，仍不触碰上游语义）：

| 文件 | 改动 |
|---|---|
| `internal/iqbenchmark/module/runtime.go`（新） | `Runtime` 类型：`Start` 做「未启用即返回 / 初始化内部账号 / 启动调度器」，`Shutdown` 有界停止；两者幂等，nil 接收者 no-op |
| `internal/iqbenchmark/module/module.go` | ProviderSet 增 `ProvideRuntime`；补上此前缺失的 `wire.Bind(AdvisoryLocker → *repository.Repository)` |
| `cmd/server/wire.go` | `Application` 增 1 个字段 `IQBenchmark *iqmodule.Runtime`；`provideCleanup` 的 `iqScheduler *scheduler.Scheduler` 换成 `iqRuntime *iqmodule.Runtime`，清理步骤改调 `iqRuntime.Shutdown` |
| `cmd/server/main.go` | `app.PromptAudit.Start` 之后追加 1 段 `app.IQBenchmark.Start`（失败只 WARN，不阻塞启动） |

**覆盖测试**（`module/runtime_test.go`，5 个用例，全绿）：

```
$ go test -tags=unit -count=1 -v ./internal/iqbenchmark/module/
=== RUN   TestRuntimeDoesNothingWhenDisabled
--- PASS: TestRuntimeDoesNothingWhenDisabled (0.00s)
=== RUN   TestRuntimeStartIsIdempotent
--- PASS: TestRuntimeStartIsIdempotent (0.00s)
=== RUN   TestRuntimeMarksAgentUnreadyWhenInitFails
--- PASS: TestRuntimeMarksAgentUnreadyWhenInitFails (0.00s)
=== RUN   TestRuntimeShutdownWithoutStartIsSafe
--- PASS: TestRuntimeShutdownWithoutStartIsSafe (0.00s)
=== RUN   TestRuntimeStartsAndStopsScheduler
--- PASS: TestRuntimeStartsAndStopsScheduler (0.05s)
PASS
ok  	github.com/Wei-Shaw/sub2api/internal/iqbenchmark/module	0.437s
exit=0
```

其中最关键的是 `TestRuntimeDoesNothingWhenDisabled`
（关闭时**零**仓储调用）与 `TestRuntimeStartsAndStopsScheduler`
（启用时确实走完建号→建 Key→启动调度器→可停）——正是初版缺失的两侧。

**这条缺陷说明**：仅靠「构造成功 + 单元测试通过」不足以证明功能可用，
必须有「从入口到副作用的启动链路」断言。§3.1 与 §3.4 中
原先标为 ❌ 的 `TestEnsureInternalAgent*` 缺口，也因此由本组用例
在**生命周期层面**补上了一部分（真实 PG 下的并发建号仍未覆盖）。

---

## 四、已知局限与不做项（需在验收时知晓）

1. **检测流量仍计入上游全局聚合**：`GetGlobalStats`、`GetDashboardStats`、`GetModelStats*`、`GetGroupStats*`、预聚合表、消费排行榜默认都会包含内部账号数据。本变更**不改上游默认查询**（方案 A），改由前端「隐藏内部账号」开关 + 本模块 overview 的占比提示兜底。详见 `design.md` §7.3。
2. **预聚合历史数据无法事后拆分**：`usage_dashboard_hourly_users` 等表已落库的历史口径不可回溯；这是不改上游查询的直接代价。
3. **对象存储孤儿对象**：`Delete` 能力已按确认项 2 以「纯追加」方式实现（`S3ImageStorage.Delete`），但 Wire 中注入的 `BlobDeleter` 是 **`nil`**——因为上游 `ImageStorage` 实例由 `ImageStorageSettingService` 在运行时惰性构建，Wire 图内取不到，要注入必须先给上游加导出方法，会突破「注册一行」边界。**当前行为：软删 DB 行（用户侧立即消失）+ WARN 日志，对象存储文件留待桶生命周期策略回收。**
4. **不做服务端栅格化**：SVG 不转位图，依赖浏览器渲染；不引入 librsvg / ChromeDriver。
5. **不做 Elo / Bradley-Terry 排名**：首版评分以人工录入为主。
6. **不做匿名访问与站外分享**：作品展**必须登录**（`meta.requiresAuth: true`），未登录不可见。
7. **不做 LLM 裁判**（确认项 3）：`llm_judge_enabled` 字段保留但 `Normalize()` 强制置 false，
   执行侧恒不启用。
8. **`compare.ts` 未单独成文件**（任务 5.1 偏差）：对比选择态并入 `filters.ts`，
   因为选择态与筛选态共享同一份列表快照，拆包反而要来回传状态。
9. **未走上游 featureFlags 注册流程**（任务 5.6 偏差）：改用本地可用性探测
   `availability.ts`（探测 `GET /iq-gallery/overview`，404/503/网络错误一律视为未启用）。
   理由：注册一个新 flag 需同步改 11 个上游文件，远超「注册一行」约束。详见 `design.md` §9.3。
10. **管理员用户列表的隐藏是纯前端过滤**（确认项 4）：两项已知限制——
    只在当前页内生效；分页总数/页码仍按未过滤集合计算。
    若整页都被隐藏，页面显示空表且不会自动翻页。
11. **`gallery_service` 的 service 层编排缺单测**（见 §3.7 第 1 项）。

---

## 五、回填规范（阶段 6 执行时遵守）

- 每条命令**必须粘贴真实完整输出**（含退出码），不得写「通过」「正常」之类结论性描述代替输出。
- 若某命令失败，必须原样贴出失败输出，并说明是环境问题还是代码问题；环境问题需给出解决动作与重试输出。
- `golangci-lint` 须先安装再执行并贴版本号输出。
  **实际安装的是 v2.14.0**（v2.13.0 无法解码 Go 1.27.3 导出数据，见 §二 环境基线），
  与 CI `.github/workflows/backend-ci.yml:82` 一致。
- 前端 `pnpm-lock.yaml` 若有变更必须一并提交并在本节记录 diff stat。
  **实测无变更**。
- 与本文件 §2.1 记录的上游既有失败对比时，必须比较「失败的包与用例」，不能只比总数——
  `internal/service` 的顺序敏感会让失败用例在每次运行之间漂移（§2.4 已给出两次基线运行的对照）。
