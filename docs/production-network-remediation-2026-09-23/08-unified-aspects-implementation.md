# 08｜统一开关与低侵入切面实现（2026-10-10）

本阶段完成统一开关和正文读取观测，作为总体性能治理规划的第一批实现。它增加可核验的阶段证据，并复用已有严格重试保护；**没有完成全部性能治理，也未部署生产或香港节点**。

## 使用与升级

- 设置页「网关服务」只保留一个「网关性能治理」总开关，默认关闭。
- 设置键：`gateway_remediation`，值为 `{"enabled":true|false}`，写入既有 settings 表，无数据库结构迁移。
- 正式接口：`GET/PUT /api/v1/admin/settings/gateway-remediation`，沿用管理员鉴权/审计边界和 `{enabled: boolean}` 契约。
- 旧 `/request-observability`、`/upstream-retry-guardrails` 接口保留，但读写同一个总开关。任一旧接口保存 false 会关闭全部已接入切面，保存 true 会同时打开观测与严格保护。
- 旧数据库键不删除、不覆盖，但不再读取。这是有意的升级边界：旧的观测开关开启不能自动授权新的行为保护。升级后需明确开启总开关；回滚旧镜像时旧键仍保留。
- 本节点保存成功后新请求使用新状态；进行中请求保留入口时的一次快照。其他节点依旧按 30 秒 TTL 触发异步刷新，不能承诺严格 30 秒全球生效；存储失败保持最近有效状态，首次失败默认关闭。
- 总开关控制本次治理切面，不控制既有上游监控插件、账号池配置或其他业务开关，也不会关闭原项目自身的安全/取消逻辑。

## 切面设计与合并边界

`GatewayRemediation` 在统一路由处只接入一次：

1. 从内存中读取一次开关快照，放入既有 request context guardrail key；
2. 开启时复用 `requestobs.State`、现有响应 writer 观察器与上游 HTTP 观察边界；
3. 包装 `Request.Body`，透明委托 Read/Close，只记录数值和错误枚举；
4. 请求结束时输出一条正文读取摘要，复用原 request-scoped logger；
5. 关闭时不包装 Body/ResponseWriter、不输出新增摘要，主 handler 继续原路径。

核心逻辑放在独立新增文件 `gateway_remediation.go`、`gateway_remediation_body.go` 和 `setting_gateway_remediation.go`。不引入 AOP 框架，不复制重试器，不修改模型转发 handler、账号选择器、计费或协议适配器。旧 service 方法保留薄适配层；新模块后续继续接在这个边界，避免散布多个开关判断。

不可承诺零合并冲突：路由初始化、管理员路由、SettingService 字段与设置页有必要接入行，属于需要人工核对的稳定接点。其余实现与测试是独立文件。源码基线为 `main-self` 的 `aa88a17c41`（已合入 upstream v0.2.15），并非先前生产 v0.2.10；上线要单独处理上游版本差异。

## 正文观测字段

事件名：`gateway remediation body read completed`，包含原日志的内部 request ID。

| 字段 | 定义 |
|---|---|
| `declared_content_length` | 请求声明长度，未知为 -1，不能当成实际接收量 |
| `body_bytes_read` | 底层 Read 实际返回的字节合计；不采集正文 |
| `body_read_complete` | 已看到 EOF 或已读足正的声明长度，且未发生读取错误；未知长度必须看到 EOF |
| `body_read_error_kind` | client_disconnect、deadline、truncated_body、max_bytes、transport、io_read、not_read、not_complete 或空值 |
| `body_read_ms` | 累计在底层 Read 调用内耗时；不包含鉴权、解析、两次 Read 间暂停或上游排空，不等于客户端侧上传吞吐时间 |

handler 没有读取正文时不发摘要，避免健康检查/认证拒绝产生噪声。摘要在请求结束时生成，所以对于仍在阻塞的在途读取，它不会实时显示进度。HTTP 层包装无法识别后续 JSON/压缩解码错误，也不能测量浏览器到 Caddy 的完整上传时间；这些保留为后续阶段任务。

