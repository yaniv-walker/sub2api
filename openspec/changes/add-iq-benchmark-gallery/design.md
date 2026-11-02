# 设计：模型智商检测作品展（IQ Benchmark Gallery）

- **Change ID**：`add-iq-benchmark-gallery`
- **分支**：`feature/iq-benchmark-gallery`（基于 `main-self`）
- **调研记录**：`research.md`（本目录）
- **上游基线**：`main-self` @ `f688e8999e`

---

## 1. Context

### 1.1 既有系统事实（已逐文件核证）

| 事实 | 证据位置 |
|------|---------|
| 路由注册分模块：`routes.RegisterAuthRoutes` / `RegisterUserRoutes` / `RegisterAdminRoutes` / `RegisterGatewayRoutes` / `RegisterPaymentRoutes` / `model_plaza` / `RegisterPageRoutes` | `backend/internal/server/router.go` |
| 面板限流按用户 ID 计数、fail-open、键前缀 `panel:`、429 走 `abortPanelRateLimited` → `AbortWithError` | `backend/internal/server/middleware/panel_rate_limit.go` |
| 底层固定窗口原语 `middleware.NewRateLimiter(redisClient).Allow(ctx, key, limit, window)` 可直接复用 | `backend/internal/middleware/rate_limiter.go` |
| `APIKeyService.ValidateKey` 校验 `apiKey.IsActive()` 与 `user.IsActive()`，disabled 用户直接 401 | `backend/internal/service/api_key_service.go:971-990` |
| `isReservedEmail()` 只拦 4 个既有合成域（LinuxDo / OIDC / WeChat / DingTalk），**不含** `.invalid` | `backend/internal/service/auth_service.go:1400` |
| 计费/配额扣减走 `BillingCacheService`，余额不足返回 `ErrInsufficientBalance` | `backend/internal/service/billing_cache_service.go:816,1041` |
| 内容审核自动封号：`AutoBanEnabled` + `BanThreshold` → 用户被置 `StatusDisabled` | `backend/internal/service/content_moderation.go:164-205,1958-1960` |
| 图片落盘抽象 `ImageStorage.Save(ctx, key, contentType, data)` 与编排器 `ImageResultUploader`（含 SSRF/私网拦截、32MiB 上限） | `backend/internal/service/image_storage.go:30-60,184` |
| 前端已内置 DOMPurify 且已封装 `sanitizeSvg` | `frontend/src/utils/sanitize.ts` |
| **前端当前只有 `en` / `zh` 两个 locale，不存在 `ja`** | `frontend/src/i18n/index.ts`、`frontend/src/i18n/locales/` |
| `users` 表 `notes` 字段存在；`signup_source` 有 CHECK 约束与 Ent `Validate` 双重白名单 | `ent/schema/user.go:66,80` |
| 调度器/worker 生命周期由 `wire.go` 的 `provideCleanup` 统一 Stop | `backend/cmd/server/wire.go` |
| 多实例安全领取既有范式：`FOR UPDATE SKIP LOCKED` + lease | `backend/internal/repository/auth_cache_invalidation_outbox_repo.go:21-50` |
| 已有 `pg_try_advisory_lock` 单实例互斥范式 | `backend/internal/repository/scheduler_outbox_repo.go:152-170` |
| 最新 migration 序号 **248**，SQL 文件共 302 个（其余为 `*_test.go`） | `backend/migrations/` |

### 1.2 目标项目约束

- Go 1.27.3 + Gin + Wire + Ent；PostgreSQL 16 + Redis。
- Vue3 + TS + pnpm；不新增重型依赖、不引入独立运行时。
- **不得改变上游核心文件的行为语义**；新增逻辑进独立目录；对既有文件的改动限制为「注册一行」级别。
- 默认关闭；新表可先迁移上线。

### 1.3 参与边界

- 只新增：`backend/internal/iqbenchmark/`、`backend/migrations/249_*`、`backend/ent/schema/iq_benchmark_*.go`、`frontend/src/features/iq-benchmark/`、2 个页面、1 个 i18n 分文件（3 语言）。
- 只接线：`handler.Handlers` 增字段、`handler.ProvideHandlers` 增参数、`server/router.go` 增一行 `RegisterIQBenchmarkRoutes`、`cmd/server/wire.go` 增 1 个 ProviderSet + 1 个 Cleanup 项、`frontend/src/router/index.ts` 增 2 条路由、`AppSidebar.vue` 增 2 条导航、`locales/*/index.ts` 各增 1 行 import/展开。
- 不动：`usage_logs` 表结构与查询、`users` 表结构、role 枚举、`users_signup_source_check`、内容审核、渠道监控、批量生图、账务与配额语义、既有路由路径。

---

## 2. Goals / Non-Goals

### Goals

1. 管理员可配置多套题目（含内置鹈鹕题）与多条「分组 + 模型」检测任务，按分钟/小时级间隔（含抖动）自动执行。
2. 每次执行生成 run 与 artifact，题目快照冻结，历史可回溯。
3. 普通用户登录后可浏览双页签作品展（图片 / 题目问答），支持筛选、横向对比、详情、全屏预览、下载、失败占位。
4. 检测成本由内部虚拟账号承担，零影响真实用户；三类熔断（配额耗尽 / 超预算 / 账号被风控禁用）自动暂停并告警。
5. 产出安全：SVG/HTML 入库前静态扫描、渲染前消毒、失败作品不丢弃。
6. 只读接口按用户两档限流 + 并发上限，Redis 故障 fail-open。
7. 容量可控：保留天数/条数上限 + 定时清理。

### Non-Goals

- 不做匿名访问、不做站外分享链接。
- 不引入 Elo / Bradley-Terry 排名或 LLM 裁判作为唯一评分依据。
- 不做服务端栅格化（不引入 librsvg / ChromeDriver / 无头浏览器）。
- 不修改上游 `usage_logs` 查询默认行为（排除检测流量默认依赖 `user_id`/`api_key_id` 过滤，见 §7）。
- 不改 `config.yaml` / `config.Config`（配置全部走 `settings` 表）。

---

## 3. Decisions

### 3.1 独立垂直模块，通过小接口接入 Wire

```
backend/internal/iqbenchmark/
├── wire.go                    # ProviderSet：仓储/服务/调度/限流/处理器
├── types.go                   # 领域模型 + 状态常量 + 错误定义
├── internal_agent.go          # IsInternalAgent + 幂等初始化（唯一判定入口）
├── settings.go                # IQBenchmarkSettings / IQGalleryRateLimitSettings + 60s 进程内缓存
├── repository/
│   ├── repository.go          # Ent 实现（Case/Task/Run/Artifact/Lease）
│   ├── query.go               # 多维筛选 SQL
│   └── lease.go               # FOR UPDATE SKIP LOCKED 领取与租约续期
├── service/
│   ├── case_service.go        # 题目 CRUD / 复制 / 排序 / 快照冻结
│   ├── task_service.go        # 任务 CRUD / enable / pause / run-now
│   ├── run_service.go         # run 编排、并发、超时、成本汇总
│   ├── invoke.go              # 网关调用（chat/completions、images/generations）
│   ├── artifact_service.go    # 产出入库、图片落盘、删除与清理
│   ├── guard.go               # 配额/预算/账号状态三重熔断
│   └── overview.go            # 分组×模型聚合
├── scheduler/
│   └── scheduler.go           # Start(ctx)/Shutdown(ctx)、租约扫描、抖动
├── sanitize/
│   ├── svg.go                 # 危险构造静态扫描（Go 正则，源自 MIT _FLAGS）
│   ├── svg_test.go            # XSS payload 单测
│   └── classify.go            # 输出形态判定（完整/可恢复 SVG、HTML、Markdown、text）
├── ratelimit/
│   └── iq_gallery_rate_limiter.go  # IQGalleryRateLimiter（两档 + 并发）
├── handler/
│   ├── admin_handler.go
│   ├── user_handler.go
│   └── dto.go                 # 管理 DTO 与公开白名单 DTO 分离
└── *_test.go
```

**理由**：与既有 `internal/securityaudit/` 的垂直模块惯例一致（该模块同样定义自己的 `ProviderSet`、既有 `service.SettingService` 通过接口注入）。`internal/service` 不反向 import `iqbenchmark`，由 `golangci-lint` 的 `depguard` 规则（`service must not import repository`）继续约束。

### 3.2 数据模型

**4 张新表**，全部 `BIGSERIAL` 主键 + `created_at/updated_at timestamptz`。SQL migration 为事实源；Ent schema 同步但**不**作为生产建表入口。

#### `iq_benchmark_cases`（题目）

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `name` | VARCHAR(100) NOT NULL | 题目名 |
| `kind` | VARCHAR(16) NOT NULL | `image` / `text` / `code`，带 CHECK |
| `prompt` | TEXT NOT NULL | 题目正文 |
| `system_prompt` | TEXT NULL | 可选系统提示词 |
| `params` | JSONB NOT NULL DEFAULT '{}' | `temperature` / `max_tokens` / `size` / `n` 等 |
| `expect_kind` | VARCHAR(16) NOT NULL | `image_url` / `svg` / `markdown` / `text`，带 CHECK |
| `scoring` | VARCHAR(16) NOT NULL DEFAULT 'manual' | `manual` / `rule` / `manual_rule` / `llm_judge` |
| `llm_judge_enabled` | BOOLEAN NOT NULL DEFAULT false | LLM 裁判开关（默认关） |
| `builtin` | BOOLEAN NOT NULL DEFAULT false | 内置题（不可删除，可禁用） |
| `enabled` | BOOLEAN NOT NULL DEFAULT true | |
| `sort_order` | INTEGER NOT NULL DEFAULT 0 | |

约束：`builtin=true` 的行删除由 service 层拒绝（`ErrCaseBuiltinUndeletable`），并额外加 `CHECK (NOT (builtin AND NOT enabled) ...)` 不成立 —— 因为内置题允许禁用，故不使用 CHECK，仅 service 层守卫 + `UPDATE ... WHERE builtin = false` 的 DELETE 语句保证（见 §11 测试）。

#### `iq_benchmark_tasks`（检测任务）

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `name` | VARCHAR(100) NOT NULL | |
| `group_id` | BIGINT NOT NULL | 目标分组 |
| `model` | VARCHAR(200) NOT NULL | 目标模型 |
| `case_ids` | JSONB NOT NULL DEFAULT '[]' | 题目 ID 数组（有序） |
| `interval_minutes` | INTEGER NOT NULL | 15 ~ 10080 |
| `jitter_seconds` | INTEGER NOT NULL DEFAULT 0 | ≥0 且 `interval*60 - jitter ≥ 900` |
| `enabled` | BOOLEAN NOT NULL DEFAULT false | |
| `status` | VARCHAR(24) NOT NULL DEFAULT 'idle' | `idle` / `running` / `quota_exhausted` / `budget_exhausted` / `account_disabled` / `paused_by_admin` |
| `status_reason` | TEXT NULL | 熔断原因摘要 |
| `next_run_at` | TIMESTAMPTZ NULL | 下次执行时间 |
| `last_run_at` | TIMESTAMPTZ NULL | |
| `lease_owner` | VARCHAR(128) NULL | 持有租约的实例 ID |
| `lease_expires_at` | TIMESTAMPTZ NULL | |

