# 13｜协议终止事件证据（2026-10-11）

本批完成两项低侵入修正：

1. failover 决策记录和退避预算检查统一受 `gateway_remediation` 请求快照控制；关闭时不产生新增状态或字段。`attempt_index` 使用当前 HTTP 上游尝试数，取消/重试/切换/耗尽原因保持固定枚举。
2. 增加显式协议终态字段。Responses 流在已经提交 HTTP 200 后写出 `response.failed` 时标记 `protocol_terminal=response.failed`、`protocol_complete=false`；access 事件因此不会把它误归类为普通 completed。通用 writer 不解析正文，不把 HTTP 200 自动当作协议完成。

这不是完整协议适配：正常 `response.completed`、Anthropic `message_stop`、SSE `DONE`、WebSocket 终止事件还需要在各 adapter 成功终点显式调用标记；目前未知终态仍保守处理。客户端取消、下游写错误和上游断流也未在本批改变原协议响应。

验证：`internal/pkg/requestobs`、`internal/server/middleware`、`internal/handler` 全量 unit 回归通过；修复了无 Request 的协议测试上下文兼容性。未部署或重启服务。

后续：按协议 adapter 接入成功终态 → 将 `protocol_incomplete` 与 `completed` 分离 → 增加客户端取消后的排空预算和 usage 完整性状态 → 再进入 Caddy/HK 实验。
