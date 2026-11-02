## ADDED Requirements

### Requirement: 检测必须使用独立内部虚拟账号且不得影响真实用户

系统 MUST 以一个独立的内部虚拟账号执行全部检测调用。该账号 MUST 满足固定字段组合：邮箱为 `iq-benchmark@iq-benchmark.invalid`、角色为普通用户（`user`）、状态为 `active`、密码哈希为一次性随机值且永不写入日志或任何响应体、`notes` 以 `iq-benchmark:internal-agent` 为前缀。系统 MUST NOT 为承载该账号而给 `users` 表新增列、MUST NOT 新增角色枚举值、MUST NOT 修改 `users_signup_source_check` 约束或扩展其白名单。检测调用产生的费用 MUST 从该内部账号自身的余额/配额扣减，MUST NOT 扣减任何真实用户的余额或配额。检测请求 MUST 携带专用 API Key，归属于该内部账号并绑定目标分组。

#### Scenario: 执行检测后真实用户余额不变

- **WHEN** 一次检测执行完成并产生用量与扣费
- **THEN** 内部账号的余额 MUST 减少对应金额
- **THEN** 所有真实用户的余额与配额 MUST 保持完全不变

#### Scenario: 内部账号状态必须为 active

- **WHEN** 系统初始化内部账号
- **THEN** 该账号 `status` MUST 为 `active`
- **THEN** 该账号 `role` MUST 为 `user` 而非管理员

#### Scenario: 不改动上游用户表结构

- **WHEN** 交付物被审查
- **THEN** MUST NOT 存在针对 `users` 表的 `ADD COLUMN` 迁移
- **THEN** MUST NOT 存在新增的角色枚举值
- **THEN** MUST NOT 存在对 `users_signup_source_check` 的修改

#### Scenario: 密码哈希不可获取

- **WHEN** 管理员通过任意管理端接口查询该内部账号
- **THEN** 响应 MUST NOT 包含密码哈希
- **THEN** 系统日志 MUST NOT 包含密码哈希或其明文

### Requirement: 内部账号判定必须收敛到唯一函数

系统 MUST 提供唯一判定函数 `IsInternalAgent(user) bool`，其判定依据 MUST 为 `notes` 字段的前缀匹配。该函数 MUST 位于新增的独立模块内。除该模块外，任何代码 MUST NOT 硬编码该邮箱字面量或 `notes` 前缀字面量用于身份判定。判定函数 MUST 对 `nil` 输入安全返回 false。

#### Scenario: 对内部账号判定为真

- **WHEN** 传入一个 `notes` 以 `iq-benchmark:internal-agent` 开头的用户
- **THEN** `IsInternalAgent` MUST 返回 true

#### Scenario: 对普通用户判定为假

- **WHEN** 传入一个 `notes` 为空或不含该前缀的用户
- **THEN** `IsInternalAgent` MUST 返回 false

#### Scenario: 空指针输入

- **WHEN** 传入 nil
- **THEN** `IsInternalAgent` MUST 返回 false 且 MUST NOT panic

#### Scenario: 前缀字面量不得外泄使用

- **WHEN** 静态检查搜索该前缀字面量
- **THEN** 除新增独立模块外 MUST 无匹配

### Requirement: 内部账号初始化必须幂等且不得阻塞主服务启动

系统 MUST 支持内部账号与其专用 API Key 的幂等初始化：先按 `notes` 前缀查找，命中则复用；未命中则创建。初始化 MUST 使用 PostgreSQL 咨询锁串行化，避免多实例并发重复创建。初始化失败（包括目标分组未配置、分组不存在、数据库错误）MUST NOT 阻塞主 API 服务启动，MUST 记录 WARN 级日志并将功能标记为「未就绪」。功能开关为 `enabled = false` 时 MUST NOT 执行初始化。

#### Scenario: 重复初始化只创建一个账号

- **WHEN** 初始化被连续调用两次
- **THEN** 数据库中该前缀的账号 MUST 只有 1 行
- **THEN** 专用 API Key MUST 只有 1 个

#### Scenario: 多实例同时初始化

- **WHEN** 两个实例同时执行初始化
- **THEN** 至多一个实例 MUST 执行创建
- **THEN** 最终账号数 MUST 为 1

#### Scenario: 目标分组未配置

- **WHEN** 初始化时设置中的目标分组为空或该分组不存在
- **THEN** 系统 MUST NOT 创建 API Key
- **THEN** 系统 MUST 返回明确的「无目标分组」错误并使功能处于未就绪状态
- **THEN** 主 API 服务 MUST 正常启动

#### Scenario: 功能未启用时不初始化

- **WHEN** 功能开关为 `enabled = false`
- **THEN** 系统 MUST NOT 创建内部账号或 API Key

### Requirement: 检测流量必须可被精确识别且不污染统计口径

检测调用产生的用量记录 MUST 携带内部账号的用户标识与专用 API Key 标识，并 MUST 在 `user_agent` 中写入固定的检测标识（`sub2api-iq-benchmark/1.0`）。系统 MUST NOT 为此给既有用量表新增列。系统的所有统计入口 MUST 具备按 `user_id` 或 `api_key_id` 过滤的能力；本功能自身的全部成本与用量统计 MUST 以内部账号的 `user_id` 或专用 `api_key_id` 为唯一过滤条件。

#### Scenario: 检测用量可被按内部标识过滤

- **WHEN** 执行一次检测后查询用量记录
- **THEN** 存在 `user_id` 为内部账号、`api_key_id` 为专用 Key 的记录
- **THEN** 该记录的 `user_agent` MUST 为 `sub2api-iq-benchmark/1.0`

