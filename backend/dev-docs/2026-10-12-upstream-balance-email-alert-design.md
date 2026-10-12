# 上游余额邮件预警方案与实现记录

日期：2026-10-12（Asia/Shanghai）。原方案依据本地 commit `d2de549efa`；用户随后确认开发，已在 `feature/upstream-balance-email` 分支实现。以下 1–9 节保留设计评审记录，实际实现与验证见第 10 节。未连接生产发送邮件、未部署。

## 1. 目标与已确认决策

为每个上游独立设置余额邮件预警，余额到达预警线后通知指定收件人。用户已确认：共享一份可自定义邮件模板，每个上游独立配置预警。

- 配置对象为归一化 URL 对应的上游，不是每个关联账号。同一上游关联多个账号不累加余额、不重复通知。
- 每个上游独立配置：启用、预警线、收件人、提醒频率、邮件语言。
- 全局共享模板：在现有“系统设置 → 邮件 → 邮件模板”新增“上游余额预警”事件，主题与 HTML 都可自定义。
- 复用已经配置的 SMTP、发件人及统一模板渲染服务。
- 新功能默认关闭，旧配置不受影响。
- 默认提醒策略建议为“每次触线仅通知一次”；可选持续低余额时每 24 小时提醒。巡检建议固定 5 分钟，首期无需新增全局调频 UI。

## 2. 已查证的现状

| 现状 | 依据 | 设计影响 |
|---|---|---|
| 上游已有独立配置弹窗，含名称、类型、开关、换算系数、凭据 | `frontend/src/views/admin/UpstreamMonitorView.vue` | 增加弹窗页签，不另造配置页面 |
| 配置存储于插件专属 `upstream_monitor_upstreams` 表 | `backend/ent/schema/upstream_monitor_upstream.go`、插件 `repository/upstream_repository.go` | 新配置与状态放插件边界内，不改宿主账户表 |
| 上游按归一化 URL 分组，只用一个关联凭据查询余额 | 插件 `handler/account_provider.go` 的 `monitorUpstreamInfos` | 复用归一化与凭据选择规则 |
| 概览读取、刷新全部会主动查询上游并写入快照 | 插件 `handler/monitor_handler.go`、`service/balance_aggregator.go` | 页面刷新与后台检查共用结果判断与去重，不另作汇总 |
| 插件内部尚无定时余额巡检 | 已检查插件生命周期及目录内 ticker/cron/scheduler 相关代码 | 必须加入后台巡检，保证页面关闭后仍可告警 |
| 验证码最终复用通知模板服务和 `EmailService.SendEmail` | `backend/internal/service/email_service.go` 的 `SendVerifyCode` | 扩展模板事件即可复用 SMTP，无需新邮件发送器 |
| 模板服务支持主题、HTML、事件白名单占位符、语言、预览、恢复默认及发送标记 | `backend/internal/service/notification_email_service.go` | 新增事件及变量、默认模板即可；发送标记本身不是并发锁 |
| 现有邮件队列仅处理验证码和重置密码任务 | `backend/internal/service/email_queue_service.go` | 不修改认证邮件队列；在插件有界后台工作中调用统一通知服务 |
| 模板编辑器已有左右编辑/预览、占位符按钮与中英文事件元数据 | `frontend/src/views/admin/settings/EmailTemplateEditor.vue` | 复用整个编辑器，仅补事件显示信息 |
| 抓取结果没有可信币种属性；快照目前固定写 CNY，Sub2API 缺少全部余额字段时会返回 0 | 插件 `service/upstream_info_fetcher.go`、`balance_aggregator.go` | 以监控显示单位比较；缺失金额必须被新告警路径识别为未知，不能误报零余额 |
| 插件 migrations/001 是旧 MySQL 风格；当前启动建表执行 PostgreSQL SQL | 插件 `plugin.go` 的 `runMigrations` | 不直接复用旧 SQL；仅对插件表做明确、可验证的增量变更 |

本地部署记录 `E:/ai/dev/docs/deploy-records/2026-10-12-hk-live-ready.md` 记载香港生产曾部署同一 commit；该记录仅为历史证据，本轮未核验服务器当前版本或 SMTP 实际发送能力。

## 3. 配置原型