约束：
- `CHECK (interval_minutes >= 15 AND interval_minutes <= 10080)`
- `CHECK (jitter_seconds >= 0)`
- **部分唯一索引**保证同一「分组+模型」只有一个启用中任务：
  `CREATE UNIQUE INDEX uq_iq_task_group_model_enabled ON iq_benchmark_tasks (group_id, model) WHERE enabled = true;`

#### `iq_benchmark_runs`（执行记录）

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `task_id` | BIGINT NOT NULL | |
| `group_id` | BIGINT NOT NULL | 快照（任务改动后仍可回溯） |
| `model` | VARCHAR(200) NOT NULL | 快照 |
| `trigger` | VARCHAR(16) NOT NULL | `schedule` / `manual` / `retry` |
| `started_at` / `finished_at` | TIMESTAMPTZ | |
| `success_count` / `failed_count` / `timeout_count` / `blocked_count` | INTEGER NOT NULL DEFAULT 0 | |
| `total_duration_ms` | BIGINT NOT NULL DEFAULT 0 | |
| `total_cost` | NUMERIC(20,10) NOT NULL DEFAULT 0 | 汇总自 usage |
| `total_input_tokens` / `total_output_tokens` | BIGINT NOT NULL DEFAULT 0 | |
| `internal_user_id` / `internal_api_key_id` | BIGINT NOT NULL | 执行账号（审计与统计过滤依据） |
| `status` | VARCHAR(16) NOT NULL | `running` / `success` / `partial` / `failed` |

#### `iq_benchmark_artifacts`（产出）

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `run_id` | BIGINT NOT NULL | |
| `task_id` | BIGINT NOT NULL | |
| `case_id` | BIGINT NOT NULL | |
| `case_snapshot` | JSONB NOT NULL | 题目快照（name/kind/prompt/system_prompt/params/expect_kind/scoring） |
| `group_id` | BIGINT NOT NULL | |
| `model` | VARCHAR(200) NOT NULL | |
| `request_params` | JSONB NOT NULL DEFAULT '{}' | 实际下发参数 |
| `render_kind` | VARCHAR(16) NOT NULL | `image_url` / `svg` / `markdown` / `text` |
| `image_url` | TEXT NULL | 图片类对象存储地址 |
| `content` | TEXT NULL | 文本类原文（已截断） |
| `content_truncated` | BOOLEAN NOT NULL DEFAULT false | 是否因超限截断 |
| `content_sha256` | CHAR(64) NULL | 原文校验（截断前） |
| `unsafe_flags` | JSONB NOT NULL DEFAULT '[]' | 命中的危险构造标记 |
| `raw_response_ref` | TEXT NULL | 原始响应（见 §8.3 存储策略） |
| `duration_ms` | INTEGER NOT NULL DEFAULT 0 | |
| `input_tokens` / `output_tokens` | INTEGER NOT NULL DEFAULT 0 | |
| `cost` | NUMERIC(20,10) NOT NULL DEFAULT 0 | |
| `status` | VARCHAR(16) NOT NULL | `success` / `failed` / `timeout` / `blocked` |
| `error_code` / `error_summary` | VARCHAR(64) / TEXT | 摘要脱敏，不含堆栈与供应商细节 |
| `score` | NUMERIC(8,2) NULL | 人工/规则评分 |
| `scored_by` / `scored_at` | BIGINT / TIMESTAMPTZ | |
| `deleted_at` | TIMESTAMPTZ NULL | 软删除（清理用） |

索引：
- `idx_iq_artifact_run (run_id)`
- `idx_iq_artifact_feed (deleted_at, created_at DESC)` —— 作品展主查询
- `idx_iq_artifact_filters (group_id, model, case_id, created_at DESC)`
- `idx_iq_artifact_status (status, created_at DESC)`

**为什么 artifact 存 `case_snapshot` 而非仅 `case_id`**：题目可编辑，历史产出必须能按当时的题面回溯；这是「同题多模型对比」正确性的前提。

### 3.3 调度模型与多实例去重

```
Scheduler.Start(ctx)
  └─ 单 goroutine ticker（默认 30s，可配）
       ├─ 1. 若 settings.enabled == false → 直接跳过本轮（零副作用）
       ├─ 2. 守卫检查：内部账号 status == active？日成本是否超预算？ ← 失败则批量熔断，不领取任务
       └─ 3. ClaimDueTasks(workerID, limit, lease=90s)
              └─ SQL: WITH candidates AS (
                        SELECT id FROM iq_benchmark_tasks
                        WHERE enabled = true AND status IN ('idle','running')
                          AND next_run_at IS NOT NULL AND next_run_at <= NOW()
                          AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
                        ORDER BY next_run_at ASC
                        LIMIT $2
                        FOR UPDATE SKIP LOCKED)
                     UPDATE iq_benchmark_tasks t
                     SET lease_owner = $1, lease_expires_at = NOW() + ($3 * INTERVAL '1 second')
                     FROM candidates c WHERE t.id = c.id
                     RETURNING t.*;
       └─ 4. 每个领取到的任务起一个受控 goroutine（semaphore 上限 4）执行 run
```

**多实例去重保证**：
1. `FOR UPDATE SKIP LOCKED` 保证同一行不会被两个实例同时领取（数据库级）。
2. 租约 `lease_expires_at` 90s，执行中每 30s 续期（`UPDATE ... WHERE id=$1 AND lease_owner=$2`，影响行数为 0 即视为租约丢失 → 立即中止本轮，避免双写）。
3. `next_run_at` 在**领取成功后立刻**推进为 `now + interval ± jitter`，即使执行崩溃也不会形成热点重试。
4. 进程内 `inflight map[int64]struct{}` 仅作单实例兜底（与既有 `ChannelMonitorRunner.tryAcquireInFlight` 同构），**不以它作为跨实例正确性依据**。

**抖动**：`jitter = rand.Intn(2*jitterSeconds+1) - jitterSeconds`；`next = now + interval*60 + jitter`，并夹紧到 `>= now + 900s`（等价于 `validateJitter` 的语义：`interval*60 - jitter >= 900`）。

**生命周期**：
- `NewScheduler(...)` 只装配字段，**不启动任何 goroutine**。
- `Start(ctx)` 幂等（`sync.Once`），由 `wire` 在装配后显式调用。
- `Shutdown(ctx)` 取消 ctx、等待 in-flight（受 ctx 死线约束）、释放持有的租约（`UPDATE ... SET lease_owner=NULL, lease_expires_at=NULL`）。接入 `provideCleanup` 的 `parallelSteps`，10s 总超时内完成。

**单任务执行并发**：同一任务内题目串行执行（避免同一「分组+模型」瞬时打满），任务之间并发（上限 4）。理由：同分组同模型的并发会放大上游 429 与账号冷却风险，且作品展对实时性无要求。

### 3.4 内部调用链路与计费口径

**调用链（必须复用本网关，禁止直连上游）**：

```
iqbenchmark/service/invoke.go
  ├─ 组装 HTTP 请求
  │    ├─ BaseURL  ← settings.internal_gateway_base_url（默认 http://127.0.0.1:<cfg.Server.Port>）
  │    ├─ Header   Authorization: Bearer <internal_api_key（仅内存，来自 settings 密文解出）>
  │    ├─ Header   User-Agent: sub2api-iq-benchmark/1.0
  │    └─ Body     按 case.kind 走 /v1/chat/completions 或 /v1/images/generations
  ├─ 出站
  │    ├─ 复用现有 SSRF/DNS 复检与响应体上限策略（不新建 transport）
  │    └─ 超时：图片 300s、文本 180s（可配），响应体上限 8MiB（超限截断并标记）
  └─ 回包处理
       ├─ 2xx → 解析 → classify → sanitize → 落库
       └─ 非 2xx → 按 error_code 分类（insufficient_balance / rate_limited / upstream_5xx / ...）
```

**为什么走 HTTP 自调而不是进程内直调 Handler**：进程内 `httptest` 式直调（`AccountTestService.RunTestBackground` 的做法）绕过 `apiKeyAuth → 分组解析 → 账号调度 → 计费 → usage 落库` 这一完整链路，会破坏「扣内部账号真实配额 + 完整记录 usage」的既定口径。走本机 HTTP 才能让网关把它当普通请求处理。

**BaseURL 解析顺序**（避免硬编码）：`settings.internal_gateway_base_url`（管理员可覆盖）→ `http://127.0.0.1:<cfg.Server.Port>`（`config.Server.Port` 默认 8080）。仅允许 `http(s)` 且主机为 loopback 或显式配置值，禁止把 API Key 发往任意外部主机。

**计费口径（零上游 schema 变更）**：
- 请求携带**专用 API Key**（归属内部虚拟账号 + 绑定目标分组）。
- 网关照常走 `APIKeyService.ValidateKey` → 分组解析 → `BillingCacheService` 扣减 → `usage_logs` 写入。
- 扣减对象是内部账号的 `balance` / `quota`，**与任何真实用户无关**。
- usage 行天然带 `user_id = internal_user_id`、`api_key_id = internal_api_key_id`、`user_agent = sub2api-iq-benchmark/1.0`。
- **不加** `usage_logs.source` 列（该表是上游大表，加列即需改 Ent schema + 触发器 + 聚合链路）。

**成本回采**：run 结束后按 `request_id` 批量查 `usage_logs`（`WHERE request_id = ANY($1)`）汇总 `actual_cost` / tokens，写入 `iq_benchmark_runs` 与各 artifact。若 usage 尚未落库（异步批插），以「下一轮调度补偿回采」的方式补齐（`runs.status='running'` 且超过 5 分钟的回采一次）。

### 3.5 内部账号隔离方案

**字段组合（严格按既定口径）**：

| 字段 | 值 | 理由 |
|---|---|---|
| `email` | `iq-benchmark@iq-benchmark.invalid` | `.invalid` 为 RFC 2606 保留 TLD，永不可路由；与既有 `LinuxDoConnectSyntheticEmailDomain` 等合成域风格一致。**注意**：`isReservedEmail()` 目前只拦 4 个既有合成域，**不含**本域，不能依赖它做防护 |
| `role` | `user` | 不得为 admin；不新增 role 枚举值 |
| `status` | `active` | **强制**：`ValidateKey` 校验 `user.IsActive()`，disabled 直接 401 |
| `password_hash` | 一次性随机（`bcrypt` 32 字节随机串） | 管理员不可知、不写日志、不下发前端 —— 登录隔离的唯一屏障 |
| `notes` | 前缀 `iq-benchmark:internal-agent` | **唯一权威隔离标识** |
| `signup_source` | 不设置（默认 `email`） | 有 CHECK 约束 + Ent `Validate` 双重白名单，扩展需改两处上游文件 |

