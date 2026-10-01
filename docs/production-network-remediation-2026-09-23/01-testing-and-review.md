# 01 功能测试与低侵入性审查（2026-10-01）

## 你能观察到的变化

本次功能增加请求和上游阶段的日志证据。页面布局、模型回答、API 正文和生产重试策略保持现有行为；启用功能本身不会优化公网线路或消除上游 503。

| 场景 | 新增的可见信息 | 解读范围 |
| --- | --- | --- |
| 普通 HTTP 返回 | 内部请求 ID、路由模板、写出字节数、请求终态 | 应用内请求关联 |
| SSE 流式输出 | `first_write_ms`、`max_write_gap_ms`、`terminal_silence_ms` | 应用写入时刻；心跳也算写入 |
| 上游请求慢 | 上游 `duration_ms`、有回调时的 `connect_ms` / `first_byte_ms`、连接复用标记 | 通用 HTTP 上游调用至响应头，不含完整响应体读取时间 |
| 上游 503 后成功 | 每次上游状态、请求累计尝试数与 5xx/传输错误数 | 便于区分失败尝试和最终返回 |
| 主动取消请求 | `client_cancelled` 或可见写错误 | 根据应用请求 context/writer 判断，仍需核对取消发起方 |
| 客户重复提供相同请求 ID | 每次内部 ID 与计费关联 ID仍独立 | 外部提示只以 SHA-256 摘要记录 |

没有新增监控页面或开关 UI。当前通过启动环境变量启用。日志级别应为 `info`，格式建议 `json`；`warn` 级别会过滤成功请求日志。现有 Ops 日志入库受其原开关控制：access log 需允许持久化；成功上游调用的 INFO 日志通常从标准输出或日志文件查看，不能假定管理页面自动包含全部记录。

## 一键本地验证（建议先做这个）

前提：Windows PowerShell、Go 1.27.x、Git、curl.exe。无需数据库、API Key或付费模型。

```powershell
Set-Location 'E:\ai\claude\dev\sub2api-original'
.\tools\test-request-observability.ps1
```

脚本构建演示程序，使用回环端口 18081/18082，依次验证关闭和开启状态。端口已有程序占用时直接报错；可用 `-Port 19081` 换一对端口。结束后脚本关闭自己启动的进程。构建文件与日志放在项目 `.tmp-go-build-cache/observability-smoke`。

验收输出必须包含 `PASS: disabled` 和 `PASS: enabled`。脚本实际检查：关闭时无新增阶段字段；开启时四段 SSE 逐段输出；模拟两次上游调用（503、200）计数为 2/1，最终结果为 `completed`；约 0.5 秒取消流后记录 `client_cancelled`。取消时 curl 的 28 退出码是测试预期。

2026-10-01 本机实测通过，流的首写与段间间隔约 300 ms，模拟重试计数符合预期。数据由本地 mock 产生，不能用来推断生产服务器或客户线路表现。

## 手工体验流式输出和日志

终端 A：

```powershell
Set-Location 'E:\ai\claude\dev\sub2api-original\backend'
$env:GOCACHE = 'E:\ai\claude\dev\sub2api-original\.tmp-go-build-cache'
go run ./cmd/observability-demo -observe -port 18080
```

终端 B：

```powershell
curl.exe -N http://127.0.0.1:18080/demo/stream
curl.exe http://127.0.0.1:18080/demo/retry
curl.exe -N --max-time 0.5 http://127.0.0.1:18080/demo/stream
```

终端 B 会看到 SSE 分四段出现；终端 A 记录结构化日志。将终端 A 改为 `-observe=false` 并重新启动可比较开关前后。演示的 `/demo/retry` 是固定模拟序列，不调用生产账号池调度器。

## 在自己的本地应用实例中验证

在已有本地开发配置和数据库依赖上启动新构建，设置：

```powershell
$env:SUB2API_REQUEST_OBSERVABILITY = '1'
$env:LOG_LEVEL = 'info'
$env:LOG_FORMAT = 'json'
```

使用自己的本地测试 Key 发起一次现有流式 API 调用，按返回的服务端关联头和时间窗查日志。Key通过本地既有 secret 管理提供，不写进此文档或提交记录。若同时提供自定义 `X-Request-ID`，该头会按旧契约回显；内部 `request_id` 独立生成，要用 `external_request_id_sha256` 及时间窗关联。若同时提供两个外部关联头，摘要优先取 `X-Client-Request-ID`。

在 Docker 内运行时，环境变量必须进入容器；仅设置宿主机环境不会改变已运行容器。此文档不修改生产 Compose 或重建生产容器。启用后增加日志量，要在本地/测试实例先检查吞吐、内存与日志容量。

## 自动验证与构建

从 backend 目录执行：

```powershell
go test ./internal/pkg/requestobs ./internal/server/middleware -count=1
go test ./internal/repository -run TestDoUpstreamRequest -count=1
go build ./...
```