### 上游监控 → 上游行“配置” → 余额预警

保留原“配置上游”弹窗宽度和表单风格，增加“基本配置 / 余额预警”页签。基本配置维持原字段；余额预警包括：

| 字段 | 规则 |
|---|---|
| 启用余额预警邮件 | 默认关闭；上游余额监控与全局插件也必须启用 |
| 余额预警线 | 必填、有限非负数字，允许 0；按完整数值比较、邮件默认显示两位小数 |
| 收件邮箱 | 开启时至少 1 个；按行或逗号输入，解析、校验、去重，上限建议 10 个 |
| 提醒频率 | 每次触线仅通知一次（默认） / 持续低余额每 24 小时提醒 |
| 邮件语言 | 中文 / English；自动任务使用持久化语言，不依赖浏览器当前语言 |
| 邮件模板 | 展示“共享模板：上游余额预警”，链接到已有模板编辑器并选中该事件 |
| 最近通知 | 从后端读取，区分未发送、已发送、部分失败、发送失败；不以入队成功当发送成功 |

“取消 / 保存配置”仍是原弹窗底部操作。关闭预警时保留原阈值和收件人，便于再启用。SMTP 未配置时显示清楚的配置提示，保存规则可允许但发送状态显示失败；不把测试发送作为保存副作用。

列表只增一行简短信息，例如“邮件预警 ≤ 100.00 · 已通知”或“预警未启用”，不增加仪表盘或新的导航项。

### 系统设置 → 邮件 → 邮件模板

沿用现有模板类型、语言、主题、HTML、占位符、左右预览、保存和恢复默认交互。新事件建议：`upstream.balance_low`，分类“管理告警”。配置弹窗的“编辑共享模板”通过既有设置路由的查询参数选中事件；返回后保留尚未保存的上游表单，不静默丢弃修改。

首期不新增真正的“发送测试邮件”按钮。原型预览仅替换示例值，不发送邮件。

## 4. 邮件模板与占位符

主题默认：`【{{site_name}}】上游 {{upstream_name}} 余额预警`

正文示例（实际默认模板沿用已有通知邮件的品牌外壳）：

```html
<p>上游 <strong>{{upstream_name}}</strong> 的余额已到达预警线。</p>
<p>当前余额：<strong>{{balance}}</strong></p>
<p>预警线：{{threshold}}</p>
<p>上游地址：{{upstream_url}}</p>
<p>检查时间：{{checked_at}}</p>
<p>请及时检查并充值，避免服务中断。</p>
<p><a href="{{upstream_url}}">访问上游</a></p>
```

| 占位符 | 内容 |
|---|---|
| `{{site_name}}` | 现有站点名 |
| `{{upstream_name}}` | 配置的上游名称；空名称回退归一化上游地址 |
| `{{upstream_url}}` | 归一化、去掉凭据及查询参数的安全上游地址，可用于 `<a href="{{upstream_url}}">访问上游</a>` |
| `{{upstream_type}}` | sub2api / nexapi |
| `{{balance}}` | 本轮有效查询的余额，按监控显示单位 |
| `{{threshold}}` | 此上游的预警线，单位同余额 |
| `{{checked_at}}` | 余额采样时间，带时区；首期按 Asia/Shanghai 展示 |
| `{{recipient_email}}`、`{{recipient_name}}` | 现有公共变量；未提供姓名可用通用称呼 |

暂不提供 `currency`，避免把尚未核实的币种标成 CNY 或 USD；不新增换汇或修改现有余额换算公式。共享模板只改变文案，不改变各上游的预警逻辑。

所有新增变量使用现有占位符白名单、HTML 转义和邮件头清理。未知变量拒绝保存；不开放原始 HTML 变量插入，不向模板传入 API key、token 或账户凭据。

新事件建议按管理员配置型通知处理（`Optional=false`），由各上游开关控制，避免现有“按邮箱+事件退订”导致同一个邮箱悄悄屏蔽所有上游。与业务用户 `balance.low` 和账号配额 `account.quota_alert` 完全分开。

## 5. 触发、重复抑制与失败处理

