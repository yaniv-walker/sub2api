# 任务清单：模型智商检测作品展

- **Change ID**：`add-iq-benchmark-gallery`
- **分支**：`feature/iq-benchmark-gallery`
- **说明**：阶段 0 已获用户确认（4 项裁决结论见 `verification.md` §一），阶段 1 已完成，当前推进阶段 2–3。

---

## 阶段 0：开工作业面（已完成并获确认）

- [x] 0.1 确认 `main-self` 工作区干净（`git status` 无未提交改动）
- [x] 0.2 从 `main-self` 创建并切换到 `feature/iq-benchmark-gallery`
- [x] 0.3 核证上游关键事实（路由/DI/限流/图片存储/Ent/migration/统计入口/i18n/侧栏）
- [x] 0.4 独立核证开源候选的 License 与内容（GitHub API + raw 文件）
- [x] 0.5 产出调研记录 `openspec/changes/add-iq-benchmark-gallery/research.md`（注：`docs/*` 被 `.gitignore` 忽略，故随 change 目录交付）
- [x] 0.6 产出本 openspec change（proposal / design / specs×5 / tasks / verification）
- [x] 0.7 回答开工前 5 个问题（见 `design.md` §9）

### ✅ 阶段 0 已确认，进入阶段 1

---

## 阶段 1：数据层（已完成）

- [x] 1.1 编写 migration `249_iq_benchmark_gallery.sql`：4 表 + 索引 + CHECK + 部分唯一索引 `uq_iq_task_group_model_enabled`
- [x] 1.2 新增 Ent schema：`iq_benchmark_case.go`、`iq_benchmark_task.go`、`iq_benchmark_run.go`、`iq_benchmark_artifact.go`
- [x] 1.3 执行 `go generate ./ent` 并检查生成物无既有 schema 变更（+7848/−16，16 处为类型列表重排，`go build ./...` 通过）
- [x] 1.4 实现 `repository/repository.go`（Case/Task/Run/Artifact CRUD）
- [x] 1.5 实现多维筛选 + 稳定排序（`ListArtifacts` / `GetOverviewBuckets`，`(created_at DESC, id DESC)`）
- [x] 1.6 实现 `repository/lease.go`（`FOR UPDATE SKIP LOCKED` 领取 + 续期 + 释放 + `pg_try_advisory_lock`）
- [x] 1.7 单测：`repository_test.go`（sqlmock）+ `migrations/iq_benchmark_gallery_migration_test.go`（迁移守卫）
- [x] 1.8 集成测试：部分唯一索引冲突、`SKIP LOCKED` 不重复领取、租约过期可接管（需真实 PG，随阶段 6 一起跑）
      实测 8 个用例：迁移可重复执行、部分唯一索引、`SKIP LOCKED` 跳过已锁行、过期租约可接管/有效租约不可接管、
      Ent schema 索引名与迁移一致、status 与 interval_minutes 的 CHECK 生效。


## 阶段 2：内部账号与计费链路（已完成）

- [x] 2.1 `internal_agent.go`：`IsInternalAgent` + `EnsureInternalAgent`（advisory lock 幂等 + 窄接口解耦）
- [x] 2.2 `settings.go`：`Settings` + `IQGalleryRateLimitSettings` + 60s 缓存 + `Normalize()` 硬上限夹紧
- [x] 2.3 `service/guard.go`：三类熔断（配额 / 预算 / 账号禁用）
- [x] 2.4 统计过滤能力：**0 处上游查询改动**（结论见 `verification.md` 第一节；3 项可选增强仅记录未实施）
- [x] 2.5 单测：判定函数（含 nil）、间隔/抖动校验、三类熔断矩阵、预算查询失败必须报错
- [x] 2.6 集成测试：扣内部账号余额、真实用户余额不变、配额耗尽熔断、超预算熔断、账号禁用熔断（**需真实 PostgreSQL**）
      分层说明：三类熔断的状态机与「暂停全部任务 / 次日恢复 / 仅人工恢复」语义由 `guard_test.go`
      的 15 个单测（mock 仓储）覆盖；本项集成测试补的是**真实 SQL 语义**——`DailyCost` 只按
      `api_key_id` 聚合（真实用户消耗绝不串入内部预算）、按自然日滚动清零、超预算后确实把任务写成
      `budget_exhausted` 并清空 `next_run_at`、`request_id` 回采只统计本次 run 且未命中数如实上报。
      共 5 个用例。