**判定函数（唯一入口）**：

```go
// internal/iqbenchmark/internal_agent.go
const InternalAgentNotesPrefix = "iq-benchmark:internal-agent"

func IsInternalAgent(u *service.User) bool {
    if u == nil { return false }
    return strings.HasPrefix(strings.TrimSpace(u.Notes), InternalAgentNotesPrefix)
}
```

全模块**只允许**通过 `IsInternalAgent` 判断；禁止在多处硬编码 email 字符串或 notes 前缀字面量（`grep` 断言进 CI：`internal/iqbenchmark` 之外不得出现该前缀字面量）。

**幂等初始化**：

```go
EnsureInternalAgent(ctx) (*service.User, *service.APIKey, error)
  1. 按 notes 前缀查 users（LIMIT 1）→ 命中则复用
  2. 未命中 → 创建 user（随机 password_hash / role=user / status=active）
  3. 按 (user_id, name='iq-benchmark-agent') 查 API Key → 命中则复用
  4. 未命中 → 创建 API Key（GroupID = settings.default_group_id）
  5. 若 settings.default_group_id 为空或分组不存在 → 不创建 Key，返回 ErrNoTargetGroup（不阻塞启动）
```

- 创建失败 / 分组缺失：**WARN 日志 + 功能降级为「未就绪」**，主 API 正常启动；管理员可在设置页修正后触发重建。
- 并发启动：以 `pg_try_advisory_lock(hashtext('iq_benchmark_agent_init'))` 串行化初始化（复用既有 `scheduler_outbox_repo.go` 范式），避免多实例重复创建。

### 3.6 三类熔断

| 触发 | 检测点 | 动作 |
|---|---|---|
| **配额耗尽** | run 收到 `INSUFFICIENT_BALANCE` / OpenAPI `insufficient_quota`，或 run 后内部账号 `balance < settings.min_balance_floor` | `tasks.status='quota_exhausted'`、`enabled` 保持不变但 `next_run_at=NULL`（停调度）、WARN 告警日志；不换账号、不静默重试 |
| **超预算** | 每轮调度前统计「今日（本地时区）检测成本」= `SUM(actual_cost) FROM usage_logs WHERE api_key_id=$1 AND created_at >= todayStart`；≥ `settings.daily_cost_budget` | 全部任务 `status='budget_exhausted'`、`next_run_at=NULL`、告警；跨日自动恢复（下一轮检测成本回落则置回 `idle` 并重排 `next_run_at`） |
| **账号被风控禁用** | 每轮调度前 + 每次 run 前：`internalUser.Status != active`（`IsInternalAgent` 判定后的实体） | 全部任务 `status='account_disabled'`、`next_run_at=NULL`、ERROR 告警（含「可能被 content_moderation 自动封号」提示）；**不重试、不空转** |

熔断状态由管理员显式「恢复」按钮清除（`POST /admin/iq-benchmark/tasks/:id/resume`），或在条件消失后由调度器的自愈检查自动清除（配额/预算两类；`account_disabled` 必须人工介入）。

### 3.7 产出存储与消毒

**图片类**：
- 走 `/v1/images/generations`，响应可能是 `b64_json` 或 `url`。
- 复用既有 `ImageResultUploader.Rewrite(ctx, taskID, result)`：它已实现 b64 解码、URL 下载（含私网拦截/重定向逐跳校验/32MiB 上限）、`ImageStorage.Save` 落盘、并把响应改写为短链接。
- 我们传入 `taskID = "iq-<runID>-<artifactOrdinal>"`，使对象键前缀为 `iq-benchmark/...`（由 `ImageResultUploader.buildKey` 的 `prefix` 决定；本模块使用独立 `ImageResultUploader` 实例，`prefix = "iq-benchmark"`，**不修改**既有实例）。
- 单文件上限：`settings.max_artifact_bytes`（默认 8 MiB）；单 run 产出上限：`settings.max_artifacts_per_run`（默认 20），超限截断并写入 `artifacts.error_code='truncated'` 的失败作品一条（记录被截断数量）。
- 对象存储未配置/不可用：图片产出降级为 `status='failed'`、`error_code='storage_disabled'`，**作品展仍显示失败占位**。

**文本类（SVG / Markdown / HTML）消毒**（双阶段）：

阶段 1 —— 入库前静态扫描（Go，`sanitize/svg.go`）：

源自 `delirious6423/pelican-bench` 的 `_FLAGS`（MIT，见调研记录 §2.1），改写为 Go `regexp`：

| 键 | 正则 | 处置 |
|---|---|---|
| `script` | `<script\b` | 命中 → `blocked` |
| `event_handler` | `\son[a-z]+\s*=` | 命中 → 剥离属性后保留 |
| `foreign_object` | `<foreignObject\b` | 命中 → `blocked` |
| `javascript_url` | `javascript\s*:` | 命中 → `blocked` |
| `entity_decl` | `<!ENTITY` | 命中 → `blocked` |
| `doctype` | `<!DOCTYPE` | 命中 → 剥离 |
| `embed_object_iframe` | `<(iframe|object|embed)\b` | 命中 → `blocked` |
| `css_import` | `@import` | 命中 → 剥离 |
| `data_uri` | `(?:href|src)\s*=\s*["']\s*data:` | 命中 → 剥离（保留 `image/svg+xml` 内联子集？否 → 一律剥离） |

- 命中「剥离类」标记：写入 `artifacts.unsafe_flags`，`status` 保持 `success`（内容已净化）。
- 命中「阻断类」标记：`status='blocked'`、`error_code='unsafe_content'`，`content` 仍保存（供管理员审阅），但公开 DTO 不下发 `content`，只下发占位信息。
- 外链引用（`href`/`xlink:href`/`src` 指向非 `#` 资源）：一律剥离并记 `external_ref` 标记。

阶段 2 —— 前端渲染前消毒：`frontend/src/features/iq-benchmark/sanitize.ts` 复用 `@/utils/sanitize` 的 `sanitizeSvg`，并对 HTML 走 `DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })`。**禁止 `v-html` 直出未消毒内容**（ESLint 自定义断言 + code review 检查项，并在 `__tests__` 里断言「未消毒输入不进入 DOM」）。

**渲染优先级**：`image_url` → `<img src>`（脚本不执行，最安全）；`svg` → 内联消毒后 SVG（经 DOMPurify）；`markdown` → `marked` 渲染 + DOMPurify；`text` → `<pre>` 纯文本。

**失败作品**：`status in (failed, timeout, blocked)` 的 artifact 同样入库，作品展以占位卡片展示（含状态角标与脱敏错误摘要）。

### 3.8 失败与重试语义

| 场景 | 记录 | 重试 |
|---|---|---|
| 上游 5xx / 网络抖动 | `status='failed'`、`error_code='upstream_error'` | 单 artifact 内最多 2 次指数退避（1s、3s）；仍失败即定稿 |
| 上游 429 | `status='failed'`、`error_code='rate_limited'` | 不重试（交下一轮调度），并给该任务加一次「退避惩罚」：`next_run_at += interval` |
| 超时 | `status='timeout'` | 不重试（避免成本翻倍） |
| 内容审核拦截（网关返回 4xx 且 `code` 含 moderation/cyber） | `status='blocked'`、`error_code='content_blocked'` | 不重试 |
| 余额不足 | `status='failed'`、`error_code='insufficient_balance'` | 不重试，触发 §3.6 熔断 |
| 危险构造阻断 | `status='blocked'`、`error_code='unsafe_content'` | 不重试 |
| 解析失败（模型输出无有效产出） | `status='failed'`、`error_code='no_artifact'` | 不重试 |

**「重跑失败项」**（`POST /admin/iq-benchmark/runs/:id/retry-failed`）：为原 run 中 `status != 'success'` 的每个 artifact 新建一条 run（`trigger='retry'`），只跑对应用题；旧 artifact 不删除，保留历史可比性。

### 3.9 清理与容量上限

- 配置：`retention_days`（默认 30，0 = 不按天数清理）、`max_artifacts`（默认 5000，0 = 不按条数清理）、`cleanup_interval_minutes`（默认 60）。
- 清理任务：调度器内独立 tick，`pg_try_advisory_lock(hashtext('iq_benchmark_cleanup'))` 保证单实例执行：
  1. 按 `retention_days` 软删 `artifacts`（`deleted_at = NOW()`），再硬删对象存储键（`ImageStorage` 需扩展 `Delete` 能力 → 见下）与超过 7 天的软删行。
  2. 按 `max_artifacts` 保留最新的 N 条（按 `created_at DESC`），其余软删。
  3. `runs` 与 `artifacts` 级联：run 下全部 artifact 被清理后，run 同步软删。
- **对象存储删除**：现有 `ImageStorage` 接口只有 `Save`。方案：本模块定义一个 `artifactBlobDeleter` 小接口（`Delete(ctx, key) error`），由 `repository.S3ImageStorage` 是否实现该接口动态断言；未实现时清理只删 DB 行并记录 WARN（`blob_orphaned` 计数），作为已知限制登记在 verification.md。
- 容量护栏：`EnsureCapacity()` 在写 artifact 前检查当前未删除条数，超 `max_artifacts * 1.2` 时拒绝新写入并告警（防止清理滞后导致无限增长）。

### 3.10 默认关闭策略

| 层 | `enabled=false` 时的行为 |
|---|---|
| Scheduler | 不启动 ticker（`Start` 内部先读设置，false 则只登记「待启用」，不建 ticker）；已启动时下一轮直接 return（零 DB 写、零出站） |
| 内部账号初始化 | 不执行（避免在关闭态创建账号/Key） |
| 管理员 API | 除 `GET /settings` 与 `PUT /settings` 外，全部返回 `503 IQ_BENCHMARK_DISABLED`，消息明确「功能未启用」 |
| 用户 API | 全部返回 `404`（配合前端 `ENABLED=false` 时隐藏导航；直接访问返回未启用页） |
| 前端 | `public settings` 注入 `iq_benchmark_enabled` → 导航隐藏；路由守卫直接跳「未启用」视图 |
| 限流 | 不参与（路由不可达，中间件不挂载） |

