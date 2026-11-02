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
- [ ] 1.8 集成测试：部分唯一索引冲突、`SKIP LOCKED` 不重复领取、租约过期可接管（需真实 PG，随阶段 6 一起跑）


## 阶段 2：内部账号与计费链路（已完成）

- [x] 2.1 `internal_agent.go`：`IsInternalAgent` + `EnsureInternalAgent`（advisory lock 幂等 + 窄接口解耦）
- [x] 2.2 `settings.go`：`Settings` + `IQGalleryRateLimitSettings` + 60s 缓存 + `Normalize()` 硬上限夹紧
- [x] 2.3 `service/guard.go`：三类熔断（配额 / 预算 / 账号禁用）
- [x] 2.4 统计过滤能力：**0 处上游查询改动**（结论见 `verification.md` 第一节；3 项可选增强仅记录未实施）
- [x] 2.5 单测：判定函数（含 nil）、间隔/抖动校验、三类熔断矩阵、预算查询失败必须报错
- [ ] 2.6 集成测试：扣内部账号余额、真实用户余额不变、配额耗尽熔断、超预算熔断、账号禁用熔断（**需真实 PostgreSQL**）
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
- [ ] 3.10 集成测试：多实例并发、租约接管、重跑失败项（**需真实 PostgreSQL**）

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

- [ ] 5.1 `features/iq-benchmark/` 基础：`api.ts`、`types.ts`、`sanitize.ts`、`rateLimit.ts`、`filters.ts`、`compare.ts`、`useArtifactFeed.ts`
- [ ] 5.2 共享组件：`ArtifactCard`、`ArtifactGrid`、`ArtifactDetail`、`ComparePanel`、`FilterBar`（`variant` 区分只读/可管理）
- [ ] 5.3 管理组件：`CaseForm`、`TaskForm`、`RunTable`、`SettingsPanel`
- [ ] 5.4 `views/user/IqGalleryView.vue`（双页签、`requiresAuth`）
- [ ] 5.5 `views/admin/IqBenchmarkView.vue`（四子页签、`requiresAdmin`）
- [ ] 5.6 路由注册 2 条 + 侧栏 2 条 + feature flag（`opt-in`）+ `enabled=false` 隐藏
- [ ] 5.7 中英双语 i18n：`locales/{zh,en}/iqBenchmark.ts`（**不做 `ja`**：`frontend/src/i18n/index.ts` 完全不动）
- [ ] 5.8 单测：`filters`、`compare`、`sanitize`、`rateLimit`、`useArtifactFeed`（防抖 ≥300ms、单飞）、组件渲染（只读不渲染管理按钮）

## 阶段 6：验证与灰度

- [ ] 6.1 `go build ./...`
- [ ] 6.2 `golangci-lint run ./...`（v2.13，需先安装）
- [ ] 6.3 `go test -tags=unit ./...`
- [ ] 6.4 `go test -tags=integration ./...`
- [ ] 6.5 `pnpm install` + 提交 `pnpm-lock.yaml`（若变更）
- [ ] 6.6 前端 type-check
- [ ] 6.7 `pnpm vitest run`
- [ ] 6.8 更新 `verification.md`：贴全部真实输出
- [ ] 6.9 灰度：迁移上线（关闭态）→ 管理员开启 + 配题/任务 → `run-now` 验证 → 开启用户侧
