# 19｜剩余开发项与验证项状态（2026-10-11）

本文记录当前改造停止点，避免把“观测已开发”误认为“性能问题已解决”。所有后续项目都必须继续使用 `gateway_remediation` 总开关，并保持切面/适配器边界设计。

## 已完成

- 统一治理开关、正文读取和前置阶段观测。
- failover 剩余 deadline 证据、退避不跨越已有 deadline。
- failover 动作、尝试序号、切换/取消/耗尽原因观测。
- Responses、raw Chat、原生 Anthropic、主要 Chat/Messages 转换路径和 WebSocket 的协议终态观测。
- 客户端断开后的 `drain_observed` 与 `drain_usage_observed` 证据字段。
- 本地定向测试、主要 service/middleware/handler 回归、构建和 vet。

## 待开发项

### P1：剩余协议适配覆盖

覆盖原生 Responses 特殊路径、媒体流和其他非通用客户端的终止事件；每个 adapter 必须明确成功、失败、不完整和取消事件。验收：不依赖 HTTP 200 推断完成，不解析或保存 prompt/响应正文，不改变现有重试和计费路径。

### P1：指标聚合与告警数据面

将现有结构化日志汇总为低基数 RED/USE 指标：前置阶段 P95/P99、上游首字节、首个有效输出、chunk gap、终态、attempt、failover、客户端取消、排空观测。验收：不使用 request ID、用户 ID、原始 URL 或错误正文作为指标标签；日志仍可按 request ID 回溯。

### P2：逐协议有效输出与 TTFT

区分心跳、协议控制帧和有效模型内容，记录协议级 first token/first event 与最大间隔。验收：SSE、Responses、WebSocket、Anthropic 各自有明确语义；心跳不重置模型输出 SLA。

### P2：资源收敛验证

在受控测试中验证客户端取消后的 upstream body、连接、goroutine、FD 和槽位最终释放。验收：取消后不启动新 attempt，排空路径不会无界增长。

### 明确暂停的范围

按用户要求，暂不开发以下功能：

- 独立请求总预算或 attempt budget 的新增配置和行为限制；
- 独立 drain budget 或强制缩短现有排空时长；
- 任何计费、usage 计算、扣费、重复计费防护或账单写入改造。

已有的 deadline 保护和观测字段保留；它们不改变默认计费和排空行为。

## 待验证项

### V1：Linux race

使用 Go 1.27.2 或更高版本的 Linux CI/容器运行受影响包及全仓 race；当前 Windows 定向 race 和 Go 1.27.0 临时镜像均不能作为通过证据。

### V2：真实 WebSocket

通过本地受控 WebSocket 请求验证 `protocol_terminal`、`protocol_complete`、取消和多 turn；保存脱敏日志样本。

### V3：真实客户端取消排空

使用受控上游和慢流测试客户端取消，核对 `drain_observed`、`drain_usage_observed`、连接/协程/槽位释放；不使用真实付费请求做压力实验。

### V4：Caddy 公网下行

只读确认实际 Caddy 版本、配置、HTTP/1.1/2/3、压缩/缓冲、write timeout 和客户端方向；用同一应用请求比较应用完成时间与 Caddy 完成时间。未获生产运维授权前不改配置。

### V5：香港 A/B

需要独立香港资源和部署授权。分别比较香港完整应用直连上游、香港仅作边缘代理两种拓扑；按地区/ISP、协议、请求大小、模型和 streaming 分组，不能把香港到新加坡跳转结果表述为香港直连收益。

## 完成判定

只有 P1 开发项、V1–V3 通过，且 Caddy/HK 实验按发布门完成后，才能宣称总体性能治理闭环。当前状态为：**主要观测与安全 failover 保护已完成；剩余开发和基础设施验证未完成；计费/预算行为按要求暂停。**