**落地方式**：新增 `settings` key `iq_benchmark_settings`（JSON），结构见 §3.11。`enabled` 变更立即生效：设置服务写库后调用 `SettingsCache.Invalidate()`，调度器每轮读取 60s 缓存；同时 `PUT /settings` 处理器显式调用 `scheduler.Reconcile(ctx)`（启→停/停→启）；`public settings` 同步暴露布尔位。

**前端如何隐藏入口（与原设计不同，已改为本地探测）**：

原计划「沿用 `featureFlags.ts` 的 `opt-in` 模式」，实施时**放弃**该做法。
原因是成本与约束直接冲突：在上游注册一个新 flag 需要同步修改
`frontend/src/utils/featureFlags.ts` 及与之联动的 11 个上游文件
（flag 注册表、`public settings` 类型、设置项 UI、i18n、若干消费点），
远超「注册一行」的边界，且全部落在上游高频变更文件上。

改用**本地可用性探测**，实现于 `frontend/src/features/iq-benchmark/availability.ts`：

- `probeIqGalleryAvailability()` 调用 `GET /api/v1/iq-gallery/overview`；
- `404`（用户侧路由未注册）、`503`（`IQ_BENCHMARK_DISABLED`）、网络错误一律判为**未启用**；
- 判为未启用时，侧栏的「作品展」入口不渲染，用户直接访问 `/iq-gallery` 也只会看到一个
  中性的空状态，不会暴露半成品界面；
- 探测结果按会话缓存，`resetIqGalleryAvailability()` 供登出/切换账号时清理。

代价（已在 `verification.md` §四 记录）：进入应用时会多一次轻量 `GET` 请求；
该请求失败不影响任何其他页面。收益是**上游接触面归零**，与本变更的核心约束一致。

### 3.11 配置结构

```go
type IQBenchmarkSettings struct {
    Enabled                bool   `json:"enabled"`                  // 默认 false
    DefaultGroupID         int64  `json:"default_group_id"`         // 内部 Key 绑定分组
    InternalGatewayBaseURL string `json:"internal_gateway_base_url"` // 空 = 127.0.0.1:cfg.Server.Port
    IntervalMinMinutes     int    `json:"interval_min_minutes"`      // 15
    IntervalMaxMinutes     int    `json:"interval_max_minutes"`      // 10080
    ModelTimeoutSeconds    int    `json:"model_timeout_seconds"`     // 300
    MaxArtifactBytes       int64  `json:"max_artifact_bytes"`        // 8 MiB
    MaxArtifactsPerRun     int    `json:"max_artifacts_per_run"`     // 20
    MaxTaskConcurrency     int    `json:"max_task_concurrency"`      // 4
    DailyCostBudget        float64 `json:"daily_cost_budget"`        // 0 = 不限
    MinBalanceFloor        float64 `json:"min_balance_floor"`        // 0 = 不限
    ScoringEnabled         bool   `json:"scoring_enabled"`           // true
    LLMJudgeEnabled        bool   `json:"llm_judge_enabled"`         // false（总开关，case 级需同时开）
    ExposeCostToUser       bool   `json:"expose_cost_to_user"`       // false
    ExposeTokensToUser     bool   `json:"expose_tokens_to_user"`     // false
    RetentionDays          int    `json:"retention_days"`            // 30
    MaxArtifacts           int    `json:"max_artifacts"`             // 5000
    CleanupIntervalMinutes int    `json:"cleanup_interval_minutes"`  // 60
}
```

**不改 `config.yaml` / `config.Config`**：全部走 `settings` 表，与 `panel_rate_limit_settings`、`channel_monitor_v2` 等既有做法一致。

### 3.12 字段暴露白名单

**管理端 DTO**（`AdminArtifactDTO` 等）可含：`raw_response_ref`、`unsafe_flags`、`internal_user_id`、`internal_api_key_id`、`cost`、`error_code`、`content`（原样）。

**公开白名单 DTO**（`PublicArtifactDTO`）—— 逐字段列举：

| 字段 | 暴露 | 说明 |
|---|---|---|
| `id` | ✅ | |
| `group_name` | ✅ | 分组**名**，非 ID |
| `model` | ✅ | |
| `case_name` | ✅ | 快照中的题面**名** |
| `kind` / `render_kind` | ✅ | |
| `image_url` | ✅ | 已落对象存储的短链 |
| `content` | ⚠️ 条件 | 仅 `render_kind in (svg, markdown, text)` 且 `status='success'` 且 `unsafe_flags` 不含阻断项时下发；否则 `null` |
| `created_at` | ✅ | |
| `duration_ms` | ✅ | |
| `status` | ✅ | success / failed / timeout / blocked |
| `score` | ✅ | 空则 null |
| `error_summary` | ⚠️ 条件 | 仅脱敏摘要（白名单码表映射），不含原始错误文本 |
| `cost` | ❌ 默认 | 仅 `settings.expose_cost_to_user=true` 时下发，且只给**单条成本**，不给成本明细拆分 |
| `input_tokens` / `output_tokens` | ❌ 默认 | 仅 `settings.expose_tokens_to_user=true` |
| `case_prompt` / `system_prompt` | ⚠️ 条件 | 仅 `case` 允许公开（`cases.公开` 字段 = `enabled && !system_prompt_private`）时下发题面正文 |
| `raw_response_ref` | ❌ | 永不暴露 |
| `internal_user_id` / `internal_api_key_id` | ❌ | 永不暴露 |
| `unsafe_flags` | ❌ | 永不暴露（可暴露 `is_unsafe: bool`） |
| `notes` / `account_id` / 上游渠道 / 供应商 | ❌ | 永不暴露 |
| `error_code` | ❌ | 内部码，公开侧只给 `error_summary` |

**实现约束**：公开 DTO 是**独立结构体**（`PublicArtifactDTO`），不嵌入、不复用 `AdminArtifactDTO`；映射函数 `ToPublicArtifact()` 逐字段赋值，禁止 struct 拷贝或 JSON 反射转换。参照既有 `dto.UserFromService` 与 `dto.UserFromServiceAdmin` 的分离惯例。

**断言测试**：`dto_public_test.go` 用 reflect 遍历 `PublicArtifactDTO` 的 JSON tag，断言黑名单集合（`raw_response_ref` / `internal_user_id` / `internal_api_key_id` / `unsafe_flags` / `notes` / `account_id` / `error_code` / 任何含 `cost` 的键）不在其中；并对 `expose_cost_to_user=false` 时的实际 JSON 做序列化断言（`strings.Contains(body, "cost")` 为 false）。

### 3.13 限流方案

**落点决策**：`IQGalleryRateLimiter` 放在 **`backend/internal/iqbenchmark/ratelimit/`**（新模块目录内），不放 `internal/server/middleware/`。

理由与 merge 冲突分析见本节末的落点对比结论。要点：`internal/server/middleware/` 是上游高频变动目录（`panel_rate_limit.go`、`gateway_remediation*.go` 等），新增文件本身不产生冲突，但**该目录下的文件普遍被上游持续修改**，把新逻辑放进去会诱导后续维护者在同目录改动，且 `middleware.ProviderSet` 需增一行（上游也在持续改 `wire.go`）。放新模块内则 `upstream/main` 合并时**零冲突面**（新目录默认不会被上游触碰）。

**落点对比结论**：

| 落点 | 新增文件冲突风险 | 需改上游文件 | 结论 |
|---|---|---|---|
| `internal/server/middleware/iq_gallery_rate_limit.go` | 文件级无冲突，但同目录被上游持续改动，后续维护易在被改文件中插入逻辑 → 高概率冲突 | `middleware/wire.go`（ProviderSet 加一行） | ❌ |
| `internal/iqbenchmark/ratelimit/`（本方案） | **零冲突**（新目录） | 无（由 `iqbenchmark.ProviderSet` 内部提供） | ✅ |

差异本质：前者把本功能的状态机放进上游共享目录，长期 merge 成本随上游演进上升；后者完全隔离，代价仅是本模块需自行持有 `*redis.Client` 与 `settingService` 引用（Wire 已能提供）。

**两档叠加计数**：

| 档 | 端点 | 默认 RPM |
|---|---|---|
| `read` | `GET /iq-gallery/overview`、`GET /iq-gallery/cases`、`GET /iq-gallery/artifacts`（列表） | 120 |
| `heavy` | `GET /iq-gallery/artifacts/:id`（返回原始内容/大字段） | 60 |

一次 heavy 请求**同时**消耗 `read` + `heavy` 两档（防单档绕过）：实现上先 `Allow(read)`，通过后再 `Allow(heavy)`；任一不通过即 429。

**计数键**：`iqgallery:user:{read|heavy}:{userID}`（`middleware.NewRateLimiter` 内部再加 `rate_limit:` 前缀）。**不复用 `panel:` 前缀**，避免挤占真实用户面板额度。

**并发上限**：见 §3.14。

**fail-open**：`Allow` 返回 error 时 `slog.Warn` + 放行，绝不返回 5xx。

**429 输出**：本模块内实现 `abortIQGalleryRateLimited(c, retryAfter)`，错误体与 `abortPanelRateLimited` **完全一致**（`AbortWithError(c, 429, "RATE_LIMITED", "Too many requests, please slow down and try again later")` + `Retry-After`）。为保证一致性，`abortIQGalleryRateLimited` 直接调用既有 `middleware.AbortWithError`（该函数已导出），**不复制** `abortPanelRateLimited` 的私有实现。

**未登录不计入**：中间件挂在 `jwtAuth` 之后；无 `AuthSubject` 时直接 `c.Next()` 放行（由后续 handler 的鉴权兜底），不计入用户维度计数。

**功能关闭时不参与**：路由组整体不注册（见 §3.10），限流中间件不会被挂载。

### 3.14 并发上限实现选型：Redis 计数租约 + 进程内信号量兜底

**选型：Redis 计数租约为主，进程内 `semaphore` 仅作单实例兜底。**

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| 仅进程内信号量 | 零 Redis 依赖、无 TTL 泄漏 | 多实例下总并发 = N×max_concurrent，防护失效 | ❌ 单独使用 |
| 仅 Redis 计数租约 | 多实例精确 | 每次请求 2 次 Redis 往返（INCR + DECR）；进程崩溃导致租约滞留需 TTL | ✅ 主方案 |
| Redis + 进程内双保险 | 多实例正确 + 进程内快速拒绝，减少 Redis 压力；Redis 故障时仍有单实例兜底 | 需保证两条路径都释放 | ✅ 采用 |

**实现**：

```
请求进入：
  1. 进程内 semaphore（容量 max_concurrent，非阻塞 TryAcquire）
     失败 → 429 + Retry-After: 1
  2. Redis 租约：INCR iqbenchmark:concurrency:{userID}
     if result > max_concurrent → DECR 并 429 + Retry-After: 2
     同时 EXPIRE 90s（每个在途请求续期，避免进程崩溃后永久占用）
  3. defer：DECR + semaphore.Release()
  4. Redis 异常 → fail-open（仅依赖 semaphore，WARN 日志）
```

