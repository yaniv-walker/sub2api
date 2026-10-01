# Caddy 静态包发布与回滚

本次只生成了部署材料，未连接生产执行写入。以下命令留待用户验收后执行。

## 目录结构与导出

```text
/srv/sub2api-homepage/
  releases/rN-xxxxxxxx/
    index.html
    homepage-assets/{home.css,home.mjs,contract.mjs,logo.svg}
    homepage/config.json
    release-manifest.json
  current -> releases/rN-xxxxxxxx
```

本机工作台点“导出静态包”，或运行 `npm run build`。结果在 `dist/rN-xxxxxxxx/`。只上传这一份目录；不要上传工程根目录的 admin、data、日志或 SSH 密钥。Caddy 用户要有目录执行权限和公开文件读取权限。

## 首次部署顺序

1. 记录正在运行的真实镜像与部署来源，保留现有 Caddy 配置。之前读取的 compose 镜像与容器版本不一致，不运行 docker compose up 来部署首页。
2. 将导出包上传到一个新 releases 目录，不覆盖旧目录。
3. 校验导出清单与配置：

```bash
python3 activate-release.py --release r1-xxxxxxxx
```

默认只读，不切版本。清单有不明文件、符号链接、路径越界、哈希或 revision 不一致即拒绝。该校验是完整性检查，正式配置语义已经由本机共享校验器完成。

4. 切换 current（显式写入）：

```bash
python3 activate-release.py --release r1-xxxxxxxx --activate
```

5. 合并 `deploy/Caddyfile.example` 到 `/etc/caddy/Caddyfile`。保持原有压缩 SSE 排除、header、日志、TLS 和代理行为，只增加首页三条路径。核对现有活跃 Caddy 配置与磁盘文件，不以磁盘文件作为唯一运行态证据。
6. `caddy adapt` / `caddy validate` 后再 reload，备份配置可立即恢复。
7. 精确验证下面路径，HTTP 200 的 SPA HTML 不等于浏览器登录已成功，需查看实际 LoginView。

| 路径 | 预期 |
|---|---|
| `/` | 静态首页 |
| `/homepage/config.json` | 发布 JSON，无草稿 |
| `/homepage-assets/home.mjs` | JavaScript，不是 SPA HTML |
| `/login`、`/register`、`/dashboard` | 原 Sub2API SPA |
| `/assets/*`、`/logo.svg` | 原 Sub2API 资源 |
| `/api/v1/settings/public` | 原 Envelope JSON |
| `/v1/*`、OAuth、支付回调、webhook、SSE | 保持现有路由/流式行为 |
| `/admin`、`/admin-api/*` | 不由静态文件服务提供，交原 Sub2API 路由 |

## 后续发布

本地保存 → 发布 → 导出 → 上传全新 release → 哈希验证 → 原子切换 current。Caddy 路由不变时不需要 reload；仅文件指针变化。public config 和稳定资源名 no-cache，避免 immutable 缓存错配。

原子 symlink 切换可避免目录半上传对外服务，但一次浏览器加载的多个请求仍可能跨越两个 release。首期 schema/资源路径保持兼容；未来需要修改公共 schema 时应引入版本资源路径与兼容迁移，不直接破坏旧页面。

## 回滚

保留旧 releases，用同一个工具验证并 `--activate` 指向旧 ID。业务网关和数据库无需重启、迁移。若首次路径规则导致异常，恢复旧 Caddy 配置并验证/reload，把整个域名重新交回原应用。这里没有自动删除历史目录或清空数据操作。

## 当前不能自动完成的部分

独立工作台“发布版本”只更新本地预览。普通首页插件目前无法通过现有 `.s2plugin` 清单安装；自动 SSH 同步也没有绑定到后台按钮。这样用户可以先验收页面和导出包，再单独决定生产部署。未来自动同步需要受控 exporter 与发布权限，不把私钥放入浏览器或公开配置。
