# 01 实现说明：请求关联与流式阶段观测

## 实现范围

本分支的阶段观测功能是**默认关闭、显式启用**的日志扩展，不改网关转发、failover、计费和响应正文。启动应用时设置 `SUB2API_REQUEST_OBSERVABILITY=1` 可启用；其他值保持关闭。仅在应用进程启动时读取，运行时修改环境变量不会切换状态。启用需经过测试、容量评估与生产变更授权。内部 `RequestID` 的服务端生成语义属于始终生效的身份边界修正，不受此开关控制；有效外部 `X-Request-ID` 仍按原约定回显。

没有新增 `/metrics` 或公网观测接口。项目现有 Ops 监控、请求日志和 Server-Timing 各自维持原有用途；本 change 复用结构化日志，不新增数据库表、外部 collector 或 Prometheus 依赖。正式指标导出、直方图桶、采样和 SLO/告警要按 01 设计的决策门另行评审，不能把日志字段误称为已部署的时序指标。

## 接入点与现有能力核验

| 位置 | 现有能力 | 本次接入 |
| --- | --- | --- |
| `server/middleware/request_logger.go` | 入口生成/回显 `X-Request-ID`，将 `RequestID` 放入 context | 内部 `RequestID` 固定由服务端生成；有效入站 `X-Request-ID` 继续回显，另作为外部关联提示保存 |
| `server/middleware/client_request_id.go` | Gateway、Gemini、Codex direct、Antigravity 等路由生成 `ClientRequestID` | 保留原有服务端生成语义；不接受客户端伪造值用于此 key |
| `service/gateway_usage_billing.go` | `ClientRequestID` 可参与计费去重 | 不改变；外部关联提示绝不写入此 key |
| `server/middleware/logger.go` | 请求完成 access log；既有字段含 client IP、路径、模型 | 仅在新开关启用时追加有限的阶段字段 |
| `handler/ops_error_logger.go` | 既有响应包装、错误收集 | 不修改；新 wrapper 位于统一路由中间件，保持 Gin writer 接口 |
| `handler/failover_loop.go` | 已记录 same-account retry、账号切换与上游状态 | 不改 failover；本次增加真实 HTTP 调用的尝试日志 |
| `repository/http_upstream.go` | 通用上游 HTTP `Do` / `DoWithTLS` | 仅有观测 context 时附加 `httptrace`，记录真实 HTTP 调用结果 |

`server/router.go` 统一安装开关；原有 `RequestLogger` 和 `Logger` 的位置不变。WebSocket、部分自建 HTTP 客户端、未经过通用 `HTTPUpstream` 的上游调用不在上游阶段日志覆盖范围内。模拟/单元测试可直接安装中间件，不依赖环境变量。

## 字段字典

### 标识

- `request_id`：服务端生成的 36 字符 UUID，每次请求独立；用于同进程日志关联。
- `ExternalRequestID`（context 内部字段）：从 `X-Client-Request-ID` 优先、其次 `X-Request-ID` 读取的**客户端可控提示**；只接受最多 64 字节的 ASCII 字母、数字、`-_.:`。不能用于鉴权、去重或计费。无有效输入时为空。新增日志中仅有 `external_request_id_sha256`，不写提示原值；此摘要是查询索引，不是抗低熵枚举的秘密保护机制。
- `client_request_id`：Gateway 既有的服务端生成 ID；用于 Ops/计费路径，不等同于上述外部提示。
- 响应 `X-Request-ID`：有效入站值继续回显以保留兼容性；否则回显内部 ID。因此提供自定义头时，响应头**不一定等于内部日志的 `request_id`**；请结合 `external_request_id_sha256` 和时间窗定位。响应 `X-Client-Request-ID` 维持既有行为。

### 请求完成事件 `http request completed`

启用时追加：`route_template`（Gin 路由模板；未匹配为 `unmatched`）、`streaming`（最终 `Content-Type` 为 `text/event-stream`）、`status_class`（如 `2xx`）、`terminal_reason`、`bytes_written`、`write_count`、`upstream_attempts`、`upstream_errors`；有效客户端提示另有 `external_request_id_sha256`。有观测值才追加 `first_write_ms`、`max_write_gap_ms`、`terminal_silence_ms`、`upstream_last_status`、`upstream_last_duration_ms`、`upstream_total_duration_ms`。