1. 插件启动后，新增 `BalanceAlertChecker` 每 5 分钟检查已保存且启用的预警规则；运行时每轮检查全局插件开关及上游监控开关，关闭后停止采集/发送。
2. 通过小型共用目标加载函数复用 URL 分组、关联账号与解密凭据逻辑，避免巡检复制规则或依赖 Gin handler。
3. 使用现有抓取器查询一个上游一次；设置请求超时与整体取消，跨上游有界并发（建议 3），同一上游本轮不重入；不以页面读取作为后台告警的前置条件。
4. 只有本轮成功、余额字段确实存在且为有限值的样本进入判断。超时、凭据错误、JSON 异常、缺失字段、NaN/Inf、无可用账号均标记“无法检查”，不当作 0，不用旧快照触发新邮件。真实 0 与负余额均可触发。
5. 当 `balance <= threshold` 时进入低余额状态。初次启用时已低于阈值，也在下一轮检查通知。
6. 创建持久化告警 episode 标识；为每个收件人记录发送成功时间。一次策略只给未成功的收件人发送；24 小时策略从各收件人最近成功发送时间计算，不按自然日切换。
7. 余额恢复至阈值之上后，要求连续两次有效后台样本确认恢复，结束本次 episode；避免阈值附近抖动反复通知。查询失败不计为恢复；手动连续刷新不能加速后台恢复确认。再次触线创建新 episode。
8. 邮件失败不标成功；下轮重试，仅补发失败收件人；连续失败使用有界退避（例如 5、10、20、40、60 分钟封顶），保留可诊断、脱敏的状态与日志。共享模板变更不重置已通知状态。
9. 手动刷新可调用同一告警判断入口以减少等待，但使用同一上游 lease、episode 和逐收件人去重；只读概览不直接发信。
10. 修改阈值/提醒策略产生新的规则版本，在下一轮重新判断；改收件人只让新增邮箱进入待发送，不重发已成功邮箱。关闭再开启时按新一次启用重新判断；名称、凭据、模板和语言修改不自动重发。

并发保护建议使用现有 PostgreSQL：每条规则有短期 lease 和版本条件更新，网络查询及 SMTP 发送在事务外完成。发送 lease 必须覆盖最长有界任务时长，或续租；写回结果校验 owner/version，过期持有者不能覆盖新状态。应用重启保留 episode 与逐收件人成功状态，多实例使用同一 lease。

复用通知服务发送去重时传入 `SourceType=upstream_monitor`、持久化上游 ID、episode 与提醒序号/收件人稳定标识。现有“先查发送标记后发信”的流程本身不原子，不能代替上述 lease。

SMTP 接受邮件与数据库写入成功之间无法构成原子事务。首期采用至少一次重试，进程恰在邮件已接受但标记未落库时退出，仍有小概率重复；不承诺严格恰好一次，也不把 SMTP 接受等同于用户收件箱必达。

## 6. 低侵入实现边界

推荐方案：新增插件专属 `upstream_monitor_balance_alerts` 表（唯一关联现有 upstream ID），同时持有规则和紧凑运行状态。**不扩展宿主账户 schema，也不为此触发全库 Ent 自动迁移。**

建议字段：

- `upstream_id` 唯一外键；`enabled` 默认 false。
- `threshold NUMERIC(20,6)`；`recipients JSONB`；`repeat_interval_seconds`（0 或 86400）；`locale`（zh/en）。
- `rule_version`、`episode_id`、`low_since`、`recovery_confirmations`。
- `recipient_delivery_state JSONB`（每收件人最近成功、下次重试、失败计数，不存 SMTP 密码）。
- `last_checked_at`、`last_balance`、`last_error_code`、`lease_owner`、`lease_until`、时间戳。

插件仓库通过现有数据库连接执行参数化 SQL，不必修改/再生成整个 Ent 模型。首期独立保存预警规则：基本配置保持现有 `PUT /upstreams`，新增管理员保护接口：

```text
GET /api/v1/plugins/upstream-monitor/upstreams/:id/balance-alert
PUT /api/v1/plugins/upstream-monitor/upstreams/:id/balance-alert
```

保存 body：

```json
{
  "enabled": true,
  "threshold": "100.000000",
  "recipients": ["ops@example.com"],
  "repeat_interval_seconds": 0,
  "locale": "zh"
}
```

