## Why

sub2api 目前只有「通道监控」（探活/配额）和「用量统计」，缺少一种**面向能力对比的横向评测**：同一个题目交给不同「分组 × 模型」执行，把产物收集成可直接浏览的「作品展」。社区已有事实标准（Simon Willison 的 pelican riding a bicycle SVG 基准，仓库 `simonw/pelican-bicycle`，218★），其运作方式正是「每有新模型就同题重跑一次」，与本项目的分组路由模型天然契合。

本变更新增「模型智商检测作品展」：按「分组 × 指定模型」定期让模型执行同一道检测题，产出（图片 / SVG / Markdown / 文本）收集成作品展，普通用户登录后可只读浏览与横向对比，管理员负责题目、任务、产出与清理。

必须新增而非扩展现有模块的原因：

- 现有 `channel_monitor` 的语义是「可用性探活」，产出是延迟/状态码等标量指标，不保存模型输出的**内容**，无法承载作品展。
- 现有 `batch_image` 的语义是「用户提交批量生图任务」，计费对象是真实用户、生命周期由用户请求驱动，不存在「按分组 × 模型定期自跑」的调度语义。
- 把两者任意其一改造为作品展，都会改变其既有行为语义（违反「不得改变上游核心文件行为语义」的硬约束）。

因此本次以**独立垂直模块 + 默认关闭 + 新增表 + 白名单 DTO**的方式引入，不改动内容审核、渠道监控、批量生图、账务与配额语义。

## What Changes

- 新增独立后端模块 `backend/internal/iqbenchmark/`：题目（Case）、检测任务（Task）、执行记录（Run）、产出（Artifact）四层领域模型，配套 repository / service / scheduler / handler / ratelimit / sanitize 子包，通过 `wire` 以小接口接入。
- 新增 PostgreSQL migration `249_iq_benchmark_gallery.sql`（新表 4 张 + 索引）与 4 个 Ent schema，SQL migration 为生产建表事实源。
- 新增**内部虚拟账号**：`email = iq-benchmark@iq-benchmark.invalid`、`role = user`、`status = active`、随机不可知 `password_hash`、`notes = iq-benchmark:internal-agent`（唯一权威隔离标识）、`signup_source` 保持默认 `email`；配套专用 API Key 绑定目标分组。**不给 users 表加列、不新增 role 枚举、不扩展 `users_signup_source_check`**。
- 新增调度器：`Start(ctx)` / `Shutdown(ctx)` 有界停止，构造函数内**不**起 goroutine；多实例以 PostgreSQL `FOR UPDATE SKIP LOCKED` + 行级租约保证同一任务不被重复领取。
- 新增执行引擎：通过**本项目自身网关**（`/v1/chat/completions`、`/v1/images/generations`）执行检测，复用分组路由、账号调度、鉴权与用量统计；图片复用现有 `ImageStorage` / `ImageResultUploader` 落盘，不新建存储体系。
- 新增产出安全处理：SVG / HTML 入库前静态扫描危险构造（脚本、事件属性、`foreignObject`、`javascript:`、`<!ENTITY>`、`<iframe|object|embed>`、`@import`、`data:` URI），前端渲染前再经 DOMPurify 消毒；失败 / 超时 / 被内容审核拦截的产出**同样入库**为失败作品。
- 新增管理员 API `/api/v1/admin/iq-benchmark/*`（题目 CRUD、任务 CRUD/enable/pause/run-now、run 列表与重跑、artifact 管理与清理、overview 聚合、settings）与普通用户只读 API `/api/v1/iq-gallery/*`（overview / artifacts / artifacts/:id / cases），后者全部返回**公开白名单 DTO**。
- 新增 `IQGalleryRateLimiter`：复用 `middleware.NewRateLimiter(redisClient)` 与既有 429 输出，按认证用户 ID 两档叠加计数（`read` / `heavy`）+ 单用户并发上限，键前缀独立为 `iqgallery:user:{scope}:{userID}`，Redis 异常 fail-open。
- 新增前端 `frontend/src/features/iq-benchmark/` 共享展示组件（只读 / 可管理由 variant 区分）+ 两个页面：`views/user/IqGalleryView.vue`（登录可见双页签作品展）与 `views/admin/IqBenchmarkView.vue`（题目/任务/执行记录/产出管理）。
- 新增中英双语 i18n（`zh` / `en`）分文件，不硬编码中文。**不做 `ja`**（仓库当前只有 `en` / `zh` 两个 locale，新增 `ja` 需改上游 `LocaleCode` 联合类型）。
- 功能默认 `enabled=false`：迁移可先上线，但调度器不启动、用户侧路由不可达、管理员接口返回明确「未启用」。

