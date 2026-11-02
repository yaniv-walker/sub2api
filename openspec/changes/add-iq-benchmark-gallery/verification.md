# 验证记录：模型智商检测作品展

- **Change ID**：`add-iq-benchmark-gallery`
- **分支**：`feature/iq-benchmark-gallery`
- **当前状态**：阶段 0 完成，**等待用户确认**；阶段 1~6 尚未开始，本文件中的命令输出将在实施后逐条回填。

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
| 3 | `go build ./...` | 无错误 | ⬜ 待执行（阶段 6） |
| 4 | `golangci-lint run ./...` | 0 issue（v2.13） | ⬜ 待执行（需先安装 v2.13） |
| 5 | `go test -tags=unit ./...` | 全绿 | ⬜ 待执行 |
| 6 | `go test -tags=integration ./...` | 全绿 | ⬜ 待执行 |
| 7 | `pnpm install --frozen-lockfile` | 无变更 | ⬜ 待执行 |
| 8 | 前端 type-check | 无错误 | ⬜ 待执行 |
| 9 | `pnpm vitest run` | 全绿 | ⬜ 待执行 |
| 10 | `git diff --stat main-self...HEAD -- backend/internal/service backend/internal/handler/dto` | 空（未改上游语义目录） | ⬜ 待执行 |

**环境基线（阶段 0 已核证）**：
- `go version go1.27.1 windows/amd64`；`go.mod` 声明 `go 1.27.3`
- `pnpm 11.21.0`；`node v24.19.0`
- `golangci-lint` **当前未安装**（`无法将"golangci-lint"项识别为...`）→ 阶段 6 前须安装 v2.13

### 2.1 上游既有失败基线（本次已核证，非本功能引入）

阶段 4 收尾时执行 `go test -tags=unit ./...`，出现若干失败包。为了确认这些
失败**是否由本功能引入**，在干净的 `f688e8999e` 上建立独立 worktree
（`git worktree add <tmp> f688e8999e --detach`）并运行同一批包，结论如下：

| 包 | HEAD 基线结果 | 本分支结果 | 结论 |
|----|--------------|-----------|------|
| `internal/repository` | FAIL（3 个 `TestPgDumper*`） | FAIL（同样 3 个） | 基线既有，与本次改动无关 |
| `internal/handler` | FAIL（`TestResolvePageImagePath`） | FAIL（同一个） | 基线既有 |
| `internal/server` | FAIL（`TestAPIContracts`，缺 `tutorial_enabled`） | FAIL（同一个） | 基线既有 |
| `internal/service` | FAIL（`TestRecordCyberPolicyEvent_*`） | FAIL（同类） | 基线既有 |
| `plugins/upstream-monitor/repository` | 不涉及 | **FAIL → 已修复** | **本次引入，见下** |

> 说明：并行执行两个测试进程时，`internal/repository` 会额外出现若干
> sqlmock 相关失败；单进程串行执行（`-count=1`）后与基线完全一致。
> 阶段 6 的正式回填必须**串行**执行，避免这个假象。

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

---

## 三、必测项矩阵（实施后逐条回填结果）

### 3.1 调度与并发

| 用例 | 断言 | 状态 |
|------|------|------|
| `TestSchedulerNoDuplicateLease` | 两实例并发领取同一到期任务，仅一方成功 | ⬜ |
| `TestSchedulerLeaseRenewLossAborts` | 续期影响 0 行 → 立即中止且不写产出 | ⬜ |
| `TestSchedulerLeaseExpiryReclaim` | 租约过期后可被其他实例接管 | ⬜ |
| `TestSchedulerNextRunAdvancesOnClaim` | 领取成功即推进 `next_run_at` | ⬜ |
| `TestIntervalAndJitterValidation` | 14/10081 分钟、负抖动、抖动越界全部 400；边界值接受 | ⬜ |
| `TestJitterBounds` | 采样落在 `±jitter` 且 `>= now+900s` | ⬜ |
| `TestMultiInstanceConcurrency`（集成） | 多实例总并发不超上限 | ⬜ |
| `TestStartIdempotent` | 连续两次 `Start` 只有一个循环 | ⬜ |
| `TestShutdownBounded` | 死线内返回且释放租约 | ⬜ |
| `TestEnabledFalseZeroSideEffects` | 4 表零行、零出站、零 Redis 键 | ⬜ |

