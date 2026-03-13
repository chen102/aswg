# 部署文档（开箱即用）

更新时间：2026-03-13  
适用范围：本地开发、内网自托管、小规模单机部署

## 1. 目标
1. 一条命令启动项目，浏览器可直接访问。
2. 保留原有后端/前端能力，不改业务逻辑。
3. 提供统一的启动、停止、状态、日志入口，降低操作复杂度。

## 2. 前置依赖
1. `Go >= 1.22`
2. `curl`（用于健康检查）
3. `codex` CLI（必需，续接模式固定为 `real`）

可选检查：

```bash
go version
curl --version
codex --version
```

## 3. 一键启动（推荐）

在仓库根目录执行：

```bash
./scripts/aswg up
```

默认行为：
1. 监听 `127.0.0.1:8082`
2. 默认适配器 `codex`
3. 续接模式固定 `CODEX_STREAM_MODE=real`
4. 启动前自动构建后端二进制到 `.run/aswg-server`
5. 后端日志写入 `.run/aswg.log`
6. PID 写入 `.run/aswg.pid`
7. 自动等待 `/api/v1/health` 成功后返回

访问地址：

```text
http://127.0.0.1:8082
```

## 4. 常用运维命令

```bash
# 查看状态（进程、端口、健康检查）
./scripts/aswg status

# 查看最近日志
./scripts/aswg logs

# 持续追踪日志
./scripts/aswg logs -f

# 停止服务
./scripts/aswg down

# 重启服务
./scripts/aswg restart
```

## 5. 配置方式

### 5.1 推荐：使用 `.env.local`

```bash
cp .env.example .env.local
```

修改 `.env.local` 中的变量后，直接重启：

```bash
./scripts/aswg restart
```

注意：`.env.local` 是由 bash `source` 读取的，带空格的值请使用引号。

### 5.2 临时参数覆盖（单次）

```bash
./scripts/aswg up --port 18080
```

支持参数：
1. `--host <host>`
2. `--port <port>`
3. `--adapters <list>`
4. `--default-adapter <name>`
5. `--auth-token <token>`
6. `--no-wait`

优先级：命令参数 > `.env.local` > `.env` > 脚本默认值

## 6. 局域网/公网部署建议

### 6.1 局域网访问

```bash
./scripts/aswg up --host 0.0.0.0 --port 8082
```

前端设置页填写：
1. `api_base_url=http://<服务器IP>:8082`
2. `ws_base_url=ws://<服务器IP>:8082`

### 6.2 鉴权建议

公网或多人环境建议启用：

```bash
./scripts/aswg restart --auth-token "<your-token>"
```

前端需携带同 token 才能访问受保护接口和 WS。

### 6.3 HTTPS / WSS

若使用反向代理（Nginx/Caddy/Traefik）：
1. 外层使用 `https://`
2. WebSocket 使用 `wss://`
3. 透传 `Authorization` 头

## 7. systemd（Linux 可选）

当你希望开机自启时，推荐使用 systemd 托管本项目目录中的脚本。

示例 `/etc/systemd/system/aswg.service`：

```ini
[Unit]
Description=Agent Session Web Gateway
After=network.target

[Service]
Type=forking
User=<deploy-user>
WorkingDirectory=/opt/aswg
ExecStart=/opt/aswg/scripts/aswg up --host 0.0.0.0 --port 8082
ExecStop=/opt/aswg/scripts/aswg down
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
```

启用：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now aswg
sudo systemctl status aswg
```

## 8. 故障排查
1. `port already in use`：端口被占用，换端口或先停掉冲突进程。
2. `codex not found`：缺少 Codex CLI，请安装后重试。
3. `health check timeout`：查看 `.run/aswg.log` 里的启动报错。
4. 页面打不开：确认 `./scripts/aswg status` 显示 `health=up`。

## 9. 兼容说明
1. 该启动方案只优化“启动和部署流程”，不更改业务接口协议。
2. 前端仍由后端静态托管（`FRONTEND_DIR` 可配置）。
3. 原有 `go run ./cmd/server` 手工启动方式仍可用。