#### Scenario: 本功能成本统计口径唯一

- **WHEN** 管理员查看检测成本统计
- **THEN** 统计 MUST 只聚合内部账号 `user_id` 或专用 `api_key_id` 的用量
- **THEN** MUST NOT 聚合任何其他用户的用量

#### Scenario: 不改动用量表结构

- **WHEN** 交付物被审查
- **THEN** MUST NOT 存在针对用量表的新增列迁移
- **THEN** MUST NOT 存在对既有统计查询默认行为的语义修改

#### Scenario: 内部账号不影响用户维度统计

- **WHEN** 查询任一真实用户的用量统计
- **THEN** 结果 MUST 仅包含该用户自身数据
- **THEN** MUST NOT 包含内部账号的任何用量

### Requirement: 检测调用必须经由本项目自身网关

系统 MUST 通过本项目自身的 OpenAI 兼容网关发起检测调用：图片类题目 MUST 调用 `/v1/images/generations`，文本与代码类题目 MUST 调用 `/v1/chat/completions`。系统 MUST NOT 直接向模型供应商上游发起调用。调用 MUST 复用既有鉴权、分组路由、账号调度、计费与用量落库链路。执行前系统 MUST 探测并记录目标模型对题目所需能力的支持情况；不支持时 MUST 记录为失败产出且 MUST NOT 静默改用其他模型。

#### Scenario: 图片题目走图片接口

- **WHEN** 执行一道 `kind = image` 的题目
- **THEN** 请求 MUST 发往 `/v1/images/generations`

#### Scenario: 文本题目走对话接口

- **WHEN** 执行一道 `kind = text` 的题目
- **THEN** 请求 MUST 发往 `/v1/chat/completions`

#### Scenario: 不直连上游供应商

- **WHEN** 交付物被审查
- **THEN** 检测调用链路 MUST NOT 出现指向模型供应商域名的出站请求
- **THEN** 所有出站 MUST 指向本项目网关地址

#### Scenario: 网关地址不可达

- **WHEN** 配置的网关地址不可达
- **THEN** 系统 MUST 记录失败产出并带明确错误码
- **THEN** 系统 MUST NOT 静默判定为成功

### Requirement: 必须实现三类熔断且在触发时暂停调度并告警

系统 MUST 实现三类熔断：

1. **配额耗尽**：当检测调用返回余额不足，或执行后内部账号余额低于配置下限时，MUST 将该任务置为 `quota_exhausted`、清空下次执行时间以停止调度，并 MUST 告警。
2. **超预算**：系统 MUST 按本地时区自然日统计检测总成本；当达到配置的日成本预算上限时 MUST 将全部任务置为 `budget_exhausted`、清空下次执行时间并 MUST 告警；跨日后成本回落时 MUST 可自动恢复。
3. **账号被禁用**：每轮调度前与每次执行前 MUST 校验内部账号状态为 `active`；若不为 `active`（例如被内容审核机制自动封禁），MUST 将全部任务置为 `account_disabled`、清空下次执行时间并 MUST 以 ERROR 级告警；该状态 MUST 只能由管理员人工恢复。

任一熔断触发时，系统 MUST NOT 静默失败、MUST NOT 自动更换其他账号、MUST NOT 无退避地重复重试。

#### Scenario: 配额耗尽

- **WHEN** 检测调用返回余额不足
- **THEN** 任务状态 MUST 变为 `quota_exhausted`
- **THEN** 下次执行时间 MUST 被清空
- **THEN** 系统 MUST 记录 WARN 级告警

#### Scenario: 达到日成本预算

- **WHEN** 当日检测累计成本达到配置上限
- **THEN** 全部任务 MUST 被置为 `budget_exhausted`
- **THEN** 调度 MUST NOT 再领取任务
- **THEN** 系统 MUST 记录告警

#### Scenario: 跨日自动恢复预算熔断

- **WHEN** 日期切换后当日检测成本回落至上限以下
- **THEN** 因预算熔断暂停的任务 MUST 可自动恢复为可调度状态

#### Scenario: 内部账号被风控禁用

- **WHEN** 执行前检测到内部账号 `status` 不为 `active`
- **THEN** 全部任务 MUST 被置为 `account_disabled`
- **THEN** 系统 MUST 记录 ERROR 级告警且提示可能被内容审核自动封禁
- **THEN** 系统 MUST NOT 发起任何模型调用

#### Scenario: 熔断后不会静默换号

- **WHEN** 任一熔断触发
- **THEN** 系统 MUST NOT 自动选用其他账号继续检测

### Requirement: 内部账号在用户可见面必须被隔离

系统 MUST 保证内部账号不出现在任何普通用户可见的列表、统计或排行榜中，且 MUST NOT 可通过管理端的用户标识字段泄露到公开接口。管理员显式查询该账号自身详情时 MUST 可见（用于运维），但管理端 MUST 为该账号的 `notes` 字段提供只读语义，拒绝通过通用用户更新接口修改其 `notes`。

#### Scenario: 普通用户列表不含内部账号

- **WHEN** 普通用户查看任何用户列表
- **THEN** 结果 MUST NOT 包含内部账号

#### Scenario: 管理端禁止修改内部账号的 notes

- **WHEN** 管理员通过通用用户更新接口尝试修改内部账号的 `notes` 字段
- **THEN** 系统 MUST 拒绝该字段修改
- **THEN** 内部账号判定 MUST 保持有效