### 3.2 数据与快照

| 用例 | 断言 | 状态 |
|------|------|------|
| `TestCaseSnapshotFrozen` | 改题目后历史产出快照不变 | ⬜ |
| `TestUniqueEnabledTaskPerGroupModel`（集成） | 部分唯一索引拒绝第二个启用任务，返回 409 | ⬜ |
| `TestCaseDeleteBlockedByEnabledTask` | 被启用任务引用时删除被拒 | ⬜ |
| `TestCaseDeleteCleansDisabledTaskRefs` | 仅被禁用任务引用时删除成功且剔除引用 | ⬜ |
| `TestBuiltinCaseUndeletable` | 内置题删除返回 4xx | ⬜ |
| `TestFailedArtifactPersisted` | failed/timeout/blocked 均入库 | ⬜ |
| `TestArtifactListStablePaging` | 同 `created_at` 时多次请求顺序一致 | ⬜ |

### 3.3 安全与消毒

| 用例 | 断言 | 状态 |
|------|------|------|
| `TestSVGSanitizerBlocksScript` | `<script>` → `blocked` | ⬜ |
| `TestSVGSanitizerStripsEventHandlers` | `onload=` 被剥离 | ⬜ |
| `TestSVGSanitizerBlocksForeignObject` | `<foreignObject>` → `blocked` | ⬜ |
| `TestSVGSanitizerBlocksJavascriptURL` | `javascript:` → `blocked` | ⬜ |
| `TestSVGSanitizerStripsExternalRefs` | 外链引用被剥离并记标记 | ⬜ |
| `TestSVGSanitizerBlocksEntityAndDoctype` | `<!ENTITY` / `<!DOCTYPE` 处理正确 | ⬜ |
| `TestOversizedResponseTruncated` | 超限响应截断 + `content_truncated=true` | ⬜ |
| `TestPublicArtifactDTOWhitelist` | reflect 断言禁止字段不在 JSON tag 中 | ⬜ |
| `TestPublicArtifactNoCostByDefault` | `expose_cost_to_user=false` 时响应无 `cost` | ⬜ |
| `TestPublicArtifactNoInternalIdentifiers` | 无 `user_id`/`api_key_id`/`account_id`/`notes`/`unsafe_flags`/`error_code` | ⬜ |
| 前端 `sanitize.spec.ts` | 未消毒危险输入不进入 DOM | ⬜ |

### 3.4 计费与隔离

| 用例 | 断言 | 状态 |
|------|------|------|
| `TestIsInternalAgent` | 前缀命中 / 普通用户 / nil 三种情形 | ⬜ |
| `TestEnsureInternalAgentIdempotent`（集成） | 两次调用只创建 1 账号 + 1 Key | ⬜ |
| `TestEnsureInternalAgentConcurrent`（集成） | 多实例并发只创建 1 份 | ⬜ |
| `TestInternalAgentPrefixNotLeaked` | 静态扫描：模块外无前缀字面量 | ⬜ |
| `TestInternalAccountDebitedNotRealUser`（集成） | 内部账号扣费，真实用户余额不变 | ⬜ |
| `TestQuotaExhaustedPausesTask`（集成） | 状态 `quota_exhausted` + `next_run_at` 空 + WARN | ⬜ |
| `TestBudgetExhaustedPausesAllTasks`（集成） | 全部 `budget_exhausted` + 跨日恢复 | ⬜ |
| `TestDisabledAccountAlertsAndPauses`（集成） | 全部 `account_disabled` + ERROR，零调用 | ⬜ |
| `TestUsageFilterableByInternalIDs`（集成） | 可按 `user_id`/`api_key_id` 过滤出检测流量 | ⬜ |
| `TestInternalAgentUserAgentTag`（集成） | `user_agent = sub2api-iq-benchmark/1.0` | ⬜ |
| `TestGlobalStatsIncludesInternalAgent` | **记录事实**：上游默认全局查询确实包含内部账号（防误判已排除） | ⬜ |
| `TestInternalAgentAbsentFromUserScopedStats` | 真实用户维度统计无内部账号数据 | ⬜ |