**为什么租约需要 TTL 而非单纯计数**：进程被 kill -9 时 `DECR` 不执行，纯计数会永久泄漏并锁死该用户。TTL 90s（> 最长请求 60s）保证最坏情况 90s 后自愈；代价是极端情况下短暂的多放行（≤ max_concurrent），在「防刷图」目标下可接受。

**为什么不用 `pg_try_advisory_lock`**：并发保护是热路径（每个详情请求），数据库连接开销远高于 Redis；且 advisory lock 需占住连接，会与连接池冲突。

### 3.15 前端架构（组件不复制两套）

```
frontend/src/features/iq-benchmark/
├── api.ts                  # adminApi / publicApi（两套 client，路径与 DTO 不同）
├── types.ts                # AdminArtifact / PublicArtifact / Case / Task / Run / Settings
├── sanitize.ts             # 复用 @/utils/sanitize + HTML profile 封装
├── compare.ts              # 同题多模型对比的纯逻辑（分组、去重、按题目/时间对齐）
├── filters.ts              # 筛选状态 → query string 的纯逻辑
├── rateLimit.ts            # 429 统一处理（读 Retry-After、禁止自动重试）
├── useArtifactFeed.ts      # 分页 + 单飞 + 防抖 composable
├── components/
│   ├── ArtifactCard.vue    # 卡片（variant: 'readonly' | 'manage'）
│   ├── ArtifactGrid.vue    # 网格/瀑布 + 骨架屏 + 空态
│   ├── ArtifactDetail.vue  # 详情（灯箱 / 全屏 / 下载 / 失败占位）
│   ├── ComparePanel.vue    # 同题多模型横向对比
│   ├── FilterBar.vue       # 分组/模型/题目/时间/状态
│   ├── CaseForm.vue        # 题目编辑（manage only）
│   ├── TaskForm.vue        # 任务配置（manage only）
│   ├── RunTable.vue        # 执行记录（manage only）
│   └── SettingsPanel.vue   # 配置（manage only）
└── __tests__/
    ├── compare.spec.ts
    ├── filters.spec.ts
    ├── sanitize.spec.ts
    ├── rateLimit.spec.ts
    ├── useArtifactFeed.spec.ts
    └── components.spec.ts
```

- `views/user/IqGalleryView.vue`：`variant="readonly"`，双页签（图片 / 题目问答），只渲染 `ArtifactGrid` + `FilterBar` + `ComparePanel` + `ArtifactDetail`。
- `views/admin/IqBenchmarkView.vue`：`variant="manage"`，四个子页签（题目管理 / 检测任务 / 执行记录 / 产出管理与清理），额外挂 `CaseForm` / `TaskForm` / `RunTable` / `SettingsPanel`。
- 共享组件通过 `variant` prop + `emits` 区分能力；`manage` 独有的按钮在 `variant==='readonly'` 时不渲染（不是 `disabled`，避免 DOM 暴露管理能力）。

### 3.16 上游核心文件接触面清单

**后端共 6 个文件**：5 个是「注册一行」级别的新增，第 6 个
（`main.go`）同样是纯追加的一小段启动调用，**均无签名改动、无行为语义改动**。
（第 5 个文件 `image_storage_s3.go` 是**纯新增一个方法**，见下方说明与 §8 第 4 项确认结论；
第 6 个文件 `main.go` 是阶段 6 验收时发现的必要补充，见下表第 6 行。）

| # | 文件 | 改动 | 为什么不可避免 |
|---|---|---|---|
| 1 | `backend/internal/handler/handler.go` | `Handlers` struct 增 1 个字段 `IQBenchmark *iqbenchmark.Handlers` | 现有路由注册函数（`routes/*.go`）统一通过 `*handler.Handlers` 取处理器；不接入则该字段无处可取。**仅加字段**，不触碰任何现有字段。 |
| 2 | `backend/internal/handler/wire.go` | `ProvideHandlers` 增 1 个入参 + `Handlers{}` 字面量增 1 个赋值；`ProviderSet` 增 1 项 `iqbenchmark.ProvideHandlers` | Wire 的构造器签名必须与新字段一致，否则 `wire_gen.go` 编译失败。**纯追加**。 |
| 3 | `backend/internal/server/router.go` | `registerRoutes` 内增 1 行 `routes.RegisterIQBenchmarkRoutes(v1, h.IQBenchmark, adminAuth, jwtAuth, auditLog)` | 路由必须挂到 `/api/v1` 之上；`registerRoutes` 是唯一的挂载点。**纯追加一行**（+2：1 行注释 + 1 行调用）。 |
| 4 | `backend/cmd/server/wire.go` | `wire.Build` 增 1 个 ProviderSet `iqbenchmark.ProviderSet`；`Application` 增 1 个字段 `IQBenchmark *iqmodule.Runtime`；`provideCleanup` 增 1 个入参 `iqRuntime *iqmodule.Runtime` 与 1 个 cleanup 步骤 | 调度器必须有 `Shutdown` 的有界停止入口（硬性要求，禁止构造函数起 goroutine 后无人停止）；`provideCleanup` 是唯一统一停止点。 |
| 6 | `backend/cmd/server/main.go` | `app.PromptAudit.Start` 之后追加 1 段 `app.IQBenchmark.Start`（失败只 WARN） | **阶段 6 发现的必要项**：`Runtime` 必须在 HTTP 就绪后启动。初版遗漏了这一步，导致功能启用后调度器仍不运行（详见 `verification.md` §3.8）。`main.go` 是唯一的启动序列所在。 |
| 5 | `backend/internal/repository/image_storage_s3.go` | `S3ImageStorage` **新增方法** `Delete(ctx, key) error`（约 15 行，不改 `Save`、不改类型字段、不加接口方法） | 清理过期产出必须能删对象存储里的文件，否则「保留 30 天」只在数据库生效、存储成本无界增长。**纯新增方法**：`ImageStorage` 接口签名保持只有 `Save`，因此既有两个实现（`S3ImageStorage` 与测试 fake）与全部调用点零改动。 |

**第 5 项的实现约束（已确认：允许加 delete，但必须纯增量）**：

- `service.ImageStorage` 接口**不新增** `Delete` —— 加接口方法会强制所有实现者与 fake 同步修改，属于改签名。
- 本模块在自己的包里定义窄接口 `type BlobDeleter interface { Delete(ctx context.Context, key string) error }`，对注入的 `service.ImageStorage` 值做类型断言；断言失败即降级为「只软删数据库行 + WARN 日志」，行为与「没有这项能力」等价。
- 因此上游唯一改动就是 `S3ImageStorage` 上多一个方法，**任何既有调用路径的行为不变**。

**新增（非接触面，即本功能自己的文件，不触碰上游）**：

后端：
- `backend/migrations/249_iq_benchmark_gallery.sql`（新文件）
- `backend/ent/schema/iq_benchmark_{case,task,run,artifact}.go`（新文件）+ `ent/` 重新生成产物
  （`ent/iqbenchmark{case,task,run,artifact}(_update|_query|_create|_delete)?.go`、
  `ent/iqbenchmark{case,task,run,artifact}/**`、`ent/migrate/schema.go`、`ent/runtime/runtime.go`）
- `backend/internal/iqbenchmark/**`（新目录，完整清单见下）
- `backend/internal/server/routes/iq_benchmark.go`（新文件，注册函数本体）
- `backend/migrations/iq_benchmark_gallery_migration_test.go`（新文件，迁移守卫单测）

`backend/internal/iqbenchmark/` 实际文件清单：

| 路径 | 职责 |
|---|---|
| `types.go` | 常量、哨兵错误、领域结构体 |
| `internal_agent.go` | 内部账号判定与幂等初始化（advisory lock） |
| `agent_store.go` | 用上游 `service.UserRepository` / `service.APIKeyRepository` 实现窄接口 |
| `agent_locker.go` | 把 `repository.TryAdvisoryLock` 适配为 `LockAcquirer` |
| `settings.go` | `iq_benchmark_settings` JSON 配置 + 60s 缓存 + `Normalize()` 夹紧 |
| `sanitize/svg.go`、`sanitize/classify.go` | SVG/HTML 消毒与产出类型判定 |
| `repository/repository.go`、`repository/lease.go` | Ent + 原生 SQL 数据访问、租约与 advisory lock |
| `service/service.go`、`guard.go`、`invoke.go`、`artifact_service.go`、`run_service.go`、`gallery_service.go` | 熔断、网关调用、产出落库、run 编排、管理侧编排 |
| `scheduler/scheduler.go` | 定时扫描、领取、续期、清理节流 |
| `handler/{handlers,dto,handlerfunc,admin_handler,user_handler}.go` | 路由处理器与公开白名单 DTO |
| `ratelimit/{rate_limiter,redis_counter}.go` | 两档限流 + 并发上限 |
| `module/module.go` | 独立的 `wire.ProviderSet` 与各适配器（避免 `iqbenchmark`↔`handler` 循环依赖） |

前端（新文件）：
- `frontend/src/features/iq-benchmark/**`
  （`types.ts`、`api.ts`、`sanitize.ts`、`filters.ts`、`rateLimit.ts`、`useArtifactFeed.ts`、
  `availability.ts`、`userFilter.ts`，9 个 `.vue` 组件，7 个 `__tests__/*.spec.ts`）
- `frontend/src/views/user/IqGalleryView.vue`
- `frontend/src/views/admin/IqBenchmarkView.vue`
- `frontend/src/i18n/locales/{zh,en}/iqBenchmark.ts`

**前端接触面（4 处，注册级别）**：

| # | 文件 | 改动 | 为什么不可避免 |
|---|---|---|---|
| 1 | `frontend/src/router/index.ts` | 增 2 条路由记录（懒加载 + `meta.requiresAuth` / `meta.requiresAdmin` / `titleKey`），+24 行 | 路由表是唯一挂载点 |
| 2 | `frontend/src/components/layout/AppSidebar.vue` | 用户侧导航数组增 1 项、管理员侧导航数组增 1 项、增 1 个可用性探测，+9 行 | 侧栏是唯一导航入口；`enabled=false` 时靠探测隐藏 |
| 3 | `frontend/src/i18n/locales/{zh,en}/index.ts` | 各增 1 行 import + 1 行展开，各 +2 行 | 各语言模块的汇总点 |
| 4 | `frontend/src/views/admin/UsersView.vue` | 增 import、1 个 `useHideInternalAccounts()`、1 个「隐藏内部账号」开关按钮、`sortedUsers` 前置过滤 1 行（另 2 行缩进调整），+24 −2 | **确认项 4 的落点**：要求「前端加开关、默认开启、不改上游查询」。开关必须挂在用户列表页，这是唯一位置 |

