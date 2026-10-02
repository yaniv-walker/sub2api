# 02 上游重试保护实现说明

本阶段新增「系统设置 → 网关服务 → 上游重试保护」开关，默认关闭，设置键为 `upstream_retry_guardrails`。它只控制新请求的 failover 判定，不改变账号池、同账号重试次数、退避和临时封禁配置。

开关开启后，OpenAI 兼容路径在检测到本次上游尝试已经向下游写出响应内容时，禁止继续透明切换账号重放；客户端 context 已取消时也禁止启动新尝试。其他入口原本已有更严格的写出保护，保持原逻辑。设置通过内存快照进入 request context，保存后新请求生效，正在进行的请求不切换策略；数据库异常保留最近一次有效状态或默认关闭。

本阶段采用低侵入方式：新增独立 SettingService、middleware 和 handler helper，只在统一路由安装 middleware，并收紧现有 `openAIForwardMayFailover` 入口。没有修改账号选择器、重试次数默认值、计费逻辑或上游请求协议。

接口：

- `GET /api/v1/admin/settings/upstream-retry-guardrails`
- `PUT /api/v1/admin/settings/upstream-retry-guardrails`，请求体 `{"enabled":true|false}`

验证覆盖：严格保护 helper、客户端取消、旧模式兼容、设置持久化/保存失败、服务端/插件定向测试、前端设置组件和 i18n 测试、嵌入构建与前端生产构建。开启前应先在测试或小流量环境观察 upstream 5xx、切换成功率、TTFT、流断连和重复计费指标。
