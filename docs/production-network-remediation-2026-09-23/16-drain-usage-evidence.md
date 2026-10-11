# 16｜客户端取消后的排空与 usage 证据（2026-10-11）

本批在已有流式服务函数的客户端断开路径增加统一摘要钩子：当服务继续读取上游以完成 usage 结算时，access 日志增加 `drain_observed=true` 和 `drain_usage_complete`。摘要只读取已有 usage 结果，不改变 `WithoutCancel` 排空上下文、计费写入、上游关闭或响应内容。

覆盖 raw Chat、OpenAI Chat、Messages、Passthrough 和原生 Anthropic 的主要流式函数；未覆盖的媒体/WebSocket 路径继续使用原有生命周期。治理开关关闭时不产生字段。

`drain_usage_complete=false` 表示客户端已断开且当前函数返回时没有可确认 usage；它不代表一定没有上游消耗。该字段不能替代供应商账单核对，也没有新增排空时长限制。本批保持现有排空行为，后续若设置显式 drain budget 必须先按协议和计费语义分别验证。

验证：requestobs、service 定向测试通过；未部署或重启本地服务。
