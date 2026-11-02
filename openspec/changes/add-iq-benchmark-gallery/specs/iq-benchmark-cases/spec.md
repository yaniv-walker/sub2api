## ADDED Requirements

### Requirement: 检测题目必须具备独立且可校验的领域模型

系统 SHALL 提供「检测题目」（Case）领域对象，字段 MUST 至少包含：名称、类型（`image` / `text` / `code`）、题面正文、可选系统提示词、请求参数（JSON）、期望输出形态（`image_url` / `svg` / `markdown` / `text`）、评分方式（`manual` / `rule` / `manual_rule` / `llm_judge`）、启用状态、排序值、是否内置。题目名称 MUST NOT 为空且长度 MUST NOT 超过 100 字符；题面正文 MUST NOT 为空。题目 MUST 持久化于独立新表，MUST NOT 复用或修改任何既有表。

#### Scenario: 管理员创建合法题目

- **WHEN** 管理员提交名称非空、类型为 `image`、题面非空、期望输出形态为 `svg` 的题目
- **THEN** 系统 MUST 创建题目并返回其 ID
- **THEN** 系统 MUST 将 `created_at` 与 `updated_at` 写入为当前时间

#### Scenario: 管理员提交非法题目

- **WHEN** 管理员提交名称空白、或题面空白、或类型不在允许集合内、或期望输出形态不在允许集合内的题目
- **THEN** 系统 MUST 以 400 状态码拒绝并返回稳定错误码
- **THEN** 系统 MUST NOT 写入任何行

#### Scenario: 题目类型与期望输出形态不匹配

- **WHEN** 管理员提交类型为 `image` 但期望输出形态为 `markdown` 的题目
- **THEN** 系统 MUST 以 400 状态码拒绝并说明二者不匹配

### Requirement: 内置题目必须不可删除但可禁用

系统 MUST 内置至少一道「鹈鹕骑自行车」SVG 检测题（题面语义等价于 `Generate an SVG of a pelican riding a bicycle`，中文题面可由管理员编辑），其 `builtin` 标记 MUST 为真。内置题目 MUST NOT 可被删除；MUST 可被禁用、可编辑题面与参数、可调整排序。删除内置题目的请求 MUST 被拒绝并返回稳定错误码。

#### Scenario: 管理员删除内置题目

- **WHEN** 管理员对 `builtin = true` 的题目发起删除
- **THEN** 系统 MUST 以 4xx 状态码拒绝并返回表示「内置题目不可删除」的稳定错误码
- **THEN** 该题目行 MUST 仍然存在且 `enabled` 值不变

#### Scenario: 管理员禁用内置题目

- **WHEN** 管理员将 `builtin = true` 的题目 `enabled` 置为 false
- **THEN** 系统 MUST 接受该修改
- **THEN** 后续新建的检测任务 MUST NOT 再可选中该题目

#### Scenario: 管理员编辑内置题目题面

- **WHEN** 管理员修改内置题目的题面正文与请求参数
- **THEN** 系统 MUST 保存修改
- **THEN** 已存在的历史产出 MUST 保持其原有题面快照不变

### Requirement: 题目必须支持复制与排序

系统 SHALL 支持复制已有题目为新题目（名称追加可辨识后缀、`builtin` 强制为假、`enabled` 沿用来源值）。系统 SHALL 支持按 `sort_order` 排序读取题目列表，相同 `sort_order` 时 MUST 以 `id` 升序作为稳定次序。

#### Scenario: 管理员复制内置题目

- **WHEN** 管理员复制一道 `builtin = true` 的题目
- **THEN** 系统 MUST 创建一道新题目，其 `builtin` MUST 为 false
- **THEN** 新题目 MUST 可被删除

#### Scenario: 复制不存在的题目

- **WHEN** 管理员复制一个不存在的题目 ID
- **THEN** 系统 MUST 返回 404 且 MUST NOT 创建任何行

### Requirement: 每次执行必须冻结题目快照

系统 MUST 在写入每条产出（Artifact）时，将当时的题目内容冻结为一份 JSON 快照写入该产出行，快照 MUST 至少包含：题目名称、类型、题面正文、系统提示词、请求参数、期望输出形态、评分方式。题目后续被编辑或删除 MUST NOT 改变任何既有产出的快照。

#### Scenario: 题目修改后历史产出不受影响

- **WHEN** 一条产出已写入且其题目随后被管理员修改题面
- **THEN** 该产出的快照内容 MUST 与修改前完全一致
- **THEN** 工作展展示该产出时 MUST 使用快照中的题面而非当前题目表内容

#### Scenario: 题目被禁用后历史产出仍可展示

- **WHEN** 某题目被置为 `enabled = false`
- **THEN** 该题目下的历史产出 MUST 仍可被列出与查看
- **THEN** 题面正文的公开下发 MUST 遵循公开白名单条件

### Requirement: 题目被检测任务引用时必须受到保护

系统 MUST 在删除题目时检查其是否被启用中的检测任务引用；若被引用，删除请求 MUST 被拒绝并返回稳定错误码。禁用中的任务引用 MUST NOT 阻止删除，但删除后该任务 MUST 自动剔除该题目 ID。

#### Scenario: 删除被启用任务引用的题目

- **WHEN** 某题目被至少一个 `enabled = true` 的检测任务的 `case_ids` 引用
- **THEN** 删除请求 MUST 被拒绝并返回表示「题目正在被使用」的稳定错误码

#### Scenario: 删除仅被禁用任务引用的题目

- **WHEN** 某题目仅被 `enabled = false` 的任务引用
- **THEN** 删除 MUST 成功
- **THEN** 该任务的 `case_ids` MUST 不再包含被删除的题目 ID

### Requirement: 题目集合必须对执行侧暴露为一个有序整体

系统 SHALL 在读取检测任务时，按 `case_ids` 的数组顺序解析为题目列表；解析 MUST 跳过已删除或不存在的题目 ID；若解析结果为空，该任务 MUST 在本轮被跳过并记录 `error_code = no_cases`，且 MUST NOT 触发任何模型调用。

#### Scenario: 任务引用的题目全部失效

- **WHEN** 调度器领取到一个 `case_ids` 全部已失效的启用任务
- **THEN** 系统 MUST NOT 发起任何模型请求
- **THEN** 系统 MUST 记录一条 run，其 `status` 为 `failed`、失败原因码为 `no_cases`

#### Scenario: 部分题目失效

- **WHEN** 任务引用 3 道题，其中 1 道已被删除
- **THEN** 本轮 MUST 仅执行剩余 2 道题
- **THEN** run 的汇总计数 MUST 只反映实际执行的题目数
