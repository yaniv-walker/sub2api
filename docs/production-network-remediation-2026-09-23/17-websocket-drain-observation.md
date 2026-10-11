# 17｜WebSocket 终态与排空观测（2026-10-11）

本批复用 WebSocket 已有 `OpenAIForwardResult.UpstreamTerminalEvent`，在已有 `AfterTurn` 回调中增加一次统一协议摘要调用。`response.completed`/`response.done` 记录完成，失败/取消/不完整事件记录非完成；未知或空值保守记录缺失终止。不会解析 WebSocket 帧、修改重放、切换账号、usage 或客户端 close code。

流式 HTTP 路径新增的 `drain_observed`/`drain_usage_complete` 摘要与 WebSocket 生命周期保持独立；WebSocket 当前已有连接关闭和 usage 逻辑，尚未把 detached usage 读取统一成有界预算。

验证：WebSocket/调度和协议分类定向测试通过。未部署、未重启本地服务。

剩余发布任务：Linux race、完整 usage/计费对账、显式 drain budget、Caddy 公网验证和香港 A/B。它们需要对应运行环境或基础设施授权，不能仅凭本地代码推断完成。