- `first_write_ms`：请求进入新中间件到**第一次成功调用响应 writer 写入字节**；不是 Caddy/客户已收到首字节，也不是模型首 token。只刷新 header 而无 body 时省略。
- `max_write_gap_ms`：相邻两次成功写入之间的最大间隔；可能包含 SSE 心跳，不等于 token 间隔。只有至少两次写入才有意义。
- `terminal_silence_ms`：最后一次成功写入到请求 handler 结束的间隔；可帮助定位流末尾停顿，但包括应用内后处理，不能单独证明客户网络故障。
- `upstream_*`：通过通用 HTTPUpstream 发起的实际 HTTP 调用；与 handler 级“选号/重试决策”不同。一个 `Do` 内部的重定向或特定兼容 fallback 可能涉及多个 transport hop，不保证一跳一条日志。
- `terminal_reason`：`completed`、`client_cancelled`、`downstream_write_error`、`upstream_error`、`deadline`、`handler_error`、`unknown`。该分类依据应用可见状态，不是根因鉴定。`completed` 可以包含正常返回的 4xx；须结合 `status_class`。

### 上游事件 `http upstream request completed`

每次被观测的通用 HTTP 调用含：`request_id`（若上下文带入口 logger）、`account_id`、`status_code`、`duration_ms`、`connection_reused`、`method`、`request_upstream_attempts`、`downstream_started`、`upstream_profile`（如存在）。`request_upstream_attempts` 是截至此调用结束已完成的尝试数，并发调用时不是严格的启动顺序；`downstream_started` 表示此调用结束时应用是否已写出响应字节。仅测得时写入 `connect_ms`、`first_byte_ms`；错误时只记录低基数 `error_class`（`context_canceled`、`context_deadline`、`transport_error`），不记录原始错误正文、URL、上游凭据或响应体。

`first_byte_ms` 是 HTTP transport 第一次响应字节回调的时间，不是 SSE 首 token。`connect_ms` 对连接复用可能没有值；代理、重定向、多地址尝试下仅能作为该 `Do` 的局部提示，不能直接解释整个 DNS/TLS 建连链路。

## 使用与只读查询

1. 仅在测试或获授权的小流量实例设置开关后重启进程；不需改 Caddy。
2. 记录启用前日志量、CPU、内存、P95 请求耗时；启用后对同类负载比较。数据量增加不可接受时撤掉开关并重启。
3. 在已有结构化日志平台按 `request_id` 查 `http request completed`、`http upstream request completed`、`gateway.failover_*`。若客户只提供其自定 `X-Request-ID`，先在受控本地环境计算该 ASCII 值的 SHA-256 十六进制小写摘要，按 `external_request_id_sha256` 和时间窗找到内部 `request_id`。例如 PowerShell：`$v='client-id'; $h=[Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($v)); [Convert]::ToHexString($h).ToLowerInvariant()`。不要将密钥代入这个查询过程。
4. 对一条慢流检查 `first_write_ms`、`max_write_gap_ms`、`upstream_attempts`、`upstream_errors` 和终态；不能以单条日志判断客户公网丢包或谁先断连。
5. 日志访问遵循现有权限与保留期；原 access log 仍有 client IP、路径和模型等既有字段。本次不扩大其收集范围，也不能声称日志完全匿名。事件导出、共享和长期留存前须另行脱敏与权限审查。

## 未完成项与后续决策

- 没有真实 `first_token_ms`：需逐协议定义有效事件/token 的边界，不能用首次 write 伪装。
- 没有独立 Prometheus/OTel 指标导出或告警，也就没有“指标标签基数自动验证”；按 01 文档决策门单独设计鉴权、网络暴露、采样与桶配置。
- 未覆盖所有非通用上游客户端、WebSocket 帧和 Caddy 到客户的真实网络写入；需要端到端样本及客户侧测量。
- 未在生产做压测或灰度。Windows 本机 `BenchmarkRequestObservabilityMiddleware`（单次 12 字节响应、各重复 3 次）测得关闭时约 301–323 ns/op、288 B/op、6 allocs/op，开启时约 804–873 ns/op、840 B/op、10 allocs/op。此数据只说明中间件本身有约 4 次分配和约 0.5 微秒级额外成本；不包括日志 I/O、真实上游、长流并发或生产负载，不能作为容量承诺。
- Windows 本机 `go test -race` 运行时曾以 `0xc0000139` 退出；2026-10-01 已在本地 Linux 容器中通过请求状态、中间件和 HTTP 上游的定向竞态测试。
- 2026-10-01 已修复插件类型/包遗漏、过期 Wire 注入和前端 pnpm 构建配置；已完成普通/嵌入前端服务端构建和前端生产构建。完整回归、可体验的本地演示与低侵入性审查见 [测试与审查说明](01-testing-and-review.md)。
