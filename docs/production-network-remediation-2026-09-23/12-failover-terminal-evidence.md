# 12｜Failover 最终决策证据（2026-10-11）

本批在统一请求快照中增加有界 `failover_decisions` 数组，最多保留 32 条。每项包含：

- `action`：`retry_same_account`、`switch_account`、`retry_selection`、`exhausted` 或 `canceled`；
- `reason`：固定枚举原因，不记录上游错误正文；
- `account_id`：账号标识，仅用于已有受控内部日志关联；
- `attempt_index`、`switch_count`：有限整数。

统一 access event 在治理开关开启时输出该数组；关闭时不创建状态、不输出字段。failover 逻辑、重试次数、候选筛选、计费与响应协议没有改变。客户端取消、退避被 deadline 截断、同账号重试、实际切换和耗尽均有明确动作。

该证据仍不是最终业务成功证明：`switch_account` 表示 failover 决策允许调用方继续选号，不代表下一账号已成功取得槽位或上游已返回成功。下一阶段需在协议 adapter/调用方补 attempt 关联和最终终态，并处理 `protocol_incomplete`、排空和 usage 完整性。

验证：requestobs 与 handler failover 定向回归通过；工作区本地服务未重启。本批未配置新开关或生产预算。
