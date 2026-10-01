# RelayGateway Caddy 静态首页

独立工程：`E:\ai\dev\sub2api-homepage`。Node.js 22+，零第三方运行依赖。

当前交付预览：<http://127.0.0.1:4182/>；配置工作台：<http://127.0.0.1:4182/admin>。QQ HTTP 链接请在这组地址测试，旧 4179/4180 窗口连接旧预览进程。下面的 npm start 使用默认端口 4178；测试期间只使用一组配置工作台，配置服务为单写者文件存储。

## 查看与测试

```powershell
cd E:\ai\dev\sub2api-homepage
npm start
```

- 首页：<http://127.0.0.1:4178/>
- 配置工作台：<http://127.0.0.1:4178/admin>

首次展示的是明确标注的示例模型目录。配置后台支持品牌、导航、按钮、API 地址、模型、能力卡片、公开公告、页脚、排序、启用开关、JSON 导入导出。

公告按钮支持 QQ 群邀请链接，例如 `http://qm.qq.com/q/xxxxx`、`http://qm.qq.com/cgi-bin/qm/qr?k=xxxxx` 或带查询参数的链接。HTTP 仅允许 `qm.qq.com`、`qun.qq.com`、`jq.qq.com`、`shang.qq.com` 这些 QQ 官方域名；其他 HTTP 外链仍会被拒绝。保存时会去除复制链接附带的首尾空白；链接会在新标签页打开，并使用 `noopener noreferrer`。

“草稿预览”显示当前表单；“保存草稿”写入 `data/state.json`，首页仍使用旧发布版本；“发布版本”更新本地首页；“导出静态包”生成 `dist/rN-xxxxxxxx/`。

本地 `/login`、`/register`、`/dashboard`、`/model-plaza` 会 302 到对应线上页面，因此可直接测试真实入口跳转。本地没有登录表单，也不代理密码请求。生产 Caddy 保留这些路径交给原 Sub2API。

```powershell
npm test
npm run build
```

## 公开接口联调

默认使用离线 fixture。停止原预览进程后运行：

```powershell
node tools/server.mjs --live-public
```

此模式仅 GET 生产的 `/api/v1/settings/public`、`/api/v1/model-plaza`，不发送凭据、不跳过 TLS 验证；模型候选需管理员勾选后保存/发布。生产不可达时操作报错且旧发布快照保留。

## 文档

- [详细设计与开发计划](docs/详细设计与开发计划.md)
- [前端联调接口契约](docs/前端联调接口契约.md)
- [Caddy 发布与回滚](docs/Caddy发布与回滚.md)
- [测试记录](docs/测试记录.md)

## 当前集成状态

已实现独立本地配置服务和静态站点；已导出可交给 Caddy 的文件。未部署生产。

当前 Sub2API `.s2plugin` manifest 只接受 `openai.oauth.outbound_transport.v1` 能力，无法直接安装普通首页插件。UI Bridge 对接方案见接口文档；生产插件包还需底座支持通用配置插件以及受控导出能力。本工程可独立维护，无需改用户、渠道、计费或网关代码。

不要把本地 Node 配置服务暴露公网；只绑定 `127.0.0.1`。生产仅部署 `dist` 内访客资源，配置状态、管理 UI、测试文件和密钥不进入静态目录。
