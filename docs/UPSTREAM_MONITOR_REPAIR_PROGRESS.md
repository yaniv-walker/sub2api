# 上游管理插件整改进度

## 目标

让 `upstream-monitor` 作为首方插件在主应用中可编译、可启动、可受管理员认证保护，并具备可验证的持久化、事件处理和用量分析能力。整改范围限定在插件及其必要的宿主接入，不改变无关业务行为。

## 当前基线（2026-09-21）

- 分支：`feature/upstream-monitor-plugin`
- 已有集成提交：`1229901d0 feat: integrate upstream-monitor plugin`
- 工作区包含未提交的临时接入调整，主 Wire 当前注释掉 `upstream-monitor.ProviderSet`。
- 插件实现重复存在于 `backend/plugins/` 与 `backend/plugins/upstream-monitor/`；整改后保留后者作为唯一目录。
- 已知问题：插件依赖的 Ent schema/生成代码缺失；SQL 迁移为 MySQL 语法；路由未接入管理员认证；事件钩子为 TODO；用量接口为占位；前端声明与仓库现状不一致。

## 任务清单

### 阶段 1：编译与目录收敛

- [x] **任务 1：补齐持久化模型和生成代码**
  - 验收：`go test ./plugins/upstream-monitor/...` 能编译；模型字段与现有 PostgreSQL/Ent 约定一致。
  - 验证：插件包测试、Ent 生成/编译检查。
  - 依赖：无。
- [x] **任务 2：收敛唯一插件目录**
  - 验收：所有导入统一指向 `plugins/upstream-monitor`；旧重复目录不再参与构建；无功能代码丢失。
  - 验证：全仓 `rg` 导入扫描、全量 Go 编译。
  - 依赖：任务 1。

### 阶段 2：宿主接入与数据边界

- [x] **任务 3：恢复 ProviderSet 和生命周期注册**
  - 验收：插件启用时由 Wire 构造、初始化和清理；禁用时不注册路由或任务。
  - 验证：Wire 生成、启动级测试。
  - 依赖：任务 1、2。
- [x] **任务 4：接入管理员认证路由**
  - 验收：插件管理接口未认证返回拒绝，管理员认证后可访问；路由前缀保持兼容。
  - 验证：Handler/Router 测试。
  - 依赖：任务 3。
- [x] **任务 5：统一 PostgreSQL/Ent 迁移方案**
  - 验收：不执行 MySQL 专用 SQL；插件初始化在 PostgreSQL 上可重复执行且不破坏已有表。
  - 验证：迁移测试或临时 PostgreSQL 集成检查。
  - 依赖：任务 1、3。

### 阶段 3：业务闭环与验证

- [x] **任务 6：实现事件钩子、用量分析和清理**
  - 验收：请求成功/失败与账号删除事件产生对应持久化或清理动作；用量接口返回真实聚合结果；清理逻辑可重复执行。
  - 验证：服务单元测试、事件回归测试。
  - 依赖：任务 5。
- [x] **任务 7：处理前端与文档一致性**
  - 验收：已有前端真实接入插件 API，或明确修正文档为未实现；不保留虚假完成声明。
  - 验证：前端类型检查/构建及必要的浏览器冒烟。
  - 依赖：任务 4、6。
- [ ] **任务 8：全量回归与发布前审查**
  - 验收：插件、后端、前端构建和相关测试通过；工作区无误纳入的临时文件；文档状态与代码一致。
  - 验证：`go test ./...`、前端检查、差异审查。
  - 依赖：任务 1-7。

## 检查点

- **检查点 A（阶段 1）：** 插件独立编译，目录唯一。
- **检查点 B（阶段 2）：** 主应用可构造，路由认证和迁移边界通过测试。
- **检查点 C（完成）：** 业务闭环、前后端一致性和全量回归通过。

## 风险与处理

| 风险 | 影响 | 处理 |
|---|---|---|
| 生成代码与当前 Ent 版本不匹配 | 高 | 先核对现有 schema/generate 约定，再生成并运行最小测试 |
| 删除重复目录误删用户改动 | 高 | 逐文件比对，先确认内容一致并保留目标目录；不使用不可逆清理命令 |
| 路由认证中间件签名不兼容 | 中 | 复用现有 `AdminAuthMiddleware`，通过路由测试锁定行为 |
| 外部上游不可用 | 中 | 业务单元测试使用可控 HTTP fixture，集成验证只检查本地边界 |

## 进度记录

- 2026-09-21：完成工作区、分支、重复目录和未提交改动核对；创建本进度文档。
- 2026-09-21：完成任务 1；新增两个 Ent schema 并生成代码，插件包测试通过。
- 2026-09-21：进入任务 2；逐文件比对确认顶层 `backend/plugins/` 与 `backend/plugins/upstream-monitor/` 仅有两处测试/模型差异，目标目录保留为唯一实现。
- 2026-09-21：完成任务 2；删除顶层重复实现，所有 Go 导入统一到 `plugins/upstream-monitor`。
- 2026-09-21：完成任务 3-4；恢复 Wire ProviderSet 和插件生命周期，插件路由统一置于管理员认证中间件之后。
- 2026-09-21：完成任务 5；插件不再调用全库 `Ent.Schema.Create`，迁移由宿主统一管理，避免 MySQL SQL 进入 PostgreSQL 部署。
- 2026-09-21：任务 6 部分完成；补充快照查询驱动的用量聚合、钩子注销和插件清理逻辑。当前主业务未发现 `HookManager.Trigger*` 调用，成功/失败请求和账号删除事件尚未真正发出，需宿主业务事件接入后再完成闭环。
- 2026-09-21：完成任务 7；扫描 `frontend/src` 未发现 upstream-monitor 页面或 API 接入，已明确记录为当前未实现，不宣称前端完成。
- 2026-09-21：完成任务 8 的可执行回归部分；插件/宿主测试、`go test ./...` 和 `go build ./cmd/server` 通过。事件触发链缺失作为发布前阻塞风险保留。
 - 2026-09-21：核对并修复 Windows 启动端口异常。机器上的 `E:\\app\\data\\config.yaml` 因无条件加入 `/app/data` 搜索路径而覆盖仓库配置，导致实际端口为 3000 且插件配置缺失。配置加载和 setup 数据目录现在仅在非 Windows 系统启用 `/app/data`；Windows 默认使用当前目录 `config.yaml`。`internal/config`、`internal/setup` 测试通过，实跑确认插件初始化、路由注册及 `Server started on 0.0.0.0:8080` 均出现。
