# 请求链路观测：本地验收与生产部署计划

日期：2026-10-01。已部署隔离的本地环境；尚未在生产执行任何操作。

## 当前本地环境

- 地址：<http://127.0.0.1:18088>，仅监听本机回环地址。
- Compose 项目：`sub2api-obs-local`，应用、PostgreSQL、Redis 共三个容器，独立命名卷；不复用原 deploy/data、postgres_data、redis_data。
- 模板：`deploy/docker-compose.observability-test.yml`。
- 登录信息：项目 `.tmp-go-build-cache/observability-local-stack/local-login.json`；配置秘密在同目录 `.env`。这些文件不进入 Git或 Docker 构建上下文。
- 镜像：`sub2api:obs-9258c07e3f`，linux/amd64。
- 本地镜像 ID：`sha256:4d45f7aef08de94b19e0184da95385d43b3b8ffceab58152a34de93f64a5b96a`。此值为 Docker image ID，尚不是发布到注册表后的 RepoDigest。
- 导出包：缓存目录 `sub2api-obs-9258c07e3f.tar`，SHA-256 `5506bcf5bdcedbf3d57b79114fbd5ab29ecbf71b297edb8216c5eeb9cad9a615`；对应 `deployment-manifest.json` 可核对平台、版本和哈希。

本轮实测：三个容器健康、`/health` 200、嵌入前端页面 200、管理员登录成功；本地 PostgreSQL 完成 289 条迁移记录；备份恢复到独立 `observability_restore` 数据库并核对迁移数量；`pg_dump --version` 为 18.6，与本地数据库工具版本匹配。

真实应用请求已验证：默认不采集；仅修改本地测试数据库的观测设置后，缓存刷新使新请求出现阶段字段，无需重启；容器重启后状态保留；关闭设置后新请求不再出现阶段字段。测试结束将观测状态恢复为关闭。该数据库 fixture 不伪造合规确认、不跳过管理员鉴权，也不证明管理员保存接口已在此新实例完成 UI 验收。

首次管理员登录要求本人阅读并确认项目自带的部署合规声明；设置接口在此之前正常返回 423。本轮未代替用户接受声明。用户完成后，应继续在真实页面验证：独立保存开关、刷新页面、查看请求日志、保存失败提示、关闭功能。管理员保存与即时生效行为此前已通过后端/前端自动测试，本地独立演示也已验证 SSE/取消/模拟 503；本环境没有配置付费模型或生产账号，不把这些结果表述为真实上游模型调用验收。

## 本地操作

从项目根目录运行（Windows PowerShell 5.1/7）：

```powershell
.\tools\start-observability-local.ps1
```

默认复用已构建镜像与测试卷。需要重建时使用：

```powershell
.\tools\start-observability-local.ps1 -Build -BuildProxy http://host.docker.internal:7897
```

代理参数仅适用于本机确实有相应代理的情况；其他机器可省略或提供自己的构建代理。APK 构建步骤已兼容标准代理变量，同时保留 HTTPS 校验。Docker 镜像使用 pnpm 9，workspace 已补齐 packages 配置，本轮实际镜像构建通过。缓存目录与 Windows 可执行文件已排除出构建上下文。

本地集成测试：

```powershell
.\tools\test-observability-local-stack.ps1
```

该脚本仅支持本机 Docker Desktop 的 `sub2api-obs-local` 测试项目，会写入本地测试 key `request_observability` 并重启一次测试应用；结束时恢复关闭。不得修改脚本去指向生产。

停止本地环境但保留数据：

```powershell
docker compose -p sub2api-obs-local --env-file .tmp-go-build-cache/observability-local-stack/.env -f deploy/docker-compose.observability-test.yml stop
```

## 生产部署顺序

### 1. 只读清点与差异核验

记录现有应用 image ID/标签、服务器架构、实际 Compose 文件与 service 名、应用/数据库/Redis 版本、持久卷与配置来源、Caddy 生效版本/配置、资源与请求错误基线。输出避免包含环境变量秘密。检查生产 `schema_migrations` 的 filename/checksum，与候选镜像源码迁移清单比对。

