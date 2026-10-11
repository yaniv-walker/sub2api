# 14｜Responses 协议终态接入（2026-10-11）

本批在 Responses SSE 直转、Responses 常规处理、Responses→Messages 和 Responses→Chat 四个已有适配器处，各增加一个函数结束摘要钩子。它读取适配器已有 terminalEventType，不新增响应解析、不缓存正文、不改变转发/重试/计费结果。

统一开关关闭时钩子不记录。已识别的 response.completed / response.done / [DONE] 记录完成；response.incomplete 与缺失终止事件由 access 日志记录 protocol_incomplete。请求取消和下游写错优先于协议归因。失败事件记录不完整，未知事件只记录 missing_terminal 固定值，不把原字符串写入日志。

该字段描述**上游适配器看到的终止信号**，不证明公网客户端已收到最后一帧，不等于 billing/usage 完整。

验证：service 中 OpenAIStreaming、ForwardAsChatCompletions、ForwardAsAnthropic 相关回归及新增分类测试通过；middleware 整包 unit 通过；后端 build 与 service/middleware vet 通过。

覆盖仍有边界：原生 Anthropic、raw Chat 和 WebSocket 使用独立解析器，必须按各自语义接入，不能只以 usage 或任意 terminal 布尔值推断成功。显式 request/attempt/drain 配置、队列证据、逐协议 TTFT 和资源收敛验证仍未完成。本批未部署、未重启运行服务。