wrapper 不预读、不缓存、不替换返回错误、不主动关闭 body、不新增 deadline，不增加每 chunk 日志或远程 collector。关闭与读取可并发，数值统计由锁保护；可选日志 sink 的异常不会改变已完成 handler 的响应。

## 验证与后续任务

本阶段测试覆盖：统一开关读写/重启/存储失败/异步刷新、旧接口联动、旧键不能自动启用、开启关闭 Body/响应透明性、未知长度 EOF、截断、取消、deadline、max bytes、Read/Close 原错误保持、进行中请求快照。复用现有 middleware 与严格重试 helper 回归。

本机验证记录：

- middleware 全包 `go test -tags=unit ./internal/server/middleware -count=1` 通过，包含真实 HTTP 延迟分块上传测试。
- service/handler/admin 的开关及保护定向测试通过；`go vet ./internal/server/middleware ./internal/service` 通过。
- `go build ./...`、`go build -tags=embed ./cmd/server` 通过。
- 前端组件/i18n 共 9 个测试、`vue-tsc --noEmit`、改动文件 ESLint、`pnpm run build` 通过。未配置浏览器 MCP，真实管理页面交互尚未验证。
- server 的两个 `tutorial_enabled` API 契约失败，用 HEAD `aa88a17c41` 源码 overlay 已复现，属于基线失败，本批未修改教程功能。
- 完整 unit 初次运行中的三个 PgDumper 测试因找不到 sh 失败；仅为测试进程补 `C:\SelfArea\DevTools\Git\bin` 后单独重跑全部 `TestPgDumper` 通过，不修改系统 PATH。
- `go test -tags=unit -p 1 -json ./...` 已完整结束：61 个包、22358 个测试/子测试通过，初次退出码为 1；失败为上述三项 Windows PATH 测试与两个基线 API 契约场景，不能把全套表述为全部通过。
- Windows 定向 race 测试以 `0xc0000139` 退出，不能宣称竞态检测通过；待 Linux 环境补跑。
- 首次验证时存在前端安装依赖与锁文件不同步及包体积警告。后续提交 `6e7c4b53b7` 已补齐 workspace overrides 并删除 pnpm 11 忽略的重复配置，冻结安装与构建通过，锁文件未修改；包体积警告仍保留。没有修改 Caddy 或部署生产镜像。

下一批按总体计划逐步实现：

| 任务 | 依赖 | 验收标准 |
|---|---|---|
| 前置阶段分段 | 本批开关/摘要 | 区分 body、auth/parse、安全检查、user/account slot 和路由，不用 auth_latency 冒充鉴权 |
| 请求/尝试预算 | 分段基线 | 预算由配置显式提供，默认不截断旧请求；退避不超剩余预算，不覆盖外部更短 deadline |
| failover 决策终态 | 前置阶段、预算 | 记录候选过滤/实际切换/耗尽原因，不以 switch_account 代表换号成功 |
| 协议完整性 | 各 adapter 边界 | 已输出不透明重放；HTTP 200/心跳不能代表有效内容与协议完成 |
| 排空资源边界 | 协议完整性、计费验证 | 客户端取消后无新尝试；在途排空有显式预算，usage 不完整时记录明确状态 |
| Caddy/香港实验 | 上述证据基线、单独部署授权 | 区分香港应用直连上游与转发实验；按 ISP、请求大小和模型比较，不承诺迁区解决供应商故障 |

所有新逻辑继续受该总开关控制，配置额度/超时值不从这次代码改造擅自推导为生产默认值。

## 后续进展

前置阶段观测的下一批实现、字段边界、验证与发布门见 [09-pre-upstream-stages.md](09-pre-upstream-stages.md)。阶段 2 的超时预算与完整长尾覆盖仍未完成，不把观测子阶段当作全部治理完成。


剩余开发项与待验证项的冻结清单见 [19-open-items-and-verification-status.md](19-open-items-and-verification-status.md)。