- [x] 2.7 交付 `THIRD_PARTY_NOTICES.md`（pelican-bench MIT、llm-case-benchmark MIT）

实现补记（阶段 2 收尾时完成，原任务清单未列）：
- `agent_store.go`：`AgentUserStore` / `AgentKeyStore` 用上游 `service.UserRepository` /
  `service.APIKeyRepository` 实现 `InternalAgentUserStore` / `InternalAgentKeyStore`；
  因上游无 notes 前缀查询，改为分页扫描（200/页，上限 50 页）。
- `agent_locker.go`：`AgentLocker` 把 `repository.TryAdvisoryLock` 适配为 `LockAcquirer`，
  锁 ID 固定为 `InternalAgentInitLockID`，避免调用方误传其他锁 ID 导致互斥失效。

## 阶段 3：调度与执行引擎（已完成）

- [x] 3.1 `scheduler/scheduler.go`：`Start`/`Shutdown`，构造函数不起 goroutine，`enabled=false` 直接返回
- [x] 3.2 租约续期循环 + 丢失即中止
- [x] 3.3 `service/invoke.go`：网关自调（chat / images）、超时、响应体上限、错误码分类
- [x] 3.4 `sanitize/svg.go`：9 类危险构造 Go 正则扫描 + 剥离/阻断分级
- [x] 3.5 `sanitize/classify.go`：输出形态判定（完整/可恢复 SVG、HTML、Markdown、text）
- [x] 3.6 `service/run_service.go`：run 编排、题目串行、任务并发上限、成本回采
- [x] 3.7 `service/artifact_service.go`：文本落库 + 失败入库 + 图片存储降级（`BlobDeleter` 为窄接口）
- [x] 3.8 清理任务：`retention_days` / `max_artifacts` / `EnsureCapacity` + 调度器内节流触发
- [x] 3.9 单测：不重复领取、间隔抖动校验、快照冻结、失败入库、XSS 消毒、超大响应截断、`enabled=false` 零副作用、清理
- [x] 3.10 集成测试：多实例并发、租约接管、重跑失败项（**需真实 PostgreSQL**）
      实测覆盖：`FOR UPDATE SKIP LOCKED` 在两事务并发下不重复领取（多实例并发）、
      过期租约可被另一实例接管而有效租约不可（租约接管）。
      「重跑失败项」在 `run-now` 语义下等价于再次领取同一任务，由上述领取/接管用例与
      调度器单测共同覆盖，未单独建用例。

实现偏差（已在 `design.md` 记录）：`BlobDeleter` 在 Wire 中注入 `nil`。
原因：上游 `ImageStorage` 实例由 `ImageStorageSettingService` 运行时惰性构建，
Wire 图内取不到；要注入必须先给上游加导出方法，会突破「注册一行」边界。
当前行为：软删数据库行（用户侧立即消失）+ WARN 日志，对象存储文件留待运维清理。


## 阶段 4：API 与限流（已完成）

- [x] 4.1 `handler/dto.go`：管理 DTO 与 `PublicArtifactDTO` 严格分离
- [x] 4.2 `handler/admin_handler.go`：cases / tasks / runs / artifacts / overview / settings + 审计动作
- [x] 4.3 `handler/user_handler.go`：5 个只读接口（overview / cases / artifacts / artifacts/:id / compare）+ `page_size` 上限校验
- [x] 4.4 `ratelimit/rate_limiter.go`：两档叠加 + 并发双保险 + fail-open + 复刻 panel 429 响应
- [x] 4.5 `internal/server/routes/iq_benchmark.go`：路由注册（`enabled=false` 时用户侧不注册、管理侧仅 settings 可用）
- [x] 4.6 Wire 接线：`handler.go` 字段、`handler/wire.go` 构造器、`cmd/server/wire.go` ProviderSet + Cleanup、`server/router.go` 一行
- [x] 4.7 单测：公开白名单 reflect 断言（4 个 Public DTO 全跑）、`page_size>50` → 400、限流矩阵（两档叠加/跨用户独立/fail-open/未登录不计数）
- [x] 4.8 路由可达性测试：`enabled=false` 用户侧 404、settings 始终可达、用户侧全为 GET、写路由全在 admin 前缀下

