# Upstream Monitor Plugin

上游账号统一监控与管理插件

## 功能概述

本插件为 sub2api 提供统一的上游账号监控与管理功能，包括：

- ✅ 余额监控：实时查询所有上游账号余额，统一显示（人民币）
- ✅ 并发监控：查看每个上游账号的并发限制
- ✅ 分组关联：展示本地分组与上游分组的关联关系
- ✅ 错误分析：汇总各上游账号的错误信息
- ✅ 消耗分析：分析使用频率和消耗量
- ✅ 余额预测：基于历史消耗预测剩余余额可用时长
- ✅ 利润率计算：计算各账号的利润率

## 插件特性

### 零侵入设计
- 不修改 sub2api 核心代码
- 使用独立的路由命名空间：`/api/v1/plugins/upstream-monitor/*`
- 使用独立的数据表，不污染现有表结构
- 通过配置文件一键启用/禁用

### 易于扩展
- 采用标准插件接口
- 支持事件钩子机制
- 版本独立，不影响主程序升级

## 安装

### 1. 配置文件

在 `deploy/config.example.yaml` 中已包含插件配置示例：

```yaml
upstream_monitor:
  enabled: true
  auto_migrate: true  # 自动创建数据库表
  
  features:
    balance_aggregation: true
    error_analysis: true
    usage_prediction: true
  
  cache:
    balance_ttl_seconds: 300      # 5 分钟
    error_stats_ttl_seconds: 600  # 10 分钟
  
  alerts:
    low_balance_threshold: 10.0   # CNY
    high_error_rate_threshold: 0.1 # 10% 错误率
  
  prediction:
    algorithm: "moving_average"   # 或 "linear_regression"
    window_days: 30
    min_data_points: 7
```

账号不在 YAML 中维护。插件直接读取后台管理员在账号管理页面添加的账号，且纳入状态为 active、包含 API Key 的 `upstream` 账号，以及带有 `credentials.base_url` 的 API Key 透传账号（后台界面创建的上游账号使用此形式）。类型识别优先使用账号扩展字段 `upstream_monitor_type`/`upstream_type`，其次使用明确的 `platform` 或 `base_url`；自定义域名无法区分时默认按 `sub2api` 处理，NexAPI 账号应在后台扩展字段明确设置为 `nexapi`。YAML 仅用于插件开关、阈值、缓存和预测参数。

### 2. 数据库迁移

启用 `auto_migrate: true` 后，应用启动时会自动创建以下表：

- `upstream_balance_snapshots`: 余额快照表
- `upstream_error_records`: 错误记录表

详细说明见 `doc/upstream-monitor-migration.md`。

### 3. 主程序集成

插件已通过 Wire 依赖注入自动集成到主程序，无需额外配置。

## API 接口

### 概览统计
```
GET /api/v1/plugins/upstream-monitor/overview
```

返回所有上游账号的汇总统计信息。

### 账号列表
```
GET /api/v1/plugins/upstream-monitor/accounts?platform=sub2api&status=active&page=1&page_size=20
```

支持的查询参数：
- `platform`: 过滤上游类型（sub2api, nexapi）
- `status`: 过滤状态（active, disabled, error）
- `group_id`: 过滤本地分组
- `low_balance`: 仅显示低余额账号（true/false）
- `has_errors`: 仅显示有错误的账号（true/false）
- `page`: 页码（默认 1）
- `page_size`: 每页数量（默认 20，最大 100）

### 账号详情
```
GET /api/v1/plugins/upstream-monitor/accounts/:id
```

获取指定账号的详细信息，包括余额、并发、分组关联等。

### 刷新余额
```
POST /api/v1/plugins/upstream-monitor/accounts/:id/refresh
Content-Type: application/json

{
  "force": true
}
```

强制刷新指定账号的余额信息。

### 批量刷新
```
POST /api/v1/plugins/upstream-monitor/refresh-all
```

刷新所有账号的余额信息（异步执行）。

### 错误统计
```
GET /api/v1/plugins/upstream-monitor/accounts/:id/errors?days=7
```

获取指定账号的错误统计信息。

### 消耗分析
```
GET /api/v1/plugins/upstream-monitor/accounts/:id/usage?days=30
```

获取指定账号的消耗分析数据。

### 余额预测
```
GET /api/v1/plugins/upstream-monitor/accounts/:id/prediction
```

基于历史数据预测余额可用时长。

## 支持的上游类型

### Sub2API
- 接口：`/api/user/self`
- 认证：Bearer Token
- 余额字段：`balance`（直接为人民币）
- 无需货币换算

### NexAPI
- 接口：`/api/user/self`
- 认证：Bearer Token
- 配额字段：`quota`（总配额）、`used_quota`（已用配额）
- 换算公式：`balance = (quota - used_quota) / 431778`

## 开发指南

### 目录结构

```
plugins/upstream-monitor/
├── config.go                # 插件配置
├── plugin.go                # 插件主文件
├── handler/                 # HTTP 处理器
│   └── monitor_handler.go
├── service/                 # 业务逻辑
│   ├── upstream_info_fetcher.go
│   ├── balance_aggregator.go
│   ├── error_analyzer.go
│   └── usage_predictor.go
├── model/                   # 数据模型
│   └── models.go
├── repository/              # 数据访问层（待实现）
└── migrations/              # 数据库迁移
    └── 001_create_tables.sql
```

### 添加新的上游类型

1. 在 `service/upstream_info_fetcher.go` 中添加新的 fetch 方法
2. 在 `FetchUpstreamInfo` 方法中添加 case 分支
3. 配置相应的认证和换算规则

### 事件钩子

插件监听以下事件：

- `HookAfterRequestFailed`: 请求失败时记录错误
- `HookAfterAccountDeleted`: 账号删除时清理插件数据

## 版本历史

### v1.0.0 (2024-09-20)
- ✅ 插件框架搭建
- ✅ Wire 依赖注入集成
- ✅ 基础服务实现
- ✅ 路由注册
- ✅ 数据库自动迁移
- ✅ Handler 实现（Mock 数据）
- ⏳ Repository 层（待实现真实数据访问）
- ⏳ 上游 API 客户端（待替换 Mock 实现）
- ⏳ 前端界面（待实现）

## 许可证

本插件遵循 sub2api 主项目的许可证。