完整回归使用 `go test -tags=unit -p 1 ./...`。本机测试环境需指定可写的 `GOCACHE`；Git 的 `bin` 目录需在 PATH（备份测试使用 sh）；若默认 Windows TEMP 是挂载/重定向路径，应将该测试进程的 TEMP/TMP 指向项目缓存下的普通目录。这样不会改变用户的全局设置。

前端保持既有锁文件版本，通过 `pnpm install --frozen-lockfile` 和 `pnpm run build` 验证。pnpm 11 需要 workspace 配置中的 `allowBuilds`；仅为 esbuild/vue-demi 开启其构建脚本，并同步现有版本覆盖规则。未升级锁文件中的依赖。

## 低侵入性审查结果

结论：观测功能采用 Go 的 Gin middleware、ResponseWriter 包装和 HTTP 边界辅助函数，没有引入通用切面框架。相比最初实现，已把共享文件里的观测逻辑移入独立新增文件。

| 既有生产文件（相对 main） | 必要接入 |
| --- | --- |
| `internal/server/router.go` | 根据环境变量安装一个中间件 |
| `internal/server/middleware/logger.go` | 一行调用追加观测字段 |
| `internal/repository/http_upstream.go` | Do/DoWithTLS 向公共辅助函数传入已有 account ID，并调用观测 helper；原有取消、解压、Body 生命周期保持原逻辑 |
| `internal/server/middleware/request_logger.go` | 内部 ID改为服务端生成，保留外部响应头契约；提示值存入独立 context key |
| `internal/pkg/ctxkey/ctxkey.go` | 增加外部关联提示 key |

`request_metadata.go` 已恢复 main 行为，外部提示的严格校验独立放入新文件，避免影响既有 Ops/计费 ID。协议 handler、前端业务页面、计费实现、生产 Caddy与数据库迁移均无观测改动。

仍存在这 5 个接入文件在拉取上游时发生普通 Git 冲突的可能；无法保证零冲突。构建修复涉及 Wire、已有插件和前端包管理配置，会作为独立提交，便于与观测功能区分。

审查并修正的问题：

- 将共享日志/上游文件中较长的功能逻辑提取到新增文件，降低后续合并面积。
- 200 流中曾出现 503 尝试时，改为 `unknown`，避免仅凭失败历史就断定最终流失败。
- 上游日志 sink 异常不得阻断响应 Body/取消资源的交还；已测试 sink panic 后响应完整且上游只调用一次。
- 确认外部关联值不进入计费去重；不明文记录客户端关联提示。

## 构建问题的具体修复

1. 从已存在的历史提交恢复遗漏的内部插件包与上游监控配置类型；注册配置默认值，保持插件默认关闭。
2. 为现有插件提供 slog logger，重新生成 Wire，修复当前构造函数参数不一致。补齐 Wire 工具校验和及已有插件 SQLite 测试依赖。
3. 配置测试使用临时空配置，避免读取开发机器的实际 `/app/data/config.yaml`。
4. 修复两个既有测试的非确定性：Ollama 测试显式构造不同时间代次；Grok 测试等待缓存完成发布，而非仅等待后台查询开始。生产调度逻辑未改。
5. 使用与项目同卷的测试临时目录验证 Windows 图片路径测试；不更改页面图片的生产路径校验。

## 设计覆盖与限制

01 文档 A/B/C 的 HTTP 请求关联、D 的通用 HTTP 上游尝试、E 的应用首次写出/写入间隔、F 的结构化日志与敏感字段控制、G 的字段说明和测试指南已实现。

仍待后续设计/实现：逐协议的真实 `first_token_ms`、模型 token 间隔、受控 Prometheus/OTel 导出与指标聚合、WebSocket 帧和非通用客户端覆盖。当前代码不解析或保存模型响应内容。应用 context 取消的具体发起方、Caddy 到客户的网络传输与香港入口收益仍需端到端测量。生产灰度与容量验收未执行。

## 最终验证记录

2026-10-01 完成以下验证：

- `go build ./...` 与 `go build -tags embed ./cmd/server`：通过。
- 前端使用原锁文件冻结安装后 `pnpm run build`：通过，保留既有打包体积等警告，锁文件无改动。
- 后端 `go test -tags=unit -p 1 -json ./...`：通过；63 个包有通过记录，21,610 个测试及子用例通过记录，无失败。环境按上文配置项目缓存、同卷临时目录与 Git sh 路径。
- 修正的 Ollama/Grok 两个测试使用 `-tags=unit -count=20`：通过。
- 本地 Linux 容器内 `go test -race`：请求状态、请求中间件、HTTP 上游相关定向用例通过。未将其表述为全项目 race 通过。
- 相关包 `go vet`、Git diff 空白检查及文档相对链接检查：通过。
- `tools/test-request-observability.ps1`：开关两种模式、逐段 SSE、模拟恢复与取消验证通过。

回归原始 JSON 记录位于本机 `.tmp-go-build-cache/verify-complete-20261001.jsonl`，未纳入 Git；本地演示日志位于 `.tmp-go-build-cache/observability-smoke`。本轮只合并当前完成的日志观测范围，后续指标/协议扩展按上述范围继续设计。
