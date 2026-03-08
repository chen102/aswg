# API 契约（v1）

状态：Draft  
版本：`v1`  
日期：2026-03-07

本文档用于前后端并行开发与联调，定义字段级契约、分页规则、错误语义与 WebSocket 协议。

## 1. 适用范围
1. 适用于 `agent-session-web-gateway` 第一阶段（MVP）后端与前端。
2. 适用于所有适配器（Codex 及后续 Agent Adapter）。
3. 若与 `docs/api.md` 不一致，以本文档为准并回写 `docs/api.md`。

## 2. 统一约定

### 2.1 基础路径
1. REST 基础路径：`/api/v1`
2. WebSocket 基础路径：`/ws/v1`

### 2.2 鉴权
1. 默认所有接口需要 `Authorization: Bearer <token>`。
   - 本地开发可将 `AUTH_TOKEN` 留空以关闭鉴权（仅建议本地/受控环境）。
2. `GET /api/v1/health` 可匿名访问。
3. WebSocket 鉴权支持两种方式：
   - 非浏览器客户端：`Authorization` Header。
   - 浏览器客户端：Query 参数 `access_token`（仅建议用于本地或受控内网）。

### 2.3 内容格式与时间
1. `Content-Type: application/json; charset=utf-8`
2. 时间统一 `RFC3339`（UTC），示例：`2026-03-07T09:30:00Z`
3. 所有 ID、Cursor 视为不透明字符串，客户端不可解析其内部结构。

### 2.4 请求追踪
1. 客户端可传 `X-Request-Id`。
2. 服务端若未收到则自动生成，并在响应体 `request_id` 返回。

### 2.5 统一响应包裹
成功响应：
```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "request_id": "req_01HTY..."
}
```

错误响应：
```json
{
  "code": 4001,
  "message": "invalid parameter: limit",
  "data": null,
  "request_id": "req_01HTY...",
  "error": {
    "type": "validation_error",
    "retryable": false,
    "details": {
      "field": "limit",
      "reason": "must be between 1 and 100"
    }
  }
}
```

## 3. 数据模型

### 3.1 AdapterInfo
```json
{
  "name": "codex",
  "display_name": "Codex",
  "enabled": true,
  "default": true,
  "capabilities": ["create_session", "discover_sessions", "events", "continue"],
  "version": "0.1.0"
}
```

### 3.2 SessionSummary
```json
{
  "adapter": "codex",
  "id": "sess_abc123",
  "title": "Refactor websocket reconnection",
  "updated_at": "2026-03-07T09:30:00Z",
  "workspace": "/workspace/project",
  "source": "cli"
}
```

### 3.3 SessionDetail
```json
{
  "adapter": "codex",
  "id": "sess_abc123",
  "title": "Refactor websocket reconnection",
  "created_at": "2026-03-06T08:00:00Z",
  "updated_at": "2026-03-07T09:30:00Z",
  "workspace": "/workspace/project",
  "source": "cli",
  "metadata": {
    "model": "gpt-5-codex"
  }
}
```

### 3.4 SessionEvent
```json
{
  "adapter": "codex",
  "session_id": "sess_abc123",
  "seq": 120,
  "ts": "2026-03-07T09:30:01Z",
  "type": "message.delta",
  "payload": {
    "raw_type": "assistant_delta",
    "text": "hello"
  },
  "normalized": {
    "role": "assistant",
    "text": "hello",
    "done": false
  }
}
```

字段说明：
1. `normalized.role`：至少支持 `user`、`assistant`。
2. `continue` 成功后，事件序列建议为：`user message` -> `assistant delta...` -> `assistant done`。

### 3.5 PagedResult
```json
{
  "items": [],
  "next_cursor": "cur_01HTY...",
  "has_more": true
}
```

### 3.6 ContinueRequest
```json
{
  "prompt": "请继续上一轮任务，先输出执行计划",
  "cwd": "/workspace/project"
}
```

字段约束：
1. `prompt`：必填，长度 `1..8000` 字符。
2. `cwd`：可选，字符串；由适配器决定是否支持。

### 3.7 RunJob
```json
{
  "job_id": "job_01HTY...",
  "adapter": "codex",
  "session_id": "sess_abc123",
  "status": "accepted",
  "started_at": "2026-03-07T09:31:00Z"
}
```

### 3.8 CreateSessionRequest
```json
{
  "title": "MVP 新会话",
  "workspace": "/workspace/project",
  "seed_prompt": "请先总结当前任务目标"
}
```

字段约束：
1. `title`：可选，长度 `<= 200`。
2. `workspace`：可选，长度 `<= 2000`。
3. `seed_prompt`：可选，长度 `<= 8000`。

## 4. REST 接口定义

### 4.1 GET /api/v1/health
用途：服务与适配器健康检查。  
鉴权：可匿名。

响应 `data`：
```json
{
  "status": "ok",
  "version": "0.1.0",
  "time": "2026-03-07T09:31:00Z",
  "adapters": [
    { "name": "codex", "status": "ok", "latency_ms": 12 }
  ]
}
```

### 4.2 GET /api/v1/adapters
用途：查询可用适配器列表。  
鉴权：必需。

响应 `data`：
```json
{
  "items": [
    {
      "name": "codex",
      "display_name": "Codex",
      "enabled": true,
      "default": true,
      "capabilities": ["create_session", "discover_sessions", "events", "continue"],
      "version": "0.1.0"
    }
  ]
}
```

### 4.3 POST /api/v1/adapters/{adapter}/sessions
用途：创建新会话。  
鉴权：必需。

请求体：`CreateSessionRequest`（所有字段可选）。

成功响应：
1. HTTP `201 Created`。
2. 响应 `data`：`SessionDetail`。

