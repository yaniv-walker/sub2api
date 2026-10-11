# 10｜Failover 决策剩余预算证据（2026-10-11）

本批只补充现有 failover 决策的观测字段，不改变重试次数、退避、账号池、候选过滤或默认行为。

在已有 `gateway.failover_*` 结构化事件中，若请求 context 有 deadline，新增 `remaining_deadline_ms`；没有 deadline 时省略该字段。记录覆盖同账号重试、账号切换、利润否决耗尽、单账号退避和单账号再次尝试。

字段是“日志生成时”的剩余父 context deadline，不是新的预算，也不承诺等于上游 HTTP client deadline。退避仍由既有 `sleepWithContext` 负责取消；客户端取消仍立即返回 `FailoverCanceled`，不会因为新增字段启动新 attempt。

验证：failover 定向测试通过，并新增 deadline 边界测试；没有为生产服务设置新值。下一步可用真实日志比较 `retry_delay` 与 `remaining_deadline_ms`，再设计显式请求/attempt budget。预算实现必须保留旧默认关闭语义，并优先尊重外部更短 deadline。
