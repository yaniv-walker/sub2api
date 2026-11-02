## ADDED Requirements

### Requirement: 只读接口必须按认证用户两档限流

系统 MUST 对作品展只读接口实施固定窗口限流，计数维度 MUST 为已认证用户的用户标识，窗口 MUST 为 1 分钟。系统 MUST 提供两档：`read` 档用于列表与聚合类接口（默认每分钟 120 次），`heavy` 档用于详情等重负载接口（默认每分钟 60 次）。一次 `heavy` 请求 MUST 同时消耗 `read` 与 `heavy` 两档配额，MUST NOT 仅消耗其中一档从而绕过另一档。限流计数键 MUST 使用本功能独立前缀，MUST NOT 复用既有面板限流的键前缀。

#### Scenario: read 档超限

- **WHEN** 同一用户在 1 分钟内发起第 121 次列表请求（默认配置）
- **THEN** 系统 MUST 返回 429
- **THEN** 响应 MUST 包含 `Retry-After`

#### Scenario: heavy 请求双档计数

- **WHEN** 同一用户已用尽 `read` 档配额后发起一次详情请求
- **THEN** 系统 MUST 返回 429
- **THEN** 该请求 MUST NOT 消耗任何 `heavy` 档配额

#### Scenario: heavy 档超限

- **WHEN** 同一用户在 1 分钟内发起第 61 次详情请求且 `read` 档仍有余额（默认配置）
- **THEN** 系统 MUST 返回 429

#### Scenario: 键前缀不得复用面板前缀

- **WHEN** 检查 Redis 中生成的限流键
- **THEN** 键 MUST 以本功能独立前缀开头
- **THEN** MUST NOT 使用既有面板限流的键前缀

### Requirement: 限流必须是用户间独立的

不同用户的限流计数 MUST 相互独立。一个用户触发 429 MUST NOT 影响任何其他用户的可用配额。

#### Scenario: 用户 A 超限不影响用户 B

- **WHEN** 用户 A 达到其配额上限
- **THEN** 用户 B 的后续请求 MUST 正常通过

### Requirement: 限流必须在 Redis 故障时放行且不得返回 5xx

当限流底层存储不可用或返回错误时，系统 MUST 放行该请求并记录 WARN 级日志。系统 MUST NOT 因限流组件故障而向用户返回 5xx。

#### Scenario: Redis 不可用

- **WHEN** 限流存储不可用
- **THEN** 请求 MUST 被放行
- **THEN** 响应状态码 MUST NOT 为 5xx
- **THEN** 系统 MUST 记录 WARN 日志

### Requirement: 429 响应体必须与既有面板限流格式一致

429 响应 MUST 使用与既有面板限流完全一致的错误体结构与状态码，并 MUST 携带 `Retry-After` 头，其值为本次限流窗口的剩余秒数。系统 MUST 复用既有的错误输出函数，MUST NOT 重新定义一套不同的 429 结构。

#### Scenario: 429 结构一致

- **WHEN** 作品展接口触发限流
- **THEN** 响应体结构 MUST 与既有面板限流 429 一致
- **THEN** 响应 MUST 携带 `Retry-After`

### Requirement: 单用户并发必须受上限约束

系统 MUST 对同一用户的在途重负载请求实施并发上限（默认 4）。实现 MUST 同时提供进程内快速拒绝层与跨实例计数层。跨实例计数 MUST 带有过期时间，以保证进程异常终止时不产生永久占用。计数存储故障时 MUST 降级为仅依赖进程内层并放行，同时记录 WARN。

#### Scenario: 达到并发上限

- **WHEN** 同一用户已有配置数量的在途重负载请求，又发起新的重负载请求
- **THEN** 系统 MUST 返回 429 并携带 `Retry-After`

#### Scenario: 请求结束后释放配额

- **WHEN** 一个重负载请求完成
- **THEN** 其占用的并发配额 MUST 被释放
- **THEN** 后续请求 MUST 可正常进入

#### Scenario: 进程被强制终止

- **WHEN** 持有并发计数的进程被强制终止未释放
- **THEN** 计数 MUST 在过期时间后自动回收
- **THEN** 该用户 MUST 在过期后恢复可用

#### Scenario: 计数存储故障

- **WHEN** 跨实例计数存储不可用
- **THEN** 系统 MUST 仅依赖进程内层判断
- **THEN** 请求 MUST NOT 因计数存储故障被拒绝为 5xx

### Requirement: 未认证请求不参与用户维度计数

限流 MUST 在认证之后执行。未认证请求 MUST NOT 计入任何用户的配额；这些请求的最终处置 MUST 由认证环节的结果决定。

#### Scenario: 未认证请求

- **WHEN** 请求未携带有效认证信息
- **THEN** 该请求 MUST NOT 消耗任何用户的限流配额
- **THEN** 响应 MUST 为 401

### Requirement: 功能未启用时限流不得参与

功能未启用时，作品展路由 MUST 不被注册，限流中间件 MUST NOT 被挂载，MUST NOT 向限流存储写入任何键。

#### Scenario: 未启用时无键写入

- **WHEN** 功能未启用且用户访问原作品展路径
- **THEN** 响应 MUST 为 404
- **THEN** 限流存储中 MUST NOT 出现本功能前缀的键

### Requirement: 前端必须正确处理限流与请求抑制

前端 MUST 在收到 429 时展示提示且 MUST NOT 自动重试。前端筛选输入 MUST 做防抖（不短于 300 毫秒）。前端分页请求 MUST 做单飞控制，同一时刻 MUST 最多存在一个在途列表请求；新请求 MUST 取代旧请求的结果而非并发堆积。

#### Scenario: 收到 429

- **WHEN** 前端收到 429
- **THEN** 界面 MUST 展示限流提示
- **THEN** 前端 MUST NOT 自动重发请求

#### Scenario: 筛选防抖

- **WHEN** 用户连续快速修改筛选条件
- **THEN** 前端 MUST 在停止输入不少于 300 毫秒后才发起请求
- **THEN** MUST NOT 每次按键都发起请求

#### Scenario: 分页单飞

- **WHEN** 用户在列表加载中再次触发翻页
- **THEN** 前端 MUST 保证同一时刻只有一个在途列表请求
- **THEN** 界面 MUST 展示最后一次请求的结果

### Requirement: 限流配置必须可管理且带缓存

系统 MUST 将两档限额、窗口与并发上限纳入可管理配置，并 MUST 使用进程内缓存（缓存有效期不超过 60 秒）以避免每次请求都读配置。配置读取失败时 MUST 沿用上一次的有效值或默认值，MUST NOT 因配置读取失败而拒绝全部请求。

#### Scenario: 管理员调整限额

- **WHEN** 管理员修改 `read` 档限额
- **THEN** 新限额 MUST 在不重启服务的前提下于缓存过期后生效

#### Scenario: 配置读取失败

- **WHEN** 读取限流配置失败
- **THEN** 系统 MUST 使用默认值或上一次的有效值
- **THEN** 请求 MUST NOT 因配置读取失败而被拒绝
