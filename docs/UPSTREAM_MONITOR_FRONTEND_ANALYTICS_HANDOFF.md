# 上游监控分析：前端联调文档

## 1. 联调范围

页面路径：`/admin/upstream-monitor`。本次联调只针对“上游详情”抽屉中的三个分析标签：错误统计、用量趋势、余额预测。

插件 API Base：`/api/v1/plugins/upstream-monitor`。请求使用现有管理员认证方式：

```http
x-api-key: admin-...
```

不要把 Admin API Key 放在 `Authorization: Bearer` 中。

## 2. 请求顺序

打开上游详情时并行请求：

```text
GET /upstreams/:id/errors?days=30
GET /upstreams/:id/usage?days=30
GET /upstreams/:id/prediction
```

详情基础数据继续来自 `GET /overview` 的 `by_upstream` 或 `GET /upstreams`。标签切换不应重复请求，除非用户改变时间范围或点击重试。

## 3. 前端类型草案

```ts
interface AnalyticsRange { from: string; to: string; days: number }
interface UpstreamErrorAnalytics {
  upstream_id: number
  upstream: { name: string; base_url: string; type: 'sub2api' | 'nexapi' }
  range: AnalyticsRange
  total_errors: number
  error_rate: number | null
  error_rate_unit: 'ratio' | 'unknown'
  affected_account_count: number
  by_type: Array<{ type: string; count: number; last_seen: string | null }>
  daily: Array<{ date: string; count: number }>
  recent: Array<{ account_id: number; error_type: string; error_code: string; message: string; occurred_at: string }>
}
interface UpstreamUsageAnalytics {
  upstream_id: number; range: AnalyticsRange; currency: 'CNY'
  starting_balance: number | null; ending_balance: number | null
  total_cost: number | null; average_daily_cost: number | null
  total_requests: number | null
  daily: Array<{ date: string; cost: number; balance: number | null; request_count: number | null }>
  anomalies: Array<{ date: string; kind: string; amount: number }>
  data_quality: { snapshot_count: number; request_events_available: boolean }
}
interface UpstreamPrediction {
  upstream_id: number; current_balance: number
  daily_burn_rate: number | null; estimated_days_left: number | null
  estimated_depletion_date: string | null; confidence: number | null
  algorithm: string; data_points: number
}
```

## 4. 页面状态

- Loading：每个标签显示骨架或加载状态，不能显示旧数据冒充最新数据。
- Empty：`daily=[]` 或 `data_points < 2` 时显示“暂无足够历史快照”。
- Partial：错误统计成功但用量失败时，错误标签仍可用，失败标签提供重试。
- Unknown：`total_requests=null` 或 `request_count=null` 时隐藏请求数图例并提示“暂无请求事件数据”。
- Error：401/403 跳转统一认证处理；404 刷新上游列表；500 保留最近一次成功内容并显示重试按钮。

## 5. 图表映射

- 错误统计：`daily[].count` 为柱状图；`by_type` 为类型排行；`recent` 为明细表。
- 用量趋势：`daily[].cost` 为主折线/柱状图；`daily[].balance` 可作为余额线；请求数仅在非空时显示。
- 余额预测：展示 `current_balance`、`daily_burn_rate`、`estimated_days_left`、`confidence`，日期字段本地化。
- 金额统一显示 CNY，保留 2 位小数；比例 `error_rate` 转百分比时乘以 100。

## 6. 联调验收

1. 配置同一根地址的两个账号，确认详情只展示一个上游余额和一组分析数据。
2. 触发至少一条插件错误，确认 `total_errors`、`by_type`、`recent` 和 `daily` 能对应。
3. 产生至少两次余额刷新快照，确认 `daily` 出现消耗数据；余额回升时显示 anomaly。
4. 无请求事件时确认界面不显示“0 次请求”，而是显示未知/暂无数据。
5. 删除或停用上游后，详情显示 404/409 状态并回到列表。
6. 在 320、768、1024、1440 px 检查图表、表格和抽屉无横向布局破坏。

## 7. 当前实现状态

以上游维度接口目前属于设计契约，后端尚未实现；账号维度的 `/accounts/:id/errors`、`/usage`、`/prediction` 已存在，但不能替代多账号共享额度的上游聚合接口。前端在后端接口完成前，不应把原型中的静态数据当作真实数据。
