# 09｜前置阶段计时切面（2026-10-10）

## 本批范围与完成标准

接续 08 的统一开关，完成总体规划阶段 2 的**观测子阶段**：在稳定共享边界拆出鉴权、正文读取/解码、JSON 校验/共享解析、安全审计、计费资格、合成路由、账号调度、sticky 查询、利润终检、槽位申请/等待、消息串行队列及限速耗时。

本批用于回答三个问题：

1. 进入第一次上游 HTTP 调用前等了多久，哪些已观测操作耗时最多？
2. 是鉴权/读取/审计/路由慢，还是槽位或消息串行队列在等待？
3. 是否真的进入等待，还是立即拿到槽位？

验收要求：关闭时不记录新阶段；开启时使用入口快照，拒绝/取消仍结束计时；重复结束不重复累计；原错误、返回结果、槽位释放与请求正文保持原语义；元数据有界，不采集正文/密钥，不增加外部查询或逐轮询日志。

**本批没有完成整个阶段 2，也没有完成预算、failover 终态、协议完整性或排空治理。** 不设置生产超时值，不改变 retry 次数、候选筛选、计费规则、Caddy 或区域部署。

## 架构与低冲突接点

- 独立 `internal/pkg/requestobs/stages.go` 提供被统一请求快照控制的 `StartStage` / `WithStage`，固定数组最多存 17 种阶段；结束函数幂等且统计并发安全。
- 现有共享函数仅插入入口/defer 计时钩子；鉴权在进入后续 handler 前明确结束，早退通过 defer 结束。无需拆分或重写鉴权流程。
- JSON 解析用独立 service 装饰器调用原解析器；JSON 合法性校验用独立 handler 装饰器调用原 `gjson.ValidBytes`。原函数和返回语义保留。
- 调度和 sticky 有同阶段嵌套去重：OpenAI 调度器回退 legacy selector、service 调用真实 Redis repository 都只累计外层一次。同请求多次独立调度仍累计 calls。
- 统一的 HTTP upstream 边界记录第一次调用入口；不用完成响应的时间倒推前置耗时。
- 复用现有 `http request completed`，只增加 `gateway_stages` 与可选 `pre_upstream_ms`，不引入日志 exporter 或新依赖。
- 不改主业务分支、重试决策、并发容量、释放函数或账单写入。必要接点是共享函数入口和少量装饰器替换，不能承诺完全没有 upstream 合并冲突。

## 字段契约

`gateway_stages` 为已结束操作的固定枚举数组，每项包含：

```json
{"stage":"user_slot_wait","calls":1,"duration_ms":125}
```

未进入的阶段不生成数组项。`duration_ms=0` 加 `calls>0` 表示操作确实执行过但累计耗时不足 1ms；不能把未观测阶段填 0。

| stage | 含义与边界 |
|---|---|
| authentication | API Key 提取/查询、Key/User/Group/IP 准入；结束于 SimpleMode/订阅与额度处理之前，拒绝时结束于返回 |
| auth_eligibility | 非 SimpleMode 的订阅读取、额度/余额检查以及鉴权上下文设置；进入下游 handler 前结束 |
| body_read_decode | 共享请求正文 reader 的整个调用，包含实际 Read/组装/解压或 preread 快速返回；不含 lenient JSON normalization |
| json_validation | 已接入 handler 的 JSON 语法合法性校验；不含后续字段提取、适配器解析或模型推理 |
| json_parse | 既有 ParseGatewayRequest 的完整调用；未调用该共享解析器的协议路径不生成此项 |
| security_audit | 共享安全审计边界，包含缓存命中和 legacy 分支；不是纯规则计算时间 |
| billing_eligibility | BillingCacheService 的资格检查；不等于计费写入时间 |
| composite_routing | 合成分组模型/端点到目标平台的 Resolve 调用 |
| routing | Anthropic/Gemini 共享 load-aware selector、OpenAI scheduler / legacy selector 的选号调用；包含其内部 sticky/资格检查/即时槽位申请等操作 |
| sticky_lookup | 共享 session account lookup；服务层包裹真实缓存查询时去重，直接 repository 查询也计时；不覆盖所有摘要链/guardian/previous-response 状态查询 |
| profit_gate | post-slot 最新利润终检；不覆盖装门定价、所有候选利润筛选 |
| user_slot_acquire / account_slot_acquire | 每次实际并发 service 申请调用的累计时间与次数，包括等待循环内的申请；不是已占用槽位的持有时长 |
| user_slot_wait / account_slot_wait | 进入既有等待循环后的总墙钟时间，包含退避、SSE ping 与轮询申请；tryImmediate 成功不生成等待项 |
| message_queue | 串行锁获取与 RPM 自适应延迟整个 AcquireWithWait 调用，立即成功也会记录调用；不是纯排队时间 |
| message_throttle | 软限速整个 ThrottleWithPing 调用，计算结果无延迟时也会有短调用记录 |

