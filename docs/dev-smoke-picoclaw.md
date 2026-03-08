# PicoClaw Adapter 本地联调（Smoke）

日期：2026-03-08  
适用：`agent-session-web-gateway` + `picoclaw` adapter

## 1. 目标
验证以下链路可用：
1. 后端成功加载 `picoclaw` adapter。
2. `continue` 可通过 `pico/ws` 收到流式事件。
3. 事件最终可在 `/events` 与 Web 端显示为 `user -> assistant delta -> done`。

## 2. 前置条件
1. 已在仓库根目录。
2. Go 环境可用（建议 `>=1.22`）。
3. 端口可用：
   - Mock Pico：`18081`
   - ASWG 后端：`8080`

## 3. 启动 Mock Pico 服务
```bash
cd backend
MOCK_PICO_HOST=127.0.0.1 \
MOCK_PICO_PORT=18081 \
MOCK_PICO_TOKEN=pico-dev-token \
go run ./cmd/mock-pico
```

期望日志：
1. `mock pico server started on 127.0.0.1:18081`

## 4. 启动 ASWG 后端（启用 picoclaw）
新开终端执行：
```bash
cd backend
SERVER_HOST=0.0.0.0 \
SERVER_PORT=8080 \
AUTH_TOKEN= \
ENABLED_ADAPTERS=codex,picoclaw \
DEFAULT_ADAPTER=picoclaw \
PICOCLAW_WS_BASE_URL=ws://127.0.0.1:18081 \
PICOCLAW_TOKEN=pico-dev-token \
FRONTEND_DIR=../frontend/src \
go run ./cmd/server
```

## 5. API 快速验证
### 5.1 adapters 列表
```bash
curl -sS http://127.0.0.1:8080/api/v1/adapters
```
期望：返回 `items` 中包含 `codex` 与 `picoclaw`。

### 5.2 创建 picoclaw 会话
```bash
curl -sS -X POST \
  -H 'Content-Type: application/json' \
  -d '{"title":"pico smoke","workspace":"/tmp/pico-smoke"}' \
  http://127.0.0.1:8080/api/v1/adapters/picoclaw/sessions
```
记录返回 `data.id` 作为 `<session_id>`。

### 5.3 触发 continue
```bash
curl -sS -X POST \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"你好，做个自我介绍"}' \
  http://127.0.0.1:8080/api/v1/adapters/picoclaw/sessions/<session_id>/continue
```

### 5.4 拉取事件
```bash
curl -sS \
  'http://127.0.0.1:8080/api/v1/adapters/picoclaw/sessions/<session_id>/events?limit=200'
```
期望：
1. 至少出现 1 条 `message.user`。
2. 至少出现 1 条 `message.delta`（assistant）。
3. 最终出现 `message.done`。

## 6. Web 端验证
1. 打开：`http://127.0.0.1:8080`
2. 设置页：
   - `api_base_url=http://127.0.0.1:8080`
   - `ws_base_url=ws://127.0.0.1:8080`
   - `default_adapter=picoclaw`
3. 选择/创建 `picoclaw` 会话，发送 prompt。
4. 观察消息区出现流式 assistant 回复。

## 7. 常见问题
1. `init picoclaw adapter failed: PICOCLAW_TOKEN is required`
   - 需设置 `PICOCLAW_TOKEN`。
2. `continue 执行失败: dial pico ws failed`
   - 检查 `PICOCLAW_WS_BASE_URL` 是否指向 `ws://127.0.0.1:18081`。
3. WebSocket 401
   - Mock Pico token 与 `PICOCLAW_TOKEN` 不一致。