## 阶段 5：前端

- [x] 5.1 `features/iq-benchmark/` 基础：`api.ts`、`types.ts`、`sanitize.ts`、`rateLimit.ts`、`filters.ts`、`compare.ts`、`useArtifactFeed.ts`
      实现偏差：未单独建 `compare.ts` —— 对比选择态（`toggleCompareSelection`、`MAX_COMPARE_ITEMS`、`isSelected`）并入 `filters.ts`。
      理由：选择态与筛选态共享同一份列表快照，拆包后要额外传入/回传状态，反而增加耦合。已在 `design.md` 记录。
- [x] 5.2 共享组件：`ArtifactCard`、`ArtifactGrid`、`ArtifactDetail`、`ComparePanel`、`FilterBar`（`variant` 区分只读/可管理）
- [x] 5.3 管理组件：`CaseForm`、`TaskForm`、`RunTable`、`SettingsPanel`
- [x] 5.4 `views/user/IqGalleryView.vue`（双页签、`requiresAuth`）
- [x] 5.5 `views/admin/IqBenchmarkView.vue`（四子页签、`requiresAdmin`）
- [x] 5.6 路由注册 2 条 + 侧栏 2 条 + feature flag（`opt-in`）+ `enabled=false` 隐藏
      实现偏差：**未**走上游 `featureFlags.ts` 的注册流程，改用本地可用性探测 `availability.ts`
      （探测 `GET /iq-gallery/overview`，404/503/网络错误一律视为未启用）。
      理由：在上游注册一个新 flag 需要同步改 11 个文件，远超「注册一行」的约束。已在 `design.md` §9.3 记录。
- [x] 5.7 中英双语 i18n：`locales/{zh,en}/iqBenchmark.ts`（**不做 `ja`**：`frontend/src/i18n/index.ts` 完全不动）
- [x] 5.8 单测：`filters`、`compare`、`sanitize`、`rateLimit`、`useArtifactFeed`（防抖 ≥300ms、单飞）、组件渲染（只读不渲染管理按钮）
      实测 7 个 spec 文件、165 个用例全绿；只读/内置变体断言的是「完全不渲染按钮」而非「按钮 disabled」。

## 阶段 6：验证与灰度

- [x] 6.1 `go build ./...`
- [x] 6.2 `golangci-lint run ./...`
      注：CI 使用 v2.14.0（v2.13.0 的 x/tools 无法解码 Go 1.27.3 的导出数据，直接 typecheck 失败）。
      本机安装 v2.14.0 后：新增包 0 issue；全仓库 42 issue 与基线 f688e8999e 完全一致（逐类别数目相同）。
- [x] 6.3 `go test -tags=unit ./...`
- [x] 6.4 `go test -tags=integration ./...`
      注：本项目集成测试一律 opt-in（需 DSN 环境变量），无 DSN 时 skip。本功能新增 13 个用例，
      以 `IQ_BENCHMARK_TEST_DSN` 指向一次性 PostgreSQL（独立 `iq_test` 库 + 每次随机 schema）全部通过。
- [x] 6.5 `pnpm install` + 提交 `pnpm-lock.yaml`（若变更）—— 锁文件无变更
- [x] 6.6 前端 type-check（`npx vue-tsc -b`）
- [x] 6.7 `pnpm vitest run`（实际用 `npx vitest run --pool=threads`，见 `verification.md`）
- [x] 6.8 更新 `verification.md`：贴全部真实输出
- [ ] 6.9 灰度：迁移上线（关闭态）→ 管理员开启 + 配题/任务 → `run-now` 验证 → 开启用户侧
      **需在真实部署环境执行**，代码仓库内无法完成，故保持未勾选。

