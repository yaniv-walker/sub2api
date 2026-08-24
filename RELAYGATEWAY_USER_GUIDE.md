# RelayGateway 用户操作指南

## 1. 注册与登录

1. 打开 RelayGateway 网站，进入“注册”。
2. 使用常用邮箱完成验证并设置独立密码。
3. 登录后先在“仪表板”检查余额、并发限制和近期用量。
4. 忘记密码时在登录页发起重置，并通过邮箱验证码设置新密码。

请勿与其他网站共用密码。验证码、密码、API Key 和登录令牌都不应发送给他人。

## 2. 购买并兑换余额

1. 打开“兑换”页面。
2. 点击“购买兑换码”进入链动小铺；如果店铺无法在站内显示，使用“在新窗口打开”。
3. 购买后复制完整兑换码，返回“兑换”页面提交。
4. 兑换成功后核对新余额和兑换记录。

兑换码只能使用一次。输入时不要增加空格，也不要在聊天或工单中公开完整兑换码。

## 3. 创建 API Key

1. 打开“API Key”页面并点击“创建 API Key”。
2. 填写便于识别的名称，例如 `个人-Codex`。
3. 选择一个分组。分组决定可用模型、上游线路和计费倍率，一个 Key 同时只属于一个分组。
4. 按需设置余额或用量上限；不需要限制时保持默认值。
5. 创建后立即妥善保存 Key，不要提交到 Git、截图或前端代码。

建议为不同设备或应用分别创建 Key。发现泄露时只需禁用或删除对应 Key，不影响其他应用。

## 4. Base URL 与倍率

后台展示的 API Base URL 是唯一推荐入口，以下示例用 `${RELAYGATEWAY_BASE_URL}` 表示。实际使用时请从“API Key”页面的“使用”按钮复制配置。

常用协议端点：

| 协议 | Base URL 或端点 |
| --- | --- |
| OpenAI 兼容 | `${RELAYGATEWAY_BASE_URL}/v1` |
| Anthropic 兼容 | `${RELAYGATEWAY_BASE_URL}`，请求路径为 `/v1/messages` |
| Gemini 兼容 | `${RELAYGATEWAY_BASE_URL}/v1beta` |
| Antigravity Claude | `${RELAYGATEWAY_BASE_URL}/antigravity` |

模型目录和用量页面显示的倍率表示相对基础价格的计费倍数。例如 `0.18x` 表示按该模型基础价格的 18% 计算。最终扣费以用量明细中的 Token、倍率和账单金额为准。

## 5. OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-relaygateway-example",
    base_url="${RELAYGATEWAY_BASE_URL}/v1",
)

response = client.responses.create(
    model="请填写分组内可用模型",
    input="你好，请用一句话介绍自己。",
)
print(response.output_text)
```

不要把 API Key 直接写入正式代码。生产环境应通过环境变量或 Secret 管理器注入。

## 6. Codex CLI

在“API Key”页面点击“使用”，选择 Codex CLI 和当前操作系统，优先复制系统生成的配置。手工配置时使用以下结构。

`~/.codex/config.toml`（Windows 为 `%USERPROFILE%\.codex\config.toml`）：

```toml
model_provider = "RelayGateway"
model = "请填写分组内可用模型"
disable_response_storage = true

[model_providers.RelayGateway]
name = "RelayGateway"
base_url = "${RELAYGATEWAY_BASE_URL}"
wire_api = "responses"
requires_openai_auth = true
```

`~/.codex/auth.json`（Windows 为 `%USERPROFILE%\.codex\auth.json`）：

```json
{
  "OPENAI_API_KEY": "sk-relaygateway-example"
}
```

## 7. Claude Code

PowerShell 临时配置：

```powershell
$env:ANTHROPIC_BASE_URL="${RELAYGATEWAY_BASE_URL}"
$env:ANTHROPIC_AUTH_TOKEN="sk-relaygateway-example"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
claude
```

macOS 或 Linux：

```bash
export ANTHROPIC_BASE_URL="${RELAYGATEWAY_BASE_URL}"
export ANTHROPIC_AUTH_TOKEN="sk-relaygateway-example"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
claude
```

只有 Key 所属分组支持 Anthropic 协议或相应模型时，该配置才可用。

## 8. 查看用量与排查问题

在“用量”页面按时间、API Key 和模型筛选请求，重点核对：

- 输入、输出和缓存 Token；
- 模型与计费倍率；
- 扣费金额、首 Token 时间和总耗时；
- HTTP 状态和错误信息。

遇到调用失败时按以下顺序检查：

1. Base URL 是否多写或漏写 `/v1`。
2. API Key 是否启用、是否属于正确分组、额度是否充足。
3. 请求模型是否在该分组中可用。
4. 并发或 RPM 是否达到限制。
5. “监控”页面中的目标渠道是否可用。

提交问题时可提供请求时间、模型、请求 ID 和错误信息，但必须隐藏 API Key、Cookie、验证码及完整提示词中的隐私数据。

## 9. 安全建议

- 每个应用使用独立 API Key，并设置合理额度。
- Key 只保存在服务端环境变量或 Secret 管理器中。
- 不在浏览器前端、公开仓库、日志或聊天记录中保存完整 Key。
- 定期检查最近用量；发现异常立即禁用 Key 并创建新 Key。
- 公共设备使用后退出登录，不勾选长期保存凭据。