阈值传十进制字符串，后端规范化最多 6 位小数；不接受非有限数、非法邮箱和任意提醒间隔。金额比较避免先四舍五入到显示两位小数。返回规则及最近检查、逐收件人发送结果摘要，绝不回传凭据。新接口沿用插件全局开关与管理员认证。自动发现但尚未保存的上游 ID 为 0 时，先保存基本配置获得稳定 ID，再进入预警设置；不把 0 当真实外键。

预警页签单独保存本页；弹窗中的未保存基本字段/预警字段保持各自草稿。切页、进入模板页或关闭有未保存改动时使用项目既有提示方式；取消不写库，不让旧基本配置 Upsert 清空预警规则。

共享邮件侧仅新增一个事件及中英文默认模板、示例变量和事件元数据；模板仍存现有 settings。邮件发送依赖通过插件 providers/Wire 注入统一 `NotificationEmailService`，不得引入反向依赖或另建 SMTP 配置。插件 `Init/Cleanup` 管理 checker 的 cancel、停止与有界等待；启动服务失败不启动重复 worker。

开发预计边界：

- 插件内新增告警 model/repository/checker/handler 及针对性测试。
- 插件 `plugin.go`、`providers.go`：增量建表和生命周期/依赖接入。
- 插件账号目标加载逻辑：提取少量共用函数，不重构请求网关。
- 抓取器：给新告警判断暴露“余额字段是否存在”的有效性与可取消 context；保持既有展示与换算语义。
- 统一邮件服务：一个事件注册、变量、默认模板及测试。
- 前端新增 `UpstreamBalanceAlertForm.vue`，既有监控页少量集成、API 类型与新请求函数。
- 既有模板编辑器补事件名称/说明及深链接初选；中英文同步。
- 生成的 Wire 文件仅按项目已有生成方式更新；不手改无关生成代码。

本轮原型文件放当前聊天的可视化目录，方案文档放项目已有 `backend/dev-docs/`（根目录 docs 被 Git 忽略，因此复用可纳入版本管理的现有文档目录）；功能实现前以用户评审后的方案为准。

## 7. 实施顺序与验证

| 步骤 | 内容 | 验证标准 |
|---|---|---|
| 1 | 规则存取、独立 PostgreSQL 表及管理员接口 | 旧配置不变、默认关闭、阈值 0 可保存、非法邮箱/数值拒绝、无权请求拒绝 |
| 2 | 邮件新事件与共享模板 | 中英文可编辑/预览/恢复；占位符替换正确、HTML 与邮件头转义；原验证码/业务低余额事件回归通过 |
| 3 | 后台 checker 与状态机 | 未开页面也检查；边界相等触发；无效样本不发送；同一上游多账号只发一份；恢复再次触线可发 |
| 4 | 失败重试与并发 | 多收件人部分失败仅补发失败者；重启不重发成功者；两实例及手动刷新并发无常规重复；lease 失效与停止正确 |
| 5 | 前端配置整合 | 原凭据不回显；预警单独保存；未保存草稿不丢；模板深链接初选；320/768/1024/1440 宽度与键盘访问 |
| 6 | 集成与上线准备 | 干净构建、明确增量 SQL、生产备份和回滚方案，真实测试邮件只在获授权的指定邮箱验证 |

后续开发验证命令（PowerShell，执行前根据实际修改选择范围）：

```powershell
Set-Location 'E:/ai/claude/dev/sub2api-original/backend'
go test ./plugins/upstream-monitor/...
go test -tags=unit ./internal/service -run 'NotificationEmail|Email|BalanceNotify'
go generate ./cmd/server
go build ./cmd/server

Set-Location 'E:/ai/claude/dev/sub2api-original/frontend'
pnpm run test:run -- src/components/admin/__tests__/UpstreamBalanceAlertForm.spec.ts
pnpm run typecheck
pnpm run check:i18n
pnpm run build
```

测试用模拟时钟、假 SMTP/发送端和 PostgreSQL 集成环境；不拿真实用户或生产邮箱做自动测试。原型阶段只验证配置交互、模板替换、响应式和控制台错误，不声称后端通知已实现。

## 8. 代码风格与授权边界

