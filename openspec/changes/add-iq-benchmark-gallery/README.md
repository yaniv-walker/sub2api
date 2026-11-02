# add-iq-benchmark-gallery

「模型智商检测作品展」——按「分组 × 模型」定期执行同一道检测题，把模型产出收集成可浏览、可横向对比的公开作品展（Pelican Benchmark 风格）。

## 文档索引

| 文件 | 内容 |
|---|---|
| `proposal.md` | 为什么做、改什么、新增/修改的 capability、影响面 |
| `design.md` | 技术决策全文（数据模型、调度、计费、隔离、消毒、限流、上游接触面、开工前 5 问回答） |
| `specs/iq-benchmark-cases/spec.md` | 题目领域模型、内置题、快照冻结、引用保护 |
| `specs/iq-benchmark-scheduling/spec.md` | 任务模型、间隔校验、多实例去重、生命周期、run、失败重试 |
| `specs/iq-benchmark-internal-agent-billing/spec.md` | 内部虚拟账号、唯一判定、幂等初始化、统计隔离、网关自调、三类熔断 |
| `specs/iq-benchmark-gallery-public/spec.md` | 公开只读作品展、白名单 DTO、消毒渲染、失败占位、容量上限、未启用不可达 |
| `specs/iq-benchmark-rate-limit/spec.md` | 两档限流、并发上限、fail-open、429 一致性、前端配合 |
| `tasks.md` | 分阶段任务清单（阶段 0 已完成，停在确认） |
| `verification.md` | 待确认项、验证命令、必测矩阵、已知局限 |

## 相关外部文档

- 开源调研记录：[`research.md`](./research.md)
- 第三方 License 声明：`backend/internal/iqbenchmark/THIRD_PARTY_NOTICES.md`（阶段 2 交付）

## 当前状态

**阶段 0（调研 + 规格 + 计划）已完成并获确认，阶段 1（数据层）已完成，进入阶段 2/3。**

4 项待裁决事项已全部裁决完毕，结论见 `verification.md` §一。

## 核心约束（贯穿全部阶段）

1. 不改变上游核心文件的行为语义；对既有文件的改动限「注册一行」/「纯新增方法」级别（后端 5 个、前端 3 个，逐文件理由见 `design.md` §3.16 / §9.3）。
2. 功能默认 `enabled=false`；关闭态零副作用（无调度、无账号、无出站、无 Redis 键）。
3. 不给 `users` / `usage_logs` 加列；不新增 role 枚举；不扩 `users_signup_source_check`。
4. 检测流量走内部虚拟账号扣自己的配额，零影响真实用户。
5. 模型输出按不可信内容处理：入库静态扫描 + 前端消毒 + 优先 `<img>` + 不做服务端栅格化。