- 2026-09-21：完成任务 6；新增稳定事件载荷，主网关成功/失败转发和管理员账号删除均触发插件钩子。失败事件写入 `upstream_error_records`，账号删除幂等清理错误记录和余额快照，成功事件写入结构化运行日志；插件仓储通过 Wire 注入。异步事件使用脱离请求取消的上下文，避免客户端断开导致记录丢失。
- 2026-09-21：任务 8 继续保留未完成。定向插件、服务和网关测试可编译；Windows 全量 handler 测试仍有既有 `TestResolvePageImagePath` 路径语义失败，需单独跨平台修复后再宣称全量通过。
- 2026-09-21：修复账号数据边界。插件 handler 不再读取 `config.yaml` 中的账号列表，改为注入主系统 `AccountRepository`，直接读取后台管理员维护的 `accounts` 表；仅纳入 active、`upstream` 类型、含 API Key 且可识别为 `sub2api`/`nexapi` 的账号。类型优先取账号扩展字段，否则从 `base_url` 推导；补充边界单元测试，并同步 Wire 与 README。插件定向测试通过。
- 2026-09-21：补充自定义域名兼容。`type=upstream` 本身只表示透传账号，无法单独区分监控协议；类型现在优先取账号扩展字段、明确的 platform，再从 URL 推导，仍无法区分时默认 `sub2api`，避免后台账号因自定义域名被过滤。NexAPI 可在账号扩展字段设置 `upstream_monitor_type=nexapi`。
- 2026-09-21：根据后台实际数据修正账号资格：管理页面创建的上游账号保存为 `type=apikey`，因此现在同时纳入带 `credentials.base_url` 的 API Key 账号；普通无 `base_url` 的 API Key 账号仍排除。补充对应单元测试。
- 2026-09-22：按补充业务设计将余额维度从“账号”调整为“上游中转站”。插件规范化账号 `base_url`，去除 `/v1` 等模型接口路径，相同协议/主机/端口的账号归为同一上游，余额批量刷新只查询一次。
- 2026-09-22：新增插件独立表 `upstream_monitor_upstreams` 与 `GET/PUT /upstreams` 管理接口。上游名称、`sub2api`/`nexapi` 类型及启用状态按根地址保存，不修改主程序账号表；管理员配置优先于自动识别。抓取器改为在账号所属上游根地址后拼接 `/api/user/self`，修复固定公共域名导致的 404。
- 2026-09-22：修复插件表初始化受 `auto_migrate=false` 阻断的问题。插件启用时始终创建自身的 `upstream_monitor_upstreams` 表；该初始化只涉及插件表，不会执行主程序全库迁移。
- 2026-09-22：实测自定义 Sub2API 上游的 `/api/user/self` 返回 404，确认当前宿主对 API Key 暴露的自省接口为 `/v1/usage`。Sub2API 抓取器改用该端点并兼容 `balance`、`remaining`、`quota.remaining`；NexAPI 保持 `/api/user/self`。真实 HTTP fixture 覆盖接口路径、认证头和解析结果。
- 2026-09-22：确认 NexAPI `/api/user/self` 拒绝模型 API Key，需要上游用户访问令牌。插件上游配置新增只写 `access_token`：复用主程序 `SecretEncryptor` 进行 AES-GCM 加密，按上游共享并优先用于余额查询；列表与保存响应仅暴露 `has_access_token`。旧表由插件启动时幂等增加列，不修改主程序账号数据。
- 2026-09-22：修正 NexAPI 余额口径：优先严格按设计公式 `(quota - used_quota) / 431778` 计算，不再优先使用可能为缓存或不同单位的 `remaining_quota`；仅在总配额字段缺失时回退。
- 2026-09-22：新增 `by_upstream` 响应明细。`total_balance` 继续表示所有上游合计，`by_upstream` 按规范化根地址返回每个上游独立余额、类型、名称和关联账号数量，避免多个上游只能看到一个总数。

## 启动端口与插件日志核验

在 PowerShell 中必须从后端目录执行以下命令，确认启动进程使用的配置来源：

```powershell
Set-Location E:\ai\claude\dev\sub2api-original\backend
Get-Location
Get-ChildItem Env:CONFIG_FILE,Env:DATA_DIR,Env:SERVER_HOST,Env:SERVER_PORT -ErrorAction SilentlyContinue
Get-Content .\config.yaml | Select-String '^server:|^    host:|^    port:'
Test-Path .\.installed
go run .\cmd\server
```

正常正式启动应出现 `Server started on 0.0.0.0:8080`，随后应出现插件初始化相关日志。若出现 `First run detected, starting setup wizard...`，说明 `NeedsSetup()` 判定的配置目录不是当前目录；若没有该日志但地址仍是 3000，则应检查实际生效配置文件中 `server.port` 或 `SERVER_PORT` 环境变量。前端端口 3000 不会自动改变后端端口。