> 第 4 项是开工前确认项 4 的直接产物，原设计只列了前 3 项。
> 它的 `−2` 行是 `sortedUsers` 里 `if (!s) return users.value` /
> `return [...users.value]` 改成引用新局部变量 `visible` 造成的缩进调整，
> **不是删除行为**：排序语义（含稳定排序）完全不变。

**i18n 范围（已确认）**：只做 `zh` + `en` **两语**，不新增 `ja`。因此 `frontend/src/i18n/index.ts`（`LocaleCode` 联合类型与 `localeLoaders`）**完全不动**，不构成接触面。原任务书的「三语」要求在本次实现中按该确认收窄。


---

## 4. Risks / Trade-offs

| 风险 | 影响 | 缓解 |
|---|---|---|
| 定期批量检测持续消耗额度 | 成本失控 | 默认关闭 + `daily_cost_budget` + `max_task_concurrency` + 配额熔断 |
| 模型输出为不可信内容（SVG/HTML XSS、外链信标） | 用户侧 XSS / IP 泄漏 | 入库静态扫描 + 前端 DOMPurify + 优先 `<img>` + 阻断类直接 `blocked` |
| HTTP 自调本机网关引入环回延迟与端口耦合 | 部署配置错误导致全量失败 | `internal_gateway_base_url` 可覆盖 + 启动时探活 + 失败明确 `error_code='gateway_unreachable'` 而非静默 |
| `users` 表用 `notes` 标记隔离 | 管理员手工改 notes 会破坏判定 | `IsInternalAgent` 单点判定 + 管理员 UI 对该账号的 notes 字段只读（后端 `Update` 时若 `IsInternalAgent` 则拒绝改 notes） |
| 内部账号可能被 `content_moderation` 自动封号 | 检测全挂且静默 | 每轮调度前 + 每次 run 前校验 `status`，非 active 立即 `account_disabled` 熔断 + ERROR 告警 |
| 统计入口默认仍包含检测流量 | 管理员看到的「总收入/调用量」含内部流量 | 提供 `user_id`/`api_key_id` 过滤能力 + 在前端 usage 页给管理员加「排除内部账号」快捷筛选（不改默认查询）；需改默认查询的入口列为可选增强（§7） |
| 对象存储未配置 | 图片作品无法展示 | 降级为失败作品 + 明确 `storage_disabled`，不阻塞文本类作品展 |
| 对象存储实现不支持 `Delete` | 数据库行删了但对象文件残留 | 窄接口 `BlobDeleter` + 类型断言；不支持时只软删 DB 行并 WARN（不静默，日志可发现问题） |
| 单 run 产出量爆炸 | 表与存储膨胀 | `max_artifacts_per_run` + `max_artifacts` + `retention_days` + `EnsureCapacity` |


## 5. Migration Plan

### 阶段 0：文档与基线（本次交付第 1 阶段，停在确认）

调研记录 + openspec change 全套（proposal / design / specs / tasks / verification）。

### 阶段 1：数据层

- `249_iq_benchmark_gallery.sql`（4 表 + 索引 + CHECK + 部分唯一索引）
- 4 个 Ent schema + 重新生成（`go generate ./ent`）
- repository（Ent 实现 + 筛选查询 + 租约领取）
- 单测：CRUD、筛选 SQL 生成、部分唯一索引冲突语义、软删除

### 阶段 2：内部账号与计费链路

- `IsInternalAgent` + 幂等初始化（advisory lock）
- settings 结构与缓存
- 三重熔断守卫
- 统计排除能力（按 `user_id`/`api_key_id` 过滤的查询封装，供 overview 与前端筛选）
- 单测：判定、幂等、扣费隔离、熔断

### 阶段 3：调度与执行引擎

- Scheduler（Start/Shutdown、租约、抖动）
- run 编排 + invoke（chat / images）+ 超时与响应体上限
- sanitize（Go 正则扫描）+ classify
- artifact 落库（图片走 `ImageResultUploader`）+ 失败入库 + 重试语义
- 清理任务
- 单测：不重复领取、抖动/间隔校验、快照冻结、失败入库、XSS、截断、`enabled=false` 零副作用、多实例、清理

### 阶段 4：API 与限流

- 管理 API（cases / tasks / runs / artifacts / overview / settings）+ 管理员审计
- 用户只读 API + 公开白名单 DTO
- `IQGalleryRateLimiter`（两档 + 并发）
- 单测：白名单断言、限流矩阵、429 一致性、`page_size` 上限

### 阶段 5：前端

- feature 目录组件 + 2 个页面 + 路由 + 侧栏 + 中英双语 i18n
- 单测：筛选、对比、消毒、429、路由权限

### 阶段 6：验证与灰度

跑通全部命令、贴真实输出、更新 `verification.md`；灰度顺序：迁移上线（关闭态）→ 管理员开启 + 配置题目/任务 → 手工 run-now 验证 → 开启用户侧。

### 回滚

1. `PUT /admin/iq-benchmark/settings {enabled:false}` → 调度停止、路由不可达（**首选，无需回滚代码**）。
2. 代码回滚：revert 提交；4 张新表保留（无上游依赖，不影响任何既有功能）。
3. 数据回滚：`DROP TABLE iq_benchmark_artifacts, iq_benchmark_runs, iq_benchmark_tasks, iq_benchmark_cases;`（仅在新表，无外键指向既有表）。
4. 内部账号：`DELETE FROM api_keys WHERE user_id = (SELECT id FROM users WHERE notes LIKE 'iq-benchmark:internal-agent%'); DELETE FROM users WHERE notes LIKE 'iq-benchmark:internal-agent%';`（手工，需确认）。

## 6. 待确认决策（Resolved Decisions）

见本文档 §9「开工前 5 个问题的回答」与 `verification.md` §「开工前确认项」。

---

## 7. 检测流量的统计打点与排除方案

### 7.1 事实基础

`usage_logs` 的既有列（`ent/schema/usage_log.go`）包含 `user_id`、`api_key_id`、`account_id`、`group_id`、`model`、`requested_model`、`request_id`、`created_at`、`user_agent`，**没有** `source` / `is_internal` 之类的来源列。检测流量写入时天然携带：

- `user_id` = 内部账号 ID
- `api_key_id` = 专用 Key ID
- `user_agent` = `sub2api-iq-benchmark/1.0`

因此「识别检测流量」有**零 schema 变更**的两个精确键：`user_id`、`api_key_id`。

### 7.2 必须排除检测流量的统计入口（逐条清单）

按「是否必须改上游查询」分类。判定标准：该入口的默认查询**是否会把内部账号计入用户可见/管理员决策所依赖的数值**。

| # | 入口 | 位置 | 默认查询是否含检测流量 | 需改上游查询？ |
|---|---|---|---|---|
| 1 | `GetGlobalStats` | `repository/usage_log_repo_stats.go` | ✅ 含（全表聚合，无 user 过滤） | ❌ 否（可选增强） |
| 2 | `GetStatsWithFilters` | 同上 | ⚠️ 取决于调用方是否传 `user_id`/`api_key_id` | ❌ 否（已有过滤能力） |
| 3 | `GetDashboardStats` / `GetDashboardStatsWithRange` | `usage_log_repo_dashboard.go` | ✅ 含（全表） | ❌ 否 |
| 4 | `GetUserStats` / `GetUserDashboardStats` / `GetAPIKeyDashboardStats` | 同上 | ❌ 不含（按 user/key 过滤，内部账号仅在其自身维度可见） | ❌ 否 |
| 5 | `GetModelStats*` | `usage_log_repo_stats.go` | ✅ 含（模型维度聚合，内部流量会抬高目标模型的调用数与成本） | ❌ 否 |
| 6 | `GetGroupStats*` | 同上 | ✅ 含（分组维度，同上） | ❌ 否 |
| 7 | `GetBatchUserUsageStats` / `GetBatchAPIKeyUsageStats` | 同上 | ✅ 含（批量，但内部账号仅在返回数组里多一条） | ❌ 否（前端可过滤） |
| 8 | **`GetUserSpendingRanking`** | `usage_log_repo_trend.go` | ✅ **含，且会把内部账号当作真实用户排进「消费排行榜」** | ⚠️ **建议改**（见 §7.3） |
| 9 | `dashboard_aggregation_repo.go` 小时/日汇总 | `repository/dashboard_aggregation_repo.go` | ✅ 含，且**预聚合表无法事后按 user_id 过滤** | ⚠️ **建议改**（见 §7.3） |
| 10 | `usage_dashboard_hourly_users` / `daily_users` 活跃用户去重表 | 同上 | ✅ 含（内部账号会进入活跃用户数 UV） | ⚠️ **建议改**（见 §7.3） |
| 11 | `custom_group_usage_rollup_repo.go` 分组日汇总 | 同上 | ✅ 含 | ❌ 否（可选增强） |
| 12 | `ops_repo_*.go`（`buildUsageWhere` 只按 `created_at`/`group_id`/`platform` 过滤） | `repository/ops_repo_*.go` | ✅ 含 | ❌ 否（运维视角，含内部流量合理） |
| 13 | `channel_monitor_v2_aggregation.go` 指标/用户指标/直方图汇总 | 同上 | ✅ 含 | ❌ 否（该聚合有自己的 `channel_monitor` 维度，不读 iq 流量为主） |
| 14 | `api_key_repo.go` 中 `FROM usage_logs` 的查询 | 同上 | ❌ 不含（按 key 维度） | ❌ 否 |
| 15 | 管理员用户列表 `UserRepository.ListWithFilters` | `repository/user_repo*.go` | ✅ **含，内部账号会出现在用户列表**（该查询甚至用 `NotesContainsFold` 搜索） | ⚠️ **建议改**（见 §7.3） |

### 7.3 方案对比：只按 `user_id` 过滤（不改默认查询） vs 改上游默认查询

**方案 A（推荐，本变更采用）：不动上游默认查询，在「读侧」过滤**

- 后端：本模块新增 `repository.ExcludeInternalAgentWhere(...)` 辅助，但**不被上游查询引用**；只在 `iqbenchmark` 自己的 overview 聚合里使用。
- 前端：管理端 usage / dashboard / 排行榜页面增加一个「排除内部检测账号」开关（前端过滤 or 传 `user_id` 之外的排除参数）。
- 代价：管理员若不开开关，看到的全局数字仍含检测流量。
- **优点**：`upstream/main` 合并时**零冲突**；不改任何上游查询语义（符合硬约束「不得改变上游核心文件行为语义」）。

**方案 B：改上游默认查询，统一排除**