## 阶段 7：A —— 新建任务的分组/模型级联下拉（已完成）

需求依据：`docs/iq-benchmark-target-dropdown.md`。目标是把两个自由输入框
（`group_id` 数字框、`model` 文本框）换成受约束的级联下拉，杜绝「填了跑不出来」。

- [x] 7.1 `service/targets.go`：`TargetsService` 复刻上游 `GET /v1/models` 的四层口径
      （`modelListingSource` / 平台默认集 / `mergeModelIDs` / 白名单过滤），
      保真 `nil` 与空切片的不同语义（OpenAI「非 nil 即用」不回退）。
- [x] 7.2 `GET /admin/iq-benchmark/targets`，支持 `?refresh=1` 精确失效上游 `modelsListCache`；
      分组/模型上限 200/500，超限回传 `truncated`。
- [x] 7.3 三个错误哨兵 `ErrTargetGroupUnavailable` / `ErrTargetModelUnavailable` /
      `ErrTargetCapabilityMismatch`，`writeError` 映射为 **400**（用户可改的输入错误，非 404）。
- [x] 7.4 `validateTask` 之后追加 `validateTarget`：校验分组 active、模型在可用集合内、
      题目能力与分组能力匹配。绕过前端直接调 API 同样被拦。
- [x] 7.5 新状态 `invalid_target`：迁移 `250_iq_benchmark_invalid_target.sql` 重写 status CHECK。
- [x] 7.6 `Guard.TripInvalidTarget` 只熔断**单个**任务（目标失效是局部配置问题，
      不应像 `account_disabled` 那样让所有任务停摆）。
- [x] 7.7 调度器在发起模型调用前做目标前置检查；只有明确的目标失效哨兵才熔断，
      查询类错误按暂时性故障放行（避免一次 DB 抖动把任务永久挂起）。
- [x] 7.8 顺带修掉既有静默失败缺陷：`GatewayClient` 的 `APIKey` 在 Wire 装配期写死为空串，
      而内部账号 Key 是运行期创建的 ⇒ 所有检测请求都带空 Bearer 打网关。
      改为 Key 随请求传入（`ChatRequest`/`ImageRequest.APIKey`），客户端无状态。
- [x] 7.9 前端 `TaskForm.vue` 级联下拉；切换分组清空模型（跨分组模型 ID 不兼容）。
- [x] 7.10 历史任务兼容：分组已停用则注入只读占位项、模型已下线则作为**禁用项保留**，
      绝不静默清空原值（否则管理员只想改名字就会永久丢掉配置）。
- [x] 7.11 30s 节流只约束 `refresh=1`，首次加载不节流（否则第二个编辑框会是空的）。
- [x] 7.12 测试：`targets_test.go` 9 例、`targets_parity_test.go` 8 例（对上游导出的
      `openai`/`claude`/`xai`/`gemini`/`antigravity`/`opencode_go`/`typesafe`/`composite`/默认分支逐一对照）、
      `scheduler_test.go` 3 例、前端 `taskFormCascade.spec.ts` 9 例 + `api.spec.ts` 补 4 例。
- [x] 7.13 集成测试补上迁移 250，并在真实 PostgreSQL 18 上实测 13 个用例全部通过；
      另用一次性 schema 实测「250 之前 `invalid_target` 被 CHECK 拒绝、之后被接受」。
- [x] 7.14 上游零改动：`internal/service` 与 `internal/handler/dto` diff 为空；
      接触面仍为 `design.md` §10 记录的 11 文件 / 108 插入 / 3 删除。

## 阶段 8：B —— 用户用自己本站 API Key 一键自测（已完成）

需求依据：`docs/iq-benchmark-user-test.md`。计费发生在网关侧、用的是用户的 Key，
所以「用谁的 Key」是唯一切换点，执行器内不得出现任何身份分支。

### 8.1 数据层

