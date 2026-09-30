# 01｜P0：端到端请求关联与流式可观测性

- **类型：**开发设计 + 实施待办
- **优先级：**P0；后续 P1–P5 的判因基础
- **状态：**部分实现于 `feature/production-request-observability`；已实现范围与未完成项见 [实现说明](01-implementation-notes.md)，不代表已部署或全部验收通过
- **目标仓库：**sub2api-original
- **关联：**[02 上游错误与重试](02-upstream-retry.md)、[03 Caddy 流式代理核验](03-caddy-streaming.md)、[04 香港入口实验](04-hk-entry-ab.md)、[05 403/404 分流](05-client-errors.md)、[06 运行监控](06-monitoring-capacity.md)

## 1. 背景与问题

生产只读排查发现客户端方向写超时/重置、上游 502/503/重试、长时间流请求并存。当前已有请求 ID 基础设施（`backend/internal/pkg/ctxkey`、Gateway `middleware.ClientRequestID()`），但需要先逐入口核实传播范围与现有日志字段，不能假设所有日志已可关联。仅凭 `context canceled` 无法判断取消发起方；完整请求时长也不能代替 TTFT（首 token 延迟）。

## 2. 目标与非目标

### 目标
- 单个请求可跨 Gateway、failover、上游调用日志关联；若无法同进程关联，至少有明确关联字段。
- 对流式与非流式请求分别记录阶段时间与终止原因。
- 可按低基数维度聚合地区/协议/API 路由/上游类型/状态，不泄漏用户或密钥信息。
- 遥测故障不得改变网关转发、计费、重试或协议响应行为。

### 非目标
- 本 change 不引入完整 OpenTelemetry 平台/外部 SaaS，不持久化请求正文，不记录 token、Cookie、Authorization 或上游凭据。
- 不把每个 request ID、用户 ID、客户 IP、账号 ID、模型任意原文作为 Prometheus label。
- 不修改生产 Caddy 或服务器配置。

## 3. 代码现状核验要求

开始实现前须检查并写入 PR 说明：
1. `backend/internal/pkg/ctxkey/ctxkey.go` 中 RequestID 与 ClientRequestID 的生成/信任语义。
2. `backend/internal/middleware` 的 ClientRequestID 实现及 Gateway、Gemini、Codex direct 等路由是否都接入。
3. `backend/internal/handler/logging.go`、`ops_error_logger.go`、Gateway handler 和 `failover_loop.go` 当前日志是否已有时间点/脱敏字段。
4. 是否已有指标库/指标端点、日志采样、运行开关；复用现有能力，不重复建设。
5. 将请求 ID 视为日志关联值而非客户端可控制的认证信息；验证客户端提供重复/异常 ID 时的长度、字符和回显策略。不得信任客户端 ID 作为唯一内部 ID。

## 4. 设计

### 4.1 请求上下文
- 为每次 ingress 生成或取得**服务端内部 request ID**，在上下文中不可变地保存；客户端提供的 ID 单独保存在已有 ClientRequestID 字段，先核实现有约定。
- 内部 ID 的来源必须为安全随机值，格式和长度固定；只允许安全字符。由服务端产生的 ID 才能用于内部跨组件关联。
- 明确响应头策略：若现有兼容协议已承诺某头则保持；否则不得未经兼容性审查新增或改变头。客户端 request ID 不覆盖内部 ID。
- 上游是否转发 request ID 按协议白名单决定；默认不发送客户可控标识给上游。

### 4.2 阶段事件/时间字段
以单调时钟计算持续时间，wall clock 仅用于日志时间。请求级结构建议字段：
- `request_id`（内部随机 ID）；`client_request_id` 仅在已脱敏/长度校验后写日志。
- `route_template`（路由模板，不是带用户 ID 的原始 URL）、`protocol`、`streaming`、`result_class`。
- `ingress_at`、`upstream_attempt_count`、`upstream_connect_ms`（若传输层可得）、`upstream_first_byte_ms`、`first_token_ms`、`first_client_write_ms`、`last_chunk_gap_ms`、`total_duration_ms`。
- 终态枚举：`completed`、`client_cancelled`、`downstream_write_error`、`upstream_error`、`deadline`、`handler_error`、`unknown`；不能从错误推断来源时使用 `unknown`。
- 上游每次尝试的状态码、错误类别、耗时、是否在客户端首字节/token 前发生、是否允许/执行 failover。不得记原始错误正文。

