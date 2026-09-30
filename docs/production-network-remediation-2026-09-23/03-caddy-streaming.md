# 03｜P2：Caddy 流式代理配置核验与变更设计

- **类型：**运维/配置整改设计（如涉及应用则另建开发任务）
- **优先级：**P2；先于任何生产超时调整
- **状态：**待核验；禁止仅按本文直接改生产
- **代码基线：**本仓库 `deploy/Caddyfile`；实际服务器生效配置尚须现场只读取证

## 1. 当前仓库观察（不是生产生效证明）

仓库示例 `deploy/Caddyfile` 配有 server `read_header 10s`、`idle 2m`，反代到 localhost:8080，HTTP transport keepalive 120s，关闭压缩，并将 encode matcher 限制到明确内容类型、排除 `text/event-stream`。这些只能说明仓库文件当前内容，不能证明服务器运行配置、Caddy 版本、适配后 JSON 或部署路径与之相同。此前生产只读摘要报告 `idle_timeout=120s`，其精确配置来源仍应复核。

## 2. 目标

验证 TLS/HTTP 版本、代理 transport、响应刷新/缓冲、SSE、取消传播和超时是否符合长流需求；每项调整先建立归因和可重现对照，并具备配置校验与回滚。

## 3. 变更前只读取证清单

- [ ] 记录 Caddy 精确版本、启动参数、运行配置来源、容器/宿主机边界、配置挂载和最近变更时间；秘密值脱敏。
- [ ] 获取实际有效配置（若通过 admin API，严格只读；不暴露凭据），记录 server/global options、site handler、reverse_proxy transport、encode、headers、日志格式。
- [ ] 核实 DNS/CDN/LB 是否在 Caddy 前面、真实 TCP 对端与可信代理设置；不可用伪造 XFF 代替网络来源。
- [ ] 抽取代表性请求：协议、是否 SSE、响应 Content-Type、是否压缩、首事件时间、chunk 间隔、断开时刻、双方日志 correlation ID。
- [ ] 查证 Caddy 官方文档中所用版本的 `idle`、`read_header`、`stream_timeout`、`stream_close_delay`、`flush_interval` 与 transport keepalive 的语义；文档与版本不匹配时按服务器版本核对。
- [ ] 查明 upstream app 在流无数据、上游 stall、客户端断开时的实际响应行为。

## 4. 设计原则

- 不把 server idle timeout、读 header 超时、上游 dial/response header timeout、keepalive 生命周期、stream timeout 当成同一概念。
- 先证明哪一段超时及发生时刻，再提出单项改动；不能由“最大流长”直接推导所有 timeout 必须设为更大值。
- 确认 SSE 响应不被意外 gzip/zstd、代理缓冲或应用缓冲；依据真实 `Content-Type` 与 handler 实现测试，不只依据文件注释。
- 不为消除断连而无限制延长资源占用；每个超时必须考虑并发连接上限、内存、上游费用、慢客户端占用和 DoS 风险。
- HTTP/1.1、HTTP/2、HTTP/3（若启用）分开验证；不因服务器端一次 curl 成功推断所有客户协议稳定。
- 错误页 handler 不能吞掉 SSE 中途错误或把响应改造成看似完整的成功；需用集成测试覆盖。

## 5. 分阶段实施

1. **配置差异审查：**生成脱敏的仓库配置—生产配置差异表；确认版本与文档语义。
2. **可重复实验：**测试环境搭建可控上游：立即响应、延迟首字节、间歇 chunk、静默超过阈值、上游 reset、客户端慢读/取消。
3. **形成变更提案：**每次仅调整一类参数；附当前/目标值、证据、预期效果、副作用、Caddy validate 命令、部署步骤和回滚值。数值由实测定，不在设计阶段捏造。
4. **灰度验证：**独立实例/小流量；同时看连接占用、FD、内存、TTFT、chunk gap、下游写失败及上游错误。
5. **决定：**如果行为并非 Caddy 超时/缓冲导致，不改生产配置，转交入口链路或应用排查。

## 6. 验收

- 实际配置与仓库模板差异、Caddy 版本与适用官方文档已归档。
- 集成测试证明 SSE chunk 按期到达客户端、不会被意外压缩/缓冲；静默/慢客户端/取消行为符合定义。
- Caddy 配置语法检查通过；升级/变更前能恢复已知良好配置。
- 对比各协议路径和变更前后指标；错误率改善不得以不受控连接累积换取。
- 生产操作由用户/运维负责人另行授权；本文不构成执行授权。
