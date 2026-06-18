# 百度授权流程说明

DPDrive使用百度 OAuth 授权码模式访问百度网盘接口。

## 配置要求

百度应用配置需要包含：

- `AppKey`
- `SecretKey`
- `redirect_uri`

推荐 `redirect_uri` 使用：

```text
oob
```

`oob` 模式下，百度授权成功后会在最终页面显示授权码，服务端再用授权码换取 `access_token` 和 `refresh_token`。

## 扫码授权流程

1. 后台请求 `/api/auth/session` 创建扫码会话。
2. 服务端打开百度 OAuth 页面，建立同一组 cookie。
3. 服务端调用百度二维码接口，返回官方二维码图片地址。
4. 前端显示 `/api/auth/qrcode-image?id=...`，该接口只代理百度官方二维码图片。
5. 前端轮询 `/api/auth/poll?id=...`。
6. 用户用百度网盘 App 扫码并在手机端确认。
7. 服务端通过百度返回的登录凭据继续 OAuth 页面。
8. 服务端获取 `stoken` 和 `bdstoken`。
9. 服务端提交百度“与百度连接”授权确认表单。
10. 百度返回 `code` 后，服务端调用 token 接口换取访问令牌。
11. token 和用户信息写入 `data/config.json`。

整个过程不需要用户手动复制或粘贴授权码。

## 常见错误

### redirect_uri_mismatch

说明百度开放平台应用配置的回调地址和程序请求的 `redirect_uri` 不一致。

处理方式：

- 如果使用 `oob`，确认 `data/config.json` 中 `redirect_uri` 为 `oob`。
- 如果使用 URL 回调，确认百度开放平台后台配置完全一致，包括协议、域名、路径和端口。

### invalid_grant

说明用于换 token 的 code 无效、过期或不是百度返回的 OAuth code。

常见原因：

- 扫码确认后等待太久，code 过期。
- 页面解析误抓了非授权码字段。
- 重复使用同一个 code。

当前实现只从重定向 URL、明确的 `oob` 授权码展示区域和相关输入框读取 code，避免误抓页面脚本变量。

### 未找到 code

说明服务端已经拿到百度授权页，但没有进入最终授权码页面。

排查方向：

- 检查百度应用权限是否包含 `basic,netdisk`。
- 检查 `AppKey`、`SecretKey`、`redirect_uri` 是否匹配。
- 查看后台错误信息中的安全摘要，确认卡在哪个表单或页面标题。

## 相关接口

| 接口 | 说明 |
| --- | --- |
| `POST /api/auth/session` | 创建扫码授权会话 |
| `GET /api/auth/qrcode-image?id=...` | 获取二维码图片 |
| `GET /api/auth/poll?id=...` | 轮询扫码状态并自动完成授权 |
| `POST /api/auth/refresh` | 刷新 token |
| `POST /api/auth/logout` | 清除已保存的 token |

## 安全注意事项

- 二维码会话只保存在服务端内存中。
- 不要在日志或 issue 中公开 `access_token`、`refresh_token`、`bduss`、`stoken`、`bdstoken` 或完整 `data/config.json`。
- `data/config.json` 已在 `.gitignore` 中排除。
