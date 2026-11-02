## ADDED Requirements

### Requirement: 检测任务必须绑定唯一的目标分组与模型

系统 SHALL 提供「检测任务」（Task）领域对象，字段 MUST 至少包含：名称、目标分组 ID、目标模型、题目 ID 有序集合、间隔分钟、抖动秒数、启用状态、运行状态、状态原因、下次执行时间、最后执行时间、租约持有者、租约到期时间。同一「分组 + 模型」组合 MUST 最多存在一个 `enabled = true` 的任务，该约束 MUST 由数据库部分唯一索引强制（`WHERE enabled = true`），MUST NOT 仅依赖应用层检查。

#### Scenario: 创建与既有启用任务冲突的分组模型组合

- **WHEN** 已存在 `(group_id=1, model="gpt-4o")` 的启用任务，管理员又为同组合创建启用任务
- **THEN** 系统 MUST 以 409 状态码拒绝并返回表示冲突的稳定错误码
- **THEN** 数据库 MUST NOT 出现两行同组合且均启用的记录

#### Scenario: 并发创建同组合任务

- **WHEN** 两个请求几乎同时为同一 `(group_id, model)` 创建启用任务
- **THEN** 至多一个请求 MUST 成功
- **THEN** 失败方 MUST 收到 409 而非 500

#### Scenario: 禁用其中一个任务后重新创建

- **WHEN** 管理员禁用同组合的既有任务，然后创建新的同组合启用任务
- **THEN** 创建 MUST 成功

#### Scenario: 启用已有任务时有同组合的另一启用任务

- **WHEN** 管理员将一个禁用任务置为启用，而该组合已存在另一启用任务
- **THEN** 系统 MUST 以 409 状态码拒绝

### Requirement: 间隔与抖动必须受到严格校验

间隔 `interval_minutes` MUST 位于 `[15, 10080]`（15 分钟至 7 天）闭区间。抖动 `jitter_seconds` MUST 大于等于 0，且 MUST 满足 `interval_minutes * 60 - jitter_seconds >= 900`（即抖动不得使最小实际间隔低于 15 分钟）。不满足任一约束时系统 MUST 以 400 状态码拒绝。抖动 MUST 在 `[-jitter_seconds, +jitter_seconds]` 区间内随机取样，且最终 `next_run_at` MUST 不小于「当前时间 + 900 秒」。

#### Scenario: 间隔低于下限

- **WHEN** 管理员提交 `interval_minutes = 14`
- **THEN** 系统 MUST 以 400 状态码拒绝并返回表示间隔非法的稳定错误码

#### Scenario: 间隔高于上限

- **WHEN** 管理员提交 `interval_minutes = 10081`
- **THEN** 系统 MUST 以 400 状态码拒绝

#### Scenario: 抖动使最小间隔低于下限

- **WHEN** 管理员提交 `interval_minutes = 15` 与 `jitter_seconds = 600`
- **THEN** 系统 MUST 以 400 状态码拒绝，因为 `900 - 600 = 300 < 900`

#### Scenario: 抖动为负

- **WHEN** 管理员提交 `jitter_seconds = -1`
- **THEN** 系统 MUST 以 400 状态码拒绝

#### Scenario: 抖动恰好取边界值

- **WHEN** 管理员提交 `interval_minutes = 30` 与 `jitter_seconds = 900`
- **THEN** 系统 MUST 接受，因为 `1800 - 900 = 900 >= 900`

#### Scenario: 抖动取样有界

- **WHEN** 任务执行一次后计算下一次执行时间，`jitter_seconds = 300`
- **THEN** 下一次执行时间 MUST 落在 `now + 1800 ± 300` 区间内（按 `interval_minutes` 换算）
- **AND** MUST 不小于 `now + 900`

### Requirement: 调度必须多实例安全且不重复执行

调度器 MUST 通过 PostgreSQL `FOR UPDATE SKIP LOCKED` 领取到期任务，MUST 为领取成功的任务写入租约持有者标识与租约到期时间（默认 90 秒）。同一任务在同一时刻 MUST 只被一个实例执行。执行过程中 MUST 周期性续期租约；续期时 MUST 以「任务 ID + 租约持有者」为更新条件，当更新影响行数为 0 时执行方 MUST 立即终止本轮，并 MUST NOT 写入该任务的产出。

#### Scenario: 两个实例同时扫描到期任务

- **WHEN** 两个调度实例在同一时刻扫描到同一个到期任务
- **THEN** 只有一个实例 MUST 领取成功
- **THEN** 另一实例 MUST 领取到 0 行且不产生任何动作

#### Scenario: 租约被他人接管后原持有者继续写入

- **WHEN** 实例 A 的租约过期并被实例 B 领取，实例 A 随后尝试续期或写入结果
- **THEN** 实例 A 的续期 MUST 失败（影响 0 行）
- **THEN** 实例 A MUST 立即放弃本轮且 MUST NOT 写入产出

#### Scenario: 进程崩溃后租约自然过期

- **WHEN** 持有租约的实例被强制终止且未释放租约
- **THEN** 租约到期时间过后该任务 MUST 可被其他实例重新领取

#### Scenario: 领取成功后立即推进下次执行时间

- **WHEN** 调度器成功领取一个到期任务
- **THEN** 系统 MUST 在开始执行前把 `next_run_at` 推进到下一次计划时间
- **THEN** 即使本次执行崩溃，该任务 MUST NOT 在同一时刻被反复重试

### Requirement: 调度器生命周期必须有界且默认可关闭