本功能本身不新增数据库结构，但当前 main 与生产版本可能有其他差异，启动会执行所有待执行迁移。不能因为“观测开关只是一个 settings key”就假定整个版本升级无迁移或可直接回退。

本地包只验证了 linux/amd64；若生产是 arm64，应按相同源码重新构建并重测，不能直接加载该包运行。本地使用新 PostgreSQL 18 不代表生产必须升级 PostgreSQL；生产应保留既有数据库/Redis版本与卷。

### 2. 演练与备份

核对迁移后，先在独立演练环境验证当前生产版本到候选版本的升级及备份恢复。演练库不得与生产应用共用；复制含账号/上游凭据的数据时须隔离、脱敏并阻止后台任务向真实上游发请求。

生产维护操作获得明确授权后，在受限目录保存数据库备份、应用 data/config、JWT/TOTP 等原有秘密配置、Compose/Caddy 配置，以及旧镜像 ID和可用回退包。使用与实际数据库主版本匹配的 pg_dump，恢复演练成功才视为有备份。不得将秘密配置打进镜像、上传公开注册表或放进 Git。

### 3. 请求排空后替换应用

**现有 main.go 只等待约 5 秒做 HTTP Shutdown。** 对数分钟长流，直接重建容器会中断请求；仅设置更长 stop_grace_period 不能延长程序自身的 5 秒上限。

应选择维护窗口，停止接收新的业务请求，依据请求状态/访问日志确认已有长流结束后再停应用。若不能完成排空，应明确告知用户可能中断，或先单独实现并验证多实例摘流/长流排空能力；当前版本不能承诺零中断更新。

将候选镜像导入或从私有注册表获取，并核对实际 ID/Digest。生产 override **只替换现有应用 service 的 image**，保留原有端口、卷、环境与依赖，不拿本地测试 Compose 覆盖生产配置。示意：

```yaml
services:
  sub2api: # 必须替换为实际应用 service 名
    image: sub2api:obs-9258c07e3f
```

使用本地导入包时先 `docker load`，再将 `docker image inspect` 的 ID 与清单比较；部署固定标签前确认它未被覆盖。使用注册表时记录注册表 RepoDigest 并按 Digest 部署。现有 Compose 与 override 先执行 `config --quiet` 校验，再仅针对应用 service 执行 `up -d --no-deps --pull never`（本地镜像情形）。这是部署设计步骤，本轮未执行这些生产命令。

### 4. 先关闭功能验证，再开启

确认观测设置为关闭；检查容器健康、管理员登录、设置读取、各实际使用协议、正常计费/用量记录、首次响应、SSE连续输出、客户端取消与上游错误路径。先比较应用升级本身与旧版本的差异。

随后从系统设置启用观测并独立保存，对照同类型请求的延迟、断连率、CPU/内存、数据库压力与日志增长。成功事件需要 INFO 日志级别。当前开关控制本节点全部新请求，不支持按用户比例灰度；如需百分比灰度，须另行设计多实例流量分配。

Caddy 无需因该开关修改超时或压缩设置。香港节点实验与这次版本更新分开执行，避免同时改变线路和应用而无法归因。

### 5. 回退与止损

如果只是日志量或采集开销升高，先从系统设置关闭，对新请求生效，无需重启。若应用升级造成故障，停止新请求、保存现场和新增业务数据，再按已演练的兼容性结果回退镜像。

若候选版本已执行不兼容迁移，不能只换回旧镜像；数据库恢复必须单独决定并考虑升级后的账务/用量记录，不可自动以旧备份覆盖新业务数据。恢复前重新备份当前现场并核对损失时间窗。

## 生产执行门槛与当前未完成项

- 尚未核对当前生产版本/架构、迁移差异和资源基线。
- 尚未做生产版本数据的升级/恢复演练。
- 本地新管理员尚待用户本人完成首次声明并做真实设置页面验收。
- 尚未用真实客户路径或上游账号进行生产级流式验收。

因此当前状态为“本地容器部署及基础集成验证通过，生产部署方案已形成”，而非“已允许直接升级生产”。生产服务器继续保持只读边界，实际部署需明确授权。
