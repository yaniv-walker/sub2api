# 上游监控：上游维度分析接口设计

## 1. 目标与边界

上游是规范化主域名、协议和端口相同的一组后台账号。余额、错误和用量均以“上游”为统计主体；账号只是该上游下的分组，不得把同一额度按账号重复累计。

本设计只扩展插件路由和插件查询逻辑，不修改主程序账号表、网关转发逻辑或主程序余额数据。接口全部要求管理员认证，基础路径为：

```text
/api/v1/plugins/upstream-monitor
```

现有账号维度接口继续保留，作为账号详情和兼容入口；新增接口不改变既有字段。

## 2. 资源标识与聚合口径

### 2.1 上游 ID

上游配置表的 `id` 是稳定资源标识。客户端应使用 `upstream_id`，不要使用名称或 URL 作为主键。

### 2.2 时间范围

分析接口使用查询参数：

- `days`：正整数，默认 `30`，允许范围 `1-90`。
- `from`、`to`：可选 ISO-8601 时间。传入后优先于 `days`，最大跨度 90 天。
- 时间按服务端 UTC 计算并返回 RFC3339 时间；前端仅负责本地化显示。

### 2.3 多账号聚合

- 错误：聚合该上游全部关联账号的错误记录；同一事件按记录计数，不按账号余额加权。
- 用量：以余额快照和请求事件为来源。余额仍只取上游统一余额的一份；消耗按快照差值计算，异常回升日的消耗记为 0 并单独计入 `anomalies`。
- 请求数：只有已记录的请求事件才计数；没有请求事件时返回 `null`，不能伪装成 0。
- 预测：基于上游余额快照的日消耗率预测，不把多个账号的余额相加。

## 3. 接口总览

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/upstreams/:id/errors` | 上游错误统计和按日趋势 |
| GET | `/upstreams/:id/usage` | 上游余额消耗、按日趋势和请求数 |
| GET | `/upstreams/:id/prediction` | 上游统一余额可用时长预测 |

## 4. 错误统计

### 请求

```http
GET /api/v1/plugins/upstream-monitor/upstreams/2/errors?days=30
```

### 响应 200

```json
{
  "upstream_id": 2,
  "upstream": { "name": "xinyun上游", "base_url": "https://api.xinyunai.cloud", "type": "nexapi" },
  "range": { "from": "2026-08-24T00:00:00Z", "to": "2026-09-23T00:00:00Z", "days": 30 },
  "total_errors": 3,
  "error_rate": 0.0008,
  "error_rate_unit": "ratio",
  "affected_account_count": 1,
  "by_type": [{ "type": "timeout", "count": 2, "last_seen": "2026-09-23T10:20:00Z" }],
  "daily": [{ "date": "2026-09-23", "count": 3 }],
  "recent": [{ "account_id": 2, "error_type": "timeout", "error_code": "504", "message": "upstream timeout", "occurred_at": "2026-09-23T10:20:00Z" }]
}
```

`error_rate` 是错误数/请求数的比例；当请求总数未知时返回 `null`，并将 `error_rate_unit` 设为 `unknown`。`recent` 最多返回 20 条，按时间倒序。

## 5. 用量趋势

### 请求

```http
GET /api/v1/plugins/upstream-monitor/upstreams/2/usage?days=30
```

### 响应 200

```json
{
  "upstream_id": 2,
  "range": { "from": "2026-08-24T00:00:00Z", "to": "2026-09-23T00:00:00Z", "days": 30 },
  "currency": "CNY",
  "starting_balance": 105.94,
  "ending_balance": 93.63,
  "total_cost": 12.31,
  "average_daily_cost": 0.41,
  "total_requests": null,
  "daily": [
    { "date": "2026-09-21", "cost": 0.38, "balance": 94.42, "request_count": null },
    { "date": "2026-09-22", "cost": 0.41, "balance": 93.63, "request_count": null }
  ],
  "anomalies": [{ "date": "2026-09-20", "kind": "balance_increased", "amount": 20.0 }],
  "data_quality": { "snapshot_count": 31, "request_events_available": false }
}
```

前端绘图优先使用 `daily[].cost`；`request_count` 为 `null` 时隐藏请求数系列并显示“暂无请求事件数据”。没有至少两个余额快照时，`daily` 为空且 `data_quality.snapshot_count` 说明原因。

## 6. 余额预测

### 请求

```http
GET /api/v1/plugins/upstream-monitor/upstreams/2/prediction
```

### 响应 200

```json
{
  "upstream_id": 2,
  "current_balance": 93.63,
  "daily_burn_rate": 0.41,
  "estimated_days_left": 228,
  "estimated_depletion_date": "2027-05-09T00:00:00Z",
  "confidence": 0.82,
  "algorithm": "moving_average",
  "data_points": 31
}
```

历史不足时仍返回 200，但 `estimated_days_left`、日期和 `confidence` 为 `null`，并返回 `data_points`，前端展示“历史数据不足”。

## 7. 错误语义

- `400 INVALID_RANGE`：日期范围或 `days` 不合法。
- `401/403`：管理员认证失败或无权限。
- `404 UPSTREAM_NOT_FOUND`：上游不存在或已无关联账号。
- `409 UPSTREAM_DISABLED`：上游已停用且策略禁止查询。
- `500 ANALYTICS_UNAVAILABLE`：存储查询失败；不得返回部分伪造数据。

错误响应统一为：

```json
{ "error": { "code": "UPSTREAM_NOT_FOUND", "message": "upstream not found" } }
```

## 8. 兼容与实现顺序

1. 先复用现有 `GET /accounts/:id/errors`、`usage`、`prediction` 的仓储查询，按上游关联账号聚合。
2. 增加按日聚合查询和统一响应模型；保留旧账号接口不变。
3. 当请求事件尚未接入时，`total_requests` 与每日 `request_count` 必须返回 `null`，不能填 0。
4. 完成后补充 handler、聚合服务和 PostgreSQL fixture 测试，再开放前端联调。