遵循现有 Go service/repository/provider 分层；小型接口注入通知依赖，不新增总线或框架。Vue 使用 Composition API、原 `.input/.btn/.card`、`Toggle` 和统一错误提示。例：`threshold` 未提供和 `"0"` 明确区分；保存预警走独立 API，不修改旧凭据表单字段。

- 必须：先核验实际版本和 schema、保护凭据、默认关闭、增量可回滚、准确报告验证结果。
- 当前可做：源码检查、方案、交互原型与本地文档。
- 后续开发以用户确认后的方案为边界；生产部署、数据库迁移和真实测试邮件依照明确授权执行。
- 不做：读取或输出私钥、重配 SMTP、改变验证码行为、清理旧配置、改支付/扣费/请求链路、未经授权部署。

用户给出的 `.pub` 路径是公钥，不是 SSH 私钥。将来连接生产需要核实对应私钥路径/已有 SSH 配置；本轮不使用该文件，不需要连接生产。

## 9. 本轮原型验证结果

已使用真实 Chrome（通过 Playwright）检查 25 项：原型打开、负数阈值拒绝、零阈值接受、非法邮箱拒绝、开关禁用字段、弹窗键盘焦点循环、模板入口与返回草稿、名称/余额占位符替换、点击占位符插入、未知占位符拒绝、中英文切换、原基本配置入口、两个上游规则独立保存，以及 320/768/1024/1440 宽度下两个页面无横向溢出。页面无运行时异常。已目视核验桌面配置、共享模板和手机配置截图。

结果仅代表设计原型，不代表后端通知、SMTP 发送、数据库变更或生产部署已完成。方案与原型使用示例上游、邮箱、金额和检查时间，未读取生产私人数据。

## 10. 已实现与实际验证（2026-10-12）

用户确认复用已有 `upstream_url`，未添加额外访问地址占位符。中英文默认邮件均包含可点击的“访问上游 / Visit upstream”链接。

### 实际落地边界

- 现有“配置上游”增加基本配置/余额预警页签，预警使用独立 GET/PUT 接口。
- 收件人去重，最多 10 个；预警线最多 14 位整数/6 位小数，允许 0；按十进制完整数值比较。等值阈值（100 与 100.00）规范化后不会重置通知状态。
- 共享邮件模板仍在现有设置编辑器；模板事件 `upstream.balance_low`，支持 zh/en、主题/HTML/变量/预览/恢复默认。链接以新标签页打开 `?tab=email&email_event=upstream.balance_low`，原页草稿保持；原生新标签浏览器行为与原设计同路由返回方案相比更简单。
- 只有显式启用规则才发送。检查器每 30 秒查询到期规则，每上游检查完成后间隔 5 分钟再次检查，跨上游最多 3 个并发。每批账户与上游配置只加载和解密一次。
- 持久化 episode、逐收件人成功与重试状态；恢复必须两次有效后台样本；一次策略或每 24 小时提醒；重试为 5/10/20/40/60 分钟封顶。
- 采样时超时或缺失余额字段不触发预警；每次发送前检查全局开关、上游启用状态与租约所有者；关停后续收件人发送会停止，已开始的 SMTP 事务无法撤回。
- 页面手动刷新只刷新展示，邮件由后台独立检查触发，避免读取页面引入发信副作用；首期列表不增加额外状态请求，通知结果集中展示在预警页签。
- 最终表只新增 `upstream_monitor_balance_alerts`，使用 `upstream_id` 外键、enabled、config/state JSONB、next_check_at、lease_owner/lease_until、updated_at。替代计划中的分散状态字段，减少 schema 与 ORM 改动。
- 插件启动执行单条 `CREATE TABLE IF NOT EXISTS`，不修改宿主账户表，不运行全库 Ent migration。规则默认关闭，服务升级不会自行创建已启用规则。
- 配置保存遇到正在运行的检查返回 409，避免覆盖检查状态；lease 为 15 分钟，检查预算 6 分钟，过期/错误所有者不能写回，活跃 lease 不占到期查询批次上限。
- 现有 SMTP 服务增加 context-aware dial 与 socket cancellation，保持 TLS/STARTTLS 行为和旧 `SendEmailWithConfig` 签名，确保插件停服能够停止发送；独立审查提出的关停、单上游禁用、到期查询饥饿问题均已修复。

### 验证证据