失败语义：
1. 参数错误返回 `4001`。
2. 适配器不存在返回 `4002`。

### 4.4 GET /api/v1/adapters/{adapter}/sessions
用途：按适配器查询会话列表。  
鉴权：必需。

Query 参数：
1. `query`：可选，按标题模糊匹配。
2. `workspace`：可选，按工作目录过滤。
3. `updated_after`：可选，RFC3339。
4. `updated_before`：可选，RFC3339。
5. `limit`：可选，默认 `20`，范围 `1..100`。
6. `cursor`：可选，分页游标。
7. `sort_by`：可选，默认 `updated_at`。
8. `sort_order`：可选，默认 `desc`，可选 `asc`/`desc`。

响应 `data`：
```json
{
  "items": [
    {
      "adapter": "codex",
      "id": "sess_abc123",
      "title": "Refactor websocket reconnection",
      "updated_at": "2026-03-07T09:30:00Z",
      "workspace": "/workspace/project",
      "source": "cli"
    }
  ],
  "next_cursor": "cur_01HTY...",
  "has_more": true
}
```

### 4.5 GET /api/v1/adapters/{adapter}/sessions/{id}
用途：查询会话详情（基础信息，不含完整事件流）。  
鉴权：必需。

响应 `data`：`SessionDetail`。

### 4.6 GET /api/v1/adapters/{adapter}/sessions/{id}/events
用途：分页读取会话历史事件。  
鉴权：必需。

Query 参数：
1. `cursor`：可选，分页游标。
2. `limit`：可选，默认 `100`，范围 `1..500`。

响应 `data`：
```json
{
  "items": [
    {
      "adapter": "codex",
      "session_id": "sess_abc123",
      "seq": 120,
      "ts": "2026-03-07T09:30:01Z",
      "type": "message.delta",
      "payload": { "raw_type": "assistant_delta", "text": "hello" },
      "normalized": { "role": "assistant", "text": "hello", "done": false }
    }
  ],
  "next_cursor": "cur_01HTY...",
  "has_more": true
}
```

### 4.7 POST /api/v1/adapters/{adapter}/sessions/{id}/continue
用途：在指定会话继续提问并启动流式事件。  
鉴权：必需。

Header：
1. `Idempotency-Key`：可选，建议用于防止重复提交。

请求体：`ContinueRequest`。

成功响应：
1. HTTP `202 Accepted`。
2. 响应 `data`：`RunJob`。

失败语义：
1. 参数错误返回 `4001`。
2. 会话不存在返回 `4003`。
3. 任务无法启动返回 `4004` 或 `5000`。
4. 成功后应至少产出一条 `normalized.role=user` 的消息事件，再输出 assistant 流式事件。
5. 若运行期失败（例如 CLI 超时），应输出 `message.done`，并在 `payload.raw_type=assistant_error` 中携带错误文本。

## 5. WebSocket 契约

### 5.1 连接地址
`GET /ws/v1/adapters/{adapter}/sessions/{id}`

可选 Query：
1. `cursor`：从指定游标续传（优先）。
2. `last_seq`：从 `last_seq + 1` 续传。
3. `access_token`：浏览器环境鉴权 token。

### 5.2 帧结构
```json
{
  "frame_type": "event",
  "request_id": "req_01HTY...",
  "seq": 121,
  "ts": "2026-03-07T09:31:10Z",
  "data": {
    "adapter": "codex",
    "session_id": "sess_abc123",
    "seq": 121,
    "ts": "2026-03-07T09:31:10Z",
    "type": "message.delta",
    "payload": { "raw_type": "assistant_delta", "text": "world" },
    "normalized": { "role": "assistant", "text": "world", "done": false }
  }
}
```

`frame_type` 取值：
1. `event`：业务事件。
2. `heartbeat`：心跳帧（默认 15 秒一帧）。
3. `error`：错误帧。
4. `done`：本次流结束。

### 5.3 顺序与去重
1. 同一 `session_id` 下，`seq` 必须单调递增。
2. 重连后允许重复收到历史帧，客户端应按 `seq` 去重。
3. 若 `cursor` 过期，服务端返回 `error` 帧并关闭连接。

### 5.4 重连建议
1. 指数退避：1s、2s、4s、8s，最大 30s。
2. 重连时携带最近已确认 `last_seq`。
3. 若连续失败超过阈值，提示用户执行连接测试或检查鉴权。

## 6. 错误码与 HTTP 状态

| 业务码 | HTTP | 含义 | 客户端策略 |
| --- | --- | --- | --- |
| `4001` | 400 | 参数错误 | 修正参数后重试 |
| `4002` | 404 | 适配器不存在或未启用 | 切换适配器 |
| `4003` | 404 | 会话不存在 | 刷新列表并重新选择 |
| `4004` | 409/500 | 续接任务启动失败 | 可重试，保留失败详情 |
| `4010` | 401 | 鉴权失败 | 更新 token 后重试 |
| `4290` | 429 | 请求过频 | 退避重试 |
| `5000` | 500 | 内部错误 | 记录 request_id，稍后重试 |

## 7. 兼容性规则
1. `v1` 内新增字段必须向后兼容，客户端应忽略未知字段。
2. 删除字段或改变字段语义必须升级主版本（`v2`）。
3. 错误码新增可在 `v1` 内进行，但不得改变已有错误码语义。

## 8. 联调最小清单
1. `health`、`adapters`、`sessions`、`session detail`、`events`、`continue` 六条 REST 路径可用。
2. `create session` 路径可用：`POST /api/v1/adapters/{adapter}/sessions`。
3. WebSocket 能稳定收到 `event` + `heartbeat` + `done`。
4. 断网重连后可通过 `last_seq` 续传且不乱序。
5. 所有错误响应都包含 `request_id`。