- [x] 8.1.1 迁移 `251_iq_benchmark_user_test.sql`：`run_id`/`task_id` 放开 NOT NULL；
      `source` + 回填 + CHECK(`scheduled`/`user_test`)；`triggered_by_user_id`（不加 FK）；
      `moderation_status`；`iq_artifact_source_consistent` 配对约束；
      `iq_benchmark_cases.allow_user_test`；`iq_benchmark_user_tests` 表；来源索引。
- [x] 8.1.2 `source` **刻意不设 DEFAULT**：有 DEFAULT 就会把忘记赋值的自测产出
      静默标成 `scheduled`，那是最坏的错误方向。改由 `CreateArtifact` 无条件 `SetSource` 保证。
- [x] 8.1.3 Ent schema 三处扩展并 `go generate`（生成包 `ent/iqbenchmarkusertest`）。
- [x] 8.1.4 `run_id`/`task_id` 为 NULL 时在领域层收敛为 `0`，使读取路径无需指针判空。
      实证：`TestIQBenchmarkUserTestArtifactKeepsNullRunAndTask`。

### 8.2 编排层

- [x] 8.2.1 `service/user_test_service.go`：`Submit` 的校验顺序按「越便宜越靠前」排列
      （开关 → 题目 → Key 归属 → Key 绑定分组 → 目标 → 配额），
      且**全部校验都在落记录之前**，否则被拒的请求会污染日配额把用户锁死。
- [x] 8.2.2 **分组以 Key 绑定为准**。发现并修掉的真实漏洞：网关按 `apiKey.GroupID`
      路由（`billing_inflight_reservation.go` 的 `resolveGroupByAPIKey`），
      原先信任请求里的 `group_id`，用户可借此让本地预检与实际执行分组不一致——
      既能绕过「分组未开放生图」的图片题预检，也会让排行榜把成绩算到错误的模型头上。
      写入记录与执行入参都改用解析后的分组；变异测试实证该断言能翻红。
- [x] 8.2.3 去重键加入分组（`user_id:case_id:group_id:model` 的 SHA-256）。
      漏掉分组会让用户 10 分钟内无法测第二个分组，而提示是「刚刚已提交过」。
- [x] 8.2.4 去重只统计 `pending`/`running`/`success`，失败的尝试不占用窗口。
- [x] 8.2.5 配额三项分工：在途并发走 Redis 原子槽位（有真实 TOCTOU，失败即真扣钱，
      故 **fail-closed**）；日次数与日成本只以数据库为准（再开一个 Redis 计数器必然漂移，
      会让用户看到的历史条数与系统拒他的理由对不上）。
- [x] 8.2.6 槽位释放用 `defer` + `slotReleased` 标志 + `sync.Once`，任一前置失败都不会
      永久占住用户的一格并发额度。
- [x] 8.2.7 `startOfDay` 用本地时区（UTC 会让东八区配额在早 8 点重置）。
- [x] 8.2.8 `MyTargets`：用户侧只读的分组/模型候选（复用 A 的 resolver），
      带 `api_key_id` 时按 Key 绑定收窄，保证「下拉里看到的 = 提交时执行的」。
      不返回全部分组，避免泄露站点分组拓扑。

### 8.3 接口与限流

- [x] 8.3.1 `GET/POST /iq-gallery/user-tests`、`GET /:id`、`GET /targets`，
      整组受 `user_test_enabled` 控制（关闭时 404，而非 403/503）。
- [x] 8.3.2 `POST` 走 `Limiter.HeavyMiddleware()`；用显式 if/else 注册，
      使未装配限流器的部署仍能自测（否则整条路由会消失）。
- [x] 8.3.3 `statusOfUserTestError` 15 个 reason → HTTP 状态映射；
      前端 `userTestError.ts` 以**逐字相同的 reason 字符串**为键，并有测试双向校验。
- [x] 8.3.4 公开作品 DTO 增 `source`，但**永不**下发 `triggered_by_user_id`
      （精确白名单测试 + 单独的隐私断言）。

### 8.4 前端

- [x] 8.4.1 `UserTestDialog.vue`：Key/题目/分组/模型四个下拉；
      题目按 `allow_user_test` 前端过滤（公开列表接口不按它过滤）；
      Key 绑定分组时锁定分组下拉并给出说明；换分组清空模型。