字段可分阶段落地。只有可准确测量且测试覆盖的时间才能输出；未测字段省略，不填 0 伪装成功。`first_token_ms` 必须定义为首个可交付给客户端的有效流数据时间点，并说明对各兼容协议如何识别；若只能测首字节，应命名 `first_byte_ms`，不可冒称首 token。

### 4.3 日志与指标
- 结构化日志按请求开始/终态或现有采样策略记录，避免每个 token/chunk 一条日志。
- 复用现有 logger 和 ops 日志能力；明确采样、保留期、权限与脱敏规则。
- 指标采用受控枚举标签：`protocol`、`route_template`、`streaming`、`status_class`、`terminal_reason`、`upstream_kind`。按代码真实类别注册允许值；禁止动态原值作为 label。
- 推荐指标：请求计数、请求完整时长直方图、TTFT 直方图、chunk gap 直方图、上游尝试/错误计数、终止原因计数、重试计数。所有直方图桶和指标暴露方式须按现有监控栈核实后确定，不先固定未经负载验证的桶值。
- 不增加响应正文采集、不记录 API Key、账号凭据、客户 IP、原始 prompt、完整查询串、完整 URL 或未脱敏上游错误。

### 4.4 开销与故障隔离
- 默认关闭或沿用已有遥测开关；指标采集应非阻塞且不能等待远程 collector。
- 指标/日志 backend 失败时转发照常继续；不得因写日志失败产生二次请求或重复计费。
- 明确内存上限、队列满行为、采样及并发安全。敏感关联字段限制访问与保留时间。

## 5. 实施任务

- [ ] A. 完成上述代码现状核验，形成字段映射表及需改文件清单。
- [ ] B. 定义 request context/event 类型及终态枚举；不制造第二套与现有 ctxkey 冲突的 request ID。
- [ ] C. 在共同 middleware/handler 边界接入内部 ID，覆盖支持的网关入口；为未接入路由补测试。
- [ ] D. 在上游 transport/failover 边界测量每次尝试耗时与结果，识别首输出前后边界。
- [ ] E. 在流写出边界测量首字节/协议可定义的首 token、最大 chunk gap、客户端写错误；流内容不进入遥测。
- [ ] F. 复用现有日志和指标设施；添加枚举/基数约束及脱敏。
- [ ] G. 添加配置说明、运维查询示例及字段字典。

## 6. 验收标准

- 单元测试验证内部 ID 固定、客户端伪造值不能覆盖内部 ID、格式校验及无敏感头泄漏。
- HTTP 非流式、SSE/流式成功、上游 503 后重试、客户端取消、下游写失败、deadline、日志/指标 backend 故障均有测试。
- 首 token/首字节语义文档与每个协议测试一致；不可测时明确标为 unavailable。
- 自动测试验证指标 label 只来自有限枚举，request ID/客户标识不会变成 label。
- 对照现有测试运行 Gateway、failover、相关协议单测；再运行后端完整 unit 测试、lint。
- 负载基准比较开启前后 CPU/内存/吞吐与尾延迟；门槛在基线测量后由项目负责人批准，不虚构数字。
- 文档记录数据保留、访问权限、脱敏字段及失败隔离行为。

## 7. 风险与决策门

- 多协议的“token”边界不同；不得声称协议无关的精确 TTFT，必要时先交付首字节/首事件指标。
- failover 有账号/计费副作用；遥测改动不得改变调用顺序、取消传播或重试条件。
- 若项目尚无指标导出设施，先提出独立方案（本地指标端点/外部 exporter 的安全边界、鉴权、网络暴露），经批准后实现，不擅自新增依赖或公开端点。
