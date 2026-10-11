# 15｜协议覆盖补充（2026-10-11）

本批继续以低侵入方式接入两个已有协议解析边界：

- raw Chat Completions 使用现有 `openAIRawStreamTerminalState.Terminated()`；收到 `[DONE]`、usage 或 finish_reason 时记录完成，否则记录缺失终止。
- 原生 Anthropic Messages 使用现有 `sawTerminalEvent`；已收到 `message_stop` 时记录完成，否则记录缺失终止。

两处都只在函数结束时读取已有布尔状态，不增加 chunk 解析、日志频率、响应修改或计费分支。统一开关关闭时不产生协议状态。

当前协议观测覆盖：Responses 主路径及转换路径、raw Chat、原生 Anthropic Messages。WebSocket 和部分原生 Responses/媒体适配器仍由各自终止语义处理，未强行套用 HTTP SSE 规则。

验证：相关 service 定向测试与协议分类测试通过。客户端取消后的排空仍沿用既有 `WithoutCancel` 路径，尚未增加可配置排空预算；Caddy/香港属于独立基础设施实验，未在本地代码中伪造完成。
