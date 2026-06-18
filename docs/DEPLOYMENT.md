# 部署文档

本文说明如何从源码构建并部署DPDrive。

## 1. 准备源码

```bash
git clone <your-dpdrive-repo-url>
cd dpdrive
```

## 2. 准备配置

复制配置模板：

```bash
cp data/config.example.json data/config.json
```

编辑 `data/config.json`：

```json
{
  "app_key": "your_baidu_app_key",
  "secret_key": "your_baidu_secret_key",
  "redirect_uri": "oob",
  "default_dir": "/",
  "admin_user": "admin",
  "admin_pass": "admin",
  "site_title": "DPDrive"
}
```

如果不想把百度密钥写入配置文件，也可以通过环境变量传入：

```bash
export DPDRIVE_BAIDU_APP_KEY=your_baidu_app_key
export DPDRIVE_BAIDU_SECRET_KEY=your_baidu_secret_key
export DPDRIVE_BAIDU_REDIRECT_URI=oob
```

## 3. 构建

```bash
go test ./...
go build -o dpdrive ./cmd/dpdrive
```

## 4. 直接运行

```bash
DPDRIVE_ADDR=:18088 DPDRIVE_DATA=./data ./dpdrive
```

访问：

```text
http://服务器IP:18088
```

## 5. 使用 systemd

复制程序和资源：

```bash
mkdir -p /opt/dpdrive
cp dpdrive /opt/dpdrive/
cp -r web data /opt/dpdrive/
cp deploy/dpdrive.service.example /etc/systemd/system/dpdrive.service
```

按实际路径修改 `/etc/systemd/system/dpdrive.service`，然后启动：

```bash
systemctl daemon-reload
systemctl enable --now dpdrive
systemctl status dpdrive --no-pager
```

查看日志：

```bash
journalctl -u dpdrive -f
```

## 6. 反向代理建议

公网部署建议使用 Nginx 或 Caddy 提供 HTTPS，然后反向代理到本地端口。

Nginx 示例：

```nginx
server {
    listen 443 ssl http2;
    server_name example.com;

    location / {
        proxy_pass http://127.0.0.1:18088;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## 7. 升级

```bash
git pull
go test ./...
go build -o dpdrive ./cmd/dpdrive
systemctl restart dpdrive
```

升级前建议备份：

```bash
cp data/config.json data/config.json.bak
```