### 3.5 限流

| 用例 | 断言 | 状态 |
|------|------|------|
| `TestReadTierLimit` | 第 121 次/分钟 → 429 | ⬜ |
| `TestHeavyConsumesBothTiers` | read 耗尽时 heavy 请求 429 且不消耗 heavy | ⬜ |
| `TestHeavyTierLimit` | 第 61 次/分钟 → 429 | ⬜ |
| `TestRateLimitKeyPrefix` | 键以 `iqgallery:` 开头，非 `panel:` | ⬜ |
| `TestRateLimitCrossUserIndependent` | A 超限不影响 B | ⬜ |
| `TestRateLimitFailOpenOnRedisError` | Redis 故障放行 + WARN，无 5xx | ⬜ |
| `TestRateLimitBodyMatchesPanel` | 429 结构与面板一致 + `Retry-After` | ⬜ |
| `TestUnauthenticatedNotCounted` | 未认证 401 且不占配额 | ⬜ |
| `TestConcurrencyCap` | 达上限 → 429；结束后释放；TTL 自愈 | ⬜ |
| `TestPageSizeAboveCapRejected` | `page_size=51` → 400；`=50` → 200 | ⬜ |
| `TestFeatureOffRoutesUnreachable` | 用户侧 404，管理侧 503，无限流键 | ⬜ |
| 前端 `rateLimit.spec.ts` | 429 展示提示且不自动重试 | ⬜ |
| 前端 `useArtifactFeed.spec.ts` | 防抖 ≥300ms + 单飞 | ⬜ |

### 3.6 清理与容量

| 用例 | 断言 | 状态 |
|------|------|------|
| `TestCleanupByRetentionDays` | 超期产出被清理，run 随之清理 | ⬜ |
| `TestCleanupByMaxArtifacts` | 只保留最新 N 条 | ⬜ |
| `TestCleanupSingleInstance`（集成） | advisory lock 保证单实例执行 | ⬜ |
| `TestEnsureCapacityRejectsOverLimit` | 超 1.2 倍上限拒写 + 告警 | ⬜ |
| `TestMaxArtifactsPerRunTruncation` | 超单 run 上限截断并记录数量 | ⬜ |

---

## 四、已知局限与不做项（需在验收时知晓）

1. **检测流量仍计入上游全局聚合**：`GetGlobalStats`、`GetDashboardStats`、`GetModelStats*`、`GetGroupStats*`、预聚合表、消费排行榜、管理员用户列表默认都会包含内部账号数据。本变更**不改上游默认查询**（方案 A），改由前端「排除内部检测账号」开关 + 本模块 overview 的占比提示兜底。详见 `design.md` §7.3。
2. **预聚合历史数据无法事后拆分**：`usage_dashboard_hourly_users` 等表已落库的历史口径不可回溯；这是不改上游查询的直接代价。
3. **对象存储孤儿对象**：若 `ImageStorage` 未提供 `Delete`（取决于确认项 2），清理只删 DB 行，对象存储中的文件需由桶生命周期策略回收。
4. **不做服务端栅格化**：SVG 不转位图，依赖浏览器渲染；不引入 librsvg / ChromeDriver。
5. **不做 Elo / Bradley-Terry 排名**：首版评分以人工录入为主。
6. **不做匿名访问与站外分享**。

---

## 五、回填规范（阶段 6 执行时遵守）

- 每条命令**必须粘贴真实完整输出**（含退出码），不得写「通过」「正常」之类结论性描述代替输出。
- 若某命令失败，必须原样贴出失败输出，并说明是环境问题还是代码问题；环境问题需给出解决动作与重试输出。
- `golangci-lint` 未安装时不得跳过，须先安装 v2.13 再执行并贴版本号输出。
- 前端 `pnpm-lock.yaml` 若有变更必须一并提交并在本节记录 diff stat。
