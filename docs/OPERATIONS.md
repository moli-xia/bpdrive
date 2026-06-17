# 接口和维护说明

本文面向部署和维护人员，说明服务运行、后台接口和常见维护操作。

## 后台会话

后台登录使用本地 cookie 会话：

- 登录接口：`POST /api/login`
- 会话检查：`GET /api/session`
- 退出后台：`POST /api/session/logout`

默认账号密码为 `admin` / `admin`，部署后应立即修改。

## 设置接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/settings` | 读取默认目录、站点名称、后台账号 |
| `POST` | `/api/settings` | 更新默认目录、站点名称、后台账号密码 |

## 文件接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/files?path=/` | 列出目录 |
| `POST` | `/api/mkdir` | 新建目录 |
| `POST` | `/api/delete` | 删除文件或目录 |
| `POST` | `/api/rename` | 重命名 |
| `POST` | `/api/move` | 移动 |
| `POST` | `/api/copy` | 复制 |
| `POST` | `/api/upload` | 上传文件 |
| `GET` | `/api/text?path=...` | 读取文本文件 |
| `POST` | `/api/text` | 保存文本文件 |

## 下载和预览

| 路径 | 说明 |
| --- | --- |
| `/download?fsid=...` | 根据 fsid 获取百度 dlink 并跳转 |
| `/d/<signed-id>` | 短下载链接 |
| `/preview?fsid=...` | 文件预览入口 |

## 日志

直接运行时日志输出到标准输出。

systemd 运行时查看日志：

```bash
journalctl -u bpdrive -f
```

## 备份

最重要的数据是 `data/config.json`，其中包含：

- 后台设置
- 百度 token
- 百度账号信息

备份示例：

```bash
cp data/config.json data/config.json.$(date +%Y%m%d%H%M%S).bak
```

## 故障排查

### 访问后跳到登录页

说明后台 cookie 不存在或已过期，重新登录即可。

### 百度接口返回 token 相关错误

先尝试后台刷新 token。如果刷新失败，需要重新扫码授权。

### 文件操作失败

检查：

- 百度 token 是否有效。
- `default_dir` 是否存在。
- 目标路径是否在默认目录内。
- 百度网盘接口是否返回限流或权限错误。

### 服务无法启动

检查：

```bash
systemctl status bpdrive --no-pager
journalctl -u bpdrive -n 100 --no-pager
```

常见原因：

- 端口已被占用。
- `WorkingDirectory` 配置错误。
- 运行用户没有读写 `data/` 的权限。
- 二进制没有执行权限。
