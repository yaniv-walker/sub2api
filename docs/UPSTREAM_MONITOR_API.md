# Upstream Monitor API 接口文档

## 1. 基本约定

- Base URL：`/api/v1/plugins/upstream-monitor`
- 所有接口要求管理员认证。
- 使用 Admin API Key 时请求头为 `x-api-key: <admin-api-key>`；JWT 才使用 `Authorization: Bearer <jwt>`。
- 成功响应为 JSON；错误响应统一为 `{ "error": "..." }`。
- 余额单位为人民币（CNY）。`total_balance` 是所有上游合计；上游维度余额使用 `by_upstream`。
- 账号数据来自主程序后台账号表；插件只保存上游类型、名称、启用状态、令牌密文和换算系数。

## 2. 上游配置

### GET `/upstreams`

返回按规范化根地址归并的上游。`/v1`、`/v1/chat/completions` 等模型请求路径不会参与归组。

响应：

```json
{
  "upstreams": [{
    "id": 2,
    "name": "xinyun上游",
    "base_url": "https://api.xinyunai.cloud",
    "type": "nexapi",
    "configured": true,
    "enabled": true,
    "account_count": 2,
    "account_ids": [1, 2],
    "has_access_token": false,
    "has_personal_access_token": true,
    "has_passkey": false,
    "quota_divider": 500000
  }],
  "total": 1
}
```

### PUT `/upstreams`

按 `base_url` 创建或更新上游配置。`type` 只能是 `sub2api` 或 `nexapi`。令牌字段只写入不回显；省略字段会保留原值。

请求：

```json
{
  "base_url": "https://api.xinyunai.cloud/v1",
  "name": "xinyun上游",
  "type": "nexapi",
  "enabled": true,
  "personal_access_token": "可选，长期个人访问令牌",
  "passkey": "可选，长期 Passkey",
  "access_token": "兼容旧版访问令牌",
  "quota_divider": 500000
}
```

响应返回 `id`、规范化后的 `base_url`、`upstream_type`、时间戳、凭据是否存在和 `quota_divider`，不返回任何令牌内容。

## 3. 总览和刷新

### GET `/overview`

返回总览卡片和上游列表所需的聚合数据：

```json
{
  "total_balance": 121.84,
  "total_accounts": 3,
  "total_upstreams": 2,
  "by_type": {},
  "by_upstream": [{
    "id": 2,
    "name": "xinyun上游",
    "base_url": "https://api.xinyunai.cloud",
    "type": "nexapi",
    "balance": 93.63,
    "account_count": 2,
    "account_id": 2
  }],
  "low_balance_count": 0,
  "error_count_24h": 0,
  "errors": {}
}
```

### POST `/refresh-all`

刷新所有启用上游。相同根地址只查询一次。

响应：`refreshed_count`、`refreshed_upstreams`、`failed_count`、`total_balance`、`by_upstream`、`errors`。

### POST `/accounts/:id/refresh`

刷新指定主程序账号。响应包含 `account_id`、`base_url`、`balance`、`refreshed_at`。账号不存在返回 404。

## 4. 账号

### GET `/accounts`

查询参数：

| 参数 | 类型 | 说明 |
|---|---|---|
| `platform` | string | `sub2api` 或 `nexapi` |
| `status` | string | 当前支持 `active`；普通无 `base_url` 的 API Key 不纳入 |

响应：`{ "accounts": [{ "id", "name", "type", "base_url", "status", "description" }], "total" }`。

### GET `/accounts/:id`

返回账号实时详情：`id`、`name`、`type`、`base_url`、`balance`、`concurrency`、`status`、`description`、`last_updated`。

## 5. 分析

- `GET /accounts/:id/errors?days=7`：错误统计。
- `GET /accounts/:id/usage?days=30`：余额快照推导的消耗分析，返回 `total_cost`、`average_daily_cost`、`daily_usage`。
- `GET /accounts/:id/prediction`：根据历史快照返回余额可用时长预测。

没有历史快照时，分析接口可能返回空数组或零值，前端应提供空状态而不是报错。

## 6. 前端错误处理

- `401/403`：提示管理员认证失效并引导重新登录。
- `404`：账号或上游已不存在，刷新列表。
- `422/400`：展示后端 `error` 文本，保留用户表单输入。
- `500`：展示“刷新失败”，保留上一次成功数据，并允许重试。
- `refresh-all` 部分失败时仍展示成功上游数据，同时在页面显示 `errors`。


## 7. 上游维度分析

由于一个上游可关联多个账号且共享统一余额，前端正式联调应使用上游维度接口，而不是把单一账号分析当作上游分析：

- `GET /upstreams/:id/errors?days=30`
- `GET /upstreams/:id/usage?days=30`
- `GET /upstreams/:id/prediction`

三个路由已接入后端：错误按上游全部关联账号聚合；余额快照按时间去重后计算共享额度消耗，不叠加账号余额；无请求事件时请求数和错误率返回 `null`。完整请求/响应契约、聚合口径、空数据和错误语义见 [UPSTREAM_MONITOR_UPSTREAM_ANALYTICS_DESIGN.md](./UPSTREAM_MONITOR_UPSTREAM_ANALYTICS_DESIGN.md)；前端类型、状态和验收步骤见 [UPSTREAM_MONITOR_FRONTEND_ANALYTICS_HANDOFF.md](./UPSTREAM_MONITOR_FRONTEND_ANALYTICS_HANDOFF.md)。

## Runtime per-upstream credentials

The administrator configures each upstream independently with `PUT /api/v1/plugins/upstream-monitor/upstreams`. The request uses `base_url` to select the normalized upstream origin and may include `type`, `name`, `enabled`, `quota_divider`, and one of `access_token`, `personal_access_token`, or `passkey`. Credential fields are write-only; responses expose only `has_*` flags. Leave a credential field empty to keep the existing value.

Credentials are encrypted with a plugin-owned key persisted in the plugin table. `TOTP_ENCRYPTION_KEY` is not required for upstream-monitor credentials. If data was encrypted by an older build and cannot be decrypted, `GET /upstreams` returns `credential_error`; re-save the credential for that upstream from the runtime configuration dialog.