- [x] 8.4.2 不在前端做任何额度计算：日次数/成本/Key 状态全以后端为准，
      前端再算一遍只会漂移成「按钮可点但一提交就报错」。
- [x] 8.4.3 错误展示走 `userTestError.ts` 的 reason→i18n 子键映射，
      绝不把后端英文 reason 直接糊给用户。
- [x] 8.4.4 `SettingsPanel` 增 5 项自测配置（默认值与后端 `DefaultSettings()` 逐字对齐）；
      `CaseForm` 增 `allow_user_test`；`FilterBar` 增来源筛选（默认 `scheduled`）；
      `ArtifactCard` 增「用户自测」徽标。
- [x] 8.4.5 自测入口按钮先探测 `/user-tests/targets`，拿不到数据就不显示——
      否则在关闭自测的站点上点开只有一片空。
- [x] 8.4.6 提交成功后自动把来源切到 `all` 并刷新：自测产出被默认的 `scheduled`
      过滤掉，不切的话用户看不到自己刚测的东西，会以为失败了。

### 8.5 验证

- [x] 8.5.1 后端单测：`internal/iqbenchmark/...` + `cmd/server` + `internal/server/routes`
      共 9 个包全部 `ok`；`golangci-lint run ./internal/iqbenchmark/...` 0 issues。- [x] 8.5.2 集成测试（真实 PostgreSQL 18）：迁移 251 + 自测租约/计费隔离，
      `internal/iqbenchmark/repository` `ok`。
- [x] 8.5.3 前端：`npx vue-tsc -b` 无错误；`npx vitest run` 394 文件 / 3257 用例全通过。
- [x] 8.5.4 变异测试实证关键断言有效：后端「忽略 Key 绑定分组」→
      `TestUserTestGroupBoundToKeyWins` 翻红；前端「不过滤 `allow_user_test`」→
      `filters out cases that are not open for self-test` 翻红。
- [x] 8.5.5 **诚实记录未能变异隔离的一处**：前端「生效分组」被三重保护
      （Key 绑定 watcher、`loadTargets` 重置、提交时用 `effectiveGroupID`），
      单点变异是等价变异、测试无法区分。真正防线在后端的
      `TestUserTestGroupBoundToKeyWins`。已在测试文件内注明，不假装有覆盖。
- [ ] 8.5.6 `tasks.md` 6.9 的灰度仍需要在真实部署环境执行（与本阶段同因，保持未勾选）。

### 8.6 审核队列的读取路径（复核时发现并补上）

- [x] 8.6.1 复核 §8.5 的「已知缺口」时发现实际比记录更糟：`adminListQuery`
      **完全不解析 `source` 与 `moderation`**，管理员既看不到自测产出、也看不到
      待审产出。`ArtifactListQuery` 也缺 `ModerationStatus` 字段。
- [x] 8.6.2 `ArtifactListQuery` 增 `ModerationStatus`，与公开侧的 `ModeratedOnly`
      保持**两个独立字段**：合并会出现「管理侧设了 pending 却被公开口径强制
      改成 approved」的静默失效。
- [x] 8.6.3 `ListArtifacts` 里 `ModeratedOnly` 优先于管理侧筛选，公开侧的收紧
      规则不可被绕过（`TestListArtifactsModeratedOnlyWinsOverAdminFilter`）。
- [x] 8.6.4 `adminListQuery` 增 `source` / `since` / `until` / `moderation`；
      时间口径沿用公开侧既有的 unix 秒，避免同一模块出现两种格式。
- [x] 8.6.5 管理页作品表格增「来源」「审核」两列与两个筛选下拉，
      `?moderation=pending` 即审核队列。
- [x] 8.6.6 变异测试实证：`filter.ModerationStatus = st` 改成 `_ = st` →
      `TestListArtifactsAdminModerationQueue` 翻红。
- [ ] 8.6.7 **审核动作仍缺**：没有任何接口能把 `pending` 改成 `approved`，
      所以待审产出「看得见但进不了公开页」。上线前必须二选一：
      补审核动作接口，或把 `user_test_require_review` 默认值改为 `false`。