调度器构造函数 MUST NOT 启动任何 goroutine。启动 MUST 通过显式 `Start(ctx)` 进行且 MUST 幂等。停止 MUST 通过显式 `Shutdown(ctx)` 进行，MUST 在给定 context 死线内取消内部循环、等待在途执行结束、并释放本实例持有的全部租约。当功能开关 `enabled = false` 时，调度器 MUST NOT 建立定时循环、MUST NOT 领取任务、MUST NOT 发起任何模型调用、MUST NOT 写入任何本功能数据表。

#### Scenario: 功能未启用时启动

- **WHEN** 服务启动时功能开关为 `enabled = false`
- **THEN** 系统 MUST NOT 建立调度 ticker
- **THEN** 系统 MUST NOT 创建内部虚拟账号、MUST NOT 创建任何 API Key
- **THEN** 数据库中本功能的 4 张表 MUST 保持零行

#### Scenario: 关闭过程有界

- **WHEN** 服务关闭且某任务仍在执行
- **THEN** `Shutdown` MUST 在死线内返回
- **THEN** 已释放租约 MUST 置为 NULL，使其他实例可立即接管

#### Scenario: 重复调用 Start

- **WHEN** `Start` 被连续调用两次
- **THEN** 系统 MUST 只存在一个调度循环
- **THEN** 第二次调用 MUST NOT 返回错误

### Requirement: 每次执行必须产生可回溯的执行记录

系统 MUST 为每一次任务执行创建一条 run 记录，字段 MUST 至少包含：任务 ID、分组 ID 与模型（均为快照值）、触发方式（`schedule` / `manual` / `retry`）、开始与结束时间、成功/失败/超时/被拦截计数、总耗时、总成本、总输入与输出 token 数、执行所用内部账号与 API Key 标识、run 状态（`running` / `success` / `partial` / `failed`）。run 的汇总值 MUST 由其下产出汇总得出。

#### Scenario: 全部题目成功

- **WHEN** 一次 run 的每道题都产出成功作品
- **THEN** run 状态 MUST 为 `success`
- **THEN** `success_count` MUST 等于题目数且 `failed_count`、`timeout_count`、`blocked_count` MUST 为 0

#### Scenario: 部分题目失败

- **WHEN** 一次 run 中部分题目失败或超时
- **THEN** run 状态 MUST 为 `partial`
- **THEN** 各计数 MUST 分别反映对应状态的产出条数

#### Scenario: 全部题目失败

- **WHEN** 一次 run 中没有任何成功产出
- **THEN** run 状态 MUST 为 `failed`

#### Scenario: 管理员手工触发

- **WHEN** 管理员对某任务触发「立即执行」
- **THEN** 系统 MUST 创建 `trigger = manual` 的 run
- **THEN** 该执行 MUST NOT 改变任务的 `next_run_at` 计划节奏（仅按正常间隔推进）

### Requirement: 任务状态必须支持人工与自动治理

任务 MUST 支持以下运行状态：`idle`、`running`、`quota_exhausted`、`budget_exhausted`、`account_disabled`、`paused_by_admin`。系统 MUST 支持管理员启用、禁用、立即执行、复制、删除任务。删除启用中的任务 MUST 先置为禁用；删除 MUST NOT 影响已产生的 run 与产出。`paused_by_admin` 状态 MUST 只能由管理员显式恢复。

#### Scenario: 管理员禁用任务

- **WHEN** 管理员将任务置为 `enabled = false`
- **THEN** 该任务 MUST NOT 再被调度领取
- **THEN** 该任务的历史 run 与产出 MUST 保持不变

#### Scenario: 删除任务

- **WHEN** 管理员删除一个任务
- **THEN** 系统 MUST 移除任务行或其启用标记
- **THEN** 该任务的历史 run 与产出 MUST 仍可被管理员查询

#### Scenario: 复制任务

- **WHEN** 管理员复制一个任务
- **THEN** 新任务 MUST 默认 `enabled = false`
- **THEN** 若与既有启用任务同组合，系统 MUST NOT 自动启用它

### Requirement: 失败必须按类别决定是否重试

系统 MUST 按下表处理失败：上游 5xx 与网络错误在单条产出内最多重试 2 次（指数退避 1 秒、3 秒）；上游 429 MUST NOT 重试且 MUST 为该任务追加一个间隔周期的退避；超时 MUST NOT 重试；内容审核拦截 MUST NOT 重试；余额不足 MUST NOT 重试并 MUST 触发配额熔断；危险构造阻断 MUST NOT 重试；无法解析出有效产出 MUST NOT 重试。系统 SHALL 支持管理员对某 run 的失败项发起重跑，重跑 MUST 新建 `trigger = retry` 的 run 且 MUST NOT 删除原有失败产出。

#### Scenario: 上游 5xx 后重试成功

- **WHEN** 某条产出首次请求收到 502，第二次请求成功
- **THEN** 该产出 MUST 记录为 `success`
- **THEN** 系统 MUST NOT 记录两次成本

#### Scenario: 重试耗尽

- **WHEN** 某条产出连续 3 次（首次 + 2 次重试）均收到 5xx
- **THEN** 该产出 MUST 记录为 `failed` 并带稳定错误码
- **THEN** 系统 MUST NOT 再进行第 4 次请求

#### Scenario: 收到 429

- **WHEN** 某条产出收到上游 429
- **THEN** 系统 MUST NOT 重试该产出
- **THEN** 该任务的下次执行时间 MUST 至少推迟一个间隔周期

#### Scenario: 重跑失败项

- **WHEN** 管理员对一个存在失败产出的 run 发起「重跑失败项」
- **THEN** 系统 MUST 只对失败状态对应的题目发起新 run
- **THEN** 原 run 与其中的失败产出 MUST 仍然存在