- 需在 3 处上游文件注入排除条件（`usage_log_repo_stats.go`、`usage_log_repo_dashboard.go`、`dashboard_aggregation_repo.go`），并在预聚合任务里加过滤。
- 代价：① 预聚合表（`usage_dashboard_hourly_users` 等）已落库的历史数据**无法事后拆分**，改查询后历史与新数据口径不一致；② 每处改动都是上游高频修改的文件，merge 冲突概率高；③ 违反「不得改变行为语义」，因为这会让所有既有管理员看到的历史数字发生变化。
- **不采用。**

**方案 C（折中，作为可选增强登记）：让检测流量"不计入"聚合源头**

- 在 `dashboard_aggregation_repo.go` 的预聚合 SQL 里加 `AND user_id <> $internalUserID`（1 处、1 行），使新产生的预聚合数据天然干净。
- 代价：1 处上游改动 + 历史数据口径不一致。
- **结论**：本变更**不做**，但作为「已知局限」记录在 `verification.md`；并在管理端提供方案 A 的开关作为兜底。

**具体落地（方案 A）**：

1. **排行榜（#8）**：前端 trend/ranking 组件在「排除内部账号」开关打开时，用 `user_id NOT IN (...)` 的客户端过滤 —— 由于排行榜返回数组，前端过滤可行且零后端改动。
2. **预聚合（#9/#10）**：在管理端 dashboard 卡片上增加角标提示「含内部检测流量（约 X%）」，X 由本模块 overview 提供（`iqbenchmark` 自己按 `api_key_id` 统计检测流量占比）。这是**信息透明**而非改口径。
3. **管理员用户列表（#15）**：前端在用户列表增加「隐藏内部账号」默认开启的开关（`UserRepository.ListWithFilters` 已支持 `user_id` 之类过滤参数即可传 exclude，若不支持则前端按 `notes` 前缀过滤 —— `UserFromServiceAdmin` 已含 `notes`）。
4. **本模块自身所有统计**：一律硬编码 `WHERE user_id = internalUserID` / `WHERE api_key_id = internalKeyID`，既用于成本回采，也用于 overview 的「检测成本」卡。

### 7.4 判定检查项（写入测试）

- `TestGlobalStatsStillCountsInternalAgent`（断言方案 A 的事实：上游默认查询**确实**会包含它）—— 用于防止未来有人误以为已排除。
- `TestIQOverviewCostUsesAPIKeyFilter`（本模块统计**只**命中内部 Key）。
- `TestPublicGalleryNoInternalIdentifiers`（公开 DTO 无 `user_id`/`api_key_id`）。
- `TestInternalAgentAbsentFromUserScopedStats`（`GetUserStats(internalUserID)` 只返回该账号自身数据，不影响他人）。

---

## 8. 与任务书的偏差与需确认项

| # | 任务书要求 | 仓库现状 | 处理 | 确认结论 |
|---|---|---|---|---|
| 1 | i18n 三语 `zh` / `en` / `ja` | `LocaleCode = 'en' \| 'zh'`，`locales/` 仅 `en`/`zh`；`localeKeyCompleteness.spec.ts` 断言 en/zh 键集一致 | **确认只做 `zh` + `en`**：不新增 `ja`，`frontend/src/i18n/index.ts` 完全不动，接触面不增加 | ✅ 已确认（收窄为两语） |
| 2 | 管理员接口「未启用时返回 not-enabled」 | 既有 feature guard 惯例返回 503 + code | 采用 503 + `IQ_BENCHMARK_DISABLED`，与 `channelMonitorAdminFeatureGuard` 一致 | 沿用惯例，无需确认 |
| 3 | 用户侧「未启用时路由不可达」 | 既有惯例是 404 或直接不注册 | 采用 404（不注册路由亦等价；选 404 便于前端区分） | 沿用惯例，无需确认 |
| 4 | 对象存储产出清理 | `ImageStorage` 只有 `Save`，无 `Delete` | **确认允许加 delete，但必须纯增量**：只在 `S3ImageStorage` 上新增 `Delete` 方法，`service.ImageStorage` 接口签名不变；本模块用窄接口 `BlobDeleter` + 类型断言；断言失败即降级「只软删 DB 行 + WARN」 | ✅ 已确认（纯增量实现） |
| 5 | LLM 裁判 | 无既有实现 | **确认不实现**：`scoring` 保留 `llm_judge` 枚举位与 `llm_judge_enabled` 字段但恒为 `false`，不写任何裁判调用代码 | ✅ 已确认（不实现） |
| 6 | 管理员用户列表显示内部账号 | `UserFromServiceAdmin` 已含 `notes` | **确认不改上游查询**：前端加「隐藏内部账号」开关、默认开启，按 `notes` 前缀在客户端过滤 | ✅ 已确认（前端开关） |


---

## 9. 开工前 5 个问题的回答

### 9.1 开源调研结论与复用/License 决策

**完整调研记录**：`research.md`（含每个候选的 URL、License、最近提交、技术栈、可复用部分、集成成本、结论与理由，以及核证方式）。

**结论摘要**：

1. 基准出处 `simonw/pelican-bicycle`（218★）**无 License**（GitHub API `license: null`，24 个文件中无 LICENSE）→ **仅参考产品思路，代码与 20 个示例 SVG 一律不拷**。
2. 无可直接复用的 Go+Vue 同栈实现；高同构候选（lmring / arena-hard-auto / OpenCompass / lm-eval-harness / FastChat / SVGenius）均为 Python 或 Next.js 独立服务 → **架构红线排除**（不引入 Python 服务、独立运行时）。
3. **唯一代码级复用（2 处，均 MIT）**：
   - `delirious6423/pelican-bench`（MIT，4★）`harness/pelican.py` 的 `_FLAGS` 9 类危险构造正则表 → 改写为 Go `regexp`，用于入库前静态扫描。
   - `joeseesun/llm-case-benchmark`（MIT，9★）`public/output-classifier.js` 的输出形态判定算法（完整/可恢复 SVG、覆盖率阈值 0.72）→ **重写为 Go**，其 57KB Node+Express+SQLite `server.js` 禁止复用。
4. **前端零新增依赖**：`dompurify ^3.4.17` 已是项目依赖（`frontend/src/utils/sanitize.ts` 已封装 `sanitizeSvg`）；Element Plus / vue-easy-lightbox / splitpanes 虽 MIT 但**不引入**（项目当前无 UI 组件库，为单功能引入会改变整体依赖面）。
5. **无 License 一律放弃**：`johnbean393/SVGBench`、`bilikaz/compare-llms`、`Yubo0826/img-comp-slider`。
6. **License 兼容性**：主项目 LGPL-3.0；MIT/Apache-2.0 兼容，需在 `THIRD_PARTY_NOTICES.md` 保留两处 MIT 版权行。
7. **安全结论**：SVG/HTML 必须按不可信输入处理；采用「Go 静态扫描打标 + 前端 DOMPurify 消毒 + 优先 `<img>` 渲染 + 不做服务端栅格化」四层。

**最终路线：参考设计 + 自研实现**。

### 9.2 必须排除检测流量的统计入口完整清单 + 是否需改上游查询

见 §7.2 的 15 项逐条清单与 §7.3 的方案对比。**核心结论**：

- **需改上游默认查询的入口：0 个（必需）；3 个（可选增强，本变更不做）** —— 即 `GetUserSpendingRanking`、`dashboard_aggregation_repo.go` 预聚合、`usage_dashboard_hourly_users/daily_users` 活跃用户去重表。
- **不需要改的前提**：检测流量天然携带 `user_id = internal_user_id`、`api_key_id = internal_api_key_id`、`user_agent = sub2api-iq-benchmark/1.0`，三个键足以精确识别，**无需给 `usage_logs` 加列**。
- **方案对比**：方案 A「只按 `user_id` 过滤、不改默认查询」→ **merge 成本 0**、不改变任何既有数字口径，但管理员不开开关时全局数字仍含检测流量；方案 B「改上游默认查询」→ merge 成本高（3 个上游高频文件）+ 预聚合历史数据口径不可回溯 + 违反「不改变既有行为语义」。**采用方案 A**，并把方案 C（预聚合源头过滤 1 行）登记为可选增强与已知局限。

### 9.3 需修改的上游核心文件最小清单

**必需：后端 5 个文件（4 个「注册一行」级别追加 + 1 个「纯新增方法」），无签名改动、无行为语义改动。**

| # | 文件 | 具体改动 | 为什么不可避免 |
|---|---|---|---|
| 1 | `backend/internal/handler/handler.go` | `Handlers` struct 增 1 字段 `IQBenchmark *iqbenchmark.Handlers` | 所有 `routes/*.go` 都经 `*handler.Handlers` 取处理器，不接入则无处可取。仅加字段。 |
| 2 | `backend/internal/handler/wire.go` | `ProvideHandlers` 增 1 入参 + 字面量增 1 赋值；`ProviderSet` 增 1 项 | Wire 构造器签名必须匹配，否则 `wire_gen.go` 编译失败。纯追加。 |
| 3 | `backend/internal/server/router.go` | `registerRoutes` 内增 1 行 `routes.RegisterIQBenchmarkRoutes(...)` | `/api/v1` 唯一挂载点。纯追加一行。 |
| 4 | `backend/cmd/server/wire.go` | `wire.Build` 增 `iqbenchmark.ProviderSet`；`provideCleanup` 增 1 入参 + 1 个停止步骤 | 调度器必须有有界停止入口（禁止构造函数起 goroutine 后无人 Stop）。 |
| 5 | `backend/internal/repository/image_storage_s3.go` | `S3ImageStorage` 新增方法 `Delete(ctx, key) error` | 清理过期产出必须能删对象文件，否则「保留 N 天」只在 DB 生效。**不改 `Save`、不改字段、不改接口**，因此对上游零语义影响。 |

**已确认不需要改动的文件（原计划中的条件性项）：**

| 文件 | 结论 |
|---|---|
| `frontend/src/i18n/index.ts` | **完全不动**：i18n 只做 `zh` + `en`，不新增 `ja`，故 `LocaleCode` 联合类型与 `localeLoaders` 均无需修改。 |
| `backend/internal/service/image_storage.go` | **完全不动**：`ImageStorage` 接口保持只有 `Save`；删除能力由 `S3ImageStorage` 的额外方法 + 本模块窄接口类型断言提供。 |

**结论**：后端 **6** 个、前端 **4** 个（见 §3.16 / §10）。≤3 的红线被突破 4 个，理由见下。

