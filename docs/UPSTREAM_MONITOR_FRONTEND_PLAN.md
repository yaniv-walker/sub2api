# Upstream Monitor 前端开发计划

## 目标

在现有管理员布局中增加 `/admin/upstream-monitor` 页面，提供上游维度余额总览、上游配置、账号详情和分析能力。页面通过插件 API 获取数据，不读取 `config.yaml`，不修改主程序账号数据。

## 页面结构

1. **总览页**：余额、上游数、账号数、24 小时错误数；上游表格；刷新全部、筛选和最近更新时间。
2. **上游配置抽屉**：名称、根地址、类型、启用开关、个人访问令牌、Passkey、兼容访问令牌、NexAPI 换算系数。
3. **账号详情抽屉**：余额、并发、状态、所属上游、刷新按钮。
4. **分析标签**：错误统计、用量趋势、余额预测。

## 实施任务

### Phase 1：接口层

- [ ] 增加 `frontend/src/api/admin/upstreamMonitor.ts` 类型和请求封装。
- [ ] 为 `overview`、`upstreams`、`accounts`、刷新和分析接口补 Vitest mock 测试。
- [ ] 统一 401、部分刷新失败和空数据的错误模型。

### Phase 2：页面骨架

- [ ] 增加管理员路由 `/admin/upstream-monitor` 和侧边栏入口。
- [ ] 完成总览卡片、上游表格、账号表格和响应式布局。
- [ ] 处理 loading、error、empty、refreshing 四类状态。

### Phase 3：配置和详情

- [ ] 完成上游配置抽屉和只写凭据字段提示。
- [ ] 完成账号详情抽屉与单账号刷新。
- [ ] 完成类型筛选、启用状态筛选、上游搜索。

### Phase 4：分析与验收

- [ ] 接入 errors、usage、prediction 标签。
- [ ] 以真实 Sub2API/NexAPI 测试上游完成接口联调。
- [ ] 在 320、768、1024、1440 px 验证布局、键盘操作和错误状态。
- [ ] 通过 `pnpm run typecheck`、相关 Vitest、`pnpm run build`。

## 数据刷新策略

- 首次进入并行请求 `overview`、`upstreams`、`accounts`。
- 手动刷新使用 `POST /refresh-all`，成功后重新拉取 `overview` 和 `accounts`。
- 不自动高频轮询；默认手动刷新，后续可增加 60 秒以上的可配置轮询。
- 保留上一次成功数据，刷新失败只更新错误提示。

## 完成标准

- 管理员可配置一个上游并看到凭据配置状态，不泄露令牌。
- 同一根地址的多个账号只显示一个上游余额。
- `total_balance` 与 `by_upstream` 的语义在 UI 中明确区分。
- 401、404、部分失败、空数据和无历史快照均有可理解的界面反馈。