已通过：

```powershell
# backend（本机默认 Go 缓存存在卷挂载问题，因此仅本次命令指定 E 盘缓存）
$env:GOCACHE='E:/ai/dev/.cache/go-build'
go test ./plugins/upstream-monitor/...
go test -tags=unit ./internal/service -run 'SMTP|EmailCancellation|NotificationEmail|EmailMessage|BalanceNotify'
go test ./internal/service -run 'NotificationEmail|Email|BalanceNotify'
go generate ./cmd/server
go build -o 'E:/ai/dev/.cache/sub2api-balance-alert.exe' ./cmd/server

# frontend
pnpm run typecheck
pnpm run build
pnpm run test:run -- src/components/admin/__tests__/UpstreamBalanceAlertForm.spec.ts src/components/admin/__tests__/UpstreamMonitorSettings.spec.ts
pnpm run test:run -- src/views/admin/__tests__/SettingsView.spec.ts
pnpm exec eslint src/components/admin/UpstreamBalanceAlertForm.vue src/components/admin/__tests__/UpstreamBalanceAlertForm.spec.ts src/api/admin/upstreamMonitor.ts src/views/admin/UpstreamMonitorView.vue src/views/admin/settings/EmailTemplateEditor.vue src/views/admin/SettingsView.vue
```

插件新增测试包括：零阈值/相等边界/非法配置、缺失余额、低余额首次发送、部分失败只补发失败者、24 小时重复提醒、两次恢复、新 episode 再发、持久化状态重载、新收件人通知、数据库不可用不发送、全局关闭不采样、采样失败释放 lease、禁用后停止后续发送、API 404/400/409 及默认关闭。

真实 Chrome 使用实际 Vue 组件与隔离 mock HTTP，13 项检查通过：两上游独立规则、零阈值 PUT、模板新标签与事件参数、模板事件初选及预览链接，320/768/1024/1440 宽度弹窗无溢出、无 Vue runtime error。已目视核验桌面与手机配置、共享模板截图。

浏览器证据位置：`frontend/output/playwright/balance-alert-browser-verification.json`、`balance-alert-desktop.png`、`balance-alert-mobile.png`、`upstream-email-template.png`（按项目规则为忽略的本地验证产物）。

本地验证构建二进制：`E:/ai/dev/.cache/sub2api-balance-alert.exe`，SHA-256 `DB6A57523E55A9212CC43A6FE48B9CD7DAF3336764BF3AD44E78620634096AFB`。这是 Windows 本地编译验证产物，不能作为 Linux 生产发布镜像。

Docker 服务未启动，因此使用项目之外临时安装的 `@electric-sql/pglite@0.3.14`（真实 PostgreSQL WASM 引擎）验证生产源码中的所有 SQL；项目依赖与 lockfile 未改。验证覆盖重复建表、外键、规则存取、互斥 claim、检查中保存冲突、逐收件人状态、定时下一次检查、收件人变更保留状态、阈值修改重置、过期租约 fencing、上游禁用检查、100 条占用 lease 后的待检查规则仍可选取、删除上游级联。该验证为隔离引擎/顺序 SQL 验证，不等同于完整生产 PostgreSQL 多连接并发集成演练。

### 限制与上线

- 本机 `go test -race ./plugins/upstream-monitor/...` 的部分测试进程返回 Windows `0xc0000139`（DLL 入口不可用），不能声称 race 检测通过；普通测试与构建通过。上线前可在 Linux 构建机补跑 race。
- 既有 SettingsView 43 项测试通过，但输出 router-link stub 和 mock XHR 警告；前端 build 有既有 Browserslist 和 chunk 大小提示，未扩大任务去修改工具链。
- SMTP 接受邮件与本地发送记录无法原子提交，极端退出窗口仍可能重复；SMTP 接受不保证最终进入收件箱。未向真实邮箱发测试邮件。
- 上线需要重新核实香港运行版本并备份，检查插件专属建表权限，在授权范围内完成单表增量和新镜像部署。当前服务未部署。
- 回滚到旧镜像可保留新表，其对旧版本无业务影响；若未来决定删除该表，必须先停用所有规则并备份，因为 DROP 会删除用户配置与通知状态，不作为默认回滚动作。