> **对原设计的两处更正**（均为实施/验收阶段的实测发现，非范围扩张）：
> 1. 前端接触面原列 3 处，实际 4 处 —— 第 4 处是
>    `frontend/src/views/admin/UsersView.vue`（确认项 4「隐藏内部账号」开关的落点）。
>    该项在设计阶段被记在「确认项」里，未同步计入接触面清单。
> 2. 后端接触面原列 5 处，实际 6 处 —— 第 6 处是 `backend/cmd/server/main.go`
>    里的一段 `app.IQBenchmark.Start`。初版只做了「构造 + 停止」，
>    漏了「启动」，导致 `enabled=true` 后调度器与内部账号都不会运行（功能静默失效）。
>    详见 `verification.md` §3.8。这一处是修复真实缺陷所必需的最小改动。

**为什么需要 5 个而不是 ≤3**：将这些改动合并到 3 处在技术上不可行 ——
- #1 与 #2 是同一个 Wire 依赖图的构造器签名与结构体字段，二者必须同时改（否则编译失败），但它们分属**两个文件**（`handler.go` 定义结构体、`wire.go` 定义构造器）。
- #3 是路由挂载点，`registerRoutes` 在 `router.go`，与 handler 装配分离。
- #4 是进程生命周期（`provideCleanup`），在 `cmd/server/wire.go`，与 HTTP 层的 Wire 图分离。
- #5 是对象存储实现的删除能力：放在别处无法访问 `S3ImageStorage` 的私有 `client`/`bucket` 字段（除非再加一个上游导出访问器，那会变成第 6 处改动且风险更高）。
- 若强行合并（例如让 `iqbenchmark` 自己在 `init()` 里注册路由），会违反本项目「显式 DI、无隐式全局副作用」的既有约定，且使 `Shutdown` 失去统一控制点 —— 代价远大于多改 1 个文件。

**每个文件的改动都是纯追加**（新增字段/入参/调用/方法），没有修改任何既有语句或既有方法体，因此**上游对这些文件的修改不会与本变更产生语义冲突**，最坏情况是行级 merge 冲突（可人工解决）。

**实测 diff 佐证**（`git diff --stat f688e8999e...HEAD`，11 个上游文件）：

```
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

那 `3 deletions` 来自两处，均已逐行核对，**没有任何既有行为被删除或改写**：
- `UsersView.vue`（2 行）：`sortedUsers` 里 `if (!s) return users.value` /
  `return [...users.value]` 改为引用新局部变量 `visible`，排序语义不变；
- `wire.go`（1 行）：`provideCleanup` 的入参 `iqScheduler *scheduler.Scheduler`
  换成 `iqRuntime *iqmodule.Runtime`，停止动作由直接调 `Scheduler.Shutdown`
  改为调 `Runtime.Shutdown`（后者内部仍调同一个 `Scheduler.Shutdown`，
  且额外负责顺序与幂等）。这是本功能自己的接线，不属于上游语义。

**零改动的确认**：不给 `users` 加列、不给 `usage_logs` 加列、不扩 `users_signup_source_check`、不加 role 枚举、不改内容审核/渠道监控/批量生图/账务配额语义、不改既有路由路径、不改 `config.yaml` 与 `config.Config` 结构体、不改任何既有统计查询。


### 9.4 限流器落点

**选择：`IQGalleryRateLimiter` 放在新模块 `backend/internal/iqbenchmark/ratelimit/iq_gallery_rate_limiter.go`。**

| 维度 | `internal/server/middleware/`（新文件） | `internal/iqbenchmark/ratelimit/`（本方案） |
|---|---|---|
| 上游 merge 冲突面 | 文件本身新增不冲突，但**同目录被上游持续改动**（`panel_rate_limit.go`、`gateway_remediation*.go` 等），且需改 `middleware/wire.go` 的 `ProviderSet`（上游也在改） | **零冲突**：新目录 + 由 `iqbenchmark.ProviderSet` 内部提供，不碰 `middleware/wire.go` |
| 依赖获取 | 由 `middleware.ProviderSet` 注入 `*redis.Client`、`settingService` | 由 `iqbenchmark.ProviderSet` 自行声明入参 `*redis.Client`、`settingService`（Wire 已能提供，无新增 provider） |
| 语义归属 | 面板限流与作品展限流混在同一目录，容易误共享 `panel:` 键前缀 | 键前缀 `iqgallery:` 与模块同处一地，不易误用 |
| 复用既有能力 | 直接可用 `abortPanelRateLimited`（私有，不可跨包） | 调用**已导出**的 `middleware.AbortWithError` + `middleware.NewRateLimiter`，429 体与面板完全一致 |
| 维护预期 | 后续维护者倾向在同目录继续加逻辑 → 冲突概率随上游演进上升 | 完全隔离，长期 merge 成本恒定 |

**实际差异（对 `upstream/main` 合并的影响）**：放入 `internal/server/middleware/` 会在**每次上游改动该目录或其 `wire.go` 时**产生潜在冲突点，且新增的 `middleware/wire.go` 一行属于上游高频变更文件；放入新模块后，`upstream/main` 合并时本功能**只在 §9.3 的 6 个后端文件 + §3.16 的 4 个前端文件上可能产生行级冲突**，其余全部为新目录，冲突面与上游演进无关。

**关键实现细节**：底层的 `middleware.NewRateLimiter(redisClient)`（`internal/middleware/rate_limiter.go`）与 `middleware.AbortWithError(c, status, code, message)`（`internal/server/middleware/middleware.go:78`）**都是导出符号**，因此新模块可直接复用而无需修改这两处文件。429 响应体与 `abortPanelRateLimited`（`panel_rate_limit.go:174`，私有）**逐字节一致**：`Retry-After` 头 + `{"code":"RATE_LIMITED","message":"Too many requests, please slow down and try again later"}`，只是由本模块自己组装（3 行），不复用那个私有函数。


### 9.5 并发上限实现选型：Redis 计数租约 vs 进程内信号量 + Redis 兜底

**选择：进程内 `semaphore`（快速拒绝层）+ Redis 计数租约（跨实例正确层）双保险。**

**权衡推理**：

| 方案 | 多实例正确性 | Redis 依赖 | 崩溃泄漏 | 每请求成本 | 结论 |
|---|---|---|---|---|---|
| 仅进程内信号量 | ❌ 总并发 = N × cap | 无 | 无 | 0 | 单独使用不可接受 |
| 仅 Redis 计数 | ✅ | 强依赖（故障即失效或全放行） | 有（`DECR` 未执行 → 永久占用） | 2 次往返（INCR/DECR） | 需 TTL 补救 |
| **双保险（采用）** | ✅ | 弱依赖（故障时降级为单实例保护，fail-open） | 有，但 TTL 90s 自愈 + 进程内层兜底 | 2 次往返 | ✅ |

**为什么先查进程内再查 Redis（顺序而非并行）**：进程内 `TryAcquire` 失败时**不消耗 Redis 配额**，直接 429。这样在单实例过载时能避免把压力传导给 Redis，也让 Redis 只承担「跨实例计数」这一必要职责。

**为什么 Redis 计数必须配 TTL**：进程被 `kill -9` 时 `defer DECR` 不执行，纯计数会永久泄漏并锁死该用户。TTL 90s（> 最长请求 60s）保证最坏情况 90s 自愈；代价是极端情况下短暂多放行 ≤ `max_concurrent`，在「防刷图」目标下可接受。

**为什么不用 `pg_try_advisory_lock`**：并发保护是热路径（每个详情请求），数据库连接开销远高于 Redis，且 advisory lock 需占住连接会与连接池冲突。

**为什么不用纯令牌桶（`golang.org/x/time/rate`）**：令牌桶是进程本地的，跨实例无效；且 `x/time/rate` 需额外依赖（当前未在 `go.mod`）。

**与两档 RPM 限流的关系**：RPM 限流（`read`/`heavy`）控制**请求速率**，并发上限控制**同时在途数**，两者独立且互补 —— 速率限流挡不住「1 秒内并发发起 50 个慢请求」这类占用型滥用，并发上限正是为此。

---

## 10. 上游接触面汇总表（供 review 逐项核对）

| 类型 | 路径 | 级别 |
|---|---|---|
| 改（追加 1 字段） | `backend/internal/handler/handler.go` | 注册一行 |
| 改（追加 1 入参 + 1 赋值 + 1 ProviderSet 项） | `backend/internal/handler/wire.go` | 注册一行 |
| 改（追加 1 行调用） | `backend/internal/server/router.go` | 注册一行 |
| 改（追加 1 ProviderSet + 1 Application 字段 + 1 Cleanup 入参/步骤） | `backend/cmd/server/wire.go` | 注册一行 |
| 改（**纯新增一个方法** `Delete`，不改接口与 `Save`） | `backend/internal/repository/image_storage_s3.go` | 新增方法（无行为语义改动） |
| 改（追加 1 段启动调用） | `backend/cmd/server/main.go` | 注册一行（阶段 6 发现，见 `verification.md` §3.8） |
| 改（追加 2 条路由） | `frontend/src/router/index.ts` | 注册一行 |
| 改（追加 2 条导航项 + 1 个可用性探测） | `frontend/src/components/layout/AppSidebar.vue` | 注册一行 |
| 改（各追加 1 import + 1 展开） | `frontend/src/i18n/locales/{zh,en}/index.ts` | 注册一行 |
| 改（追加 1 import + 1 开关 + 1 过滤行） | `frontend/src/views/admin/UsersView.vue` | 注册一行（确认项 4 落点） |
| 新增 | `backend/internal/iqbenchmark/**` | 新目录 |
| 新增 | `backend/internal/server/routes/iq_benchmark.go` | 新文件 |
| 新增 | `backend/migrations/249_iq_benchmark_gallery.sql` | 新文件 |
| 新增 | `backend/ent/schema/iq_benchmark_{case,task,run,artifact}.go` | 新文件 |
| 新增 | `frontend/src/features/iq-benchmark/**` | 新目录 |
| 新增 | `frontend/src/views/user/IqGalleryView.vue`、`frontend/src/views/admin/IqBenchmarkView.vue` | 新文件 |
| 新增 | `frontend/src/i18n/locales/{zh,en}/iqBenchmark.ts` | 新文件 |
| 新增 | `openspec/changes/add-iq-benchmark-gallery/**`（含 `research.md`）、`backend/internal/iqbenchmark/THIRD_PARTY_NOTICES.md` | 新文件 |

**总计：后端 6 个既有文件（5 个注册级 + 1 个纯新增方法）、前端 4 个既有文件（均为注册级）。**
实测 diff：`11 files changed, 108 insertions(+), 3 deletions(-)`
（3 行删除：`UsersView.vue` 2 行缩进调整 + `wire.go` 1 行把 `Scheduler` 换成 `Runtime`，
均无行为语义改动）。

不涉及的既有文件：`frontend/src/i18n/index.ts`（不做 `ja`）、
`backend/internal/service/image_storage.go`（接口不变）、
`frontend/src/utils/featureFlags.ts`（改用本地可用性探测）、
以及全部统计/计费/用户查询文件。

