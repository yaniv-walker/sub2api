# RelayGateway 部署与运维手册

## 1. 上线前准备

- 腾讯云新加坡轻量应用服务器，首发可使用 2 vCPU / 4 GB / 60 GB SSD；建议 Ubuntu 24.04 LTS、x86_64 架构。
- 一个已解析到服务器公网 IP 的 HTTPS 域名。
- 腾讯云 COS 私有存储桶，地域优先与服务器一致。
- 仅允许当前业务用途的上游 API 凭据。
- RelayGateway Logo、favicon 和链动小铺正式 HTTPS 地址。

不要把 `.env`、上游 API Key、COS Secret 或 SMTP 密码提交到 Git。生产 `.env` 权限设为 `600`，服务器安全组只开放 `22`、`80`、`443`。

## 2. 部署

1. 将仓库检出到服务器固定目录，并切换到已验证提交。
2. 复制 `deploy/.env.example` 为 `deploy/.env`，生成并填写独立的数据库密码、Redis 密码、管理员密码、JWT Secret 和 TOTP 加密密钥。
3. 设置 `BIND_HOST=127.0.0.1`，由 Caddy 统一承接公网请求。
4. 设置 Caddy 环境变量 `RELAYGATEWAY_DOMAIN`，例如 `api.relay-gateway.xyz`。
5. 在 `deploy` 目录执行 `docker compose config --quiet`，确认无误后执行 `docker compose up -d`。
6. 用 `docker compose ps` 和 `curl -fsS http://127.0.0.1:8080/health` 检查健康状态。

`deploy/docker-compose.yml` 中的应用、PostgreSQL 和 Redis 镜像已固定到 digest。轻量服务器只作为 MVP 配置，接近 20 个活跃用户或出现持续内存压力后升级套餐；接近 50 个峰值流式并发时迁移到 8 vCPU / 16 GB CVM。升级前先完成备份与恢复演练，再单独更新 digest。

轻量服务器升级前确认目标套餐仍在新加坡可售，并保留快照和 COS 备份。轻量服务器升级到更高套餐通常可以保留实例数据和公网地址，但迁移到 CVM、跨架构或跨地域不保证一键完成，应按“新建实例 → 恢复备份 → 切换 DNS”准备回滚方案。

## 3. 运行时配置

在“管理后台 → 系统设置”配置：

- 站点名称：`RelayGateway`
- API Base URL：正式 HTTPS API 地址
- 文档地址：`https://<域名>/custom/relaygateway-guide`
- SMTP：`smtp.resend.com:587`，用户名 `resend`，密码为 Resend API Key，TLS 开启
- 发件人：`sys@relay-gateway.xyz`，显示名称 `RelayGateway`
- 注册、邮箱验证、密码重置：开启

链动小铺正式 URL 到位后同时更新：

1. “自定义菜单”中 `liandong-shop` 的 URL；
2. “购买订阅 URL”，供兑换页“购买兑换码”按钮复用。

先执行 SMTP 连接测试和测试邮件，再分别冒烟注册验证码与密码重置邮件。

## 4. 上游账号、模型与倍率

1. 在“账号管理”新建 OpenAI API Key 账号，Base URL 填上游地址，API Key 只在后台填写。
2. 先使用“同步上游模型”核对实际模型 ID，再创建 OpenAI 分组。
3. 分组只上架已验证可调用的模型，并设置面向用户的 `rate_multiplier`，例如 `0.18` 对应 `0.18x`。
4. 将账号绑定到该分组，账号并发先设为上游授权范围内的保守值。
5. 用普通用户创建只绑定该分组的 API Key，完成非流式和流式各一次调用。
6. 在“用量”核对输入 Token、输出 Token、倍率和扣费金额；上游未返回 usage 时，由 Sub2API 原生计费链路计算。

不要在未核实转售或代理权限前把账号加入付费目录。

## 5. PostgreSQL 备份

优先复用“管理后台 → 系统设置 → 备份管理”的原生 S3 备份，不维护第二套脚本。

腾讯 COS 配置参考：

- Endpoint：`https://cos.<region>.myqcloud.com`
- Region：COS 存储桶地域，例如 `ap-singapore`
- Bucket：完整存储桶名称
- Prefix：`relaygateway/backups/`
- Force path style：关闭
- Access Key：使用仅有目标存储桶最小读写权限的子账号密钥

先测试 S3 连接，再手动创建一份备份。确认状态为 `completed` 后设置：

- Cron：`0 3 * * *`
- Retain days：`7`
- Retain count：`7`

每月在隔离测试实例执行一次恢复演练：使用与生产相同版本启动空实例，导入最近备份，核对管理员登录、用户数、分组、API Key 数和用量记录。恢复会覆盖目标数据库，只能在明确的演练或事故窗口执行。

## 6. 日志与故障排查

- 应用日志：`docker compose logs --tail 200 sub2api`
- PostgreSQL：`docker compose logs --tail 200 postgres`
- Redis：`docker compose logs --tail 200 redis`
- Caddy：`/var/log/caddy/relaygateway.log`
- 健康检查：`curl -fsS http://127.0.0.1:8080/health`

日志不得记录完整 API Key、JWT、Cookie、SMTP 密码、COS Secret 或完整会话内容。排查请求时优先使用时间、请求 ID、模型、HTTP 状态和脱敏后的用户/API Key 标识。

2 核 4 GB 首发实例建议额外设置：Docker 日志单文件不超过 100 MB、最多保留 3 个轮转文件；PostgreSQL 最大连接数先控制在 50 以内；定期检查内存、磁盘、流量包和容器重启次数。

## 7. 大陆访问验收

上线后用至少两个大陆运营商网络执行同一组请求，记录 DNS、TCP/TLS 连接、首 Token、总耗时、HTTP 状态和流中断情况：

1. 登录并刷新页面；
2. 创建 API Key；
3. 调用 `/v1/models`；
4. 非流式请求一次；
5. 流式请求持续至少 60 秒；
6. 查看用量和余额变化。

新加坡单点先作为唯一入口。只有真实数据证明不可接受时，再评估具备合法资质的大陆接入层；不要让大陆节点保存用户余额、上游 Secret 或计费账本，避免形成双写和账务不一致。

## 8. 上线检查

- 容器健康，重启后会话、数据和配置保留。
- HTTPS 证书有效，HTTP 自动跳转 HTTPS。
- 管理员启用 2FA，默认管理员密码已更换。
- SMTP、注册、重置密码均通过。
- 店铺链接、兑换、API Key、模型调用、用量扣费均通过。
- COS 手动备份和隔离恢复演练通过。
- 日志脱敏，上游授权和 LGPL-3.0 分发材料已核对。
