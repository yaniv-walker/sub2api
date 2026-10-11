# 11｜重试退避预算边界（2026-10-11）

本批在既有 failover 共享 helper 上补充请求 deadline 边界，不增加次数，不设置新生产值。

`FailoverState` 的同账号重试、账号切换和单账号退避都复用 `sleepWithContext`。当父 context 没有 deadline 时保持原等待行为；当存在 deadline 且完整退避时间已经超过剩余时间时，helper 立即返回取消/终止信号，调用方不会在 deadline 后启动下一次上游尝试。等待期间仍监听 context cancellation，timer 会被释放。

这不是独立的总请求 budget：它只尊重调用方已经提供的更短 deadline。没有 deadline 的旧请求、旧配置和生产默认值不变。下一步仍需把请求/attempt budget 作为显式配置接入统一开关，并为预算耗尽补明确终态和指标；本批不将 context deadline 推断为业务 SLA。

验证：failover 定向测试、新增 deadline/退避边界测试通过；未部署生产、未重启本地服务。