## Capabilities

### New Capabilities

- `iq-benchmark-cases`: 定义检测题目（类型、题面、系统提示词、请求参数、期望输出形态、评分方式、启停、排序）、内置鹈鹕题的不可删除语义、题目复制，以及每次执行冻结题目快照。
- `iq-benchmark-scheduling`: 定义「分组 + 模型 + 题目集合 + 间隔配置」的检测任务、间隔与抖动校验、立即执行 / 启停 / 复制 / 删除、唯一启用约束、多实例去重领取、run 记录与失败重试语义。
- `iq-benchmark-internal-agent-billing`: 定义内部虚拟账号的字段组合与幂等初始化、`IsInternalAgent` 唯一判定、扣自己配额不影响真实用户、配额耗尽 / 超预算 / 账号被风控禁用三类熔断、以及检测流量必须被排除的统计入口清单。
- `iq-benchmark-gallery-public`: 定义普通用户可见的双页签只读作品展、公开白名单 DTO 的字段暴露与禁止暴露范围、筛选 / 横向对比 / 详情、失败作品占位、分页上限、以及 `enabled=false` 时路由不可达。
- `iq-benchmark-rate-limit`: 定义只读接口按认证用户 ID 的两档叠加限流、单用户并发上限、fail-open 语义、429 错误体与 `Retry-After`、独立键前缀，以及前端 429/防抖/单飞配合。

### Modified Capabilities

无。本次不修改任何已发布 capability 的既有需求语义，全部为新增。现有网关、内容审核、渠道监控、批量生图、账务与配额行为保持不变。

## Impact

- **后端模块**：新增 `backend/internal/iqbenchmark/`（含 `repository`、`service`、`scheduler`、`handler`、`ratelimit`、`sanitize` 子包）；`internal/service` 不反向 import 新模块。
- **数据库**：新增 migration `249_iq_benchmark_gallery.sql`（4 张表：`iq_benchmark_cases` / `iq_benchmark_tasks` / `iq_benchmark_runs` / `iq_benchmark_artifacts` + 索引）；同步新增 4 个 Ent schema 文件。**不修改任何既有表结构**（不给 `users` 加列、不给 `usage_logs` 加列、不改 CHECK 约束）。
- **上游核心文件接触面**：后端 5 个（4 个「注册一行」级别新增 + `S3ImageStorage.Delete` 纯新增方法）、前端 3 个（均为注册级），无签名改动、无行为语义改动，逐文件说明见 `design.md` §3.16 / §9.3 / §10。
- **配置**：设置项存于既有 `settings` 表（新增 key `iq_benchmark_settings`），**不改 `config.yaml` 结构与 `config.Config` 结构体**。
- **Redis**：新增键前缀 `iqgallery:user:{read|heavy}:{userID}` 与 `iqbenchmark:concurrency:{userID}`、`iqbenchmark:task:{id}:inflight`；不占用 `panel:` 前缀。
- **对象存储**：复用既有 `ImageStorage`，产出对象前缀 `iq-benchmark/<run_id>/<artifact_id>`；未配置对象存储时图片产出降级为失败作品（`storage_disabled`），不影响主流程。删除能力通过 `S3ImageStorage` 新增的 `Delete` 方法 + 本模块窄接口 `BlobDeleter` 类型断言获得，**不改 `ImageStorage` 接口签名**。
- **前端**：新增 `frontend/src/features/iq-benchmark/`、`views/user/IqGalleryView.vue`、`views/admin/IqBenchmarkView.vue`；`router/index.ts` 注册 2 条路由；`AppSidebar.vue` 增加 2 条导航项；i18n 新增 `locales/{zh,en}/iqBenchmark.ts`。**不新增前端依赖**（复用已有 `dompurify`）。
- **计费与统计**：检测流量以内部账号 `user_id` 与专用 `api_key_id` 记账；不影响任何真实用户余额或配额。管理端全局聚合默认仍会包含该内部账号（不改上游查询），可通过 `user_id` / `api_key_id` 过滤排除；需改默认查询的入口列为可选增强项并标注 merge 成本。
- **兼容性**：无既有 API breaking change；功能默认关闭，开启前不产生任何调度、计费或出站流量。
- **风险**：定期批量检测持续消耗额度 → 以「默认关闭 + 日成本预算上限 + 配额耗尽自动暂停」三重约束兜底；模型输出为不可信内容 → 以「入库静态扫描 + 前端消毒 + 优先 `<img>` 渲染 + 不做服务端栅格化」兜底。