`pre_upstream_ms`：从统一观测 state 创建到第一次 `observeUpstreamDo` 入口的墙钟差。没有观察到 HTTP attempt 时不生成；请求仍可包含鉴权拒绝、正文失败、取消等阶段摘要。

重要限制：

- 阶段是**操作边界时间，存在嵌套**。`routing` 包含 sticky 与即时申请；wait 包含循环里的申请；不能把阶段相加后与 pre_upstream_ms 作精确等式。
- 同请求后续重试/failover、WebSocket 多轮或异步工作仍可能调用这些边界，阶段累计不限于第一次上游调用之前。没有每 attempt/每 turn 的阶段分配，不把 totals 冒充单次前置时间。
- pre_upstream_ms 目前只覆盖已接入 HTTP repository 调用，不把 WebSocket dial、其他网络客户端或客户端实际上传开始时间冒充同一边界。
- lenient normalization、handler 内零散字段提取、摘要/guardian 状态、部分特殊协议 reader、账号候选过滤子步骤仍可能未分段。不能声称能精确解释所有 P95/P99 前置耗时。
- 没有新增实时队列深度查询，避免观测反过来增加 Redis/DB 请求。队列深度/拒绝原因和独立等待 deadline 留在后续任务。
- 摘要只包含已结束操作。它不实时输出正在阻塞的调用，也不等待 detached goroutine 收敛。

## 本地部署与验证记录

开发前核对：源码在 `feature/gateway-remediation-aspects`，基线提交 `6e7c4b53b7`。已部署的本地容器 `sub2api-obs-local-sub2api-1` 使用 `sub2api:local-update-20261010-143716`，`127.0.0.1:18088/health` 返回 healthy JSON。本批未替换或重启此服务。

测试先复现缺少阶段摘要/鉴权边界，再接入计时；检查开启/关闭、立即获取/实际等待、取消清理、嵌套去重、重复结束、无 attempt 字段缺席、解析结果/错误不变，以及真实 access logger 的有界结构。

验证结果：

- 15 个新增测试覆盖上述开关、边界、去重、日志和透明性要求，通过。
- requestobs、httputil、middleware、handler 整包 unit 回归通过。真实 access logger 测试初次因 `int` 与日志 sink 的 `int64` 断言类型不匹配而失败，修正测试后 middleware 整包重跑通过。
- service 的选号、并发、计费资格、利润控制、解析、合成路由与 sticky，以及 repository 的 HTTP 观测/新增边界定向回归通过。
- `go build ./...`、带前端资源的 `go build -tags=embed ... ./cmd/server`、所有改动包的 `go vet` 通过。
- 未重跑全仓 unit。08 记录的基线 server API 契约问题仍未在本批修复，不把定向回归称为全仓通过。
- Linux `-race` 未完成：独立临时容器完成 gcc 依赖准备后，Go 1.27.0 因低于项目要求 1.27.2（镜像 GOTOOLCHAIN=local）拒绝执行测试，退出码为 1。临时容器已自动清理，不宣称 race 通过；发布前仍需在符合版本要求的 Linux CI/compiler 环境补跑。

Windows 构建/测试使用原先独立 GOCACHE/TEMP，未改变系统环境变量；Linux 验证尝试使用只读源码挂载，不接入业务数据库/Redis。

## 灰度阅读方式与后续发布门

本批部署后沿用已有「网关性能治理」总开关，无新开关或新数据库键。关闭即可停止本批观测，不需删除数据或回滚 schema。当前运行的旧镜像不会因为源码提交自动获得新字段。

采样/对照建议：

1. 用内部 request ID 关联 access event、body read summary 与 upstream event，不采集 prompt/API Key。
2. 按协议、streaming、请求大小分组，统计 pre_upstream_ms P50/P95/P99 和各 stage 的 duration/calls。
3. 先筛 pre_upstream_ms 的长尾，再读取同请求的阶段。发现某阶段解释不足时补具体稳定接点；不要用 auth_latency_ms 代替真实鉴权。
4. 没有 attempt 的请求单独分析，不能与“成功进入上游”的前置 P95 混为一组。
5. 在固定版本和窗口获得基线后，才为 request/attempt、上传、槽位、串行队列设显式预算。预算值必须由业务允许的最长等待和观测证据决定，默认保持原行为，优先保留外部更短 deadline。

下一批依次为：补未覆盖的实测长尾边界及队列证据 → 显式请求/尝试预算 → failover 最终决策 → 协议完整性与排空资源边界。Caddy/香港验证仍属于独立部署阶段，不在本批修改生产配置。
