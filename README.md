# 度盘

度盘是一个 Go 编写的百度网盘 Web 管理程序。它提供后台登录、百度网盘扫码授权、文件浏览、上传、下载、重命名、删除、移动、复制、文本预览/编辑等常用网盘操作。

项目默认以单个 Go 服务运行，前端静态资源内嵌在同一 HTTP 服务目录中，不依赖 PHP。

## 功能概览

- 后台登录：使用本地管理员账号保护管理界面。
- 百度扫码授权：页面内显示百度官方二维码，扫码确认后由服务端自动完成 OAuth 授权码换取 token。
- 文件管理：浏览目录、新建目录、上传、删除、重命名、移动、复制。
- 下载与预览：支持生成本地下载入口，图片/音频/视频/文本文件可直接预览。
- 文本编辑：常见文本文件可在线读取并覆盖保存。
- Token 刷新：访问令牌过期后可通过刷新令牌续期。
- 站点设置：支持修改站点名称、默认目录、后台账号和密码。

## 目录结构

```text
.
├── cmd/bpdrive/main.go          # 程序入口
├── internal/app/                # 服务端业务逻辑
│   ├── auth_qr.go               # 百度扫码登录和 OAuth 授权流程
│   ├── baidu.go                 # 百度网盘 API 客户端
│   ├── path.go                  # 路径清理和根目录约束
│   ├── server.go                # HTTP 路由和接口
│   └── store.go                 # 配置和 token 持久化
├── web/static/                  # 前端页面、样式、脚本和站标
├── data/config.example.json     # 配置模板，不包含真实密钥
└── deploy/bpdrive.service.example
```

## 环境要求

- Go 1.15 或更高版本。
- 一个百度开放平台应用，授权回调地址建议使用 `oob`。
- Linux 服务器可选配 systemd 托管。

## 配置

运行时配置保存在 `data/config.json`。首次启动时如果文件不存在，程序会自动创建默认配置。也可以复制模板：

```bash
cp data/config.example.json data/config.json
```

关键字段说明：

| 字段 | 说明 |
| --- | --- |
| `app_key` | 百度开放平台应用 AppKey |
| `secret_key` | 百度开放平台应用 SecretKey |
| `redirect_uri` | 百度 OAuth 回调地址，扫码授权推荐 `oob` |
| `default_dir` | 网盘默认展示目录 |
| `admin_user` | 后台登录账号 |
| `admin_pass` | 后台登录密码 |
| `site_title` | 网站显示名称 |
| `token` | 百度 OAuth token，扫码授权成功后自动写入 |
| `user` | 百度账号信息，授权成功后自动写入 |

也可以通过环境变量提供百度应用配置：

```bash
export BPDRIVE_BAIDU_APP_KEY=your_baidu_app_key
export BPDRIVE_BAIDU_SECRET_KEY=your_baidu_secret_key
export BPDRIVE_BAIDU_REDIRECT_URI=oob
```

真实的 `data/config.json` 包含密钥和 token，已经被 `.gitignore` 排除，不应提交到公开仓库。

## 构建

```bash
go test ./...
go build -o bpdrive ./cmd/bpdrive
```

## 运行

```bash
BPDRIVE_ADDR=:18088 BPDRIVE_DATA=./data ./bpdrive
```

环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `BPDRIVE_ADDR` | `:8088` | HTTP 监听地址 |
| `BPDRIVE_DATA` | `./data` | 配置和 token 数据目录 |
| `BPDRIVE_BAIDU_APP_KEY` | 空 | 百度 AppKey，可覆盖空配置 |
| `BPDRIVE_BAIDU_SECRET_KEY` | 空 | 百度 SecretKey，可覆盖空配置 |
| `BPDRIVE_BAIDU_REDIRECT_URI` | `oob` | 百度 OAuth 回调地址 |

启动后访问：

```text
http://服务器IP:18088
```

默认后台账号密码为 `admin` / `admin`。首次部署后应立即在后台设置中修改密码。

## systemd 部署

参考模板：`deploy/bpdrive.service.example`。

示例部署到 `/opt/bpdrive`：

```bash
mkdir -p /opt/bpdrive
cp bpdrive /opt/bpdrive/
cp -r web data /opt/bpdrive/
cp deploy/bpdrive.service.example /etc/systemd/system/bpdrive.service
systemctl daemon-reload
systemctl enable --now bpdrive
```

如果程序路径或运行用户不同，需要同步修改 service 文件中的 `WorkingDirectory`、`ExecStart`、`User` 和 `Group`。

## 百度扫码授权

1. 登录后台。
2. 打开授权区域并刷新二维码。
3. 使用百度网盘 App 扫码。
4. 在手机端确认授权。
5. 服务端自动完成扫码登录、授权确认、授权码换 token，并保存到 `data/config.json`。

授权流程使用百度官方二维码图片和 OAuth 页面，不生成假二维码，也不要求用户手动粘贴授权码。

## 安全说明

- 不要提交 `data/config.json`。
- 不要提交编译出的 `bpdrive` 二进制。
- 部署后修改默认后台密码。
- 公网部署建议放在 HTTPS 反向代理之后。
- 百度 OAuth 应用密钥建议通过服务器配置或私有配置文件保存，不要写入公开源码。

## 更多文档

- [部署文档](docs/DEPLOYMENT.md)
- [百度授权流程说明](docs/BAIDU_AUTH.md)
- [接口和维护说明](docs/OPERATIONS.md)
